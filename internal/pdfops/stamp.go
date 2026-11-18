package pdfops

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/font"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"

	"github.com/milvasoft/pidief/internal/assets"
)

var (
	fontOnce sync.Once
	fontName string
	fontConf *model.Configuration
	fontErr  error
)

// ensureFont installs the embedded Onest font as a pdfcpu user font, so text watermarks are
// written with an embedded Unicode font (the core 14 fonts cannot encode ş, ğ, ı, İ).
// pdfcpu keeps font metrics in its configuration directory, so a private temporary one is
// created once per process; documents themselves never touch the disk.
func ensureFont(ctx context.Context) (string, *model.Configuration, error) {
	fontOnce.Do(func() {
		dir, err := os.MkdirTemp("", "pidief-pdfcpu-")
		if err != nil {
			fontErr = err
			return
		}
		init, err := api.InitializeConfigurationWithOptions(api.ConfigurationOptions{Root: dir, Mode: api.ConfigurationModeAuto})
		if err != nil {
			fontErr = err
			return
		}
		conf := init.Configuration
		fontDir, ok := conf.UserFontStore()
		if !ok {
			fontErr = fmt.Errorf("pdfcpu user fonts unavailable")
			return
		}
		if err := font.InstallFontFromBytesQuiet(fontDir, "PidiefStamp", assets.StampTTF); err != nil {
			fontErr = err
			return
		}
		repo := font.RepositoryForDir(fontDir)
		for _, n := range []string{"PidiefStamp-Regular", "PidiefStamp"} {
			if ok, err := repo.IsUserFont(ctx, n); err == nil && ok {
				fontName = n
				break
			}
		}
		if fontName == "" {
			entries, _ := os.ReadDir(fontDir)
			fontErr = fmt.Errorf("stamp font not registered in %s (%d files)", fontDir, len(entries))
			return
		}
		conf.ValidationMode = model.ValidationRelaxed
		fontConf = conf
	})
	if fontErr != nil {
		return "", nil, fontErr
	}
	return fontName, fontConf.Clone(), nil
}

// StampOptions describes a text or image watermark/stamp.
type StampOptions struct {
	Text     string // supports %p (page number) and %P (page count)
	Image    []byte // PNG/JPEG/WebP/TIFF; used when Text is empty
	OnTop    bool   // stamp (above content) vs watermark (behind content)
	FontSize int
	Scale    float64 // relative to page width; 0 = absolute font size / natural image size
	Opacity  float64
	Rotation float64
	Position string // c, tl, tc, tr, l, r, bl, bc, br
	OffsetX  float64
	OffsetY  float64
	Color    string // #rrggbb
	Pages    string
	Password string
}

var positions = map[string]bool{"c": true, "tl": true, "tc": true, "tr": true, "l": true, "r": true, "bl": true, "bc": true, "br": true}

// Stamp applies a watermark (behind the content) or a stamp (on top).
func Stamp(ctx context.Context, pdf []byte, o StampOptions) ([]byte, error) {
	if o.Text == "" && len(o.Image) == 0 {
		return nil, fmt.Errorf("%w: text or image is required", ErrBadInput)
	}
	if o.Position == "" {
		o.Position = "c"
	}
	if !positions[o.Position] {
		return nil, fmt.Errorf("%w: position must be one of c, tl, tc, tr, l, r, bl, bc, br", ErrBadInput)
	}
	if o.Opacity <= 0 || o.Opacity > 1 {
		return nil, fmt.Errorf("%w: opacity must be in (0, 1]", ErrBadInput)
	}
	sel, err := Selection(o.Pages)
	if err != nil {
		return nil, err
	}
	desc := []string{
		fmt.Sprintf("pos:%s", o.Position),
		fmt.Sprintf("off:%g %g", o.OffsetX, o.OffsetY),
		fmt.Sprintf("op:%g", o.Opacity),
		fmt.Sprintf("rot:%g", o.Rotation),
	}
	if o.Scale > 0 {
		desc = append(desc, fmt.Sprintf("scale:%g rel", o.Scale))
	} else {
		desc = append(desc, "scale:1 abs")
	}
	var wm *model.Watermark
	conf := Conf(o.Password)
	if o.Text != "" {
		name, fconf, err := ensureFont(ctx)
		if err != nil {
			return nil, fmt.Errorf("font: %w", err)
		}
		if o.Password != "" {
			fconf.UserPW, fconf.OwnerPW = o.Password, o.Password
		}
		conf = fconf
		if o.FontSize <= 0 {
			o.FontSize = 48
		}
		color := o.Color
		if color == "" {
			color = "#808080"
		}
		if !strings.HasPrefix(color, "#") || (len(color) != 7 && len(color) != 4) {
			return nil, fmt.Errorf("%w: color must be #rrggbb", ErrBadInput)
		}
		desc = append(desc, "fontname:"+name, fmt.Sprintf("points:%d", o.FontSize), "fillc:"+color, "strokec:"+color)
		wm, err = api.TextWatermark(ctx, o.Text, strings.Join(desc, ", "), o.OnTop, false, types.POINTS, conf)
		if err != nil {
			return nil, bad(err)
		}
	} else {
		wm, err = api.ImageWatermarkForReader(ctx, bytes.NewReader(o.Image), strings.Join(desc, ", "), o.OnTop, false, types.POINTS)
		if err != nil {
			return nil, bad(err)
		}
	}
	return run(func(rs io.ReadSeeker, w io.Writer) error { return api.AddWatermarks(ctx, rs, w, sel, wm, conf) }, pdf)
}
