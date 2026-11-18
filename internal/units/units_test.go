package units

import (
	"math"
	"testing"
)

func TestLengthAndPaper(t *testing.T) {
	for in, want := range map[string]float64{"25.4mm": 72, "2.54cm": 72, "1in": 72, "72pt": 72, "96px": 72, "10": 28.3465, "": 0} {
		got, err := Length(in)
		if err != nil || math.Abs(got-want) > 0.01 {
			t.Errorf("Length(%q) = %v, %v; want %v", in, got, err, want)
		}
	}
	if _, err := Length("abc"); err == nil {
		t.Error("want error")
	}
	a4, _ := Paper("a4")
	custom, err := Paper("210x297mm")
	if err != nil || math.Abs(a4.W-custom.W) > 0.1 || math.Abs(a4.H-custom.H) > 0.1 {
		t.Errorf("custom = %+v, %v", custom, err)
	}
	if l, err := Paper("8.5x11in"); err != nil || l.W != 612 || l.H != 792 {
		t.Errorf("letter = %+v %v", l, err)
	}
	if _, err := Paper("A9"); err == nil {
		t.Error("want error")
	}
}
