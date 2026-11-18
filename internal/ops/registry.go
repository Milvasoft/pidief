package ops

import (
	"context"
	"fmt"
	"html"
	"path"
	"strings"
	"time"

	"github.com/milvasoft/pidief/internal/chromium"
	"github.com/milvasoft/pidief/internal/imgpdf"
	"github.com/milvasoft/pidief/internal/pdfops"
	"github.com/milvasoft/pidief/internal/raster"
	"github.com/milvasoft/pidief/internal/tmpl"
	"github.com/milvasoft/pidief/internal/units"
)

// ---------- parameter structs ----------

// PageParams are the HTML/template rendering options.
type PageParams struct {
	Paper             string  `json:"paper" default:"A4" help:"paper size: A0-A6, B4, B5, Letter, Legal, Tabloid or WxH with unit (210x297mm)"`
	Landscape         bool    `json:"landscape" help:"landscape orientation"`
	Margin            string  `json:"margin" default:"10mm" help:"all margins (mm, cm, in, pt, px); a bare number is mm"`
	MarginTop         string  `json:"marginTop" help:"top margin, overrides margin"`
	MarginRight       string  `json:"marginRight" help:"right margin, overrides margin"`
	MarginBottom      string  `json:"marginBottom" help:"bottom margin, overrides margin"`
	MarginLeft        string  `json:"marginLeft" help:"left margin, overrides margin"`
	Header            string  `json:"header" help:"header HTML template; classes pageNumber, totalPages, title, date are filled in (or file header.html)"`
	Footer            string  `json:"footer" help:"footer HTML template (or file footer.html)"`
	PageNumbers       bool    `json:"pageNumbers" help:"add a centred 'Sayfa X / Y' footer when no footer is given"`
	PageNumberText    string  `json:"pageNumberText" default:"Sayfa {page} / {pages}" help:"text of the page number footer; {page} and {pages} are replaced"`
	PrintBackground   bool    `json:"printBackground" default:"true" help:"print background colours and images"`
	PreferCSSPageSize bool    `json:"preferCssPageSize" help:"use the size from CSS @page instead of paper"`
	Scale             float64 `json:"scale" default:"1" help:"rendering scale (0.1-2)"`
	PageRanges        string  `json:"pageRanges" help:"pages to print, e.g. 1-3,5"`
	Media             string  `json:"media" default:"print" enum:"print,screen" help:"CSS media type"`
	WaitForSelector   string  `json:"waitForSelector" help:"wait until this CSS selector exists"`
	WaitForExpression string  `json:"waitForExpression" help:"wait until this JavaScript expression is truthy, e.g. window.ready === true"`
	WaitDelay         string  `json:"waitDelay" default:"0s" help:"extra wait before printing, e.g. 500ms"`
	WaitNetworkIdle   bool    `json:"waitNetworkIdle" help:"wait until the network is idle"`
	AllowExternal     bool    `json:"allowExternal" help:"allow loading http(s) resources from the internet (off by default; the server may forbid it)"`
	FailOnResourceErr bool    `json:"failOnResourceError" help:"fail when a referenced resource is missing or blocked"`
	Onest             bool    `json:"onest" default:"true" help:"make the embedded Onest font available as font-family Onest"`
	Tagged            bool    `json:"tagged" help:"generate a tagged (accessible) PDF"`
	Outline           bool    `json:"outline" help:"generate bookmarks from headings"`
	Timeout           string  `json:"timeout" default:"30s" help:"conversion timeout"`
	Title             string  `json:"title" help:"document title (metadata); default: the HTML <title>"`
	Author            string  `json:"author" help:"document author (metadata)"`
	Subject           string  `json:"subject" help:"document subject (metadata)"`
	Keywords          string  `json:"keywords" help:"document keywords (metadata)"`
	PDFA              bool    `json:"pdfa" help:"convert the result to PDF/A-2b"`
}

// TemplateParams add the template engine options.
type TemplateParams struct {
	PageParams
	Data     string `json:"data" help:"JSON data for the template (or file data.json)"`
	Strict   bool   `json:"strict" help:"fail when the template uses a value the data does not have"`
	Empty    string `json:"empty" help:"text printed for an empty value, e.g. -"`
	Timezone string `json:"timezone" default:"Europe/Istanbul" help:"time zone for dates"`
}

// TemplatePreviewParams render the template to HTML only.
type TemplatePreviewParams struct {
	Data     string `json:"data" help:"JSON data for the template (or file data.json)"`
	Strict   bool   `json:"strict" help:"fail when the template uses a value the data does not have"`
	Empty    string `json:"empty" help:"text printed for an empty value, e.g. -"`
	Timezone string `json:"timezone" default:"Europe/Istanbul" help:"time zone for dates"`
}

// ImagesParams convert images to PDF.
type ImagesParams struct {
	Paper     string `json:"paper" default:"A4" help:"page size, or fit to use the image size"`
	Landscape string `json:"orientation" default:"auto" enum:"auto,portrait,landscape" help:"page orientation; auto follows each image"`
	Margin    string `json:"margin" default:"0mm" help:"page margin"`
	MaxDPI    int    `json:"maxDpi" default:"200" help:"downsample images above this resolution (0 keeps the original)"`
	Quality   int    `json:"quality" default:"80" help:"JPEG quality for re-encoded images"`
	Grayscale bool   `json:"grayscale" help:"convert to grayscale (smaller scans)"`
	Title     string `json:"title" help:"document title"`
}

// PasswordParams is embedded in operations on existing PDFs.
type PasswordParams struct {
	Password string `json:"password" help:"password of an encrypted input"`
}

// MergeParams merge PDFs.
type MergeParams struct{ PasswordParams }

// SplitParams split a PDF.
type SplitParams struct {
	PasswordParams
	Span   int      `json:"span" help:"split every N pages (default 1 when nothing else is given)"`
	At     []int    `json:"at" help:"start a new part at these pages, e.g. 3,7"`
	Ranges []string `json:"ranges" help:"one part per range, e.g. 1-3 (repeat for more)"`
}

// PagesParams select or delete pages.
type PagesParams struct {
	PasswordParams
	Pages string `json:"pages" help:"page selection: 1-3,5 even odd !2 l (last) l-1"`
}

// RotateParams rotate pages.
type RotateParams struct {
	PasswordParams
	Pages   string `json:"pages" help:"page selection (default all)"`
	Degrees int    `json:"degrees" default:"90" help:"clockwise rotation: 90, 180, 270, -90"`
}

// StampParams add a text or image watermark/stamp.
type StampParams struct {
	PasswordParams
	Text     string  `json:"text" help:"text; %p is the page number and %P the page count (or upload an image)"`
	Pages    string  `json:"pages" help:"page selection (default all)"`
	FontSize int     `json:"fontSize" default:"48" help:"font size in points"`
	Scale    float64 `json:"scale" help:"size relative to the page width (0-1); 0 uses fontSize / image size"`
	Opacity  float64 `json:"opacity" help:"opacity (0-1]; default 0.3 for watermark, 1 for stamp"`
	Rotation float64 `json:"rotation" help:"rotation in degrees; default 45 for watermark, 0 for stamp"`
	Position string  `json:"position" default:"c" enum:"c,tl,tc,tr,l,r,bl,bc,br" help:"anchor"`
	OffsetX  float64 `json:"offsetX" help:"horizontal offset in points"`
	OffsetY  float64 `json:"offsetY" help:"vertical offset in points"`
	Color    string  `json:"color" default:"#808080" help:"text colour #rrggbb"`
	Behind   bool    `json:"behind" help:"put it behind the page content (hidden where the page paints backgrounds, as Chromium output does)"`
}

// CompressParams optimise a PDF.
type CompressParams struct {
	PasswordParams
	ImageQuality   int  `json:"imageQuality" default:"70" help:"JPEG quality for recompressed images (0 keeps images untouched)"`
	MaxImagePixels int  `json:"maxImagePixels" default:"2000" help:"downscale images whose longest side is larger"`
	Grayscale      bool `json:"grayscale" help:"convert recompressed images to grayscale"`
}

// PDFAParams convert to PDF/A-2b.
type PDFAParams struct {
	PasswordParams
	Title    string `json:"title" help:"document title"`
	Author   string `json:"author" help:"document author"`
	Subject  string `json:"subject" help:"document subject"`
	Keywords string `json:"keywords" help:"document keywords"`
}

// EncryptParams protect a PDF.
type EncryptParams struct {
	UserPassword  string `json:"userPassword" help:"password to open the document"`
	OwnerPassword string `json:"ownerPassword" help:"password to change permissions (default: userPassword)"`
	AllowPrint    bool   `json:"allowPrint" default:"true" help:"allow printing"`
	AllowCopy     bool   `json:"allowCopy" help:"allow copying text and images"`
	AllowModify   bool   `json:"allowModify" help:"allow changing the document"`
	AllowAnnotate bool   `json:"allowAnnotate" help:"allow comments and form filling"`
}

// MetadataParams set document information.
type MetadataParams struct {
	PasswordParams
	Title      string            `json:"title" help:"title"`
	Author     string            `json:"author" help:"author"`
	Subject    string            `json:"subject" help:"subject"`
	Keywords   string            `json:"keywords" help:"keywords"`
	Creator    string            `json:"creator" help:"creating application"`
	Properties map[string]string `json:"properties" help:"custom properties key=value (repeatable)"`
}

// RenderParams render pages to images.
type RenderParams struct {
	PasswordParams
	Pages     string `json:"pages" help:"page selection (default all)"`
	DPI       int    `json:"dpi" default:"96" help:"resolution (max 600)"`
	Format    string `json:"format" default:"png" enum:"png,jpeg,webp" help:"image format"`
	Quality   int    `json:"quality" default:"85" help:"jpeg/webp quality"`
	MaxWidth  int    `json:"maxWidth" help:"fit into this width in pixels (thumbnails)"`
	MaxHeight int    `json:"maxHeight" help:"fit into this height in pixels"`
}

// InfoParams read document information.
type InfoParams struct{ PasswordParams }

// ---------- registry ----------

var registry = []*Op{
	{
		Name: "html", Path: "/v1/html", Summary: "Convert HTML (with CSS, fonts, images) to PDF with headless Chromium",
		Inputs:     Inputs{Min: 1, Kind: "html", Description: "index.html (or the first .html file) plus any assets it references by relative path"},
		TextInputs: []TextInput{{Field: "html", File: "index.html", Help: "HTML document (instead of an index.html file)"}},
		Output:     "pdf", Browser: true,
		NewParams: func() any { return &PageParams{} },
		Run:       runHTML,
	},
	{
		Name: "template", Path: "/v1/template", Summary: "Fill an HTML template (Handlebars/Mustache syntax, tr-TR helpers) with JSON data and convert to PDF",
		Inputs:     Inputs{Min: 1, Kind: "html", Description: "template.html (or the first .html file), optional data.json, assets"},
		TextInputs: []TextInput{{Field: "template", File: "template.html", Help: "template HTML (instead of a template.html file)"}},
		Output:     "pdf", Browser: true,
		NewParams: func() any { return &TemplateParams{} },
		Run:       runTemplate,
	},
	{
		Name: "template preview", Path: "/v1/template/preview", Summary: "Fill an HTML template with JSON data and return the HTML (preview)",
		Inputs:     Inputs{Min: 1, Kind: "html", Description: "template.html (or the first .html file), optional data.json"},
		TextInputs: []TextInput{{Field: "template", File: "template.html", Help: "template HTML"}},
		Output:     "html",
		NewParams:  func() any { return &TemplatePreviewParams{} },
		Run:        runTemplatePreview,
	},
	{
		Name: "images", Path: "/v1/images", Summary: "Convert photos and scans (JPEG, PNG, WebP, GIF, TIFF, BMP) to one PDF, one image per page",
		Inputs: Inputs{Min: 1, Kind: "image", Description: "images, in page order"},
		Output: "pdf", NewParams: func() any { return &ImagesParams{} }, Run: runImages,
	},
	{
		Name: "merge", Path: "/v1/merge", Summary: "Merge PDFs in the given order",
		Inputs: Inputs{Min: 2, Kind: "pdf", Description: "two or more PDFs"},
		Output: "pdf", NewParams: func() any { return &MergeParams{} },
		Run: func(ctx context.Context, env *Env, in []File, p any) (*Output, error) {
			docs := make([][]byte, len(in))
			for i, f := range in {
				docs[i] = f.Data
			}
			out, err := pdfops.Merge(ctx, docs, p.(*MergeParams).Password)
			if err != nil {
				return nil, err
			}
			return pdfOut("merged.pdf", out), nil
		},
	},
	{
		Name: "split", Path: "/v1/split", Summary: "Split a PDF into parts (every N pages, at pages, or by ranges); returns a ZIP",
		Inputs: Inputs{Min: 1, Max: 1, Kind: "pdf", Description: "one PDF"},
		Output: "pdfs", NewParams: func() any { return &SplitParams{} },
		Run: func(ctx context.Context, env *Env, in []File, p any) (*Output, error) {
			sp := p.(*SplitParams)
			parts, err := pdfops.Split(ctx, in[0].Data, sp.Span, sp.At, sp.Ranges, sp.Password)
			if err != nil {
				return nil, err
			}
			out := &Output{}
			for _, part := range parts {
				out.Files = append(out.Files, File{Name: part.Name, ContentType: "application/pdf", Data: part.Data})
			}
			return out, nil
		},
	},
	{
		Name: "pages select", Path: "/v1/pages/select", Summary: "Keep the selected pages, in the given order (also reorders and duplicates)",
		Inputs: Inputs{Min: 1, Max: 1, Kind: "pdf", Description: "one PDF"},
		Output: "pdf", NewParams: func() any { return &PagesParams{} },
		Run: func(ctx context.Context, env *Env, in []File, p any) (*Output, error) {
			pp := p.(*PagesParams)
			out, err := pdfops.Collect(ctx, in[0].Data, pp.Pages, pp.Password)
			if err != nil {
				return nil, err
			}
			return pdfOut(OutputName(in, ".pdf"), out), nil
		},
	},
	{
		Name: "pages delete", Path: "/v1/pages/delete", Summary: "Delete the selected pages",
		Inputs: Inputs{Min: 1, Max: 1, Kind: "pdf", Description: "one PDF"},
		Output: "pdf", NewParams: func() any { return &PagesParams{} },
		Run: func(ctx context.Context, env *Env, in []File, p any) (*Output, error) {
			pp := p.(*PagesParams)
			out, err := pdfops.Remove(ctx, in[0].Data, pp.Pages, pp.Password)
			if err != nil {
				return nil, err
			}
			return pdfOut(OutputName(in, ".pdf"), out), nil
		},
	},
	{
		Name: "pages rotate", Path: "/v1/pages/rotate", Summary: "Rotate pages by a multiple of 90 degrees",
		Inputs: Inputs{Min: 1, Max: 1, Kind: "pdf", Description: "one PDF"},
		Output: "pdf", NewParams: func() any { return &RotateParams{} },
		Run: func(ctx context.Context, env *Env, in []File, p any) (*Output, error) {
			rp := p.(*RotateParams)
			out, err := pdfops.Rotate(ctx, in[0].Data, rp.Degrees, rp.Pages, rp.Password)
			if err != nil {
				return nil, err
			}
			return pdfOut(OutputName(in, ".pdf"), out), nil
		},
	},
	{
		Name: "watermark", Path: "/v1/watermark", Summary: "Add a semi-transparent diagonal text or image watermark (e.g. TASLAK) to every page",
		Inputs: Inputs{Min: 1, Max: 2, Kind: "any", Description: "one PDF and optionally one image (PNG/JPEG/WebP) as the watermark"},
		Output: "pdf", NewParams: func() any { return &StampParams{} },
		Run: func(ctx context.Context, env *Env, in []File, p any) (*Output, error) {
			return runStamp(ctx, in, p.(*StampParams), true)
		},
	},
	{
		Name: "stamp", Path: "/v1/stamp", Summary: "Add a text or image stamp on top of the page content (e.g. 'ONAYLI', a signature, page numbers)",
		Inputs: Inputs{Min: 1, Max: 2, Kind: "any", Description: "one PDF and optionally one image (PNG/JPEG/WebP) as the stamp"},
		Output: "pdf", NewParams: func() any { return &StampParams{} },
		Run: func(ctx context.Context, env *Env, in []File, p any) (*Output, error) {
			return runStamp(ctx, in, p.(*StampParams), false)
		},
	},
	{
		Name: "compress", Path: "/v1/compress", Summary: "Shrink a PDF: recompress and downscale images, remove duplicate and unused objects",
		Inputs: Inputs{Min: 1, Max: 1, Kind: "pdf", Description: "one PDF"},
		Output: "pdf", NewParams: func() any { return &CompressParams{} },
		Run: func(ctx context.Context, env *Env, in []File, p any) (*Output, error) {
			cp := p.(*CompressParams)
			out, _, err := pdfops.Compress(ctx, in[0].Data, pdfops.CompressOptions{
				ImageQuality: cp.ImageQuality, MaxImagePixels: cp.MaxImagePixels, Grayscale: cp.Grayscale, Password: cp.Password,
			})
			if err != nil {
				return nil, err
			}
			return pdfOut(OutputName(in, ".pdf"), out), nil
		},
	},
	{
		Name: "pdfa", Path: "/v1/pdfa", Summary: "Convert to PDF/A-2b (XMP metadata, sRGB output intent, printable annotations); fonts must be embedded",
		Inputs: Inputs{Min: 1, Max: 1, Kind: "pdf", Description: "one PDF"},
		Output: "pdf", NewParams: func() any { return &PDFAParams{} },
		Run: func(ctx context.Context, env *Env, in []File, p any) (*Output, error) {
			ap := p.(*PDFAParams)
			data := in[0].Data
			if ap.Password != "" {
				var err error
				if data, err = pdfops.Decrypt(ctx, data, ap.Password); err != nil {
					return nil, err
				}
			}
			res, err := pdfops.ToPDFA2B(ctx, data, pdfops.Metadata{Title: ap.Title, Author: ap.Author, Subject: ap.Subject, Keywords: ap.Keywords}, env.now())
			if err != nil {
				return nil, err
			}
			return pdfOut(OutputName(in, ".pdf"), res.Data), nil
		},
	},
	{
		Name: "encrypt", Path: "/v1/encrypt", Summary: "Protect a PDF with a password (AES-256) and permissions",
		Inputs: Inputs{Min: 1, Max: 1, Kind: "pdf", Description: "one PDF"},
		Output: "pdf", NewParams: func() any { return &EncryptParams{} },
		Run: func(ctx context.Context, env *Env, in []File, p any) (*Output, error) {
			ep := p.(*EncryptParams)
			out, err := pdfops.Encrypt(ctx, in[0].Data, pdfops.EncryptOptions{
				UserPassword: ep.UserPassword, OwnerPassword: ep.OwnerPassword,
				AllowPrint: ep.AllowPrint, AllowCopy: ep.AllowCopy, AllowModify: ep.AllowModify, AllowAnnotate: ep.AllowAnnotate,
			})
			if err != nil {
				return nil, err
			}
			return pdfOut(OutputName(in, ".pdf"), out), nil
		},
	},
	{
		Name: "decrypt", Path: "/v1/decrypt", Summary: "Remove the password of a PDF",
		Inputs: Inputs{Min: 1, Max: 1, Kind: "pdf", Description: "one PDF"},
		Output: "pdf", NewParams: func() any { return &PasswordParams{} },
		Run: func(ctx context.Context, env *Env, in []File, p any) (*Output, error) {
			pw := p.(*PasswordParams).Password
			if pw == "" {
				return nil, invalid("password is required")
			}
			out, err := pdfops.Decrypt(ctx, in[0].Data, pw)
			if err != nil {
				return nil, err
			}
			return pdfOut(OutputName(in, ".pdf"), out), nil
		},
	},
	{
		Name: "metadata", Path: "/v1/metadata", Summary: "Set document information (title, author, subject, keywords, creator, custom properties)",
		Inputs: Inputs{Min: 1, Max: 1, Kind: "pdf", Description: "one PDF"},
		Output: "pdf", NewParams: func() any { return &MetadataParams{} },
		Run: func(ctx context.Context, env *Env, in []File, p any) (*Output, error) {
			mp := p.(*MetadataParams)
			c, err := pdfops.ReadContext(ctx, in[0].Data, mp.Password)
			if err != nil {
				return nil, err
			}
			if err := pdfops.SetMetadata(c, pdfops.Metadata{Title: mp.Title, Author: mp.Author, Subject: mp.Subject, Keywords: mp.Keywords, Creator: mp.Creator}, mp.Properties, env.now()); err != nil {
				return nil, err
			}
			out, err := pdfops.WriteContext(ctx, c)
			if err != nil {
				return nil, err
			}
			return pdfOut(OutputName(in, ".pdf"), out), nil
		},
	},
	{
		Name: "info", Path: "/v1/info", Summary: "Page count, page sizes, metadata, fonts (embedded or not), encryption and PDF/A claim as JSON",
		Inputs: Inputs{Min: 1, Max: 1, Kind: "pdf", Description: "one PDF"},
		Output: "json", NewParams: func() any { return &InfoParams{} },
		Run: func(ctx context.Context, env *Env, in []File, p any) (*Output, error) {
			info, err := pdfops.GetInfo(ctx, in[0].Data, p.(*InfoParams).Password)
			if err != nil {
				return nil, err
			}
			return &Output{JSON: info}, nil
		},
	},
	{
		Name: "text", Path: "/v1/text", Summary: "Extract the text of every page as JSON",
		Inputs: Inputs{Min: 1, Max: 1, Kind: "pdf", Description: "one PDF"},
		Output: "json", NewParams: func() any { return &InfoParams{} },
		Run: func(ctx context.Context, env *Env, in []File, p any) (*Output, error) {
			pages, err := env.Raster.Text(ctx, in[0].Data, p.(*InfoParams).Password)
			if err != nil {
				return nil, err
			}
			type page struct {
				Number int    `json:"number"`
				Text   string `json:"text"`
			}
			res := struct {
				Pages []page `json:"pages"`
			}{}
			for i, t := range pages {
				res.Pages = append(res.Pages, page{i + 1, strings.ReplaceAll(t, "\r\n", "\n")})
			}
			return &Output{JSON: res}, nil
		},
	},
	{
		Name: "render", Path: "/v1/render", Summary: "Render pages to images (thumbnails and previews): PNG, JPEG or WebP; one page returns the image, more return a ZIP",
		Inputs: Inputs{Min: 1, Max: 1, Kind: "pdf", Description: "one PDF"},
		Output: "images", NewParams: func() any { return &RenderParams{} },
		Run: runRender,
	},
}

// ---------- implementations ----------

func pickMain(in []File, preferred string) (main File, assets []File, err error) {
	idx := -1
	for i, f := range in {
		if strings.EqualFold(path.Base(f.Name), preferred) {
			idx = i
			break
		}
	}
	if idx < 0 {
		for i, f := range in {
			ext := strings.ToLower(path.Ext(f.Name))
			if ext == ".html" || ext == ".htm" || ext == ".hbs" || ext == ".mustache" {
				idx = i
				break
			}
		}
	}
	if idx < 0 {
		if len(in) == 1 {
			idx = 0
		} else {
			return File{}, nil, invalid("no %s (or .html) file among the inputs", preferred)
		}
	}
	for i, f := range in {
		if i != idx {
			assets = append(assets, f)
		}
	}
	return in[idx], assets, nil
}

// takeFile removes a named file (header.html, data.json) from the assets.
func takeFile(assets []File, name string) ([]File, *File) {
	for i, f := range assets {
		if strings.EqualFold(path.Base(f.Name), name) {
			rest := append(append([]File{}, assets[:i]...), assets[i+1:]...)
			return rest, &f
		}
	}
	return assets, nil
}

func (e *Env) chromiumOptions(p *PageParams) (chromium.Options, time.Duration, error) {
	var o chromium.Options
	paper, err := units.Paper(p.Paper)
	if err != nil {
		return o, 0, invalid("%v", err)
	}
	o.PaperWidth, o.PaperHeight = units.Inches(paper.W), units.Inches(paper.H)
	m, err := units.Length(p.Margin)
	if err != nil {
		return o, 0, invalid("margin: %v", err)
	}
	side := func(s string, name string) (float64, error) {
		if strings.TrimSpace(s) == "" {
			return units.Inches(m), nil
		}
		v, err := units.Length(s)
		if err != nil {
			return 0, invalid("%s: %v", name, err)
		}
		return units.Inches(v), nil
	}
	if o.MarginTop, err = side(p.MarginTop, "marginTop"); err != nil {
		return o, 0, err
	}
	if o.MarginRight, err = side(p.MarginRight, "marginRight"); err != nil {
		return o, 0, err
	}
	if o.MarginBottom, err = side(p.MarginBottom, "marginBottom"); err != nil {
		return o, 0, err
	}
	if o.MarginLeft, err = side(p.MarginLeft, "marginLeft"); err != nil {
		return o, 0, err
	}
	if p.Scale < 0.1 || p.Scale > 2 {
		return o, 0, invalid("scale must be between 0.1 and 2")
	}
	o.Landscape, o.PrintBackground, o.PreferCSSPageSize, o.Scale = p.Landscape, p.PrintBackground, p.PreferCSSPageSize, p.Scale
	o.PageRanges = p.PageRanges
	o.HeaderTemplate, o.FooterTemplate = p.Header, p.Footer
	if o.FooterTemplate == "" && p.PageNumbers {
		o.FooterTemplate = PageNumberFooter(p.PageNumberText)
		if o.MarginBottom < 0.45 {
			o.MarginBottom = 0.45
		}
	}
	o.Tagged, o.Outline = p.Tagged, p.Outline
	o.WaitForSelector, o.WaitForExpression, o.WaitNetworkIdle = p.WaitForSelector, p.WaitForExpression, p.WaitNetworkIdle
	if o.WaitDelay, err = time.ParseDuration(p.WaitDelay); err != nil || o.WaitDelay < 0 {
		return o, 0, invalid("waitDelay: %q is not a duration like 500ms", p.WaitDelay)
	}
	if p.AllowExternal && !e.AllowExternal {
		return o, 0, invalid("allowExternal is disabled on this server (PIDIEF_ALLOW_EXTERNAL)")
	}
	o.AllowExternal, o.FailOnResourceError = p.AllowExternal, p.FailOnResourceErr
	o.MediaScreen = p.Media == "screen"
	o.InjectOnest = p.Onest
	timeout, err := time.ParseDuration(p.Timeout)
	if err != nil || timeout <= 0 {
		return o, 0, invalid("timeout: %q is not a duration like 30s", p.Timeout)
	}
	if e.MaxTimeout > 0 && timeout > e.MaxTimeout {
		timeout = e.MaxTimeout
	}
	if o.WaitDelay >= timeout {
		return o, 0, invalid("waitDelay must be shorter than timeout")
	}
	return o, timeout, nil
}

// PageNumberFooter is the default "Sayfa X / Y" footer.
func PageNumberFooter(text string) string {
	if text == "" {
		text = "Sayfa {page} / {pages}"
	}
	t := html.EscapeString(text)
	t = strings.ReplaceAll(t, "{page}", `<span class="pageNumber"></span>`)
	t = strings.ReplaceAll(t, "{pages}", `<span class="totalPages"></span>`)
	return `<div style="width:100%;font-family:Onest,'DejaVu Sans',Arial,sans-serif;font-size:8pt;color:#555;text-align:center;-webkit-print-color-adjust:exact">` + t + `</div>`
}

func (e *Env) printHTML(ctx context.Context, doc string, assets []File, p *PageParams, in []File) (*Output, error) {
	assets, hf := takeFile(assets, "header.html")
	if hf != nil && p.Header == "" {
		p.Header = string(hf.Data)
	}
	assets, ff := takeFile(assets, "footer.html")
	if ff != nil && p.Footer == "" {
		p.Footer = string(ff.Data)
	}
	o, timeout, err := e.chromiumOptions(p)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	ca := make([]chromium.Asset, len(assets))
	for i, a := range assets {
		ca[i] = chromium.Asset{Name: a.Name, Data: a.Data}
	}
	pdf, err := e.Browser.PrintPDF(ctx, doc, ca, o)
	if err != nil {
		return nil, err
	}
	return e.finishPDF(ctx, pdf, p, in)
}

func (e *Env) finishPDF(ctx context.Context, pdf []byte, p *PageParams, in []File) (*Output, error) {
	meta := pdfops.Metadata{Title: p.Title, Author: p.Author, Subject: p.Subject, Keywords: p.Keywords, Creator: "pidief (Chromium)"}
	if p.PDFA {
		res, err := pdfops.ToPDFA2B(ctx, pdf, meta, e.now())
		if err != nil {
			return nil, err
		}
		pdf = res.Data
	} else {
		c, err := pdfops.ReadContext(ctx, pdf, "")
		if err != nil {
			return nil, err
		}
		if err := pdfops.SetMetadata(c, meta, nil, e.now()); err != nil {
			return nil, err
		}
		if pdf, err = pdfops.WriteContext(ctx, c); err != nil {
			return nil, err
		}
	}
	return pdfOut(OutputName(in, ".pdf"), pdf), nil
}

func runHTML(ctx context.Context, env *Env, in []File, params any) (*Output, error) {
	p := params.(*PageParams)
	main, assets, err := pickMain(in, "index.html")
	if err != nil {
		return nil, err
	}
	return env.printHTML(ctx, string(main.Data), assets, p, []File{main})
}

func templateData(assets []File, data string) ([]File, any, error) {
	assets, df := takeFile(assets, "data.json")
	raw := []byte(data)
	if df != nil && strings.TrimSpace(data) == "" {
		raw = df.Data
	}
	d, err := tmpl.DecodeJSON(raw)
	if err != nil {
		return nil, nil, invalid("%v", err)
	}
	return assets, d, nil
}

func renderTemplate(src string, data any, strict bool, empty, tz string) (string, error) {
	t, err := tmpl.Parse(src)
	if err != nil {
		return "", invalid("%v", err)
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return "", invalid("timezone: %v", err)
	}
	out, err := t.Render(data, tmpl.Options{Strict: strict, Empty: empty, Location: loc})
	if err != nil {
		return "", invalid("%v", err)
	}
	return out, nil
}

func runTemplate(ctx context.Context, env *Env, in []File, params any) (*Output, error) {
	p := params.(*TemplateParams)
	main, assets, err := pickMain(in, "template.html")
	if err != nil {
		return nil, err
	}
	assets, data, err := templateData(assets, p.Data)
	if err != nil {
		return nil, err
	}
	doc, err := renderTemplate(string(main.Data), data, p.Strict, p.Empty, p.Timezone)
	if err != nil {
		return nil, err
	}
	assets, hf := takeFile(assets, "header.html")
	if hf != nil && p.Header == "" {
		p.Header = string(hf.Data)
	}
	assets, ff := takeFile(assets, "footer.html")
	if ff != nil && p.Footer == "" {
		p.Footer = string(ff.Data)
	}
	for _, s := range []*string{&p.Header, &p.Footer} {
		if *s != "" {
			if *s, err = renderTemplate(*s, data, p.Strict, p.Empty, p.Timezone); err != nil {
				return nil, err
			}
		}
	}
	return env.printHTML(ctx, doc, assets, &p.PageParams, []File{main})
}

func runTemplatePreview(ctx context.Context, env *Env, in []File, params any) (*Output, error) {
	p := params.(*TemplatePreviewParams)
	main, assets, err := pickMain(in, "template.html")
	if err != nil {
		return nil, err
	}
	_, data, err := templateData(assets, p.Data)
	if err != nil {
		return nil, err
	}
	doc, err := renderTemplate(string(main.Data), data, p.Strict, p.Empty, p.Timezone)
	if err != nil {
		return nil, err
	}
	return &Output{Files: []File{{Name: OutputName([]File{main}, ".html"), ContentType: "text/html; charset=utf-8", Data: []byte(doc)}}}, nil
}

func runImages(ctx context.Context, env *Env, in []File, params any) (*Output, error) {
	p := params.(*ImagesParams)
	margin, err := units.Length(p.Margin)
	if err != nil {
		return nil, invalid("margin: %v", err)
	}
	imgs := make([]imgpdf.Input, len(in))
	for i, f := range in {
		imgs[i] = imgpdf.Input{Name: f.Name, Data: f.Data}
	}
	out, err := imgpdf.Convert(ctx, imgs, imgpdf.Options{
		PageSize: p.Paper, Landscape: p.Landscape, Margin: margin, MaxDPI: p.MaxDPI,
		Quality: p.Quality, Grayscale: p.Grayscale, Title: p.Title,
	})
	if err != nil {
		return nil, err
	}
	return pdfOut(OutputName(in, ".pdf"), out), nil
}

// runStamp: a watermark and a stamp differ only in their defaults (opacity, rotation).
func runStamp(ctx context.Context, in []File, p *StampParams, watermark bool) (*Output, error) {
	var pdf, img *File
	for i := range in {
		if IsPDF(in[i].Data) {
			if pdf != nil {
				return nil, invalid("only one PDF is allowed")
			}
			pdf = &in[i]
		} else {
			img = &in[i]
		}
	}
	if pdf == nil {
		return nil, invalid("a PDF input is required")
	}
	o := pdfops.StampOptions{
		Text: p.Text, OnTop: !p.Behind, FontSize: p.FontSize, Scale: p.Scale, Opacity: p.Opacity,
		Rotation: p.Rotation, Position: p.Position, OffsetX: p.OffsetX, OffsetY: p.OffsetY,
		Color: p.Color, Pages: p.Pages, Password: p.Password,
	}
	if img != nil {
		if p.Text != "" {
			return nil, invalid("give either text or an image, not both")
		}
		o.Image = img.Data
	}
	if o.Opacity == 0 {
		o.Opacity = map[bool]float64{true: 0.25, false: 1}[watermark]
	}
	if o.Rotation == 0 && watermark && o.Text != "" {
		o.Rotation = 45
	}
	out, err := pdfops.Stamp(ctx, pdf.Data, o)
	if err != nil {
		return nil, err
	}
	return pdfOut(OutputName([]File{*pdf}, ".pdf"), out), nil
}

func runRender(ctx context.Context, env *Env, in []File, params any) (*Output, error) {
	p := params.(*RenderParams)
	var pages []int
	if strings.TrimSpace(p.Pages) != "" {
		n, err := env.Raster.PageCount(ctx, in[0].Data, p.Password)
		if err != nil {
			return nil, err
		}
		sel, err := pdfops.Selection(p.Pages)
		if err != nil {
			return nil, err
		}
		pages, err = pdfops.SelectedPages(n, sel)
		if err != nil {
			return nil, err
		}
	}
	imgs, err := env.Raster.Render(ctx, in[0].Data, raster.RenderOptions{
		Pages: pages, DPI: p.DPI, MaxWidth: p.MaxWidth, MaxHeight: p.MaxHeight,
		Format: p.Format, Quality: p.Quality, Password: p.Password,
	})
	if err != nil {
		if strings.Contains(err.Error(), "out of range") || strings.Contains(err.Error(), "dpi") || strings.Contains(err.Error(), "format") {
			return nil, invalid("%v", err)
		}
		return nil, err
	}
	base := strings.TrimSuffix(OutputName(in, ""), ".")
	ext := map[string]string{"png": ".png", "jpeg": ".jpg", "webp": ".webp"}[p.Format]
	out := &Output{}
	for _, im := range imgs {
		out.Files = append(out.Files, File{Name: fmt.Sprintf("%s-%03d%s", base, im.Page, ext), ContentType: im.ContentType, Data: im.Data})
	}
	return out, nil
}
