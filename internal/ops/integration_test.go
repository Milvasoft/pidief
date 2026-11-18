package ops_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/milvasoft/pidief/internal/chromium"
	"github.com/milvasoft/pidief/internal/ops"
	"github.com/milvasoft/pidief/internal/pdfops"
	"github.com/milvasoft/pidief/internal/raster"
	"github.com/milvasoft/pidief/internal/testutil"
)

var env *ops.Env

func TestMain(m *testing.M) {
	env = &ops.Env{
		Browser:       chromium.New(chromium.Config{ExecPath: chromium.ExecPathFromEnv(), NoSandbox: os.Getenv("PIDIEF_CHROMIUM_NO_SANDBOX") == "true"}),
		Raster:        raster.New(2),
		AllowExternal: true,
		MaxTimeout:    time.Minute,
	}
	code := m.Run()
	env.Browser.Close()
	env.Raster.Close()
	os.Exit(code)
}

func needBrowser(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := env.Browser.Ready(ctx); err != nil {
		if os.Getenv("PIDIEF_REQUIRE_BROWSER") == "1" {
			t.Fatalf("chromium: %v", err)
		}
		t.Skipf("chromium not available: %v", err)
	}
}

func run(t *testing.T, path string, files []ops.File, set func(p any)) *ops.Output {
	t.Helper()
	out, err := runErr(path, files, set)
	if err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	return out
}

func runErr(path string, files []ops.File, set func(p any)) (*ops.Output, error) {
	op := ops.ByPath(path)
	p := op.Params()
	if set != nil {
		set(p)
	}
	if err := op.Validate(files); err != nil {
		return nil, err
	}
	return op.Run(context.Background(), env, files, p)
}

func pageTexts(t *testing.T, pdf []byte) []string {
	t.Helper()
	txt, err := env.Raster.Text(context.Background(), pdf, "")
	if err != nil {
		t.Fatal(err)
	}
	return txt
}

const turkish = "Çağrı Şükrü İğdır ığüşöç ĞÜŞÖÇİ"

func reportHTML(rows int) string {
	var b strings.Builder
	b.WriteString(`<!doctype html><html lang="tr"><head><meta charset="utf-8"><title>Cari Ekstre</title>
<style>body{font-family:Onest;font-size:10pt} table{width:100%;border-collapse:collapse}
td,th{border:1px solid #888;padding:3px} tr{break-inside:avoid}</style></head><body>
<h1>` + turkish + `</h1><img src="img/logo.png" width="32"><table><thead><tr><th>Sıra</th><th>Açıklama</th><th>Borç</th></tr></thead><tbody>`)
	for i := 1; i <= rows; i++ {
		fmt.Fprintf(&b, "<tr><td>%d</td><td>Hak ediş ödemesi şantiye %d</td><td>₺%d.250,00</td></tr>", i, i, i)
	}
	b.WriteString(`</tbody></table></body></html>`)
	return b.String()
}

func TestHTMLToPDFTurkishFontsHeadersPageNumbers(t *testing.T) {
	needBrowser(t)
	logo := testutil.PNG(t, testutil.Gradient(32, 32))
	out := run(t, "/v1/html", []ops.File{{Name: "index.html", Data: []byte(reportHTML(120))}, {Name: "img/logo.png", Data: logo}},
		func(p any) {
			pp := p.(*ops.PageParams)
			pp.PageNumbers = true
			pp.FailOnResourceErr = true
			pp.Author = "Milvasoft"
		})
	pdf := out.Files[0].Data
	if out.Files[0].ContentType != "application/pdf" || !ops.IsPDF(pdf) {
		t.Fatal("not a PDF")
	}
	info, err := pdfops.GetInfo(context.Background(), pdf, "")
	if err != nil {
		t.Fatal(err)
	}
	if info.PageCount < 3 {
		t.Fatalf("page count = %d, want a multi-page report", info.PageCount)
	}
	if w, h := info.Pages[0].Width, info.Pages[0].Height; w < 594 || w > 597 || h < 841 || h > 843 {
		t.Fatalf("A4 expected, got %vx%v", w, h)
	}
	if info.Metadata.Title != "Cari Ekstre" || info.Metadata.Author != "Milvasoft" || info.Metadata.Producer != "pidief" {
		t.Fatalf("metadata = %+v", info.Metadata)
	}
	onest := false
	for _, f := range info.Fonts {
		if !f.Embedded {
			t.Errorf("font %s (%s) is not embedded", f.Name, f.Type)
		}
		if strings.Contains(f.Name, "Onest") {
			onest = true
		}
	}
	if !onest {
		t.Errorf("Onest is not among the fonts: %+v", info.Fonts)
	}
	texts := pageTexts(t, pdf)
	if !strings.Contains(texts[0], turkish) {
		t.Errorf("Turkish heading missing on page 1: %q", texts[0][:min(200, len(texts[0]))])
	}
	all := strings.Join(texts, "\n")
	if !strings.Contains(all, "Hak ediş ödemesi şantiye 120 ₺120.250,00") {
		t.Error("last row missing")
	}
	for i, tx := range texts {
		if !strings.Contains(tx, fmt.Sprintf("Sayfa %d / %d", i+1, len(texts))) {
			t.Errorf("page %d: page number footer missing", i+1)
		}
		if !strings.Contains(tx, "Sıra Açıklama Borç") {
			t.Errorf("page %d: table header not repeated", i+1)
		}
	}
}

func TestHTMLOptionsLandscapeMarginsHeader(t *testing.T) {
	needBrowser(t)
	out := run(t, "/v1/html", []ops.File{{Name: "a.html", Data: []byte(`<html><body><p>Kira makbuzu</p></body></html>`)}}, func(p any) {
		pp := p.(*ops.PageParams)
		pp.Paper, pp.Landscape, pp.Margin = "A5", true, "20mm"
		pp.Header = `<div style="font-size:9px;width:100%;text-align:right">Örnek Yapı · <span class="title"></span></div>`
	})
	pdf := out.Files[0].Data
	info, _ := pdfops.GetInfo(context.Background(), pdf, "")
	if p := info.Pages[0]; p.Width < p.Height || p.Width < 594 || p.Width > 597 {
		t.Fatalf("A5 landscape expected, got %vx%v", p.Width, p.Height)
	}
	if tx := pageTexts(t, pdf)[0]; !strings.Contains(tx, "Örnek Yapı") || !strings.Contains(tx, "Kira makbuzu") {
		t.Fatalf("text = %q", tx)
	}
}

func TestHTMLExternalResourcesBlockedAndTimeout(t *testing.T) {
	needBrowser(t)
	html := []byte(`<html><body><img src="https://example.com/x.png"><p>ok</p></body></html>`)
	_, err := runErr("/v1/html", []ops.File{{Name: "index.html", Data: html}}, func(p any) { p.(*ops.PageParams).FailOnResourceErr = true })
	if !errors.Is(err, chromium.ErrResource) || !ops.IsClientError(err) {
		t.Fatalf("want blocked resource error, got %v", err)
	}
	// Without failOnResourceError the page still prints.
	run(t, "/v1/html", []ops.File{{Name: "index.html", Data: html}}, nil)

	// A server that forbids external resources rejects the parameter.
	strict := *env
	strict.AllowExternal = false
	op := ops.ByPath("/v1/html")
	p := op.Params().(*ops.PageParams)
	p.AllowExternal = true
	if _, err := op.Run(context.Background(), &strict, []ops.File{{Name: "index.html", Data: html}}, p); !ops.IsClientError(err) {
		t.Fatalf("want rejection, got %v", err)
	}

	start := time.Now()
	_, err = runErr("/v1/html", []ops.File{{Name: "index.html", Data: []byte(`<p>never ready</p>`)}}, func(p any) {
		pp := p.(*ops.PageParams)
		pp.WaitForExpression = "window.ready === true"
		pp.Timeout = "2s"
	})
	if !errors.Is(err, context.DeadlineExceeded) || time.Since(start) > 10*time.Second {
		t.Fatalf("want timeout, got %v after %s", err, time.Since(start))
	}
	// The expression wait works when the page sets the flag.
	run(t, "/v1/html", []ops.File{{Name: "index.html", Data: []byte(`<p id="x"></p><script>setTimeout(()=>{document.getElementById('x').textContent='hazır';window.ready=true},200)</script>`)}},
		func(p any) { p.(*ops.PageParams).WaitForExpression = "window.ready === true" })
}

func TestTemplateToPDFAndPreview(t *testing.T) {
	needBrowser(t)
	tpl := `<html><head><title>{{sozlesme.no}}</title></head><body style="font-family:Onest">
<h1>{{cari.unvan}}</h1><p>Tarih: {{tarih sozlesme.tarih "d MMMM yyyy"}}</p>
<table>{{#each kalemler}}<tr><td>{{@number}}</td><td>{{ad}}</td><td>{{para tutar}}</td></tr>{{/each}}</table>
<p>Toplam: {{para (toplam kalemler "tutar")}} ({{yaziyla (toplam kalemler "tutar")}})</p>
{{#if onayli}}<p>ONAYLI</p>{{else}}<p>TASLAK</p>{{/if}}</body></html>`
	data := `{"cari":{"unvan":"Örnek Yapı A.Ş."},"sozlesme":{"no":"SZ-2026-014","tarih":"2026-08-31"},"onayli":true,
"kalemler":[{"ad":"Kalıp işçiliği","tutar":1234.56},{"ad":"Çelik donatı","tutar":"10.000,00"}]}`
	files := []ops.File{{Name: "template.html", Data: []byte(tpl)}, {Name: "data.json", Data: []byte(data)}}
	out := run(t, "/v1/template", files, func(p any) {
		p.(*ops.TemplateParams).Footer = `<div style="font-size:8px;width:100%;text-align:center">{{sozlesme.no}} · <span class="pageNumber"></span></div>`
	})
	tx := pageTexts(t, out.Files[0].Data)[0]
	for _, want := range []string{"Örnek Yapı A.Ş.", "31 Ağustos 2026", "1 Kalıp işçiliği ₺1.234,56", "2 Çelik donatı ₺10.000,00",
		"₺11.234,56", "on bir bin iki yüz otuz dört Türk lirası elli altı kuruş", "ONAYLI", "SZ-2026-014 · 1"} {
		if !strings.Contains(tx, want) {
			t.Errorf("missing %q in %q", want, tx)
		}
	}
	prev := run(t, "/v1/template/preview", files, nil)
	if h := string(prev.Files[0].Data); !strings.Contains(h, "<h1>Örnek Yapı A.Ş.</h1>") || prev.Files[0].ContentType[:9] != "text/html" {
		t.Fatalf("preview = %q", h[:min(300, len(h))])
	}
	if _, err := runErr("/v1/template", []ops.File{{Name: "t.html", Data: []byte("{{#each x}}")}}, nil); !ops.IsClientError(err) {
		t.Fatalf("want template syntax error, got %v", err)
	}
}

func TestPDFAAndRenderFromHTML(t *testing.T) {
	needBrowser(t)
	out := run(t, "/v1/html", []ops.File{{Name: "index.html", Data: []byte(reportHTML(40))}, {Name: "img/logo.png", Data: testutil.PNG(t, testutil.Gradient(32, 32))}},
		func(p any) {
			pp := p.(*ops.PageParams)
			pp.PDFA, pp.Title, pp.PageNumbers = true, "Cari Ekstre – PDF/A", true
		})
	pdf := out.Files[0].Data
	info, err := pdfops.GetInfo(context.Background(), pdf, "")
	if err != nil {
		t.Fatal(err)
	}
	if info.PDFA != "2B" || info.Metadata.Title != "Cari Ekstre – PDF/A" {
		t.Fatalf("pdfa=%q title=%q", info.PDFA, info.Metadata.Title)
	}
	verapdf(t, pdf)

	img := run(t, "/v1/render", []ops.File{{Name: "e.pdf", Data: pdf}}, func(p any) {
		rp := p.(*ops.RenderParams)
		rp.Pages, rp.Format, rp.MaxWidth = "1", "webp", 200
	})
	if len(img.Files) != 1 || img.Files[0].ContentType != "image/webp" || img.Files[0].Name != "e-001.webp" {
		t.Fatalf("render: %+v", img.Files)
	}
	all := run(t, "/v1/render", []ops.File{{Name: "e.pdf", Data: pdf}}, func(p any) { p.(*ops.RenderParams).DPI = 30 })
	if len(all.Files) != info.PageCount {
		t.Fatalf("render all: %d images for %d pages", len(all.Files), info.PageCount)
	}
}

// verapdf validates with the veraPDF CLI image when PIDIEF_VERAPDF=1 and Docker are available.
func verapdf(t *testing.T, pdf []byte) {
	if os.Getenv("PIDIEF_VERAPDF") != "1" {
		return
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.pdf"), pdf, 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("docker", "run", "--rm", "-v", dir+":/data", "verapdf/cli:latest", "--flavour", "2b", "--format", "text", "/data/a.pdf")
	cmd.Env = append(os.Environ(), "MSYS_NO_PATHCONV=1")
	b, err := cmd.CombinedOutput()
	if err != nil || !strings.Contains(string(b), "PASS") {
		t.Fatalf("veraPDF: %v\n%s", err, b)
	}
	t.Log("veraPDF: PASS 2b")
}

func TestImagesWatermarkCompressViaRegistry(t *testing.T) {
	img := testutil.JPEG(t, testutil.Gradient(1600, 1200), 6)
	out := run(t, "/v1/images", []ops.File{{Name: "tarama.jpg", Data: img}, {Name: "b.png", Data: testutil.PNG(t, testutil.Gradient(200, 300))}}, func(p any) {
		p.(*ops.ImagesParams).Margin = "5mm"
	})
	pdf := out.Files[0].Data
	if out.Files[0].Name != "tarama.pdf" {
		t.Errorf("name = %s", out.Files[0].Name)
	}
	wm := run(t, "/v1/watermark", []ops.File{{Name: "tarama.pdf", Data: pdf}}, func(p any) {
		sp := p.(*ops.StampParams)
		sp.Text, sp.Rotation = "TASLAK – İÇ KULLANIM", 0.001 // ~horizontal so text extraction keeps one line
	})
	if tx := strings.Join(pageTexts(t, wm.Files[0].Data), "\n"); !strings.Contains(tx, "TASLAK – İÇ KULLANIM") {
		t.Errorf("watermark text missing: %q", tx)
	}
	st := run(t, "/v1/stamp", []ops.File{{Name: "tarama.pdf", Data: pdf}}, func(p any) {
		sp := p.(*ops.StampParams)
		sp.Text, sp.Position, sp.FontSize = "Sayfa %p / %P", "bc", 10
	})
	if tx := pageTexts(t, st.Files[0].Data); !strings.Contains(tx[1], "Sayfa 2 / 2") {
		t.Errorf("stamp text missing: %q", tx[1])
	}
	c := run(t, "/v1/compress", []ops.File{{Name: "tarama.pdf", Data: pdf}}, func(p any) { p.(*ops.CompressParams).MaxImagePixels = 800 })
	if len(c.Files[0].Data) >= len(pdf) {
		t.Errorf("compress did not shrink: %d -> %d", len(pdf), len(c.Files[0].Data))
	}
	split := run(t, "/v1/split", []ops.File{{Name: "tarama.pdf", Data: pdf}}, nil)
	if len(split.Files) != 2 {
		t.Errorf("split parts = %d", len(split.Files))
	}
	if _, err := runErr("/v1/merge", []ops.File{{Name: "a.pdf", Data: pdf}, {Name: "b.txt", Data: []byte("x")}}, nil); !ops.IsClientError(err) {
		t.Errorf("non-PDF input should be rejected, got %v", err)
	}
}

func TestStampOnChromiumPDFWithOnest(t *testing.T) {
	needBrowser(t)
	out := run(t, "/v1/html", []ops.File{{Name: "index.html", Data: []byte(`<p style="font-family:Onest">Sözleşme metni</p>`)}}, nil)
	st := run(t, "/v1/stamp", []ops.File{{Name: "s.pdf", Data: out.Files[0].Data}}, func(p any) {
		sp := p.(*ops.StampParams)
		sp.Text, sp.Position, sp.FontSize = "ONAYLI – İmza", "br", 14
	})
	if tx := pageTexts(t, st.Files[0].Data)[0]; !strings.Contains(tx, "ONAYLI – İmza") || !strings.Contains(tx, "Sözleşme metni") {
		t.Fatalf("text = %q", tx)
	}
}
