// Package imgpdf turns photos and scans (JPEG, PNG, WebP, GIF, TIFF, BMP) into a PDF:
// EXIF orientation is applied, each image is fitted to the page, downsampled to a maximum
// DPI and stored as JPEG. Untouched JPEGs are passed through without re-encoding.
package imgpdf

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	_ "image/gif"
	"image/jpeg"
	_ "image/png"
	"strings"
	"time"

	_ "golang.org/x/image/bmp"
	xdraw "golang.org/x/image/draw"
	_ "golang.org/x/image/tiff"
	_ "golang.org/x/image/webp"

	"github.com/milvasoft/pidief/internal/units"
)

// ErrBadImage marks undecodable input.
var ErrBadImage = errors.New("bad image")

// Options controls the conversion.
type Options struct {
	PageSize  string  // paper name, or "fit" to make each page the size of its image
	Landscape string  // auto (page follows the image), portrait, landscape
	Margin    float64 // points
	MaxDPI    int     // downsample above this resolution at the placed size (0 = keep)
	Quality   int     // JPEG quality when re-encoding
	Grayscale bool
	MaxPixels int // reject images larger than this many pixels (decompression bomb guard)
	Title     string
}

// Input is one image file.
type Input struct {
	Name string
	Data []byte
}

type page struct {
	w, h         float64 // page size, points
	x, y, dw, dh float64 // image placement
	jpeg         []byte
	pw, ph       int
	gray         bool
}

// Convert builds the PDF.
func Convert(ctx context.Context, in []Input, o Options) ([]byte, error) {
	if len(in) == 0 {
		return nil, fmt.Errorf("%w: no images", ErrBadImage)
	}
	if o.Quality <= 0 || o.Quality > 100 {
		o.Quality = 80
	}
	if o.MaxPixels <= 0 {
		o.MaxPixels = 80_000_000
	}
	var pages []page
	for _, f := range in {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		p, err := preparePage(f, o)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", f.Name, err)
		}
		pages = append(pages, p)
	}
	return writePDF(pages, o.Title), nil
}

func preparePage(f Input, o Options) (page, error) {
	cfg, format, err := image.DecodeConfig(bytes.NewReader(f.Data))
	if err != nil {
		if isHEIC(f.Data) {
			return page{}, fmt.Errorf("%w: HEIC is not supported, send JPEG", ErrBadImage)
		}
		return page{}, fmt.Errorf("%w: %v", ErrBadImage, err)
	}
	if cfg.Width*cfg.Height > o.MaxPixels {
		return page{}, fmt.Errorf("%w: %dx%d is above the pixel limit", ErrBadImage, cfg.Width, cfg.Height)
	}
	orient := 1
	if format == "jpeg" {
		orient = exifOrientation(f.Data)
	}
	iw, ih := cfg.Width, cfg.Height
	if orient >= 5 {
		iw, ih = ih, iw
	}

	// Page geometry.
	var pw, ph float64
	if strings.EqualFold(o.PageSize, "fit") {
		// 1 px = 1/150 inch keeps phone photos at a sane physical size.
		pw, ph = float64(iw)*72/150+2*o.Margin, float64(ih)*72/150+2*o.Margin
	} else {
		sz, err := units.Paper(o.PageSize)
		if err != nil {
			return page{}, err
		}
		pw, ph = sz.W, sz.H
		land := strings.ToLower(o.Landscape)
		if land == "" || land == "auto" {
			if (iw > ih) != (pw > ph) {
				pw, ph = ph, pw
			}
		} else if (land == "landscape") != (pw > ph) {
			pw, ph = ph, pw
		}
	}
	aw, ah := pw-2*o.Margin, ph-2*o.Margin
	if aw <= 0 || ah <= 0 {
		return page{}, fmt.Errorf("%w: margin too large for page", ErrBadImage)
	}
	scale := min(aw/float64(iw), ah/float64(ih))
	dw, dh := float64(iw)*scale, float64(ih)*scale
	p := page{w: pw, h: ph, dw: dw, dh: dh, x: (pw - dw) / 2, y: (ph - dh) / 2}

	// Target pixel size for the max DPI.
	tw, th := iw, ih
	if o.MaxDPI > 0 {
		maxW := int(dw / 72 * float64(o.MaxDPI))
		if maxW < iw {
			tw = max(1, maxW)
			th = max(1, ih*tw/iw)
		}
	}
	if format == "jpeg" && orient == 1 && tw == iw && !o.Grayscale && cfg.ColorModel != color.CMYKModel {
		p.jpeg, p.pw, p.ph = f.Data, iw, ih
		p.gray = cfg.ColorModel == color.GrayModel
		return p, nil
	}
	img, _, err := image.Decode(bytes.NewReader(f.Data))
	if err != nil {
		return page{}, fmt.Errorf("%w: %v", ErrBadImage, err)
	}
	img = orientImage(img, orient)
	if tw != iw {
		dst := image.NewRGBA(image.Rect(0, 0, tw, th))
		xdraw.CatmullRom.Scale(dst, dst.Bounds(), img, img.Bounds(), xdraw.Over, nil)
		img = dst
	}
	// Flatten transparency onto white.
	flat := image.NewRGBA(img.Bounds())
	draw.Draw(flat, flat.Bounds(), image.White, image.Point{}, draw.Src)
	draw.Draw(flat, flat.Bounds(), img, img.Bounds().Min, draw.Over)
	var out image.Image = flat
	if o.Grayscale {
		g := image.NewGray(flat.Bounds())
		draw.Draw(g, g.Bounds(), flat, image.Point{}, draw.Src)
		out = g
		p.gray = true
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, out, &jpeg.Options{Quality: o.Quality}); err != nil {
		return page{}, err
	}
	p.jpeg, p.pw, p.ph = buf.Bytes(), out.Bounds().Dx(), out.Bounds().Dy()
	return p, nil
}

func isHEIC(b []byte) bool {
	return len(b) > 12 && string(b[4:8]) == "ftyp" &&
		(strings.HasPrefix(string(b[8:12]), "hei") || strings.HasPrefix(string(b[8:12]), "mif") || string(b[8:12]) == "heix")
}

// exifOrientation reads tag 0x0112 from the APP1 Exif segment (1 when absent).
func exifOrientation(b []byte) int {
	if len(b) < 4 || b[0] != 0xFF || b[1] != 0xD8 {
		return 1
	}
	i := 2
	for i+4 <= len(b) {
		if b[i] != 0xFF {
			return 1
		}
		marker := b[i+1]
		if marker == 0xDA || marker == 0xD9 { // start of scan / end
			return 1
		}
		segLen := int(binary.BigEndian.Uint16(b[i+2:]))
		if segLen < 2 || i+2+segLen > len(b) {
			return 1
		}
		seg := b[i+4 : i+2+segLen]
		if marker == 0xE1 && len(seg) > 14 && string(seg[:6]) == "Exif\x00\x00" {
			return tiffOrientation(seg[6:])
		}
		i += 2 + segLen
	}
	return 1
}

func tiffOrientation(t []byte) int {
	if len(t) < 8 {
		return 1
	}
	var bo binary.ByteOrder
	switch string(t[:2]) {
	case "II":
		bo = binary.LittleEndian
	case "MM":
		bo = binary.BigEndian
	default:
		return 1
	}
	off := int(bo.Uint32(t[4:]))
	if off+2 > len(t) {
		return 1
	}
	n := int(bo.Uint16(t[off:]))
	for k := 0; k < n; k++ {
		e := off + 2 + k*12
		if e+12 > len(t) {
			return 1
		}
		if bo.Uint16(t[e:]) == 0x0112 {
			v := int(bo.Uint16(t[e+8:]))
			if v >= 1 && v <= 8 {
				return v
			}
			return 1
		}
	}
	return 1
}

// orientImage applies an EXIF orientation (1-8) so the image displays upright.
func orientImage(src image.Image, o int) image.Image {
	if o <= 1 || o > 8 {
		return src
	}
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	dw, dh := w, h
	if o >= 5 {
		dw, dh = h, w
	}
	dst := image.NewRGBA(image.Rect(0, 0, dw, dh))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			var nx, ny int
			switch o {
			case 2:
				nx, ny = w-1-x, y
			case 3:
				nx, ny = w-1-x, h-1-y
			case 4:
				nx, ny = x, h-1-y
			case 5:
				nx, ny = y, x
			case 6:
				nx, ny = h-1-y, x
			case 7:
				nx, ny = h-1-y, w-1-x
			case 8:
				nx, ny = y, w-1-x
			}
			dst.Set(nx, ny, src.At(b.Min.X+x, b.Min.Y+y))
		}
	}
	return dst
}

func writePDF(pages []page, title string) []byte {
	var buf bytes.Buffer
	var offsets []int
	obj := func(body string, stream []byte) int {
		offsets = append(offsets, buf.Len())
		n := len(offsets)
		fmt.Fprintf(&buf, "%d 0 obj\n%s", n, body)
		if stream != nil {
			buf.WriteString("\nstream\n")
			buf.Write(stream)
			buf.WriteString("\nendstream")
		}
		buf.WriteString("\nendobj\n")
		return n
	}
	buf.WriteString("%PDF-1.7\n%\xE2\xE3\xCF\xD3\n")
	// 1: catalog, 2: pages (placeholders written in order below).
	np := len(pages)
	firstPage := 3
	kids := make([]string, np)
	for i := range pages {
		kids[i] = fmt.Sprintf("%d 0 R", firstPage+i*3)
	}
	obj("<< /Type /Catalog /Pages 2 0 R >>", nil)
	obj(fmt.Sprintf("<< /Type /Pages /Count %d /Kids [%s] >>", np, strings.Join(kids, " ")), nil)
	for i, p := range pages {
		pageNr := firstPage + i*3
		cs := "/DeviceRGB"
		if p.gray {
			cs = "/DeviceGray"
		}
		content := fmt.Sprintf("q %.3f 0 0 %.3f %.3f %.3f cm /Im0 Do Q", p.dw, p.dh, p.x, p.y)
		obj(fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 %.2f %.2f] /Resources << /XObject << /Im0 %d 0 R >> >> /Contents %d 0 R >>",
			p.w, p.h, pageNr+2, pageNr+1), nil)
		obj(fmt.Sprintf("<< /Length %d >>", len(content)), []byte(content))
		obj(fmt.Sprintf("<< /Type /XObject /Subtype /Image /Width %d /Height %d /ColorSpace %s /BitsPerComponent 8 /Filter /DCTDecode /Length %d >>",
			p.pw, p.ph, cs, len(p.jpeg)), p.jpeg)
	}
	info := fmt.Sprintf("<< /Producer (pidief) /CreationDate (D:%s) ", time.Now().UTC().Format("20060102150405Z"))
	if title != "" {
		info += "/Title <FEFF" + utf16Hex(title) + "> "
	}
	infoNr := obj(info+">>", nil)
	xref := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f \n", len(offsets)+1)
	for _, off := range offsets {
		fmt.Fprintf(&buf, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R /Info %d 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(offsets)+1, infoNr, xref)
	return buf.Bytes()
}

func utf16Hex(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r > 0xFFFF {
			r1, r2 := 0xD800+((r-0x10000)>>10), 0xDC00+((r-0x10000)&0x3FF)
			fmt.Fprintf(&b, "%04X%04X", r1, r2)
			continue
		}
		fmt.Fprintf(&b, "%04X", r)
	}
	return b.String()
}
