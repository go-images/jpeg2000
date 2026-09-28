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
// It asks for a DIFFERENCE and not for a figure. An absolute threshold is a
// property of the BUILD, not of the code: the same decode that allocates 10.6
// bytes a pixel here allocates 47.6 under `go test -race`, and a threshold of
// 12 failed nine of ten CI lanes while the saving it was meant to detect was
// intact in every one of them. The gap between the two decodes is measured in
// the same build by the same binary, so it travels.
//
// The four-tile decode is what the one-tile decode is compared against, since
// it CANNOT adopt -- no tile of it covers a component. Measured both ways:
// with the hand-over the gap is 4.8 bytes a pixel here and 4.3 on CI; with the
// hand-over removed it is 0.8, because then both sides copy. Three separates
// them with room on each side, and is not a number this test can drift past
// while the copy is gone.
func TestOneTileHandsItsCoefficientsOverInsteadOfCopying(t *testing.T) {
	const n = 1024
	src := greySource(n)
	decode := func(b []byte) (image.Image, error) { return Decode(bytes.NewReader(b)) }
	plain := bytesPerPixel(t, encodeGrey(t, src, &EncodeOptions{Lossless: true}), n*n, decode)
	tiled := bytesPerPixel(t, encodeGrey(t, src, &EncodeOptions{
		Lossless: true, TileWidth: n / 2, TileHeight: n / 2}), n*n, decode)

	const gap = 3.0
	t.Logf("one tile %.2f bytes/pixel, four tiles %.2f, gap %.2f", plain, tiled, tiled-plain)
	if tiled-plain < gap {
		t.Errorf("a one-tile decode allocated %.2f bytes a pixel against %.2f for four tiles, "+
			"a gap of %.2f and not the %.1f an int32 plane is: the coefficients are being "+
			"copied rather than handed over", plain, tiled, tiled-plain, gap)
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
