// Package assets holds files embedded into the pidief binary.
package assets

import _ "embed"

// Onest static instances (SIL Open Font License 1.1, see fonts/OFL.txt), generated from the
// upstream variable font with fontTools varLib.instancer. Static TrueType fonts are embedded
// by Chromium as real font subsets (CIDFontType2); a variable font would become Type3.
var (
	//go:embed fonts/Onest-Regular.ttf
	OnestRegular []byte
	//go:embed fonts/Onest-Medium.ttf
	OnestMedium []byte
	//go:embed fonts/Onest-SemiBold.ttf
	OnestSemiBold []byte
	//go:embed fonts/Onest-Bold.ttf
	OnestBold []byte
)

// OnestTTF is the regular weight.
var OnestTTF = OnestRegular

// StampTTF is Onest Regular renamed to "PidiefStamp" (OFL allows modified versions; Onest has no
// Reserved Font Name). Watermarks and stamps use it so pdfcpu never confuses it with an Onest
// subset that Chromium already embedded in the document.
//
//go:embed fonts/PidiefStamp-Regular.ttf
var StampTTF []byte

// OnestFace is one weight served to Chromium.
type OnestFace struct {
	Weight int
	File   string
	Data   []byte
}

// OnestFaces lists the embedded weights.
func OnestFaces() []OnestFace {
	return []OnestFace{
		{400, "Onest-Regular.ttf", OnestRegular},
		{500, "Onest-Medium.ttf", OnestMedium},
		{600, "Onest-SemiBold.ttf", OnestSemiBold},
		{700, "Onest-Bold.ttf", OnestBold},
	}
}

// SRGBICC is a compact sRGB v2 ICC profile (CC0), used as the PDF/A output intent.
//
//go:embed icc/sRGB-v2-micro.icc
var SRGBICC []byte
