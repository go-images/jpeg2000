package jpeg2000

import (
	"bytes"
	"image"
	"image/color"
	"os"
	"testing"
)

// plane makes one component of w by h from a function of the position.
func plane(w, h int, f func(x, y int) int32) [][]int32 {
	out := make([][]int32, h)
	for y := range out {
		out[y] = make([]int32, w)
		for x := range out[y] {
			out[y][x] = f(x, y)
		}
	}
	return out
}

// TestAGreyPictureIsTheSamePixelsAsTheFourByteOne.
//
// greyPicture is a fast path beside convertToRGBA, not a second opinion: for every
// sample it must write the byte the four-byte path writes in that pixel's red
// channel. The samples below span the whole signed range a DC-shifted eight-bit
// component can hold, including the values that clamp at both ends.
func TestAGreyPictureIsTheSamePixelsAsTheFourByteOne(t *testing.T) {
	const w, h = 40, 13
	comps := [][][]int32{plane(w, h, func(x, y int) int32 {
		// -200 .. +200 across the picture, so both clamps are exercised.
		return int32((x+y*w)%401 - 200)
	})}
	depths, signed := []int{8}, []bool{false}

	grey := greyPicture(comps, w, h, depths, signed)
	if grey == nil {
		t.Fatal("the plain shape was refused")
	}
	rgba := convertToRGBA(comps, w, h, depths, signed, false)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			g := grey.Pix[y*grey.Stride+x]
			r := rgba.Pix[rgba.PixOffset(x, y)]
			if g != r {
				t.Fatalf("(%d,%d) sample %d: grey %d, four-byte %d",
					x, y, comps[0][y][x], g, r)
			}
		}
	}
}

// TestGreyPictureRefusesWhatItCannotAnswerFor.
//
// Every shape it declines is one convertToRGBA handles: more than one component,
// a depth other than eight, a signed component, and rows that do not match the
// picture's size -- which is what a subsampled component looks like from here.
// Answering for any of them would be a different picture, not a faster one.
func TestGreyPictureRefusesWhatItCannotAnswerFor(t *testing.T) {
	const w, h = 8, 4
	ok := [][][]int32{plane(w, h, func(x, y int) int32 { return int32(x) })}
	for _, tc := range []struct {
		name   string
		comps  [][][]int32
		depths []int
		signed []bool
	}{
		{"three components", [][][]int32{ok[0], ok[0], ok[0]}, []int{8, 8, 8}, []bool{false, false, false}},
		{"twelve bits", ok, []int{12}, []bool{false}},
		{"signed", ok, []int{8}, []bool{true}},
		{"a short row", [][][]int32{{make([]int32, w), make([]int32, w-1), make([]int32, w), make([]int32, w)}}, []int{8}, []bool{false}},
		{"too few rows", [][][]int32{plane(w, h-1, func(x, y int) int32 { return 0 })}, []int{8}, []bool{false}},
		{"no components", [][][]int32{}, []int{8}, []bool{false}},
	} {
		if g := greyPicture(tc.comps, w, h, tc.depths, tc.signed); g != nil {
			t.Errorf("%s: answered %dx%d instead of declining", tc.name, g.Bounds().Dx(), g.Bounds().Dy())
		}
	}
	// And a zero-sized picture, which image.NewGray would accept and nothing wants.
	if g := greyPicture(ok, 0, h, []int{8}, []bool{false}); g != nil {
		t.Error("a zero width was answered for")
	}
}

// TestAOneComponentDecodeComesBackAsGrey, through the public entry point, because
// the type is what a caller sees and what saves it three bytes a pixel.
func TestAOneComponentDecodeComesBackAsGrey(t *testing.T) {
	b, err := os.ReadFile("testdata/multiblock_256x256.j2k")
	if err != nil {
		t.Fatal(err)
	}
	// BOTH entry points. They have a decode tail each, and a fast path added to
	// one of them is a fast path half the callers never reach: removing it from
	// DecodeWithUpsampling's tail alone was a mutation no test could see.
	for _, tc := range []struct {
		name   string
		decode func([]byte) (image.Image, error)
	}{
		{"Decode", func(b []byte) (image.Image, error) { return Decode(bytes.NewReader(b)) }},
		{"DecodeWithUpsampling", func(b []byte) (image.Image, error) { return DecodeWithUpsampling(bytes.NewReader(b)) }},
	} {
		img, err := tc.decode(b)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if _, ok := img.(*image.Gray); !ok {
			t.Errorf("%s: a one-component codestream decoded to %T, want *image.Gray", tc.name, img)
		}
	}
}

// TestDecodeAgreesWithDecodeConfigAboutTheColourModel is the defect stated
// exactly: DecodeConfig answered color.GrayModel for a one-component
// codestream, and Decode handed back an *image.RGBA. A caller that sized a
// buffer from the config and filled it from the decode was sizing it for a
// quarter of what arrived.
//
// Both arities are asked, because a version of this that answered GrayModel to
// everything would satisfy the one-component half alone.
func TestDecodeAgreesWithDecodeConfigAboutTheColourModel(t *testing.T) {
	grey, err := os.ReadFile("testdata/multiblock_256x256.j2k")
	if err != nil {
		t.Fatal(err)
	}

	// Three components, encoded here rather than committed: what matters is the
	// arity, and the encoder is the package's own account of it.
	colour := image.NewRGBA(image.Rect(0, 0, 32, 32))
	for y := 0; y < 32; y++ {
		for x := 0; x < 32; x++ {
			colour.SetRGBA(x, y, color.RGBA{R: uint8(x * 8), G: uint8(y * 8), B: 40, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := Encode(&buf, colour, &EncodeOptions{Lossless: true}); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name string
		data []byte
		want color.Model
	}{
		{"one component", grey, color.GrayModel},
		{"three components", buf.Bytes(), color.RGBAModel},
	} {
		cfg, err := DecodeConfig(bytes.NewReader(tc.data))
		if err != nil {
			t.Fatalf("%s: DecodeConfig: %v", tc.name, err)
		}
		if cfg.ColorModel != tc.want {
			t.Errorf("%s: DecodeConfig promised %T, want %T", tc.name, cfg.ColorModel, tc.want)
		}
		img, err := Decode(bytes.NewReader(tc.data))
		if err != nil {
			t.Fatalf("%s: Decode: %v", tc.name, err)
		}
		if img.ColorModel() != cfg.ColorModel {
			t.Errorf("%s: DecodeConfig promised %T and Decode gave %T (%T)",
				tc.name, cfg.ColorModel, img.ColorModel(), img)
		}
	}
}
