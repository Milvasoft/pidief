package pdfops

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
	xdraw "golang.org/x/image/draw"
)

// CompressOptions controls Compress.
type CompressOptions struct {
	ImageQuality   int  // JPEG quality for recompressed images (1-100); 0 = keep images
	MaxImagePixels int  // longest side of an image after downscaling; 0 = no downscale
	Grayscale      bool // convert recompressed images to grayscale
	Password       string
}

// CompressStats reports what Compress did.
type CompressStats struct {
	BytesBefore     int `json:"bytesBefore"`
	BytesAfter      int `json:"bytesAfter"`
	ImagesSeen      int `json:"imagesSeen"`
	ImagesRewritten int `json:"imagesRewritten"`
}

// Compress recompresses large raster images (JPEG and 8-bit Flate RGB/gray) and removes
// duplicate/unused objects (pdfcpu optimize), writing compact object and xref streams.
func Compress(ctx context.Context, pdf []byte, o CompressOptions) ([]byte, *CompressStats, error) {
	c, err := ReadContext(ctx, pdf, o.Password)
	if err != nil {
		return nil, nil, err
	}
	st := &CompressStats{BytesBefore: len(pdf)}
	if o.ImageQuality > 100 {
		return nil, nil, fmt.Errorf("%w: imageQuality must be 1-100", ErrBadInput)
	}
	if o.ImageQuality > 0 {
		for objNr, entry := range c.Table {
			if err := ctx.Err(); err != nil {
				return nil, nil, err
			}
			if entry == nil || entry.Free {
				continue
			}
			sd, ok := entry.Object.(types.StreamDict)
			if !ok || sd.Subtype() == nil || *sd.Subtype() != "Image" {
				continue
			}
			st.ImagesSeen++
			nsd, ok := recompressImage(sd, o)
			if ok {
				c.Table[objNr].Object = *nsd
				st.ImagesRewritten++
			}
		}
	}
	if err := api.OptimizeContext(ctx, c); err != nil {
		return nil, nil, bad(err)
	}
	c.Write.WriteToObjectStream = true
	out, err := WriteContext(ctx, c)
	if err != nil {
		return nil, nil, err
	}
	if len(out) > len(pdf) && st.ImagesRewritten == 0 {
		out = pdf // nothing gained
	}
	st.BytesAfter = len(out)
	return out, st, nil
}

func recompressImage(sd types.StreamDict, o CompressOptions) (*types.StreamDict, bool) {
	if sd.BooleanEntry("ImageMask") != nil && *sd.BooleanEntry("ImageMask") {
		return nil, false
	}
	for _, k := range []string{"Mask", "Decode", "Alternates"} {
		if _, ok := sd.Find(k); ok {
			return nil, false
		}
	}
	if bpc := sd.IntEntry("BitsPerComponent"); bpc == nil || *bpc != 8 {
		return nil, false
	}
	cs := sd.NameEntry("ColorSpace")
	if cs == nil || (*cs != "DeviceRGB" && *cs != "DeviceGray") {
		return nil, false
	}
	w, h := sd.IntEntry("Width"), sd.IntEntry("Height")
	if w == nil || h == nil || *w <= 0 || *h <= 0 {
		return nil, false
	}
	if len(sd.FilterPipeline) != 1 {
		return nil, false
	}
	var img image.Image
	switch sd.FilterPipeline[0].Name {
	case "DCTDecode":
		im, err := jpeg.Decode(bytes.NewReader(sd.Raw))
		if err != nil {
			return nil, false
		}
		img = im
	case "FlateDecode":
		if sd.FilterPipeline[0].DecodeParms != nil {
			return nil, false
		}
		if err := sd.DecodeWithLimit(maxDecodedStream); err != nil {
			return nil, false
		}
		img = rawToImage(sd.Content, *w, *h, *cs == "DeviceGray")
		if img == nil {
			return nil, false
		}
	default:
		return nil, false
	}
	b := img.Bounds()
	if o.MaxImagePixels > 0 && max(b.Dx(), b.Dy()) > o.MaxImagePixels {
		scale := float64(o.MaxImagePixels) / float64(max(b.Dx(), b.Dy()))
		nw, nh := max(1, int(float64(b.Dx())*scale)), max(1, int(float64(b.Dy())*scale))
		dst := image.NewRGBA(image.Rect(0, 0, nw, nh))
		xdraw.CatmullRom.Scale(dst, dst.Bounds(), img, b, xdraw.Src, nil)
		img = dst
	}
	gray := o.Grayscale || *cs == "DeviceGray"
	if gray {
		g := image.NewGray(img.Bounds())
		xdraw.Draw(g, g.Bounds(), img, img.Bounds().Min, xdraw.Src)
		img = g
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: o.ImageQuality}); err != nil {
		return nil, false
	}
	if buf.Len() >= len(sd.Raw) {
		return nil, false
	}
	nsd := types.StreamDict{Dict: types.NewDict(), Raw: buf.Bytes()}
	for k, v := range sd.Dict {
		switch k {
		case "Filter", "DecodeParms", "Length", "Width", "Height", "ColorSpace":
		default:
			nsd.Insert(k, v)
		}
	}
	nb := img.Bounds()
	nsd.InsertName("Filter", "DCTDecode")
	nsd.InsertInt("Width", nb.Dx())
	nsd.InsertInt("Height", nb.Dy())
	if gray {
		nsd.InsertName("ColorSpace", "DeviceGray")
	} else {
		nsd.InsertName("ColorSpace", "DeviceRGB")
	}
	nsd.FilterPipeline = []types.PDFFilter{{Name: "DCTDecode"}}
	l := int64(buf.Len())
	nsd.StreamLength = &l
	nsd.InsertInt("Length", buf.Len())
	return &nsd, true
}

func rawToImage(b []byte, w, h int, gray bool) image.Image {
	if gray {
		if len(b) < w*h {
			return nil
		}
		g := image.NewGray(image.Rect(0, 0, w, h))
		copy(g.Pix, b[:w*h])
		return g
	}
	if len(b) < w*h*3 {
		return nil
	}
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := 0; i < w*h; i++ {
		img.Pix[i*4] = b[i*3]
		img.Pix[i*4+1] = b[i*3+1]
		img.Pix[i*4+2] = b[i*3+2]
		img.Pix[i*4+3] = 255
	}
	_ = color.RGBA{}
	return img
}
