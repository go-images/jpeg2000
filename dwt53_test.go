package jpeg2000

import (
	"math/rand/v2"
	"slices"
	"testing"

	"github.com/go-simd/highway/hwy"
	"github.com/go-simd/highway/hwy/contrib/wavelet"
)

// synthesize2D53Highway is Synthesize2D_53_WithDims as the package ran it
// before the plain loop, through highway's wavelet kernels, kept unchanged
// as the ORACLE the loop is held to. Nothing ships it.
func synthesize2D53Highway(coeffs [][]int32, resDims []ResBounds) {
	levels := len(resDims) - 1
	if levels < 1 {
		return
	}
	maxDim := 0
	for _, rd := range resDims {
		maxDim = max(maxDim, rd.Width, rd.Height)
	}
	maxHalf := (maxDim + 1) / 2
	lanes := hwy.MaxLanes[int32]()
	low53 := make([]int32, maxHalf)
	high53 := make([]int32, maxHalf)
	col := make([]int32, maxDim)
	colBuf := make([]int32, maxDim*lanes)
	lowCols := make([]int32, maxHalf*lanes)
	highCols := make([]int32, maxHalf*lanes)

	for level := levels; level >= 1; level-- {
		resIdx := levels - level + 1
		levelWidth := resDims[resIdx].Width
		levelHeight := resDims[resIdx].Height
		casH := resDims[resIdx].X0 % 2
		casV := resDims[resIdx].Y0 % 2

		for y := range levelHeight {
			wavelet.Synthesize53(coeffs[y][:levelWidth], casH, low53, high53)
		}
		x := 0
		for ; x+lanes <= levelWidth; x += lanes {
			for y := range levelHeight {
				copy(colBuf[y*lanes:y*lanes+lanes], coeffs[y][x:x+lanes])
			}
			wavelet.Synthesize53Cols(colBuf[:levelHeight*lanes], levelHeight, casV, lowCols, highCols)
			for y := range levelHeight {
				copy(coeffs[y][x:x+lanes], colBuf[y*lanes:y*lanes+lanes])
			}
		}
		for ; x < levelWidth; x++ {
			for y := range levelHeight {
				col[y] = coeffs[y][x]
			}
			wavelet.Synthesize53(col[:levelHeight], casV, low53, high53)
			for y := range levelHeight {
				coeffs[y][x] = col[y]
			}
		}
	}
}

// resolutions returns the bounds of each resolution, coarsest first, of a
// tile of w by h at (x0, y0) decomposed levels times: T.800 B-15, a
// resolution's bounds are the tile's divided by 2^(levels-r), rounded up.
func resolutions(w, h, x0, y0, levels int) []ResBounds {
	ceilDiv := func(a, d int) int { return (a + d - 1) / d }
	var out []ResBounds
	for r := 0; r <= levels; r++ {
		d := 1 << (levels - r)
		rx0, ry0 := ceilDiv(x0, d), ceilDiv(y0, d)
		rx1, ry1 := ceilDiv(x0+w, d), ceilDiv(y0+h, d)
		out = append(out, ResBounds{Width: rx1 - rx0, Height: ry1 - ry0, X0: rx0, Y0: ry0})
	}
	return out
}

// TestSynthesize53MatchesHighway compares the plain loop with highway's
// kernels on every build. The shapes cover both parities in each direction
// (odd tile origins), sizes of one, two and three samples, widths that are not
// a multiple of any vector length, and several levels. The samples are signed
// and large, so the arithmetic shift meets negative sums: the lifting steps
// are defined with floor (T.800 F.3.8), and >> is floor only because it
// shifts arithmetically.
func TestSynthesize53MatchesHighway(t *testing.T) {
	rng := rand.New(rand.NewPCG(3, 4))
	shapes := []struct{ w, h, x0, y0, levels int }{
		{1, 1, 0, 0, 1}, {1, 1, 1, 1, 1}, {2, 1, 1, 0, 1}, {1, 2, 0, 1, 1},
		{3, 3, 0, 0, 1}, {3, 3, 1, 1, 1}, {17, 9, 0, 0, 2}, {17, 9, 1, 0, 2},
		{17, 9, 0, 1, 2}, {33, 31, 1, 1, 3}, {64, 64, 0, 0, 5}, {130, 7, 3, 5, 2},
		{7, 130, 5, 3, 2}, {100, 77, 9, 13, 4},
	}
	for _, s := range shapes {
		dims := resolutions(s.w, s.h, s.x0, s.y0, s.levels)
		want := int32Plane(s.w, s.h)
		for _, row := range want {
			for i := range row {
				row[i] = rng.Int32N(1<<20) - 1<<19
			}
		}
		got := int32Plane(s.w, s.h)
		for y := range want {
			copy(got[y], want[y])
		}
		synthesize2D53Highway(want, dims)
		Synthesize2D_53_WithDims(got, dims)
		if !slices.EqualFunc(got, want, slices.Equal[[]int32]) {
			t.Errorf("%dx%d at (%d,%d), %d levels: the loop and highway differ", s.w, s.h, s.x0, s.y0, s.levels)
		}
	}
}

// TestSynthesize53DoesNotAllocatePerSample is the defect itself as a test: a
// transform that allocates per vector makes allocations that grow with the
// picture. Highway's fallback made roughly a million for a picture this size.
// The loop allocates its buffers once per call. The bound is not an exact
// count, because Go keeps a small variable-sized buffer on the stack: the
// count is 3 at 32x32 and 4 at 512x512.
func TestSynthesize53DoesNotAllocatePerSample(t *testing.T) {
	dims := resolutions(512, 512, 0, 0, 3)
	c := int32Plane(512, 512)
	if n := testing.AllocsPerRun(5, func() { Synthesize2D_53_WithDims(c, dims) }); n > 4 {
		t.Fatalf("%v allocations for one 512x512 synthesis; the buffers are allocated once, so this is at most 4", n)
	}
}
