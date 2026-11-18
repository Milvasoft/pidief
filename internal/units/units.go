// Package units parses lengths and paper sizes.
package units

import (
	"fmt"
	"strconv"
	"strings"
)

// Size is a paper size in points (1/72 inch).
type Size struct{ W, H float64 }

var papers = map[string]Size{
	"a0": {2383.94, 3370.39}, "a1": {1683.78, 2383.94}, "a2": {1190.55, 1683.78},
	"a3": {841.89, 1190.55}, "a4": {595.28, 841.89}, "a5": {419.53, 595.28}, "a6": {297.64, 419.53},
	"b4": {708.66, 1000.63}, "b5": {498.90, 708.66},
	"letter": {612, 792}, "legal": {612, 1008}, "tabloid": {792, 1224},
}

// PaperNames lists the known paper names.
const PaperNames = "A0-A6, B4, B5, Letter, Legal, Tabloid or WxH with a unit (210x297mm, 8.5x11in)"

// Length parses "10mm", "1cm", "0.5in", "12pt", "16px" or a bare number (mm) into points.
func Length(s string) (float64, error) {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "" {
		return 0, nil
	}
	unit := "mm"
	for _, u := range []string{"mm", "cm", "in", "pt", "px"} {
		if strings.HasSuffix(s, u) {
			unit = u
			s = strings.TrimSpace(strings.TrimSuffix(s, u))
			break
		}
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil || v < 0 {
		return 0, fmt.Errorf("invalid length %q", s)
	}
	switch unit {
	case "mm":
		return v * 72 / 25.4, nil
	case "cm":
		return v * 72 / 2.54, nil
	case "in":
		return v * 72, nil
	case "px":
		return v * 72 / 96, nil
	}
	return v, nil
}

// Paper parses a paper name or "WxH<unit>" into points (portrait as given).
func Paper(s string) (Size, error) {
	k := strings.ToLower(strings.TrimSpace(s))
	if k == "" {
		k = "a4"
	}
	if p, ok := papers[k]; ok {
		return p, nil
	}
	parts := strings.Split(k, "x")
	if len(parts) == 2 {
		unit := ""
		for _, u := range []string{"mm", "cm", "in", "pt", "px"} {
			if strings.HasSuffix(parts[1], u) {
				unit = u
			}
		}
		w, err1 := Length(withUnit(parts[0], unit))
		h, err2 := Length(parts[1])
		if err1 == nil && err2 == nil && w > 0 && h > 0 {
			return Size{w, h}, nil
		}
	}
	return Size{}, fmt.Errorf("unknown paper size %q (%s)", s, PaperNames)
}

func withUnit(v, unit string) string {
	v = strings.TrimSpace(v)
	for _, u := range []string{"mm", "cm", "in", "pt", "px"} {
		if strings.HasSuffix(v, u) {
			return v
		}
	}
	return v + unit
}

// Inches converts points to inches.
func Inches(pt float64) float64 { return pt / 72 }
