package jpeg2000

import (
	"image"
	"testing"
)

// planes builds n component planes of h rows by w columns from a generator.
func planes(n, w, h int, f func(c, x, y int) int32) [][][]int32 {
	out := make([][][]int32, n)
	for c := range out {
		out[c] = make([][]int32, h)
		for y := range out[c] {
			out[c][y] = make([]int32, w)
			for x := range out[c][y] {
				out[c][y][x] = f(c, x, y)
			}
		}
	}
	return out
}

func cmykHeader(numComps int, mct bool, wavelet WaveletType) *MainHeader {
	h := &MainHeader{NumComps: numComps, MCT: mct, WaveletFilter: wavelet}
	for range numComps {
		h.BitDepth = append(h.BitDepth, 8)
		h.Signed = append(h.Signed, false)
	}
	return h
}

func cmykMeta(space JP2ColorSpace) *JP2Metadata {
	return &JP2Metadata{ColorMethod: JP2ColorEnumerated, ColorSpace: space, NumComps: 4}
}

// TestAFileThatSaysCMYKIsReadAsInk is the defect this replaces. Four planes
// were read as red, green, blue and nothing, so a CMYK picture lost its black
// plate: 255 levels from the reference on every pixel of the one in
// go-pdfkit's corpus.
func TestAFileThatSaysCMYKIsReadAsInk(t *testing.T) {
	// Samples are stored with the display offset removed, so a stored -128
	// comes back as 0 and a stored +127 as 255.
	comps := planes(4, 2, 1, func(c, x, y int) int32 {
		return []int32{-128, -64, 0, 127}[c]
	})
	d := &Decoder{header: cmykHeader(4, false, Wavelet53), jp2Meta: cmykMeta(JP2ColorCMYK), components: comps}
	got := d.picture()
	cm, ok := got.(*image.CMYK)
	if !ok {
		t.Fatalf("a file declaring CMYK decoded as %T", got)
	}
	want := [4]uint8{0, 64, 128, 255}
	for x := range 2 {
		i := cm.PixOffset(x, 0)
		for c := range 4 {
			if cm.Pix[i+c] != want[c] {
				t.Errorf("pixel %d channel %d = %d, want %d", x, c, cm.Pix[i+c], want[c])
			}
		}
	}
}

// TestFourComponentsAreNotAssumedToBeInk: the FILE is asked, not the count.
// Four components are as often red, green, blue and an alpha channel.
func TestFourComponentsAreNotAssumedToBeInk(t *testing.T) {
	comps := planes(4, 2, 1, func(c, x, y int) int32 { return 0 })
	for name, meta := range map[string]*JP2Metadata{
		"no JP2 header at all": nil,
		"sRGB":                 cmykMeta(JP2ColorSRGB),
		"unknown colour space": cmykMeta(JP2ColorUnknown),
		"an ICC profile rather than an enumerated space": {
			ColorMethod: JP2ColorICC, ColorSpace: JP2ColorCMYK, NumComps: 4},
	} {
		d := &Decoder{header: cmykHeader(4, false, Wavelet53), jp2Meta: meta, components: comps}
		if got := d.picture(); !isRGBA(got) {
			t.Errorf("%s: decoded as %T, want *image.RGBA", name, got)
		}
	}
}

func isRGBA(img image.Image) bool { _, ok := img.(*image.RGBA); return ok }

// TestTheBlackPlateIsOutsideTheTransform. RCT and ICT are defined over three
// correlated planes; the fourth is never part of one. If the transform reached
// it, the plate would come back as something else entirely.
func TestTheBlackPlateIsOutsideTheTransform(t *testing.T) {
	for _, tc := range []struct {
		name    string
		wavelet WaveletType
	}{
		{"reversible, so RCT", Wavelet53},
		{"irreversible, so ICT", Wavelet97},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// A flat picture: the three correlated planes carry no chroma, so
			// whatever transform runs must return them unchanged, and the
			// fourth plane must arrive at its own value.
			comps := planes(4, 4, 4, func(c, x, y int) int32 {
				if c == 3 {
					return 0 // mid ink on the plate
				}
				if c == 0 {
					return 0 // mid on the luma-like plane
				}
				return 0 // no chroma
			})
			d := &Decoder{header: cmykHeader(4, true, tc.wavelet), jp2Meta: cmykMeta(JP2ColorCMYK), components: comps}
			cm, ok := d.picture().(*image.CMYK)
			if !ok {
				t.Fatalf("decoded as something other than CMYK")
			}
			i := cm.PixOffset(2, 2)
			if cm.Pix[i+3] != 128 {
				t.Errorf("the black plate came back as %d, want 128 -- the transform reached it", cm.Pix[i+3])
			}
			for c := range 3 {
				if cm.Pix[i+c] != 128 {
					t.Errorf("channel %d = %d, want 128 on a picture with no chroma", c, cm.Pix[i+c])
				}
			}
		})
	}
}

// TestBothEntryPointsAgreeAboutWhatAFileIs. Decode and DecodeWithUpsampling
// take different paths to an image, and a file must not be CMYK down one and
// RGBA down the other.
func TestBothEntryPointsAgreeAboutWhatAFileIs(t *testing.T) {
	comps := planes(4, 2, 2, func(c, x, y int) int32 { return int32(c*16 - 64) })
	mk := func() *Decoder {
		return &Decoder{header: cmykHeader(4, false, Wavelet53), jp2Meta: cmykMeta(JP2ColorCMYK), components: planes(4, 2, 2, func(c, x, y int) int32 { return comps[c][y][x] })}
	}
	a, b := mk().picture(), mk().pictureUpsampled()
	ca, oka := a.(*image.CMYK)
	cb, okb := b.(*image.CMYK)
	if !oka || !okb {
		t.Fatalf("picture() gave %T and pictureUpsampled() gave %T", a, b)
	}
	if len(ca.Pix) != len(cb.Pix) {
		t.Fatalf("different sizes: %d and %d", len(ca.Pix), len(cb.Pix))
	}
	for i := range ca.Pix {
		if ca.Pix[i] != cb.Pix[i] {
			t.Fatalf("the two entry points disagree at byte %d: %d and %d", i, ca.Pix[i], cb.Pix[i])
		}
	}
}

// TestADeeperPlateIsScaledToEightBits: a 12-bit component is not eight bits
// with the top nibble empty.
func TestADeeperPlateIsScaledToEightBits(t *testing.T) {
	h := &MainHeader{NumComps: 4, MCT: false, WaveletFilter: Wavelet53,
		BitDepth: []int{12, 12, 12, 12}, Signed: []bool{false, false, false, false}}
	// Stored full scale for 12 bits is +2047, which must reach 255.
	comps := planes(4, 1, 1, func(c, x, y int) int32 { return 2047 })
	d := &Decoder{header: h, jp2Meta: cmykMeta(JP2ColorCMYK), components: comps}
	cm, ok := d.picture().(*image.CMYK)
	if !ok {
		t.Fatal("not CMYK")
	}
	for c := range 4 {
		if cm.Pix[c] != 255 {
			t.Errorf("channel %d at full scale = %d, want 255", c, cm.Pix[c])
		}
	}
}
