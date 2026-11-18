// Package cli builds the pidief command line from the ops registry.
package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/milvasoft/pidief/internal/chromium"
	"github.com/milvasoft/pidief/internal/ops"
	"github.com/milvasoft/pidief/internal/raster"
	"github.com/milvasoft/pidief/internal/server"
)

// ExitError carries an exit code.
type ExitError struct {
	Code int
	Err  error
}

func (e *ExitError) Error() string { return e.Err.Error() }

// Root returns the root command.
func Root() *cobra.Command {
	root := &cobra.Command{
		Use:   "pidief",
		Short: "pidief: PDF toolkit (HTML/template/images to PDF, merge, split, pages, watermark, stamp, compress, PDF/A, encrypt, render)",
		Long: "pidief is Milvasoft's PDF toolkit. Every command has the same HTTP endpoint under `pidief serve`\n" +
			"(command `pages rotate` = POST /v1/pages/rotate) with the same parameters.\n\nApache-2.0. Source: https://github.com/Milvasoft/pidief",
		Version:       server.Version,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	env := &lazyEnv{}
	groups := map[string]*cobra.Command{}
	for _, op := range ops.All() {
		parts := strings.Fields(op.Name)
		parent := root
		for i, p := range parts[:len(parts)-1] {
			key := strings.Join(parts[:i+1], " ")
			if g, ok := groups[key]; ok {
				parent = g
				continue
			}
			// The group may itself be an op (template + template preview).
			var found *cobra.Command
			for _, c := range parent.Commands() {
				if c.Name() == p {
					found = c
				}
			}
			if found == nil {
				found = &cobra.Command{Use: p, Short: p + " commands"}
				parent.AddCommand(found)
			}
			groups[key] = found
			parent = found
		}
		cmd := opCommand(op, env)
		parent.AddCommand(cmd)
		groups[op.Name] = cmd
	}
	root.AddCommand(serveCommand(), openapiCommand(), healthcheckCommand())
	return root
}

// lazyEnv starts Chromium/PDFium only when a command needs them.
type lazyEnv struct{ env *ops.Env }

func (l *lazyEnv) get() *ops.Env {
	if l.env == nil {
		noSandbox := strings.EqualFold(os.Getenv("PIDIEF_CHROMIUM_NO_SANDBOX"), "true")
		l.env = &ops.Env{
			Browser:       chromium.New(chromium.Config{ExecPath: chromium.ExecPathFromEnv(), NoSandbox: noSandbox, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}),
			Raster:        raster.New(1),
			AllowExternal: true, // the local user decides
		}
	}
	return l.env
}

func (l *lazyEnv) close() {
	if l.env != nil {
		l.env.Browser.Close()
		l.env.Raster.Close()
	}
}

func opCommand(op *ops.Op, lenv *lazyEnv) *cobra.Command {
	params := op.Params()
	var output string
	texts := map[string]*string{}
	cmd := &cobra.Command{
		Use:   lastWord(op.Name) + " [files...]",
		Short: op.Summary,
		Long: op.Summary + "\n\nInputs: " + op.Inputs.Description + ".\nAPI: POST " + op.Path +
			"\nA file argument of - reads standard input. Directories are not expanded; pass assets explicitly.",
		RunE: func(cmd *cobra.Command, args []string) error {
			var files []ops.File
			for _, ti := range op.TextInputs {
				if v := *texts[ti.Field]; v != "" {
					files = append(files, ops.File{Name: ti.File, Data: []byte(v)})
				}
			}
			for _, a := range args {
				f, err := readInput(a, op)
				if err != nil {
					return &ExitError{2, err}
				}
				files = append(files, f)
			}
			// Assets of HTML are addressed relative to the main document's folder.
			if op.Inputs.Kind == "html" && len(args) > 1 {
				rebaseAssets(files, args)
			}
			if err := op.Validate(files); err != nil {
				return &ExitError{2, err}
			}
			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			defer lenv.close()
			out, err := op.Run(ctx, lenv.get(), files, params)
			if err != nil {
				code := 1
				if ops.IsClientError(err) {
					code = 2
				}
				return &ExitError{code, err}
			}
			return writeOutput(cmd, op, files, out, output)
		},
	}
	cmd.Flags().StringVarP(&output, "output", "o", "", "output file, - for stdout; for several files a directory or a .zip path")
	for _, ti := range op.TextInputs {
		s := new(string)
		texts[ti.Field] = s
		cmd.Flags().StringVar(s, ti.Field, "", ti.Help)
	}
	for _, f := range ops.Fields(params) {
		addFlag(cmd.Flags(), params, f)
	}
	return cmd
}

func lastWord(s string) string {
	f := strings.Fields(s)
	return f[len(f)-1]
}

func readInput(arg string, op *ops.Op) (ops.File, error) {
	if arg == "-" {
		b, err := io.ReadAll(os.Stdin)
		if err != nil {
			return ops.File{}, err
		}
		name := "stdin"
		switch {
		case ops.IsPDF(b):
			name = "stdin.pdf"
		case op.Inputs.Kind == "html":
			name = "index.html"
		}
		return ops.File{Name: name, Data: b}, nil
	}
	b, err := os.ReadFile(arg)
	if err != nil {
		return ops.File{}, err
	}
	return ops.File{Name: filepath.ToSlash(arg), Data: b}, nil
}

// rebaseAssets names assets relative to the directory of the first HTML file.
func rebaseAssets(files []ops.File, args []string) {
	base := ""
	for _, a := range args {
		ext := strings.ToLower(filepath.Ext(a))
		if ext == ".html" || ext == ".htm" || ext == ".hbs" {
			base = filepath.Dir(a)
			break
		}
	}
	for i := range files {
		if rel, err := filepath.Rel(base, filepath.FromSlash(files[i].Name)); err == nil && !strings.HasPrefix(rel, "..") {
			files[i].Name = filepath.ToSlash(rel)
		} else {
			files[i].Name = filepath.Base(files[i].Name)
		}
	}
}

func writeOutput(cmd *cobra.Command, op *ops.Op, in []ops.File, out *ops.Output, output string) error {
	stdout := cmd.OutOrStdout()
	if out.JSON != nil {
		b, _ := json.MarshalIndent(out.JSON, "", "  ")
		if output == "" || output == "-" {
			_, err := fmt.Fprintln(stdout, string(b))
			return err
		}
		return os.WriteFile(output, b, 0o644)
	}
	if len(out.Files) == 1 && op.Output != "pdfs" {
		f := out.Files[0]
		if output == "-" {
			_, err := stdout.Write(f.Data)
			return err
		}
		if output == "" {
			output = f.Name
			if len(in) > 0 && !strings.HasPrefix(in[0].Name, "stdin") {
				// Next to the input, never overwriting it.
				cand := filepath.Join(filepath.Dir(filepath.FromSlash(in[0].Name)), f.Name)
				if !sameFile(cand, in[0].Name) {
					output = cand
				}
			}
		}
		if err := os.WriteFile(output, f.Data, 0o644); err != nil {
			return err
		}
		fmt.Fprintf(cmd.ErrOrStderr(), "%s (%d bytes)\n", output, len(f.Data))
		return nil
	}
	if strings.HasSuffix(strings.ToLower(output), ".zip") || output == "-" {
		z, err := ops.Zip(out.Files)
		if err != nil {
			return err
		}
		if output == "-" {
			_, err = stdout.Write(z)
			return err
		}
		return os.WriteFile(output, z, 0o644)
	}
	if output == "" {
		output = "."
	}
	if err := os.MkdirAll(output, 0o750); err != nil {
		return err
	}
	for _, f := range out.Files {
		p := filepath.Join(output, filepath.Base(f.Name))
		if err := os.WriteFile(p, f.Data, 0o644); err != nil {
			return err
		}
		fmt.Fprintf(cmd.ErrOrStderr(), "%s (%d bytes)\n", p, len(f.Data))
	}
	return nil
}

func sameFile(a, b string) bool {
	pa, _ := filepath.Abs(a)
	pb, _ := filepath.Abs(filepath.FromSlash(b))
	return strings.EqualFold(pa, pb)
}

// flagValue adapts a struct field to pflag.Value using the shared string binder.
type flagValue struct {
	params any
	field  ops.Field
	set    bool
}

func (v *flagValue) String() string {
	rv := ops.FieldValue(v.params, v.field)
	switch rv.Kind() {
	case reflect.Slice, reflect.Map:
		if rv.Len() == 0 {
			return ""
		}
	}
	return fmt.Sprint(rv.Interface())
}

func (v *flagValue) Set(s string) error {
	add := v.set
	v.set = true
	form := map[string][]string{v.field.Name: {s}}
	if add && (v.field.Kind == reflect.Slice || v.field.Kind == reflect.Map) {
		// Repeated flag: keep previous values.
		rv := ops.FieldValue(v.params, v.field)
		if v.field.Kind == reflect.Slice {
			prev := make([]string, rv.Len())
			for i := range prev {
				prev[i] = fmt.Sprint(rv.Index(i).Interface())
			}
			form[v.field.Name] = append(prev, s)
		} else {
			var vals []string
			for _, k := range rv.MapKeys() {
				vals = append(vals, fmt.Sprintf("%s=%s", k, rv.MapIndex(k)))
			}
			form[v.field.Name] = append(vals, s)
		}
	}
	return ops.BindForm(v.params, form)
}

func (v *flagValue) Type() string {
	switch v.field.Kind {
	case reflect.Bool:
		return "bool"
	case reflect.Int:
		return "int"
	case reflect.Float64:
		return "float"
	case reflect.Slice:
		return "list"
	case reflect.Map:
		return "key=value"
	}
	return "string"
}

func (v *flagValue) IsBoolFlag() bool { return v.field.Kind == reflect.Bool }

func addFlag(fs *pflag.FlagSet, params any, f ops.Field) {
	help := f.Help
	if len(f.Enum) > 0 {
		help += " (" + strings.Join(f.Enum, "|") + ")"
	}
	fl := fs.VarPF(&flagValue{params: params, field: f}, f.Flag, "", help)
	if f.Kind == reflect.Bool {
		fl.NoOptDefVal = "true"
	}
	if f.Default != "" {
		fl.DefValue = f.Default
	}
}

func serveCommand() *cobra.Command {
	var b strings.Builder
	for _, e := range server.EnvVars {
		fmt.Fprintf(&b, "  %-32s %-14s %s\n", e.Name, e.Default, e.Help)
	}
	return &cobra.Command{
		Use:   "serve",
		Short: "Start the HTTP API",
		Long:  "Start the HTTP API. Configuration comes from the environment:\n\n" + b.String(),
		RunE: func(cmd *cobra.Command, args []string) error {
			return Serve()
		},
	}
}

func healthcheckCommand() *cobra.Command {
	var url string
	return &cobra.Command{
		Use:    "healthcheck",
		Short:  "Exit 0 when the local API answers /health/live (for container health checks)",
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if url == "" {
				addr := os.Getenv("PIDIEF_ADDR")
				if addr == "" {
					addr = ":3000"
				}
				if strings.HasPrefix(addr, ":") {
					addr = "127.0.0.1" + addr
				}
				url = "http://" + addr + "/health/live"
			}
			c := &http.Client{Timeout: 3 * time.Second}
			res, err := c.Get(url)
			if err != nil {
				return &ExitError{1, err}
			}
			res.Body.Close()
			if res.StatusCode != http.StatusOK {
				return &ExitError{1, fmt.Errorf("status %d", res.StatusCode)}
			}
			return nil
		},
	}
}

func openapiCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "openapi",
		Short: "Print the OpenAPI document of the HTTP API",
		RunE: func(cmd *cobra.Command, args []string) error {
			_, err := cmd.OutOrStdout().Write(server.OpenAPI())
			return err
		},
	}
}

// NewLogger builds the structured logger from PIDIEF_LOG_LEVEL / PIDIEF_LOG_FORMAT.
func NewLogger() *slog.Logger {
	var level slog.Level
	_ = level.UnmarshalText([]byte(strings.ToLower(os.Getenv("PIDIEF_LOG_LEVEL"))))
	opts := &slog.HandlerOptions{Level: level}
	if strings.EqualFold(os.Getenv("PIDIEF_LOG_FORMAT"), "text") {
		return slog.New(slog.NewTextHandler(os.Stderr, opts))
	}
	return slog.New(slog.NewJSONHandler(os.Stderr, opts))
}

// Serve runs the HTTP server until SIGINT/SIGTERM.
func Serve() error {
	log := NewLogger()
	cfg, err := server.LoadConfig()
	if err != nil {
		return &ExitError{2, err}
	}
	if len(cfg.APIKeys) == 0 {
		log.Warn("PIDIEF_API_KEYS is empty: the API accepts requests without a key")
	}
	browser := chromium.New(chromium.Config{ExecPath: cfg.ChromiumPath, NoSandbox: cfg.NoSandbox, RestartAfter: cfg.RestartAfter, Logger: log})
	defer browser.Close()
	rast := raster.New(cfg.RasterWorkers)
	defer rast.Close()
	env := &ops.Env{Browser: browser, Raster: rast, AllowExternal: cfg.AllowExternal, MaxTimeout: cfg.MaxTimeout}

	reg := prometheus.NewRegistry()
	reg.MustRegister(collectors.NewGoCollector(), collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
	srv := server.New(cfg, env, log, reg)
	defer srv.Close()
	hs := &http.Server{
		Addr:              cfg.Addr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	errc := make(chan error, 1)
	go func() {
		log.Info("pidief listening", "addr", cfg.Addr, "version", server.Version, "concurrency", cfg.Concurrency)
		errc <- hs.ListenAndServe()
	}()
	// Warm up Chromium in the background so the first request is fast.
	go func() {
		wctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := browser.Ready(wctx); err != nil {
			log.Error("chromium is not available", "error", err.Error())
		}
	}()
	select {
	case err := <-errc:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-ctx.Done():
	}
	log.Info("shutting down")
	srv.StartShutdown()
	sctx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	return hs.Shutdown(sctx)
}
