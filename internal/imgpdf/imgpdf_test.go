package imgpdf_test

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"testing"

	"github.com/gen2brain/webp"

	"github.com/milvasoft/pidief/internal/imgpdf"
	"github.com/milvasoft/pidief/internal/pdfops"
	"github.com/milvasoft/pidief/internal/testutil"
)

func TestConvertOrientationAndFit(t *testing.T) {
	ctx := context.Background()
	land := testutil.Gradient(400, 200)
	// Orientation 6 (rotate 90° CW): stored 400x200, displayed 200x400 -> portrait page.
	rotated := testutil.JPEG(t, land, 6)
	plain := testutil.JPEG(t, land, 0)
	alpha := image.NewNRGBA(image.Rect(0, 0, 100, 100))
	alpha.Set(10, 10, color.NRGBA{0, 0, 255, 128})
	var wb bytes.Buffer
	if err := webp.Encode(&wb, testutil.Gradient(120, 80), webp.Options{Quality: 80}); err != nil {
		t.Fatal(err)
	}
	pdf, err := imgpdf.Convert(ctx, []imgpdf.Input{
		{Name: "a.jpg", Data: rotated},
		{Name: "b.jpg", Data: plain},
		{Name: "c.png", Data: testutil.PNG(t, alpha)},
		{Name: "d.webp", Data: wb.Bytes()},
	}, imgpdf.Options{PageSize: "A4", Landscape: "auto", Margin: 28, MaxDPI: 150, Quality: 80, Title: "Tarama Çağrı"})
	if err != nil {
		t.Fatal(err)
	}
	info, err := pdfops.GetInfo(ctx, pdf, "")
	if err != nil {
		t.Fatal(err)
	}
	if info.PageCount != 4 {
		t.Fatalf("pages = %d", info.PageCount)
	}
	if p := info.Pages[0]; p.Width > p.Height {
		t.Errorf("EXIF-rotated image should give a portrait page, got %vx%v", p.Width, p.Height)
	}
	if p := info.Pages[1]; p.Width < p.Height {
		t.Errorf("landscape image should give a landscape page, got %vx%v", p.Width, p.Height)
	}
	if info.Metadata.Title != "Tarama Çağrı" {
		t.Errorf("title = %q", info.Metadata.Title)
	}
	// The untouched JPEG is passed through byte for byte.
	if !bytes.Contains(pdf, plain) {
		t.Error("plain JPEG was re-encoded")
	}
}

func TestRejectsBadInput(t *testing.T) {
	ctx := context.Background()
	if _, err := imgpdf.Convert(ctx, []imgpdf.Input{{Name: "x.jpg", Data: []byte("nope")}}, imgpdf.Options{}); err == nil {
		t.Fatal("want error")
	}
	heic := append([]byte{0, 0, 0, 24}, []byte("ftypheic0000")...)
	_, err := imgpdf.Convert(ctx, []imgpdf.Input{{Name: "x.heic", Data: heic}}, imgpdf.Options{})
	if err == nil || !bytes.Contains([]byte(err.Error()), []byte("HEIC")) {
		t.Fatalf("want HEIC error, got %v", err)
	}
}
