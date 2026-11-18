// Package raster renders PDF pages to images and extracts text with PDFium compiled to
// WebAssembly (run by wazero, pure Go, no cgo). PDFium is Chromium's PDF engine (BSD-3/Apache-2.0).
package raster

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"image/png"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gen2brain/webp"
	"github.com/klippa-app/go-pdfium"
	"github.com/klippa-app/go-pdfium/references"
	"github.com/klippa-app/go-pdfium/requests"
	"github.com/klippa-app/go-pdfium/webassembly"
	"github.com/tetratelabs/wazero"
	xdraw "golang.org/x/image/draw"
)

// Engine is a lazily initialised pool of PDFium WebAssembly instances.
type Engine struct {
	maxWorkers int
	once       sync.Once
	pool       pdfium.Pool
	initErr    error
}

// New returns an Engine with at most maxWorkers concurrent PDFium instances.
func New(maxWorkers int) *Engine {
	if maxWorkers < 1 {
		maxWorkers = 1
	}
	return &Engine{maxWorkers: maxWorkers}
}

func (e *Engine) instance(ctx context.Context) (pdfium.Pdfium, error) {
	e.once.Do(func() {
		e.pool, e.initErr = webassembly.Init(webassembly.Config{
			MinIdle:  0,
			MaxIdle:  1,
			MaxTotal: e.maxWorkers,
			// No filesystem access for the sandboxed module: documents are passed as bytes.
			FSConfig: wazero.NewFSConfig(),
		})
	})
	if e.initErr != nil {
		return nil, fmt.Errorf("init pdfium: %w", e.initErr)
	}
	timeout := 60 * time.Second
	if dl, ok := ctx.Deadline(); ok {
		timeout = time.Until(dl)
	}
	return e.pool.GetInstance(timeout)
}

// Close releases the pool.
func (e *Engine) Close() {
	if e.pool != nil {
		_ = e.pool.Close()
	}
}

// ErrPassword is returned when a document needs a password.
var ErrPassword = errors.New("document is encrypted: password required or wrong")

func (e *Engine) withDoc(ctx context.Context, data []byte, password string, fn func(inst pdfium.Pdfium, count int, doc references.FPDF_DOCUMENT) error) error {
	inst, err := e.instance(ctx)
	if err != nil {
		return err
	}
	defer inst.Close()
	req := &requests.OpenDocument{File: &data}
	if password != "" {
		req.Password = &password
	}
	d, err := inst.OpenDocument(req)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "password") {
			return ErrPassword
		}
		return fmt.Errorf("open pdf: %w", err)
	}
	defer func() { _, _ = inst.FPDF_CloseDocument(&requests.FPDF_CloseDocument{Document: d.Document}) }()
	pc, err := inst.FPDF_GetPageCount(&requests.FPDF_GetPageCount{Document: d.Document})
	if err != nil {
		return err
	}
	return fn(inst, pc.PageCount, d.Document)
}

// Image is one rendered page.
type Image struct {
	Page          int // 1-based
	Width, Height int
	Data          []byte
	ContentType   string
}

// RenderOptions controls page rendering.
type RenderOptions struct {
	Pages      []int  // 1-based; empty = all
	DPI        int    // default 96
	MaxWidth   int    // downscale to this width (thumbnails); 0 = no limit
	MaxHeight  int    // downscale to this height; 0 = no limit
	Format     string // png, jpeg, webp
	Quality    int    // jpeg/webp quality 1-100
	Background bool   // paint white behind transparent pages (always for jpeg)
	Password   string
}

// Render renders the selected pages.
func (e *Engine) Render(ctx context.Context, pdf []byte, o RenderOptions) ([]Image, error) {
	if o.DPI <= 0 {
		o.DPI = 96
	}
	if o.DPI > 600 {
		return nil, fmt.Errorf("dpi %d is above the limit of 600", o.DPI)
	}
	if o.Quality <= 0 || o.Quality > 100 {
		o.Quality = 85
	}
	format := strings.ToLower(o.Format)
	if format == "" {
		format = "png"
	}
	if format == "jpg" {
		format = "jpeg"
	}
	if format != "png" && format != "jpeg" && format != "webp" {
		return nil, fmt.Errorf("unsupported image format %q (png, jpeg, webp)", o.Format)
	}
	var out []Image
	err := e.withDoc(ctx, pdf, o.Password, func(inst pdfium.Pdfium, count int, doc references.FPDF_DOCUMENT) error {
		pages := o.Pages
		if len(pages) == 0 {
			for i := 1; i <= count; i++ {
				pages = append(pages, i)
			}
		}
		for _, p := range pages {
			if err := ctx.Err(); err != nil {
				return err
			}
			if p < 1 || p > count {
				return fmt.Errorf("page %d out of range (1-%d)", p, count)
			}
			img, err := e.renderPage(inst, doc, p-1, o)
			if err != nil {
				return err
			}
			data, err := encode(img, format, o.Quality, o.Background || format == "jpeg")
			if err != nil {
				return err
			}
			b := img.Bounds()
			out = append(out, Image{Page: p, Width: b.Dx(), Height: b.Dy(), Data: data, ContentType: "image/" + format})
		}
		return nil
	})
	return out, err
}

func (e *Engine) renderPage(inst pdfium.Pdfium, doc references.FPDF_DOCUMENT, index int, o RenderOptions) (image.Image, error) {
	page := requests.Page{ByIndex: &requests.PageByIndex{Document: doc, Index: index}}
	dpi := o.DPI
	if o.MaxWidth > 0 || o.MaxHeight > 0 {
		// Render straight at the target size: cheaper and sharper than downscaling.
		size, err := inst.GetPageSize(&requests.GetPageSize{Page: page})
		if err != nil {
			return nil, err
		}
		w := size.Width * float64(dpi) / 72
		h := size.Height * float64(dpi) / 72
		scale := 1.0
		if o.MaxWidth > 0 && w > float64(o.MaxWidth) {
			scale = float64(o.MaxWidth) / w
		}
		if o.MaxHeight > 0 && h*scale > float64(o.MaxHeight) {
			scale = float64(o.MaxHeight) / h
		}
		if scale < 1 {
			dpi = max(1, int(float64(dpi)*scale))
		}
	}
	res, err := inst.RenderPageInDPI(&requests.RenderPageInDPI{Page: page, DPI: dpi, RenderForm: true, Document: &doc})
	if err != nil {
		return nil, fmt.Errorf("render page %d: %w", index+1, err)
	}
	defer res.Cleanup()
	// Copy out of the WebAssembly memory before Cleanup.
	src := res.Result.RenderedImage
	dst := image.NewRGBA(src.Bounds())
	draw.Draw(dst, dst.Bounds(), src, src.Bounds().Min, draw.Src)
	if o.MaxWidth > 0 && dst.Bounds().Dx() > o.MaxWidth {
		nh := dst.Bounds().Dy() * o.MaxWidth / dst.Bounds().Dx()
		scaled := image.NewRGBA(image.Rect(0, 0, o.MaxWidth, max(1, nh)))
		xdraw.CatmullRom.Scale(scaled, scaled.Bounds(), dst, dst.Bounds(), xdraw.Src, nil)
		dst = scaled
	}
	return dst, nil
}

func encode(img image.Image, format string, quality int, flatten bool) ([]byte, error) {
	if flatten {
		bg := image.NewRGBA(img.Bounds())
		draw.Draw(bg, bg.Bounds(), &image.Uniform{C: color.White}, image.Point{}, draw.Src)
		draw.Draw(bg, bg.Bounds(), img, img.Bounds().Min, draw.Over)
		img = bg
	}
	var buf bytes.Buffer
	var err error
	switch format {
	case "png":
		enc := png.Encoder{CompressionLevel: png.BestSpeed}
		err = enc.Encode(&buf, img)
	case "jpeg":
		err = jpeg.Encode(&buf, img, &jpeg.Options{Quality: quality})
	case "webp":
		err = webp.Encode(&buf, img, webp.Options{Quality: quality, Method: 4})
	}
	return buf.Bytes(), err
}

// Text returns the plain text of every page. Lines are rebuilt from character positions:
// PDFium's own line breaking splits Chromium's per-glyph text objects into one char per line.
func (e *Engine) Text(ctx context.Context, pdf []byte, password string) ([]string, error) {
	var out []string
	err := e.withDoc(ctx, pdf, password, func(inst pdfium.Pdfium, count int, doc references.FPDF_DOCUMENT) error {
		for i := 0; i < count; i++ {
			if err := ctx.Err(); err != nil {
				return err
			}
			cs, err := pageChars(inst, doc, i)
			if err != nil {
				return err
			}
			out = append(out, layoutText(cs))
		}
		return nil
	})
	return out, err
}

// pageChars reads every non-generated character with its loose (advance-based) box, which
// keeps narrow glyphs such as "1" from looking like a word gap.
func pageChars(inst pdfium.Pdfium, doc references.FPDF_DOCUMENT, index int) ([]charBox, error) {
	tp, err := inst.FPDFText_LoadPage(&requests.FPDFText_LoadPage{Page: requests.Page{ByIndex: &requests.PageByIndex{Document: doc, Index: index}}})
	if err != nil {
		return nil, err
	}
	defer func() { _, _ = inst.FPDFText_ClosePage(&requests.FPDFText_ClosePage{TextPage: tp.TextPage}) }()
	n, err := inst.FPDFText_CountChars(&requests.FPDFText_CountChars{TextPage: tp.TextPage})
	if err != nil {
		return nil, err
	}
	cs := make([]charBox, 0, n.Count)
	for i := 0; i < n.Count; i++ {
		gen, err := inst.FPDFText_IsGenerated(&requests.FPDFText_IsGenerated{TextPage: tp.TextPage, Index: i})
		if err != nil || gen.IsGenerated {
			continue
		}
		u, err := inst.FPDFText_GetUnicode(&requests.FPDFText_GetUnicode{TextPage: tp.TextPage, Index: i})
		if err != nil || u.Unicode == 0 || u.Unicode == 13 || u.Unicode == 10 {
			continue
		}
		box, err := inst.FPDFText_GetLooseCharBox(&requests.FPDFText_GetLooseCharBox{TextPage: tp.TextPage, Index: i})
		if err != nil {
			continue
		}
		r := box.Rect
		cs = append(cs, charBox{text: string(rune(u.Unicode)),
			left: float64(min(r.Left, r.Right)), right: float64(max(r.Left, r.Right)),
			top: float64(max(r.Top, r.Bottom)), bottom: float64(min(r.Top, r.Bottom))})
	}
	return cs, nil
}

type charBox struct {
	text                     string
	left, right, top, bottom float64
}

// layoutText groups characters into lines by vertical centre and orders them left to right.
func layoutText(cs []charBox) string {
	type line struct {
		cy, h float64
		chars []charBox
	}
	var lines []*line
	for _, c := range cs {
		h := c.top - c.bottom
		cy := (c.top + c.bottom) / 2
		if strings.TrimSpace(c.text) == "" && h <= 0 {
			continue // generated space without a box; gaps are recomputed below
		}
		var best *line
		for _, l := range lines {
			if math.Abs(l.cy-cy) <= 0.5*max(l.h, h, 1) {
				best = l
				break
			}
		}
		if best == nil {
			best = &line{cy: cy, h: h}
			lines = append(lines, best)
		}
		best.chars = append(best.chars, c)
		best.h = max(best.h, h)
	}
	sort.SliceStable(lines, func(i, j int) bool { return lines[i].cy > lines[j].cy })
	var b strings.Builder
	for li, l := range lines {
		sort.SliceStable(l.chars, func(i, j int) bool { return l.chars[i].left < l.chars[j].left })
		var sb strings.Builder
		prevRight := math.Inf(-1)
		for _, c := range l.chars {
			if strings.TrimSpace(c.text) == "" {
				if !strings.HasSuffix(sb.String(), " ") && sb.Len() > 0 {
					sb.WriteByte(' ')
				}
				prevRight = c.right
				continue
			}
			if sb.Len() > 0 && !strings.HasSuffix(sb.String(), " ") && c.left-prevRight > 0.1*l.h {
				sb.WriteByte(' ')
			}
			sb.WriteString(c.text)
			prevRight = c.right
		}
		if li > 0 {
			b.WriteString("\n")
		}
		b.WriteString(strings.TrimSpace(sb.String()))
	}
	return b.String()
}

// PageCount returns the number of pages.
func (e *Engine) PageCount(ctx context.Context, pdf []byte, password string) (int, error) {
	n := 0
	err := e.withDoc(ctx, pdf, password, func(_ pdfium.Pdfium, count int, _ references.FPDF_DOCUMENT) error {
		n = count
		return nil
	})
	return n, err
}
