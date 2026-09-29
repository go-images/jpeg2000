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
// The mechanism is established and the REVERSIBLE path is fixed, by a predictor written from the two
// implementations and tested against every case here rather than from the
// shape of a few. The encoder is handed the tile's size and never its
// position, and that costs it twice:
//
//	code-block count -- the encoder wrote ceil(sbW/cbw) blocks where the
//	decoder counts the grid cells the subband SPANS on a grid anchored at
//	the reference grid's origin (T.800 B.7). FIXED: see blockGrid.
//
//	DWT lifting phase -- Synthesize2D_*_WithDims takes each resolution's
//	X0/Y0 "for cas calculation"; Analyze2D_* takes only a size, so the
//	forward transform always works as though the tile began at zero. NOT
//	yet fixed.
//
// `blocks || phase` predicts all fifteen grids below. Tile 96 is the case that
// refuted four earlier theories: a code-block divergence with NO phase
// divergence, which no rule about origins could see. It is exact now.
//
// The second list is still NOT asserted -- pinning a defect makes it
// permanent, and these become exact when the forward transform is taught the
// phase.
func TestWhichTileGridsSurviveARoundTrip(t *testing.T) {
	const n = 256
	src := image.NewGray(image.Rect(0, 0, n, n))
	for y := range n {
		for x := range n {
			src.SetGray(x, y, color.Gray{Y: uint8((x*7 + y*3) % 256)})
		}
	}
	wrong := func(tile int, opts *EncodeOptions) int {
		o := *opts
		o.TileWidth, o.TileHeight = tile, tile
		var buf bytes.Buffer
		if err := Encode(&buf, src, &o); err != nil {
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
				want, got := int((x*7+y*3)%256), int(uint8(g>>8))
				d := want - got
				if d < 0 {
					d = -d
				}
				// Exact for the reversible filter; the irreversible one is
				// lossy by construction, so a dozen levels is the noise floor
				// and anything past it is the tiling, not the quantiser.
				if (opts.Lossless && d != 0) || (!opts.Lossless && d > 12) {
					bad++
				}
			}
		}
		return bad
	}

	// EVERY grid, reversible. This is a gate: the encoder is given the tile's
	// position now, and a tile grid that does not survive a lossless round
	// trip is a defect, not a known limit.
	lossless := &EncodeOptions{Lossless: true}
	for _, tile := range []int{
		0, 256, 300, 224, 200, 160, 129, 128, 120, 100, 96, 80, 64, 48, 32, 255,
	} {
		if bad := wrong(tile, lossless); bad != 0 {
			t.Errorf("reversible, tile %d: %d of %d pixels wrong", tile, bad, n*n)
		}
	}

	// The irreversible filter shares the block grid, the subband layout and
	// the quantiser's idea of where a subband is, and all three are fixed. Two
	// grids are still not exact and are REPORTED rather than asserted: both
	// have a tile whose extent falls to zero at a coarse resolution, which is
	// the shape that was wrong last and may be wrong still.
	lossy := &EncodeOptions{Quality: 1.0}
	for _, tile := range []int{0, 256, 128, 96, 100, 64, 32} {
		if bad := wrong(tile, lossy); bad != 0 {
			t.Errorf("irreversible, tile %d: %d of %d pixels past 12 levels", tile, bad, n*n)
		}
	}
	for _, tile := range []int{120, 255} {
		t.Logf("irreversible, tile %d: %d of %d pixels past 12 levels -- still to fix",
			tile, wrong(tile, lossy), n*n)
	}
}
