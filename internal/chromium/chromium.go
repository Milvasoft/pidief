// Package chromium converts HTML to PDF with a headless Chromium driven over the DevTools protocol.
//
// One browser process is started lazily and reused; every conversion gets its own tab (target)
// in its own browser context, so cookies, storage and caches never leak between requests.
// The documents and their assets are served from memory under a virtual origin
// (https://pidief.local/) through the Fetch domain, so nothing is written to disk.
package chromium

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"mime"
	"net/url"
	"os"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/cdproto/fetch"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"

	"github.com/milvasoft/pidief/internal/assets"
)

// Origin is the virtual origin the documents are served from.
const Origin = "https://pidief.local"

// Config configures the browser process.
type Config struct {
	ExecPath  string // empty: search the usual locations
	NoSandbox bool   // needed in most containers (no user namespaces)
	// RestartAfter restarts the browser after this many conversions to bound memory growth (0 = never).
	RestartAfter int
	// StartTimeout bounds launching the browser and attaching to it (default 60s).
	StartTimeout time.Duration
	Logger       *slog.Logger
}

// Browser is a lazily started, shared headless Chromium.
type Browser struct {
	cfg Config

	mu       sync.Mutex
	allocCtx context.Context
	cancel   context.CancelFunc
	browser  context.Context
	uses     int
	active   *sync.WaitGroup // conversions running on the current process
}

// New returns a Browser; the process starts on first use.
func New(cfg Config) *Browser {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if cfg.StartTimeout <= 0 {
		cfg.StartTimeout = 60 * time.Second
	}
	return &Browser{cfg: cfg, active: &sync.WaitGroup{}}
}

// ExecPathFromEnv returns the browser path from PIDIEF_CHROMIUM_PATH, or "" to auto-detect.
func ExecPathFromEnv() string { return os.Getenv("PIDIEF_CHROMIUM_PATH") }

func (b *Browser) start() error {
	opts := append([]chromedp.ExecAllocatorOption{}, chromedp.DefaultExecAllocatorOptions[:]...)
	opts = append(opts,
		chromedp.Flag("font-render-hinting", "none"), // stable glyph metrics in print
		chromedp.Flag("disable-extensions", true),
		chromedp.Flag("disable-background-networking", true),
		chromedp.Flag("disable-sync", true),
		chromedp.Flag("disable-default-apps", true),
		chromedp.Flag("mute-audio", true),
		chromedp.Flag("hide-scrollbars", true),
		chromedp.Flag("disable-features", "Translate,MediaRouter,OptimizationHints,AutofillServerCommunication"),
		chromedp.Flag("lang", "tr-TR"),
	)
	if b.cfg.ExecPath != "" {
		opts = append(opts, chromedp.ExecPath(b.cfg.ExecPath))
	}
	if b.cfg.NoSandbox {
		opts = append(opts, chromedp.NoSandbox)
	}
	allocCtx, cancelAlloc := chromedp.NewExecAllocator(context.Background(), opts...)
	bctx, cancelBrowser := chromedp.NewContext(allocCtx)
	// The first Do starts the browser and binds its life to bctx, so it cannot carry a timeout
	// itself; bound it from outside instead (a browser that never answers must not hang callers).
	started := make(chan error, 1)
	go func() { started <- chromedp.Do(bctx) }()
	var err error
	select {
	case err = <-started:
	case <-time.After(b.cfg.StartTimeout):
		err = fmt.Errorf("no answer within %s", b.cfg.StartTimeout)
	}
	if err != nil {
		cancelBrowser()
		cancelAlloc()
		return fmt.Errorf("start chromium: %w", err)
	}
	b.allocCtx = allocCtx
	b.browser = bctx
	b.cancel = func() { cancelBrowser(); cancelAlloc() }
	b.uses = 0
	b.cfg.Logger.Info("chromium started")
	return nil
}

// acquire returns the browser context, starting or recycling the process when needed.
func (b *Browser) acquire() (context.Context, func(), error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.browser != nil && (b.browser.Err() != nil ||
		(b.cfg.RestartAfter > 0 && b.uses >= b.cfg.RestartAfter)) {
		old, wg := b.cancel, b.active
		b.browser, b.cancel = nil, nil
		b.active = &sync.WaitGroup{}
		go func() { wg.Wait(); old() }() // let running conversions finish first
		b.cfg.Logger.Info("chromium recycled")
	}
	if b.browser == nil {
		if err := b.start(); err != nil {
			return nil, nil, err
		}
	}
	b.uses++
	wg := b.active
	wg.Add(1)
	return b.browser, wg.Done, nil
}

// Ready reports whether the browser can be started (used by /health/ready).
func (b *Browser) Ready(ctx context.Context) error {
	bctx, done, err := b.acquire()
	if err != nil {
		return err
	}
	defer done()
	if bctx.Err() != nil {
		return bctx.Err()
	}
	return nil
}

// Close stops the browser process.
func (b *Browser) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.cancel != nil {
		b.cancel()
		b.browser, b.cancel = nil, nil
	}
}

// Asset is a file served under the virtual origin.
type Asset struct {
	Name string // relative path, e.g. "img/logo.png"
	Data []byte
}

// Options of one conversion. Lengths are in inches; the caller converts units.
type Options struct {
	PaperWidth, PaperHeight                          float64
	MarginTop, MarginRight, MarginBottom, MarginLeft float64
	Landscape                                        bool
	PrintBackground                                  bool
	PreferCSSPageSize                                bool
	Scale                                            float64
	PageRanges                                       string
	HeaderTemplate, FooterTemplate                   string
	Tagged, Outline                                  bool
	WaitForSelector, WaitForExpression               string
	WaitDelay                                        time.Duration
	WaitNetworkIdle                                  bool
	AllowExternal                                    bool
	FailOnResourceError                              bool
	MediaScreen                                      bool
	InjectOnest                                      bool
}

// ErrResource is returned when a referenced resource is missing and FailOnResourceError is set.
var ErrResource = errors.New("resource could not be loaded")

const fontDir = "/_pidief/fonts/"

// OnestFontFace returns the @font-face rules pidief injects so `font-family: Onest` works
// everywhere (400, 500, 600, 700; local() first so an installed copy is reused).
func OnestFontFace() string {
	var b strings.Builder
	b.WriteString(`<style id="pidief-onest">`)
	for _, f := range assets.OnestFaces() {
		fmt.Fprintf(&b, `@font-face{font-family:"Onest";src:url("%s%s") format("truetype");font-weight:%d;font-style:normal;font-display:block}`,
			fontDir, f.File, f.Weight)
	}
	b.WriteString(`</style>`)
	return b.String()
}

// InjectHead inserts snippet right after <head> (or at the top when there is no head).
func InjectHead(html, snippet string) string {
	lower := strings.ToLower(html)
	if i := strings.Index(lower, "<head"); i >= 0 {
		if j := strings.Index(lower[i:], ">"); j >= 0 {
			k := i + j + 1
			return html[:k] + snippet + html[k:]
		}
	}
	if i := strings.Index(lower, "<html"); i >= 0 {
		if j := strings.Index(lower[i:], ">"); j >= 0 {
			k := i + j + 1
			return html[:k] + "<head>" + snippet + "</head>" + html[k:]
		}
	}
	return snippet + html
}

// PrintPDF renders indexHTML (served as https://pidief.local/index.html) with assets to PDF.
func (b *Browser) PrintPDF(ctx context.Context, indexHTML string, files []Asset, o Options) ([]byte, error) {
	bctx, done, err := b.acquire()
	if err != nil {
		return nil, err
	}
	defer done()

	// New tab in a fresh, isolated browser context.
	tctx, cancelTab := chromedp.NewContext(bctx, chromedp.WithNewBrowserContext())
	defer cancelTab()
	// Bind the tab to the caller's deadline/cancellation.
	stop := context.AfterFunc(ctx, cancelTab)
	defer stop()

	if o.InjectOnest {
		indexHTML = InjectHead(indexHTML, OnestFontFace())
	}
	served := map[string][]byte{"/index.html": []byte(indexHTML)}
	for _, f := range files {
		p := "/" + strings.TrimLeft(path.Clean("/"+strings.ReplaceAll(f.Name, "\\", "/")), "/")
		served[p] = f.Data
	}
	for _, f := range assets.OnestFaces() {
		served[fontDir+f.File] = f.Data
	}

	// Start the tab, then wire request interception before navigating.
	if err := chromedp.Do(tctx); err != nil {
		return nil, fmt.Errorf("open tab: %w", err)
	}
	paused := chromedp.Events(tctx, fetch.RequestPaused)
	if _, err := chromedp.Call(tctx, fetch.Enable, fetch.EnableParams{
		Patterns: []*fetch.RequestPattern{{URLPattern: "*", RequestStage: fetch.RequestStageRequest}},
	}); err != nil {
		return nil, fmt.Errorf("enable fetch: %w", err)
	}
	var resErrMu sync.Mutex
	var resErrs []string
	go func() {
		for ev, err := range paused {
			if err != nil {
				return
			}
			go b.handleRequest(tctx, &ev, served, o.AllowExternal, func(msg string) {
				resErrMu.Lock()
				resErrs = append(resErrs, msg)
				resErrMu.Unlock()
			})
		}
	}()

	var idle func() error
	if o.WaitNetworkIdle {
		if _, err := chromedp.Call(tctx, page.SetLifecycleEventsEnabled, page.SetLifecycleEventsEnabledParams{Enabled: true}); err != nil {
			return nil, err
		}
		life := chromedp.Events(tctx, page.LifecycleEvent)
		idle = func() error {
			for ev, err := range life {
				if err != nil {
					return err
				}
				if ev.Name == "networkIdle" {
					return nil
				}
			}
			return tctx.Err()
		}
	}
	if o.MediaScreen {
		if _, err := chromedp.Call(tctx, emulation.SetEmulatedMedia, emulation.SetEmulatedMediaParams{Media: "screen"}); err != nil {
			return nil, err
		}
	}

	if err := chromedp.Do(tctx, chromedp.Navigate(Origin+"/index.html")); err != nil {
		return nil, ctxErr(ctx, fmt.Errorf("navigate: %w", err))
	}
	if idle != nil {
		if err := idle(); err != nil {
			return nil, ctxErr(ctx, fmt.Errorf("wait network idle: %w", err))
		}
	}
	// Web fonts must be ready before printing, or Chromium prints fallback glyphs.
	if err := chromedp.Do(tctx, chromedp.Evaluate[chromedp.Void](`document.fonts.ready.then(() => true)`, chromedp.EvalAwaitPromise)); err != nil {
		return nil, ctxErr(ctx, fmt.Errorf("wait fonts: %w", err))
	}
	if o.WaitForSelector != "" {
		if err := chromedp.Do(tctx, chromedp.WaitReady(chromedp.CSS(o.WaitForSelector))); err != nil {
			return nil, ctxErr(ctx, fmt.Errorf("wait for selector %q: %w", o.WaitForSelector, err))
		}
	}
	if o.WaitForExpression != "" {
		if _, err := chromedp.Run(tctx, chromedp.Poll[bool](o.WaitForExpression)); err != nil {
			return nil, ctxErr(ctx, fmt.Errorf("wait for expression: %w", err))
		}
	}
	if o.WaitDelay > 0 {
		if err := chromedp.Do(tctx, chromedp.Sleep(o.WaitDelay)); err != nil {
			return nil, ctxErr(ctx, err)
		}
	}
	if o.FailOnResourceError {
		resErrMu.Lock()
		errs := append([]string(nil), resErrs...)
		resErrMu.Unlock()
		if len(errs) > 0 {
			return nil, fmt.Errorf("%w: %s", ErrResource, strings.Join(errs, "; "))
		}
	}

	p := page.PrintToPDFParams{
		Landscape:               &o.Landscape,
		PrintBackground:         &o.PrintBackground,
		Scale:                   o.Scale,
		PaperWidth:              o.PaperWidth,
		PaperHeight:             o.PaperHeight,
		MarginTop:               &o.MarginTop,
		MarginBottom:            &o.MarginBottom,
		MarginLeft:              &o.MarginLeft,
		MarginRight:             &o.MarginRight,
		PageRanges:              o.PageRanges,
		PreferCSSPageSize:       &o.PreferCSSPageSize,
		GenerateTaggedPDF:       &o.Tagged,
		GenerateDocumentOutline: &o.Outline,
		TransferMode:            page.PrintToPDFTransferModeReturnAsBase64,
	}
	if o.HeaderTemplate != "" || o.FooterTemplate != "" {
		t := true
		p.DisplayHeaderFooter = &t
		p.HeaderTemplate = orEmptySpan(o.HeaderTemplate)
		p.FooterTemplate = orEmptySpan(o.FooterTemplate)
	}
	res, err := chromedp.Call(tctx, page.PrintToPDF, p)
	if err != nil {
		return nil, ctxErr(ctx, fmt.Errorf("print to pdf: %w", err))
	}
	return res.Data, nil
}

func orEmptySpan(s string) string {
	if strings.TrimSpace(s) == "" {
		return "<span></span>"
	}
	return s
}

func ctxErr(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return fmt.Errorf("%w: %v", ctx.Err(), err)
	}
	return err
}

func (b *Browser) handleRequest(tctx context.Context, ev *fetch.EventRequestPaused, served map[string][]byte, allowExternal bool, onErr func(string)) {
	u, err := url.Parse(ev.Request.URL)
	if err == nil && u.Scheme == "https" && u.Host == "pidief.local" {
		p := path.Clean("/" + strings.TrimPrefix(u.Path, "/"))
		if unescaped, err := url.PathUnescape(p); err == nil {
			p = unescaped
		}
		data, ok := served[p]
		if !ok && p == "/favicon.ico" {
			_, _ = chromedp.Call(tctx, fetch.FulfillRequest, fetch.FulfillRequestParams{RequestID: ev.RequestID, ResponseCode: 204})
			return
		}
		if !ok {
			onErr("not found: " + p)
			_, _ = chromedp.Call(tctx, fetch.FulfillRequest, fetch.FulfillRequestParams{
				RequestID: ev.RequestID, ResponseCode: 404,
				ResponseHeaders: []*fetch.HeaderEntry{{Name: "Content-Type", Value: "text/plain"}},
			})
			return
		}
		ct := mime.TypeByExtension(path.Ext(p))
		if ct == "" {
			ct = "application/octet-stream"
		}
		if strings.HasPrefix(ct, "text/") && !strings.Contains(ct, "charset") {
			ct += "; charset=utf-8"
		}
		_, _ = chromedp.Call(tctx, fetch.FulfillRequest, fetch.FulfillRequestParams{
			RequestID: ev.RequestID, ResponseCode: 200,
			ResponseHeaders: []*fetch.HeaderEntry{
				{Name: "Content-Type", Value: ct},
				{Name: "Access-Control-Allow-Origin", Value: "*"},
			},
			Body: data,
		})
		return
	}
	if allowExternal && u != nil && (u.Scheme == "http" || u.Scheme == "https") {
		_, _ = chromedp.Call(tctx, fetch.ContinueRequest, fetch.ContinueRequestParams{RequestID: ev.RequestID})
		return
	}
	onErr("blocked external request: " + ev.Request.URL)
	_, _ = chromedp.Call(tctx, fetch.FailRequest, fetch.FailRequestParams{
		RequestID: ev.RequestID, ErrorReason: network.ErrorReasonBlockedByClient,
	})
}
