package jpeg2000

import (
	"bytes"
	"image"
	"image/color"
	"testing"
)

// TestWhichTileGridsSurviveARoundTrip is a MAP, not a gate.
//
// Encoding with an explicit tile grid produces a codestream that both this
// package's decoder and OpenJPEG read wrongly -- arbitrated in both directions
// on two grid sizes, with a one-tile control that passes. The grids that work
// and the grids that do not are recorded here rather than described, because
// the rule behind them is NOT established: four theories were measured against
// this map and all four were refuted (partial tiles, origins that are powers of
// two, origins that are multiples of 2^levels, and subbands that sit off the
// code-block grid).
//
// It asserts only what is known: the grids in the first list are exact today
// and must stay exact. The second list is NOT asserted -- pinning a defect
// makes it permanent, and these are expected to become exact when the encoder
// is taught the tile's position.
func TestWhichTileGridsSurviveARoundTrip(t *testing.T) {
	const n = 256
	src := image.NewGray(image.Rect(0, 0, n, n))
	for y := range n {
		for x := range n {
			src.SetGray(x, y, color.Gray{Y: uint8((x*7 + y*3) % 256)})
		}
	}
	wrong := func(tile int) int {
		var buf bytes.Buffer
		if err := Encode(&buf, src, &EncodeOptions{
			Lossless: true, TileWidth: tile, TileHeight: tile}); err != nil {
			t.Fatalf("tile %d: encode: %v", tile, err)
		}
		img, err := Decode(bytes.NewReader(buf.Bytes()))
		if err != nil {
			t.Fatalf("tile %d: decode: %v", tile, err)
		}
		bad := 0
		for y := range n {
			for x := range n {
				g, _, _, _ := img.At(x, y).RGBA()
				if uint8(g>>8) != uint8((x*7+y*3)%256) {
					bad++
				}
			}
		}
		return bad
	}

	// Exact today. A change that breaks one of these has broken tiling
	// further, whatever it was meant to fix.
	for _, tile := range []int{0, 256, 300, 128, 64, 32, 160, 224} {
		if bad := wrong(tile); bad != 0 {
			t.Errorf("tile %d: %d of %d pixels wrong, and this grid was exact",
				tile, bad, n*n)
		}
	}

	// Known wrong, reported not asserted. The count is logged so that a fix
	// shows up as these going to zero rather than as a test nobody changed.
	for _, tile := range []int{96, 100, 120, 48, 80, 129, 200, 255} {
		t.Logf("tile %d: %d of %d pixels wrong (known, see the note in packet.go)",
			tile, wrong(tile), n*n)
	}
}
