// Package pdfops wraps pdfcpu (Apache-2.0) for structural PDF operations on in-memory documents.
package pdfops

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

// Producer is written into documents pidief changes.
const Producer = "pidief"

// Conf returns a stateless pdfcpu configuration (no config dir on disk).
func Conf(password string) *model.Configuration {
	c := model.NewStatelessConfiguration()
	c.ValidationMode = model.ValidationRelaxed
	if password != "" {
		c.UserPW = password
		c.OwnerPW = password
	}
	return c
}

// ErrBadInput marks client errors (bad page selection, wrong password, broken PDF).
var ErrBadInput = errors.New("bad input")

func bad(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return fmt.Errorf("%w: %v", ErrBadInput, err)
}

func run(f func(rs io.ReadSeeker, w io.Writer) error, in []byte) ([]byte, error) {
	var out bytes.Buffer
	if err := f(bytes.NewReader(in), &out); err != nil {
		return nil, bad(err)
	}
	return out.Bytes(), nil
}

// Selection parses a pdfcpu page selection ("1-3,5", "even", "!2", "l-1"); empty = all.
func Selection(s string) ([]string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	sel, err := api.ParsePageSelection(s)
	if err != nil {
		return nil, fmt.Errorf("%w: page selection %q: %v", ErrBadInput, s, err)
	}
	return sel, nil
}

// Merge concatenates documents in order.
func Merge(ctx context.Context, docs [][]byte, password string) ([]byte, error) {
	if len(docs) < 2 {
		return nil, fmt.Errorf("%w: merge needs at least 2 PDFs", ErrBadInput)
	}
	rs := make([]io.ReadSeeker, len(docs))
	for i, d := range docs {
		rs[i] = bytes.NewReader(d)
	}
	var out bytes.Buffer
	if err := api.MergeRaw(ctx, rs, &out, false, Conf(password)); err != nil {
		return nil, bad(err)
	}
	return out.Bytes(), nil
}

// PageCount returns the number of pages.
func PageCount(ctx context.Context, pdf []byte, password string) (int, error) {
	n, err := api.PageCount(ctx, bytes.NewReader(pdf), Conf(password))
	return n, bad(err)
}

// Collect builds a document from the selected pages, in the given order (duplicates allowed).
func Collect(ctx context.Context, pdf []byte, selection, password string) ([]byte, error) {
	sel, err := Selection(selection)
	if err != nil {
		return nil, err
	}
	if sel == nil {
		return nil, fmt.Errorf("%w: pages is required", ErrBadInput)
	}
	return run(func(rs io.ReadSeeker, w io.Writer) error { return api.Collect(ctx, rs, w, sel, Conf(password)) }, pdf)
}

// Remove deletes the selected pages.
func Remove(ctx context.Context, pdf []byte, selection, password string) ([]byte, error) {
	sel, err := Selection(selection)
	if err != nil {
		return nil, err
	}
	if sel == nil {
		return nil, fmt.Errorf("%w: pages is required", ErrBadInput)
	}
	return run(func(rs io.ReadSeeker, w io.Writer) error { return api.RemovePages(ctx, rs, w, sel, Conf(password)) }, pdf)
}

// Rotate rotates the selected pages (all when empty) by a multiple of 90 degrees.
func Rotate(ctx context.Context, pdf []byte, degrees int, selection, password string) ([]byte, error) {
	if degrees%90 != 0 {
		return nil, fmt.Errorf("%w: rotation must be a multiple of 90", ErrBadInput)
	}
	sel, err := Selection(selection)
	if err != nil {
		return nil, err
	}
	return run(func(rs io.ReadSeeker, w io.Writer) error { return api.Rotate(ctx, rs, w, degrees, sel, Conf(password)) }, pdf)
}

// Part is one output of Split.
type Part struct {
	Name      string
	From, To  int
	Data      []byte
	PageCount int
}

// Split splits by fixed span (every n pages), at page numbers (a new part starts at each
// given page) or by explicit ranges ("1-3", "4-", "7,9"). Exactly one mode must be set.
func Split(ctx context.Context, pdf []byte, span int, at []int, ranges []string, password string) ([]Part, error) {
	n, err := PageCount(ctx, pdf, password)
	if err != nil {
		return nil, err
	}
	modes := 0
	if span > 0 {
		modes++
	}
	if len(at) > 0 {
		modes++
	}
	if len(ranges) > 0 {
		modes++
	}
	if modes == 0 {
		span = 1
	} else if modes > 1 {
		return nil, fmt.Errorf("%w: use only one of span, at, ranges", ErrBadInput)
	}
	var sels []string
	switch {
	case span > 0:
		for from := 1; from <= n; from += span {
			sels = append(sels, fmt.Sprintf("%d-%d", from, min(n, from+span-1)))
		}
	case len(at) > 0:
		sort.Ints(at)
		starts := []int{1}
		for _, p := range at {
			if p < 2 || p > n {
				return nil, fmt.Errorf("%w: split page %d out of range (2-%d)", ErrBadInput, p, n)
			}
			if p != starts[len(starts)-1] {
				starts = append(starts, p)
			}
		}
		for i, s := range starts {
			end := n
			if i+1 < len(starts) {
				end = starts[i+1] - 1
			}
			sels = append(sels, fmt.Sprintf("%d-%d", s, end))
		}
	default:
		sels = ranges
	}
	var parts []Part
	for i, s := range sels {
		data, err := Collect(ctx, pdf, s, password)
		if err != nil {
			return nil, err
		}
		pc, _ := PageCount(ctx, data, "")
		name := fmt.Sprintf("part-%03d_%s.pdf", i+1, strings.NewReplacer(",", "_", " ", "").Replace(s))
		parts = append(parts, Part{Name: name, Data: data, PageCount: pc})
	}
	return parts, nil
}

// EncryptOptions configures password protection.
type EncryptOptions struct {
	UserPassword  string // needed to open
	OwnerPassword string // needed to change permissions
	AllowPrint    bool
	AllowCopy     bool
	AllowModify   bool
	AllowAnnotate bool
}

// Encrypt protects a document with AES-256.
func Encrypt(ctx context.Context, pdf []byte, o EncryptOptions) ([]byte, error) {
	if o.UserPassword == "" && o.OwnerPassword == "" {
		return nil, fmt.Errorf("%w: userPassword or ownerPassword is required", ErrBadInput)
	}
	if o.OwnerPassword == "" {
		o.OwnerPassword = o.UserPassword
	}
	c := Conf("")
	c.UserPW = o.UserPassword
	c.OwnerPW = o.OwnerPassword
	c.EncryptUsingAES = true
	c.EncryptKeyLength = 256
	perm := model.PermissionsNone
	if o.AllowPrint {
		perm |= model.PermissionPrintRev2 | model.PermissionPrintRev3
	}
	if o.AllowCopy {
		perm |= model.PermissionExtract | model.PermissionExtractRev3
	}
	if o.AllowModify {
		perm |= model.PermissionModify | model.PermissionAssembleRev3
	}
	if o.AllowAnnotate {
		perm |= model.PermissionModAnnFillForm | model.PermissionFillRev3
	}
	c.Permissions = perm
	return run(func(rs io.ReadSeeker, w io.Writer) error { return api.Encrypt(ctx, rs, w, c) }, pdf)
}

// Decrypt removes password protection.
func Decrypt(ctx context.Context, pdf []byte, password string) ([]byte, error) {
	return run(func(rs io.ReadSeeker, w io.Writer) error { return api.Decrypt(ctx, rs, w, Conf(password)) }, pdf)
}

// Info is the JSON summary of a document.
type Info struct {
	PageCount int               `json:"pageCount"`
	Version   string            `json:"version"`
	Encrypted bool              `json:"encrypted"`
	Tagged    bool              `json:"tagged"`
	Linear    bool              `json:"linearized"`
	FileSize  int               `json:"fileSize"`
	Metadata  Metadata          `json:"metadata"`
	Pages     []PageInfo        `json:"pages"`
	Fonts     []FontInfo        `json:"fonts"`
	PDFA      string            `json:"pdfa,omitempty"`
	Custom    map[string]string `json:"properties,omitempty"`
}

// PageInfo describes one page in points (1/72 inch).
type PageInfo struct {
	Number   int     `json:"number"`
	Width    float64 `json:"width"`
	Height   float64 `json:"height"`
	Rotation int     `json:"rotation"`
}

// FontInfo describes one font resource.
type FontInfo struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Embedded bool   `json:"embedded"`
	Encoding string `json:"encoding,omitempty"`
}

// Metadata are the document information fields.
type Metadata struct {
	Title        string `json:"title,omitempty"`
	Author       string `json:"author,omitempty"`
	Subject      string `json:"subject,omitempty"`
	Keywords     string `json:"keywords,omitempty"`
	Creator      string `json:"creator,omitempty"`
	Producer     string `json:"producer,omitempty"`
	CreationDate string `json:"creationDate,omitempty"`
	ModDate      string `json:"modDate,omitempty"`
}

// ReadContext parses a document.
func ReadContext(ctx context.Context, pdf []byte, password string) (*model.Context, error) {
	c, err := api.ReadContext(ctx, bytes.NewReader(pdf), Conf(password))
	if err != nil {
		return nil, bad(err)
	}
	if err := c.EnsurePageCount(); err != nil {
		return nil, bad(err)
	}
	return c, nil
}

// WriteContext serialises a document.
func WriteContext(ctx context.Context, c *model.Context) ([]byte, error) {
	var out bytes.Buffer
	if err := api.WriteContext(ctx, c, &out); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// GetInfo returns page sizes, metadata and fonts.
func GetInfo(ctx context.Context, pdf []byte, password string) (*Info, error) {
	c, err := ReadContext(ctx, pdf, password)
	if err != nil {
		return nil, err
	}
	info := &Info{
		PageCount: c.PageCount,
		Version:   c.HeaderVersion.String(),
		Encrypted: c.Encrypt != nil,
		Tagged:    c.Tagged,
		Linear:    c.Read.Linearized,
		FileSize:  len(pdf),
		Metadata:  ReadMetadata(c),
		Fonts:     Fonts(c),
		Custom:    map[string]string{},
	}
	if d := infoDict(c); d != nil {
		std := map[string]bool{"Title": true, "Author": true, "Subject": true, "Keywords": true, "Creator": true,
			"Producer": true, "CreationDate": true, "ModDate": true, "Trapped": true}
		for k := range d {
			if !std[k] {
				if v := infoString(c, d, k); v != "" {
					info.Custom[k] = v
				}
			}
		}
	}
	dims, err := c.PageDims(ctx)
	if err == nil {
		for i, d := range dims {
			pi := PageInfo{Number: i + 1, Width: round2(d.Width), Height: round2(d.Height)}
			if pd, _, inh, err := c.PageDict(ctx, i+1, false); err == nil && pd != nil && inh != nil {
				pi.Rotation = inh.Rotate
			}
			info.Pages = append(info.Pages, pi)
		}
	}
	info.PDFA = pdfaClaim(c)
	return info, nil
}

func round2(f float64) float64 { return float64(int(f*100+0.5)) / 100 }

func infoString(c *model.Context, d types.Dict, key string) string {
	o, ok := d.Find(key)
	if !ok {
		return ""
	}
	o, err := c.Dereference(o)
	if err != nil || o == nil {
		return ""
	}
	s, err := types.StringOrHexLiteral(o)
	if err != nil || s == nil {
		return ""
	}
	return *s
}

func infoDict(c *model.Context) types.Dict {
	if c.Info == nil {
		return nil
	}
	d, err := c.DereferenceDict(*c.Info)
	if err != nil {
		return nil
	}
	return d
}

// ReadMetadata returns the Info dictionary fields.
func ReadMetadata(c *model.Context) Metadata {
	d := infoDict(c)
	if d == nil {
		return Metadata{}
	}
	return Metadata{
		Title: infoString(c, d, "Title"), Author: infoString(c, d, "Author"),
		Subject: infoString(c, d, "Subject"), Keywords: infoString(c, d, "Keywords"),
		Creator: infoString(c, d, "Creator"), Producer: infoString(c, d, "Producer"),
		CreationDate: infoString(c, d, "CreationDate"), ModDate: infoString(c, d, "ModDate"),
	}
}

// PDFString encodes s as a PDF text string (UTF-16BE hex when not ASCII).
func PDFString(s string) types.Object {
	for _, r := range s {
		if r > 126 || r < 32 {
			return types.NewHexLiteral([]byte(types.EncodeUTF16String(s)))
		}
	}
	esc, err := types.Escape(s)
	if err != nil {
		return types.NewHexLiteral([]byte(types.EncodeUTF16String(s)))
	}
	return types.StringLiteral(*esc)
}

// SetMetadata updates the Info dictionary. Empty fields are left unchanged; custom
// properties are added as extra Info keys. Producer and ModDate are stamped by pidief.
func SetMetadata(c *model.Context, m Metadata, custom map[string]string, now time.Time) error {
	if c.Info == nil {
		ir, err := c.IndRefForNewObject(types.NewDict())
		if err != nil {
			return err
		}
		c.Info = ir
	}
	d := infoDict(c)
	if d == nil {
		return errors.New("info dictionary is not a dictionary")
	}
	set := func(k, v string) {
		if v != "" {
			d.Update(k, PDFString(v))
		}
	}
	set("Title", m.Title)
	set("Author", m.Author)
	set("Subject", m.Subject)
	set("Keywords", m.Keywords)
	set("Creator", m.Creator)
	producer := m.Producer
	if producer == "" {
		producer = Producer
	}
	d.Update("Producer", PDFString(producer))
	date := types.StringLiteral(types.DateString(now))
	if _, ok := d.Find("CreationDate"); !ok {
		d.Update("CreationDate", date)
	}
	d.Update("ModDate", date)
	for k, v := range custom {
		if k == "" || strings.ContainsAny(k, " /()<>[]{}%#") {
			return fmt.Errorf("%w: invalid property name %q", ErrBadInput, k)
		}
		d.Update(k, PDFString(v))
	}
	c.PreserveInfoDict = true
	return nil
}

// Fonts lists the font dictionaries of the document and whether each is embedded.
func Fonts(c *model.Context) []FontInfo {
	seen := map[string]bool{}
	var out []FontInfo
	for objNr := 1; objNr <= *c.Size; objNr++ {
		entry, ok := c.Table[objNr]
		if !ok || entry == nil || entry.Free {
			continue
		}
		o, err := c.Dereference(*types.NewIndirectRef(objNr, 0))
		if err != nil || o == nil {
			continue
		}
		d, ok := o.(types.Dict)
		if !ok || d.Type() == nil || *d.Type() != "Font" {
			continue
		}
		sub := d.Subtype()
		if sub == nil || *sub == "Type0" {
			continue // the descendant CIDFont carries the descriptor
		}
		name := ""
		if n := d.NameEntry("BaseFont"); n != nil {
			name = *n
		}
		fi := FontInfo{Name: name, Type: *sub}
		if *sub == "Type3" {
			fi.Embedded = true // glyphs are content streams
		} else if fdo, ok := d.Find("FontDescriptor"); ok {
			if fd, err := c.DereferenceDict(fdo); err == nil && fd != nil {
				for _, k := range []string{"FontFile", "FontFile2", "FontFile3"} {
					if _, ok := fd.Find(k); ok {
						fi.Embedded = true
					}
				}
			}
		}
		if e := d.NameEntry("Encoding"); e != nil {
			fi.Encoding = *e
		}
		key := fi.Name + "|" + fi.Type + "|" + strconv.FormatBool(fi.Embedded)
		if !seen[key] {
			seen[key] = true
			out = append(out, fi)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// SelectedPages resolves a parsed selection against a page count (ascending, 1-based).
func SelectedPages(pageCount int, sel []string) ([]int, error) {
	set, err := api.PagesForSelection(pageCount, sel, true)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrBadInput, err)
	}
	var out []int
	for p, ok := range set {
		if ok {
			out = append(out, p)
		}
	}
	sort.Ints(out)
	if len(out) == 0 {
		return nil, fmt.Errorf("%w: page selection matches no page", ErrBadInput)
	}
	return out, nil
}

// maxDecodedStream bounds a decoded stream (image, XMP) to protect against decompression bombs.
const maxDecodedStream = 256 << 20
