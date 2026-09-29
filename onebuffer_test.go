package jpeg2000

import (
	"bytes"
	"image"
	"image/color"
	"os"
	"runtime"
	"testing"
	"unsafe"
)

// TestTheFloatPlaneIsTheIntPlane is the merge itself: the 9/7 coefficients and
// the component plane are ONE allocation, seen twice.
//
// It compares ADDRESSES, because every other test here passes whether they are
// the same memory or two copies -- which is the point of the change and also
// what makes it invisible.
func TestTheFloatPlaneIsTheIntPlane(t *testing.T) {
	ints := make([]int32, 8)
	view := float32View(ints)
	if len(view) != len(ints) {
		t.Fatalf("the view is %d long for %d ints", len(view), len(ints))
	}
	if unsafe.Pointer(&view[0]) != unsafe.Pointer(&ints[0]) {
		t.Fatal("the view does not start where the ints do")
	}
	// A float written through one is readable as its bits through the other,
	// which is the whole arrangement: the synthesis writes floats and the
	// conversion reads them back from the same slots.
	view[3] = 1.5
	if got, want := ints[3], int32(0x3FC00000); got != want {
		t.Errorf("ints[3] = %#x after writing 1.5 through the view, want %#x", got, want)
	}
	// And the other way, which is what the in-place conversion does.
	ints[3] = 42
	if view[3] == 1.5 {
		t.Error("the view still reads 1.5 after the int slot was overwritten")
	}
	// An empty component has no first element to take the address of.
	if v := float32View(nil); v != nil {
		t.Errorf("an empty plane gave a view of %d", len(v))
	}
	if v := float32View([]int32{}); v != nil {
		t.Errorf("an empty slice gave a view of %d", len(v))
	}
}

// TestAFloatPlaneNeverCoexistsWithTheIntegerPath pins the property the merge
// RESTS ON, which is decided in another file.
//
// decodeSubband writes the integer row when inv.reversible and the float row
// otherwise. Sharing one buffer is safe only while those two cannot both apply,
// and that is `reversible = wavelet == Wavelet53 && quantStyle == 0` in
// subbandConstants -- somewhere this change does not touch, which is exactly
// why it is asserted here rather than assumed.
//
// Every quantisation style, with and without a tile override of each of the two
// markers that can change the answer.
func TestAFloatPlaneNeverCoexistsWithTheIntegerPath(t *testing.T) {
	for _, style := range []byte{0, 1, 2} {
		for _, tileCOD := range []bool{false, true} {
			for _, tileQCD := range []bool{false, true} {
				for _, wavelet := range []WaveletType{Wavelet53, Wavelet97} {
					h := &MainHeader{
						WaveletFilter: wavelet, QuantStyle: style, GuardBits: 2,
						OriginalExponents: []int{10, 10, 10, 10},
						BitDepth:          []int{8}, Signed: []bool{false}, NumComps: 1,
					}
					tile := &Tile{
						HasTileCOD: tileCOD, TileWaveletFilter: wavelet,
						HasTileQCD: tileQCD, TileQuantStyle: style,
						TileGuardBits: 2, TileExponents: []int{10, 10, 10, 10},
					}
					td := &TileDecoder{header: h, tile: tile}
					sb := &Subband{Type: SubbandLL, Width: 4, Height: 4}
					c := td.subbandConstants(sb, 0, 0)

					floatPlaneExists := td.getWaveletFilter() == Wavelet97
					if floatPlaneExists && c.reversible {
						t.Errorf("style %d, tileCOD %v, tileQCD %v, %v: a float plane is "+
							"allocated AND the integer path applies -- one buffer for both "+
							"would have them overwrite each other",
							style, tileCOD, tileQCD, wavelet)
					}
					// The control: the integer path must still be reachable, or
					// the check above is satisfied by a rule that never fires.
					if wavelet == Wavelet53 && style == 0 && !c.reversible {
						t.Errorf("style %d, tileCOD %v, tileQCD %v: the integer path is "+
							"unreachable, so the test above says nothing",
							style, tileCOD, tileQCD)
					}
				}
			}
		}
	}
}

// TestAMergedBufferDecodesWhatTwoDid is the picture, end to end: a 9/7
// codestream through the shared buffer must be what the committed fixture and a
// lossless round trip say it is.
func TestAMergedBufferDecodesWhatTwoDid(t *testing.T) {
	b, err := os.ReadFile("testdata/multiblock97_256x256.j2k")
	if err != nil {
		t.Fatal(err)
	}
	img, err := Decode(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	if bo := img.Bounds(); bo.Dx() != 256 || bo.Dy() != 256 {
		t.Fatalf("the 9/7 fixture decoded to %dx%d", bo.Dx(), bo.Dy())
	}
	var ink int
	for y := 0; y < 256; y++ {
		for x := 0; x < 256; x++ {
			r, _, _, _ := img.At(x, y).RGBA()
			if v := uint8(r >> 8); v != 0 && v != 255 {
				ink++
			}
		}
	}
	if ink == 0 {
		t.Fatal("the 9/7 fixture drew nothing, so it says nothing about the buffer")
	}

	// A colour 9/7 encode-decode, which takes the three-component path where
	// three shared buffers are live at once.
	src := image.NewRGBA(image.Rect(0, 0, 64, 64))
	for y := 0; y < 64; y++ {
		for x := 0; x < 64; x++ {
			src.SetRGBA(x, y, color.RGBA{R: uint8(x * 4), G: uint8(y * 4), B: 90, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := Encode(&buf, src, &EncodeOptions{Quality: 1.0}); err != nil {
		t.Fatal(err)
	}
	out, err := Decode(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	// Lossy, so not exact -- but a buffer read as the wrong type is not "a few
	// levels out", it is noise. Ten levels is far inside one and far outside
	// the other.
	var worst int
	for y := 0; y < 64; y++ {
		for x := 0; x < 64; x++ {
			gr, gg, gb, _ := out.At(x, y).RGBA()
			wr, wg, wb, _ := src.At(x, y).RGBA()
			for _, d := range []int{int(gr>>8) - int(wr>>8), int(gg>>8) - int(wg>>8), int(gb>>8) - int(wb>>8)} {
				if d < 0 {
					d = -d
				}
				if d > worst {
					worst = d
				}
			}
		}
	}
	t.Logf("worst channel error through the shared buffer: %d levels", worst)
	if worst > 10 {
		t.Errorf("worst channel error %d levels, want at most 10", worst)
	}
}

// TestA97DecodeDoesNotCostAPlaneMoreThanA53One asks the merge as a cost, at the
// DECODER, because every test above is satisfied by a float32View that nothing
// calls. Replacing the view with a fresh allocation at the one place it is used
// broke nothing until this existed.
//
// It asks for a DIFFERENCE and not a figure: an absolute bytes-per-pixel number
// is a property of the build, and one written here failed nine CI lanes earlier
// in this package's history while the saving it tested was intact in all of
// them. The two decodes are of the same picture, in the same build, in the same
// process -- one 5/3, which never had a float plane, and one 9/7, which did.
//
// Measured both ways on a 512x512 grey picture: the gap is 1.35 bytes a pixel
// with the merge and 5.35 without, a difference of 4.00 -- one float32 plane,
// exactly. Three separates them with more than a byte a pixel on each side.
func TestA97DecodeDoesNotCostAPlaneMoreThanA53One(t *testing.T) {
	const n = 512
	src := image.NewGray(image.Rect(0, 0, n, n))
	for y := 0; y < n; y++ {
		for x := 0; x < n; x++ {
			src.SetGray(x, y, color.Gray{Y: uint8((x*7 + y*3) % 256)})
		}
	}
	encode := func(o *EncodeOptions) []byte {
		var b bytes.Buffer
		if err := Encode(&b, src, o); err != nil {
			t.Fatal(err)
		}
		return b.Bytes()
	}
	cost := func(data []byte) float64 {
		if _, err := Decode(bytes.NewReader(data)); err != nil { // warm
			t.Fatal(err)
		}
		var before, after runtime.MemStats
		runtime.GC()
		runtime.ReadMemStats(&before)
		if _, err := Decode(bytes.NewReader(data)); err != nil {
			t.Fatal(err)
		}
		runtime.ReadMemStats(&after)
		return float64(after.TotalAlloc-before.TotalAlloc) / float64(n*n)
	}
	reversible := cost(encode(&EncodeOptions{Lossless: true}))
	irreversible := cost(encode(&EncodeOptions{Quality: 1.0}))

	gap := irreversible - reversible
	t.Logf("5/3 %.2f bytes/pixel, 9/7 %.2f, gap %.2f", reversible, irreversible, gap)
	const allowed = 3.0
	if gap > allowed {
		t.Errorf("a 9/7 decode cost %.2f bytes a pixel against %.2f for a 5/3 one, a gap of "+
			"%.2f and not the %.1f a shared buffer gives: the float coefficients are in a "+
			"plane of their own", irreversible, reversible, gap, allowed)
	}
}
