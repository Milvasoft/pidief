package pdfops

import (
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"

	"github.com/milvasoft/pidief/internal/assets"
)

// PDFAResult is the outcome of a PDF/A conversion.
type PDFAResult struct {
	Data     []byte
	Warnings []string
}

// ToPDFA2B post-processes a document (typically Chromium output, whose fonts are always
// embedded) into PDF/A-2b: XMP metadata matching the Info dictionary, an sRGB output intent,
// printable annotations, PDF 1.7 header, no encryption, no JavaScript and no embedded files.
//
// It does not convert colours or fonts: a document with non-embedded fonts is reported as an
// error, because PDF/A forbids them and fixing it needs the original font files.
func ToPDFA2B(ctx context.Context, pdf []byte, m Metadata, now time.Time) (*PDFAResult, error) {
	c, err := ReadContext(ctx, pdf, "")
	if err != nil {
		return nil, err
	}
	if c.Encrypt != nil {
		return nil, fmt.Errorf("%w: PDF/A does not allow encryption, decrypt first", ErrBadInput)
	}
	res := &PDFAResult{}
	for _, f := range Fonts(c) {
		if !f.Embedded {
			return nil, fmt.Errorf("%w: font %q is not embedded; PDF/A needs embedded fonts", ErrBadInput, f.Name)
		}
	}
	root, err := c.Catalog()
	if err != nil {
		return nil, err
	}

	// Forbidden: JavaScript, embedded files (2b allows only PDF/A attachments), encryption.
	if no, ok := root.Find("Names"); ok {
		if names, err := c.DereferenceDict(no); err == nil && names != nil {
			for _, k := range []string{"JavaScript", "EmbeddedFiles"} {
				if _, ok := names.Find(k); ok {
					names.Delete(k)
					res.Warnings = append(res.Warnings, "removed /Names/"+k)
				}
			}
		}
	}
	if oa, ok := root.Find("OpenAction"); ok {
		if d, err := c.DereferenceDict(oa); err == nil && d != nil {
			if s := d.NameEntry("S"); s != nil && *s == "JavaScript" {
				root.Delete("OpenAction")
				res.Warnings = append(res.Warnings, "removed JavaScript OpenAction")
			}
		}
	}
	root.Delete("AA")

	// Annotations must be printable and not hidden.
	for p := 1; p <= c.PageCount; p++ {
		pd, _, _, err := c.PageDict(ctx, p, false)
		if err != nil || pd == nil {
			continue
		}
		ao, ok := pd.Find("Annots")
		if !ok {
			continue
		}
		arr, err := c.DereferenceArray(ao)
		if err != nil {
			continue
		}
		for _, a := range arr {
			ad, err := c.DereferenceDict(a)
			if err != nil || ad == nil {
				continue
			}
			flags := 0
			if f := ad.IntEntry("F"); f != nil {
				flags = *f
			}
			flags = (flags | 4) &^ (1 | 2 | 32) // Print on; Invisible, Hidden, NoView off
			ad.Update("F", types.Integer(flags))
			ad.Delete("AA")
			if act, ok := ad.Find("A"); ok {
				if d, err := c.DereferenceDict(act); err == nil && d != nil {
					if s := d.NameEntry("S"); s != nil && (*s == "JavaScript" || *s == "Launch") {
						ad.Delete("A")
					}
				}
			}
		}
	}

	// Info dictionary and XMP must agree; stamp both from the same values.
	cur := ReadMetadata(c)
	merge := func(a, b string) string {
		if a != "" {
			return a
		}
		return b
	}
	meta := Metadata{
		Title: merge(m.Title, cur.Title), Author: merge(m.Author, cur.Author),
		Subject: merge(m.Subject, cur.Subject), Keywords: merge(m.Keywords, cur.Keywords),
		Creator: merge(m.Creator, cur.Creator), Producer: Producer,
	}
	now = now.UTC().Truncate(time.Second)
	if c.Info == nil {
		ir, err := c.IndRefForNewObject(types.NewDict())
		if err != nil {
			return nil, err
		}
		c.Info = ir
	}
	info := infoDict(c)
	for _, k := range []string{"Title", "Author", "Subject", "Keywords", "Creator", "Producer", "CreationDate", "ModDate"} {
		info.Delete(k)
	}
	if err := SetMetadata(c, meta, nil, now); err != nil {
		return nil, err
	}
	pdfDate := types.StringLiteral(now.Format("D:20060102150405Z"))
	info.Update("CreationDate", pdfDate)
	info.Update("ModDate", pdfDate)

	xmp := buildXMP(meta, now)
	sd := types.StreamDict{Dict: types.NewDict(), Content: xmp}
	sd.InsertName("Type", "Metadata")
	sd.InsertName("Subtype", "XML")
	if err := sd.Encode(); err != nil {
		return nil, err
	}
	xmpRef, err := c.IndRefForNewObject(sd)
	if err != nil {
		return nil, err
	}
	root.Update("Metadata", *xmpRef)

	icc := types.StreamDict{Dict: types.NewDict(), Content: assets.SRGBICC}
	icc.InsertInt("N", 3)
	if err := icc.Encode(); err != nil {
		return nil, err
	}
	iccRef, err := c.IndRefForNewObject(icc)
	if err != nil {
		return nil, err
	}
	oi := types.NewDict()
	oi.InsertName("Type", "OutputIntent")
	oi.InsertName("S", "GTS_PDFA1")
	oi.Insert("OutputConditionIdentifier", types.StringLiteral("sRGB IEC61966-2.1"))
	oi.Insert("Info", types.StringLiteral("sRGB IEC61966-2.1"))
	oi.Insert("RegistryName", types.StringLiteral("http://www.color.org"))
	oi.Insert("DestOutputProfile", *iccRef)
	root.Update("OutputIntents", types.Array{oi})

	if lang := root.StringEntry("Lang"); lang == nil {
		root.Update("Lang", types.StringLiteral("tr-TR"))
	}
	v17 := model.V17
	c.HeaderVersion = &v17
	c.RootVersion = nil
	root.Delete("Version")

	out, err := WriteContext(ctx, c)
	if err != nil {
		return nil, err
	}
	res.Data = out
	return res, nil
}

func xmlEscape(s string) string {
	var b bytes.Buffer
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}

func buildXMP(m Metadata, t time.Time) []byte {
	d := t.Format("2006-01-02T15:04:05Z")
	var b strings.Builder
	b.WriteString("<?xpacket begin=\"" + string(rune(0xFEFF)) + "\" id=\"W5M0MpCehiHzreSzNTczkc9d\"?>\n")
	b.WriteString(`<x:xmpmeta xmlns:x="adobe:ns:meta/">` + "\n")
	b.WriteString(`<rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">` + "\n")
	b.WriteString(`<rdf:Description rdf:about="" xmlns:pdfaid="http://www.aiim.org/pdfa/ns/id/" xmlns:dc="http://purl.org/dc/elements/1.1/" xmlns:xmp="http://ns.adobe.com/xap/1.0/" xmlns:pdf="http://ns.adobe.com/pdf/1.3/">` + "\n")
	b.WriteString("<pdfaid:part>2</pdfaid:part>\n<pdfaid:conformance>B</pdfaid:conformance>\n")
	b.WriteString("<dc:format>application/pdf</dc:format>\n")
	if m.Title != "" {
		b.WriteString(`<dc:title><rdf:Alt><rdf:li xml:lang="x-default">` + xmlEscape(m.Title) + "</rdf:li></rdf:Alt></dc:title>\n")
	}
	if m.Author != "" {
		b.WriteString("<dc:creator><rdf:Seq><rdf:li>" + xmlEscape(m.Author) + "</rdf:li></rdf:Seq></dc:creator>\n")
	}
	if m.Subject != "" {
		b.WriteString(`<dc:description><rdf:Alt><rdf:li xml:lang="x-default">` + xmlEscape(m.Subject) + "</rdf:li></rdf:Alt></dc:description>\n")
	}
	if m.Keywords != "" {
		b.WriteString("<pdf:Keywords>" + xmlEscape(m.Keywords) + "</pdf:Keywords>\n")
	}
	b.WriteString("<pdf:Producer>" + xmlEscape(m.Producer) + "</pdf:Producer>\n")
	if m.Creator != "" {
		b.WriteString("<xmp:CreatorTool>" + xmlEscape(m.Creator) + "</xmp:CreatorTool>\n")
	}
	b.WriteString("<xmp:CreateDate>" + d + "</xmp:CreateDate>\n<xmp:ModifyDate>" + d + "</xmp:ModifyDate>\n<xmp:MetadataDate>" + d + "</xmp:MetadataDate>\n")
	b.WriteString("</rdf:Description>\n</rdf:RDF>\n</x:xmpmeta>\n")
	// Padding lets editors update the packet in place.
	b.WriteString(strings.Repeat(strings.Repeat(" ", 99)+"\n", 20))
	b.WriteString(`<?xpacket end="w"?>`)
	return []byte(b.String())
}

var pdfaPartRe = regexp.MustCompile(`pdfaid:part(?:>|=")\s*(\d)`)
var pdfaConfRe = regexp.MustCompile(`pdfaid:conformance(?:>|=")\s*([ABUabu])`)

// pdfaClaim returns e.g. "2B" when the XMP metadata declares PDF/A conformance.
func pdfaClaim(c *model.Context) string {
	root, err := c.Catalog()
	if err != nil {
		return ""
	}
	mo, ok := root.Find("Metadata")
	if !ok {
		return ""
	}
	sd, _, err := c.DereferenceStreamDict(mo)
	if err != nil || sd == nil {
		return ""
	}
	if err := sd.DecodeWithLimit(maxDecodedStream); err != nil {
		return ""
	}
	p := pdfaPartRe.FindSubmatch(sd.Content)
	if p == nil {
		return ""
	}
	conf := ""
	if m := pdfaConfRe.FindSubmatch(sd.Content); m != nil {
		conf = strings.ToUpper(string(m[1]))
	}
	return string(p[1]) + conf
}
