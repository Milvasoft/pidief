// Package ops is the single registry of pidief operations. The CLI and the HTTP API are both
// generated from it, so every CLI command has exactly one API endpoint with the same
// parameters, inputs and outputs.
package ops

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"path"
	"strings"
	"time"

	"github.com/milvasoft/pidief/internal/chromium"
	"github.com/milvasoft/pidief/internal/imgpdf"
	"github.com/milvasoft/pidief/internal/pdfops"
	"github.com/milvasoft/pidief/internal/raster"
)

// File is an input or output document.
type File struct {
	Name        string `json:"name"`
	ContentType string `json:"contentType,omitempty"`
	Data        []byte `json:"-"`
}

// Output is either files or a JSON value.
type Output struct {
	Files []File
	JSON  any
}

// Env holds the shared engines and server-wide limits.
type Env struct {
	Browser *chromium.Browser
	Raster  *raster.Engine
	// AllowExternal permits the allowExternal parameter (false: requests asking for it are rejected).
	AllowExternal bool
	// MaxTimeout caps the per-request timeout parameter.
	MaxTimeout time.Duration
	Now        func() time.Time
}

// Inputs describes the files an operation takes.
type Inputs struct {
	Min, Max    int    // Max 0 = unlimited
	Kind        string // pdf, image, html, any
	Description string
}

// TextInput maps a string parameter of a JSON body or form to an input file.
type TextInput struct {
	Field string // e.g. "html"
	File  string // e.g. "index.html"
	Help  string
}

// Op is one operation.
type Op struct {
	Name       string // CLI command path, e.g. "pages select"
	Path       string // API path, e.g. "/v1/pages/select"
	Summary    string
	Inputs     Inputs
	TextInputs []TextInput
	Output     string // pdf, html, json, images, pdfs
	Browser    bool   // uses Chromium
	NewParams  func() any
	Run        func(ctx context.Context, env *Env, files []File, params any) (*Output, error)
}

// ErrInvalid marks request errors (HTTP 400 / CLI exit code 2).
var ErrInvalid = errors.New("invalid request")

func invalid(format string, a ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalid, fmt.Sprintf(format, a...))
}

// IsClientError reports whether err was caused by the request.
func IsClientError(err error) bool {
	return errors.Is(err, ErrInvalid) || errors.Is(err, pdfops.ErrBadInput) ||
		errors.Is(err, imgpdf.ErrBadImage) || errors.Is(err, raster.ErrPassword) ||
		errors.Is(err, chromium.ErrResource)
}

// All returns the registry in display order.
func All() []*Op { return registry }

// ByPath finds an op by API path.
func ByPath(p string) *Op {
	for _, o := range registry {
		if o.Path == p {
			return o
		}
	}
	return nil
}

// NewParams returns params with defaults applied.
func (o *Op) Params() any {
	p := o.NewParams()
	if err := ApplyDefaults(p); err != nil {
		panic(err)
	}
	return p
}

// Validate checks the input files against the op's input spec.
func (o *Op) Validate(files []File) error {
	if len(files) < o.Inputs.Min {
		return invalid("%s needs at least %d input file(s): %s", o.Name, o.Inputs.Min, o.Inputs.Description)
	}
	if o.Inputs.Max > 0 && len(files) > o.Inputs.Max {
		return invalid("%s takes at most %d input file(s)", o.Name, o.Inputs.Max)
	}
	if o.Inputs.Kind == "pdf" {
		for _, f := range files {
			if !IsPDF(f.Data) {
				return invalid("%s is not a PDF", f.Name)
			}
		}
	}
	return nil
}

// IsPDF sniffs the PDF header (within the first KiB, as readers allow).
func IsPDF(b []byte) bool {
	n := min(len(b), 1024)
	return bytes.Contains(b[:n], []byte("%PDF-"))
}

// OutputName derives an output file name from the first input.
func OutputName(in []File, ext string) string {
	base := "document"
	if len(in) > 0 && in[0].Name != "" {
		base = strings.TrimSuffix(path.Base(strings.ReplaceAll(in[0].Name, "\\", "/")), path.Ext(in[0].Name))
		if base == "index" || base == "template" {
			base = "document"
		}
	}
	return base + ext
}

func pdfOut(name string, data []byte) *Output {
	return &Output{Files: []File{{Name: name, ContentType: "application/pdf", Data: data}}}
}

func (e *Env) now() time.Time {
	if e.Now != nil {
		return e.Now()
	}
	return time.Now()
}
