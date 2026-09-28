package jpeg2000

import (
	"bytes"
	"image"
	"image/color"
	"runtime"
	"testing"
)

// greySource is a picture with no flat runs, so a plane composed from the wrong
// rows or the wrong offset shows rather than hides.
func greySource(n int) *image.Gray {
	src := image.NewGray(image.Rect(0, 0, n, n))
	for y := 0; y < n; y++ {
		for x := 0; x < n; x++ {
			src.SetGray(x, y, color.Gray{Y: uint8((x*7 + y*3) % 256)})
		}
	}
	return src
}

func encodeGrey(t *testing.T, src *image.Gray, opts *EncodeOptions) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := Encode(&buf, src, opts); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func bytesPerPixel(t *testing.T, data []byte, px int, decode func([]byte) (image.Image, error)) float64 {
	t.Helper()
	if _, err := decode(data); err != nil { // warm: first-call fixed costs are not the subject
		t.Fatal(err)
	}
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	if _, err := decode(data); err != nil {
		t.Fatal(err)
	}
	runtime.ReadMemStats(&after)
	return float64(after.TotalAlloc-before.TotalAlloc) / float64(px)
}

// TestOneTileHandsItsCoefficientsOverInsteadOfCopying is the change stated as a
// cost. A tile that covers its whole component used to have every row copied
// onto a buffer of the same shape -- the identity, at four bytes a sample --
// and the two buffers were alive at the same time.
//
// It is asked in bytes per pixel because that is what the saving IS: one int32
// plane. Measured, the two paths sit at 10.6 and 14.6 bytes a pixel on the same
// picture, a difference of 4.03 -- so a threshold of 12 separates them with a
// margin of more than a byte a pixel on each side, and is not a number this
// test can drift past while the copy is gone.
//
// The multi-tile decode beside it is the control: it CANNOT adopt, it must stay
// above the threshold, and a test that only measured the fast path would pass
// just as well if the measurement were meaningless.
func TestOneTileHandsItsCoefficientsOverInsteadOfCopying(t *testing.T) {
	const n = 1024
	src := greySource(n)
	plain := bytesPerPixel(t, encodeGrey(t, src, &EncodeOptions{Lossless: true}), n*n,
		func(b []byte) (image.Image, error) { return Decode(bytes.NewReader(b)) })
	tiled := bytesPerPixel(t, encodeGrey(t, src, &EncodeOptions{
		Lossless: true, TileWidth: n / 2, TileHeight: n / 2}), n*n,
		func(b []byte) (image.Image, error) { return Decode(bytes.NewReader(b)) })

	t.Logf("one tile %.2f bytes/pixel, four tiles %.2f", plain, tiled)
	const threshold = 12.0
	if plain >= threshold {
		t.Errorf("a one-tile decode allocated %.2f bytes a pixel, want below %.1f: "+
			"the coefficients are being copied rather than handed over", plain, threshold)
	}
	if tiled <= plain {
		t.Errorf("four tiles allocated %.2f bytes a pixel and one tile %.2f: "+
			"the one-tile figure is not measuring what it claims", tiled, plain)
	}
}

// TestEveryTilingReachesTheSamePicture is the safety the adoption needs. It
// takes a path that hands a buffer over, and the shapes beside it -- more than
// one tile, a reduced decode -- must still go through the composition loop and
// reach the same pixels.
func TestEveryTilingReachesTheSamePicture(t *testing.T) {
	const n = 256
	src := greySource(n)
	want, err := Decode(bytes.NewReader(encodeGrey(t, src, &EncodeOptions{Lossless: true})))
	if err != nil {
		t.Fatal(err)
	}
	g, ok := want.(*image.Gray)
	if !ok {
		t.Fatalf("a one-component decode gave %T", want)
	}
	// The adopted plane is the picture the encoder was given, which is the only
	// check that says the hand-over handed over the RIGHT buffer.
	for y := 0; y < n; y++ {
		for x := 0; x < n; x++ {
			if got, exp := g.GrayAt(x, y).Y, src.GrayAt(x, y).Y; got != exp {
				t.Fatalf("adopted plane at (%d,%d) is %d, want %d", x, y, got, exp)
			}
		}
	}

	for _, tc := range []struct {
		name string
		enc  *EncodeOptions
		dec  DecodeOptions
		w    int
	}{
		{"four tiles", &EncodeOptions{Lossless: true, TileWidth: n / 2, TileHeight: n / 2}, DecodeOptions{}, n},
		// A PARTIAL tile -- 100x100 over 256 -- is deliberately absent. A
		// lossless round trip through one loses 55 245 of 65 536 pixels, on
		// main as well as here, so it is a defect this change neither causes
		// nor repairs. Asserting the broken count would pin it; asserting the
		// right one would fail for a reason that has nothing to do with the
		// hand-over being tested. It is reported separately.
		{"reduced by one resolution", &EncodeOptions{Lossless: true}, DecodeOptions{Reduce: 1}, n / 2},
	} {
		img, err := DecodeWithOptions(bytes.NewReader(encodeGrey(t, src, tc.enc)), tc.dec)
		if err != nil {
			t.Errorf("%s: %v", tc.name, err)
			continue
		}
		b := img.Bounds()
		if b.Dx() != tc.w || b.Dy() != tc.w {
			t.Errorf("%s: %dx%d, want %dx%d", tc.name, b.Dx(), b.Dy(), tc.w, tc.w)
			continue
		}
		if tc.w != n {
			continue // a reduced decode is a different picture, not this one
		}
		for y := 0; y < n; y++ {
			for x := 0; x < n; x++ {
				gr, _, _, _ := img.At(x, y).RGBA()
				if got, exp := uint8(gr>>8), g.GrayAt(x, y).Y; got != exp {
					t.Fatalf("%s at (%d,%d): %d, want %d", tc.name, x, y, got, exp)
				}
			}
		}
	}
}

// TestCoversWholeComponentInsistsOnEachThingSeparately gives every clause a
// case where it is the ONLY one that can refuse. Asked through the decoder they
// mask each other -- a tile that starts late is also a tile that ends early --
// and two of them cannot be reached from the encoder at all, which never writes
// a non-zero image origin. Each of the seven mutations that removed a clause
// survived the whole suite before this test existed.
func TestCoversWholeComponentInsistsOnEachThingSeparately(t *testing.T) {
	whole := func(w, h int) [][]int32 {
		rows := make([][]int32, h)
		for y := range rows {
			rows[y] = make([]int32, w)
		}
		return rows
	}
	ragged := whole(4, 3)
	ragged[2] = ragged[2][:3]

	for _, tc := range []struct {
		why        string
		rows       [][]int32
		offX, offY int
		w, h       int
		want       bool
	}{
		{"the tile IS the component", whole(4, 3), 0, 0, 4, 3, true},
		{"it starts to the right of the origin", whole(4, 3), 1, 0, 4, 3, false},
		{"it starts below the origin", whole(4, 3), 0, 1, 4, 3, false},
		{"it is short of the component's height", whole(4, 2), 0, 0, 4, 3, false},
		{"it is taller than the component", whole(4, 4), 0, 0, 4, 3, false},
		{"its rows are not the component's width", whole(3, 3), 0, 0, 4, 3, false},
		{"one row is short of the others", ragged, 0, 0, 4, 3, false},
		{"the component has no width", whole(0, 3), 0, 0, 0, 3, false},
		{"the component has no height", nil, 0, 0, 4, 0, false},
	} {
		if got := coversWholeComponent(tc.rows, tc.offX, tc.offY, tc.w, tc.h); got != tc.want {
			t.Errorf("%s: %v, want %v", tc.why, got, tc.want)
		}
	}
}

// TestAComponentNoTileWroteIsStillAComponent covers the floor under the lazy
// allocation. Nothing downstream asks whether a plane was written -- it indexes
// rows -- so a component no tile reached must still be rows of zeroes rather
// than nil. Before the allocation moved out of decodeTiles it could not happen;
// now it can, and removing the backfill broke no other test.
func TestAComponentNoTileWroteIsStillAComponent(t *testing.T) {
	d := &Decoder{header: &MainHeader{
		Width: 8, Height: 4, NumComps: 2,
		BitDepth: []int{8, 8}, Signed: []bool{false, false},
		XRsiz: []int{1, 1}, YRsiz: []int{1, 1},
	}}
	if err := d.decodeTiles(); err != nil {
		t.Fatal(err)
	}
	for c, plane := range d.components {
		if len(plane) != 4 {
			t.Fatalf("component %d has %d rows, want 4", c, len(plane))
		}
		for y, row := range plane {
			if len(row) != 8 {
				t.Fatalf("component %d row %d is %d wide, want 8", c, y, len(row))
			}
		}
	}
}
