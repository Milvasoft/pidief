package pdfops_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/milvasoft/pidief/internal/imgpdf"
	"github.com/milvasoft/pidief/internal/pdfops"
	"github.com/milvasoft/pidief/internal/testutil"
)

// samplePDF builds an n-page PDF without Chromium.
func samplePDF(t *testing.T, n int) []byte {
	t.Helper()
	var in []imgpdf.Input
	for i := 0; i < n; i++ {
		in = append(in, imgpdf.Input{Name: "p.png", Data: testutil.PNG(t, testutil.Gradient(300+i*10, 400))})
	}
	pdf, err := imgpdf.Convert(context.Background(), in, imgpdf.Options{PageSize: "A4", Landscape: "portrait", Quality: 70})
	if err != nil {
		t.Fatal(err)
	}
	return pdf
}

func count(t *testing.T, pdf []byte, pw string) int {
	t.Helper()
	n, err := pdfops.PageCount(context.Background(), pdf, pw)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func TestPageOperations(t *testing.T) {
	ctx := context.Background()
	a, b := samplePDF(t, 3), samplePDF(t, 2)
	merged, err := pdfops.Merge(ctx, [][]byte{a, b}, "")
	if err != nil {
		t.Fatal(err)
	}
	if n := count(t, merged, ""); n != 5 {
		t.Fatalf("merged pages = %d", n)
	}
	sel, err := pdfops.Collect(ctx, merged, "5,1,1", "")
	if err != nil {
		t.Fatal(err)
	}
	if n := count(t, sel, ""); n != 3 {
		t.Fatalf("collect pages = %d", n)
	}
	del, err := pdfops.Remove(ctx, merged, "2-3", "")
	if err != nil {
		t.Fatal(err)
	}
	if n := count(t, del, ""); n != 3 {
		t.Fatalf("remove pages = %d", n)
	}
	rot, err := pdfops.Rotate(ctx, merged, 90, "even", "")
	if err != nil {
		t.Fatal(err)
	}
	info, _ := pdfops.GetInfo(ctx, rot, "")
	if info.Pages[1].Rotation != 90 || info.Pages[0].Rotation != 0 {
		t.Fatalf("rotation = %d/%d", info.Pages[0].Rotation, info.Pages[1].Rotation)
	}
	if _, err := pdfops.Rotate(ctx, merged, 45, "", ""); err == nil {
		t.Fatal("45 degrees should fail")
	}
	if _, err := pdfops.Collect(ctx, merged, "x-y", ""); err == nil {
		t.Fatal("bad selection should fail")
	}
}

func TestSplit(t *testing.T) {
	ctx := context.Background()
	pdf := samplePDF(t, 5)
	cases := []struct {
		span   int
		at     []int
		ranges []string
		want   []int
	}{
		{span: 2, want: []int{2, 2, 1}},
		{at: []int{3}, want: []int{2, 3}},
		{ranges: []string{"1-2", "4-"}, want: []int{2, 2}},
		{want: []int{1, 1, 1, 1, 1}},
	}
	for _, c := range cases {
		parts, err := pdfops.Split(ctx, pdf, c.span, c.at, c.ranges, "")
		if err != nil {
			t.Fatal(err)
		}
		var got []int
		for _, p := range parts {
			got = append(got, p.PageCount)
		}
		if len(got) != len(c.want) {
			t.Fatalf("%+v: parts %v", c, got)
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Fatalf("%+v: parts %v", c, got)
			}
		}
	}
}

func TestEncryptDecryptMetadata(t *testing.T) {
	ctx := context.Background()
	pdf := samplePDF(t, 2)
	enc, err := pdfops.Encrypt(ctx, pdf, pdfops.EncryptOptions{UserPassword: "gizli-Şifre1", AllowPrint: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pdfops.PageCount(ctx, enc, ""); err == nil {
		t.Fatal("encrypted PDF opened without password")
	}
	info, err := pdfops.GetInfo(ctx, enc, "gizli-Şifre1")
	if err != nil || !info.Encrypted {
		t.Fatalf("info: %v encrypted=%v", err, info != nil && info.Encrypted)
	}
	dec, err := pdfops.Decrypt(ctx, enc, "gizli-Şifre1")
	if err != nil {
		t.Fatal(err)
	}
	if n := count(t, dec, ""); n != 2 {
		t.Fatalf("pages = %d", n)
	}

	c, err := pdfops.ReadContext(ctx, dec, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := pdfops.SetMetadata(c, pdfops.Metadata{Title: "Kira Makbuzu – Ağustos", Author: "Örnek Yapı"}, map[string]string{"BelgeNo": "KM-2026/08"}, time.Now()); err != nil {
		t.Fatal(err)
	}
	out, err := pdfops.WriteContext(ctx, c)
	if err != nil {
		t.Fatal(err)
	}
	info, err = pdfops.GetInfo(ctx, out, "")
	if err != nil {
		t.Fatal(err)
	}
	if info.Metadata.Title != "Kira Makbuzu – Ağustos" || info.Metadata.Author != "Örnek Yapı" || info.Custom["BelgeNo"] != "KM-2026/08" {
		t.Fatalf("metadata = %+v %+v", info.Metadata, info.Custom)
	}
	if info.Metadata.Producer != "pidief" {
		t.Fatalf("producer = %q", info.Metadata.Producer)
	}
}

func TestStampUsesEmbeddedUnicodeFont(t *testing.T) {
	ctx := context.Background()
	pdf := samplePDF(t, 2)
	out, err := pdfops.Stamp(ctx, pdf, pdfops.StampOptions{Text: "ONAYLI ŞĞÜİÖÇ %p/%P", OnTop: true, FontSize: 24, Opacity: 1, Position: "tc"})
	if err != nil {
		t.Fatal(err)
	}
	c, err := pdfops.ReadContext(ctx, out, "")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, f := range pdfops.Fonts(c) {
		if strings.Contains(f.Name, "PidiefStamp") {
			found = true
			if !f.Embedded {
				t.Fatal("stamp font not embedded")
			}
		}
	}
	if !found {
		t.Fatalf("stamp font missing: %+v", pdfops.Fonts(c))
	}
	img := testutil.PNG(t, testutil.Gradient(50, 50))
	if _, err := pdfops.Stamp(ctx, pdf, pdfops.StampOptions{Image: img, OnTop: true, Opacity: 0.5, Position: "br", Scale: 0.2}); err != nil {
		t.Fatal(err)
	}
	if _, err := pdfops.Stamp(ctx, pdf, pdfops.StampOptions{Text: "x", Opacity: 0.5, Position: "zz"}); err == nil {
		t.Fatal("bad position should fail")
	}
}

func TestCompressShrinksImages(t *testing.T) {
	ctx := context.Background()
	var in []imgpdf.Input
	for i := 0; i < 2; i++ {
		in = append(in, imgpdf.Input{Name: "big.jpg", Data: testutil.JPEG(t, testutil.Gradient(2400, 3200), 0)})
	}
	pdf, err := imgpdf.Convert(ctx, in, imgpdf.Options{PageSize: "A4", MaxDPI: 0, Quality: 95})
	if err != nil {
		t.Fatal(err)
	}
	out, st, err := pdfops.Compress(ctx, pdf, pdfops.CompressOptions{ImageQuality: 60, MaxImagePixels: 1200})
	if err != nil {
		t.Fatal(err)
	}
	if st.ImagesRewritten != 2 || len(out) >= len(pdf)/2 {
		t.Fatalf("compress: %+v (%d -> %d)", st, len(pdf), len(out))
	}
	if n := count(t, out, ""); n != 2 {
		t.Fatalf("pages = %d", n)
	}
}

func TestPDFAOnImagePDF(t *testing.T) {
	ctx := context.Background()
	res, err := pdfops.ToPDFA2B(ctx, samplePDF(t, 1), pdfops.Metadata{Title: "Sözleşme Örneği", Author: "Milvasoft"}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	info, err := pdfops.GetInfo(ctx, res.Data, "")
	if err != nil {
		t.Fatal(err)
	}
	if info.PDFA != "2B" || info.Version != "1.7" || info.Metadata.Title != "Sözleşme Örneği" {
		t.Fatalf("pdfa=%q version=%q title=%q", info.PDFA, info.Version, info.Metadata.Title)
	}
	enc, _ := pdfops.Encrypt(ctx, samplePDF(t, 1), pdfops.EncryptOptions{UserPassword: "x"})
	if _, err := pdfops.ToPDFA2B(ctx, enc, pdfops.Metadata{}, time.Now()); err == nil {
		t.Fatal("encrypted input should fail")
	}
}
