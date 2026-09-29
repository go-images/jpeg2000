package jpeg2000

import (
	"github.com/ajroetker/go-highway/hwy/contrib/wavelet"
)

// Analyze2D_53 performs forward 2D 5/3 wavelet transform (lossless).
// Input: image samples in coeffs[y][x].
// Output: subbands arranged as [LL, LH; HL, HH] per level.
// width, height: dimensions of the full image.
// levels: number of decomposition levels.
//
// Per JPEG2000 standard (ITU-T T.800): forward transform applies vertical
// analysis first (columns), then horizontal analysis (rows). This is the
// opposite of the inverse transform which does horizontal then vertical.
func Analyze2D_53(coeffs [][]int32, width, height, levels int) {
	Analyze2D_53_At(coeffs, 0, 0, width, height, levels)
}

// Analyze2D_53_At is the forward 5/3 transform of a tile-component that begins
// at (tcx0, tcy0) on the reference grid and ends before (tcx1, tcy1).
//
// The POSITION is not decoration. Each lifting step alternates between even and
// odd samples, and which of the two a subband's first sample is depends on
// where the tile begins -- not on how wide it is. Synthesize2D_53_WithDims
// takes each resolution's X0/Y0 for exactly this, calling it cas; the forward
// transform used to assume every tile began at zero, so a tile that did not
// was analysed in one phase and synthesised in the other.
//
// Analyze2D_53 keeps the old shape and passes an origin of zero, which is what
// an untiled picture and every existing caller have.
func Analyze2D_53_At(coeffs [][]int32, tcx0, tcy0, tcx1, tcy1, levels int) {
	if levels < 1 {
		return
	}

	// Pre-allocate reusable buffers for the largest dimension.
	maxDim := max(tcx1-tcx0, tcy1-tcy0)
	maxHalf := (maxDim + 1) / 2
	low53 := make([]int32, maxHalf)
	high53 := make([]int32, maxHalf)
	col := make([]int32, max(tcy1-tcy0, 0))

	// Process from finest to coarsest level. At `level` the data is the
	// resolution whose origin is the tile's, shifted level-1 times: its
	// EXTENT is the difference of two ceilings and not the ceiling of a
	// difference, which are the same number only when the origin is zero.
	for level := 1; level <= levels; level++ {
		n := level - 1
		rx0, rx1 := ceilShift(tcx0, n), ceilShift(tcx1, n)
		ry0, ry1 := ceilShift(tcy0, n), ceilShift(tcy1, n)
		levelWidth, levelHeight := rx1-rx0, ry1-ry0
		if levelWidth <= 0 || levelHeight <= 0 {
			continue
		}
		casH, casV := rx0&1, ry0&1

		// Vertical analysis first (process columns)
		for x := range levelWidth {
			for y := range levelHeight {
				col[y] = coeffs[y][x]
			}
			wavelet.Analyze53(col[:levelHeight], casV, low53, high53)
			for y := range levelHeight {
				coeffs[y][x] = col[y]
			}
		}

		// Horizontal analysis second (process rows)
		for y := range levelHeight {
			wavelet.Analyze53(coeffs[y][:levelWidth], casH, low53, high53)
		}
	}
}

// Analyze2D_97 performs forward 2D 9/7 wavelet transform (lossy).
// Input: image samples in coeffs[y][x].
// Output: subbands arranged as [LL, LH; HL, HH] per level.
// width, height: dimensions of the full image.
// levels: number of decomposition levels.
//
// Per JPEG2000 standard (ITU-T T.800): forward transform applies vertical
// analysis first (columns), then horizontal analysis (rows). This is the
// opposite of the inverse transform which does horizontal then vertical.
func Analyze2D_97(coeffs [][]float64, width, height, levels int) {
	Analyze2D_97_At(coeffs, 0, 0, width, height, levels)
}

// Analyze2D_97_At is Analyze2D_53_At's twin for the irreversible filter: the
// same reason, the same arithmetic, the other wavelet.
func Analyze2D_97_At(coeffs [][]float64, tcx0, tcy0, tcx1, tcy1, levels int) {
	if levels < 1 {
		return
	}

	// Pre-allocate column buffer outside the loop.
	col := make([]float64, max(tcy1-tcy0, 0))

	// Process from finest to coarsest level
	for level := 1; level <= levels; level++ {
		n := level - 1
		rx0, rx1 := ceilShift(tcx0, n), ceilShift(tcx1, n)
		ry0, ry1 := ceilShift(tcy0, n), ceilShift(tcy1, n)
		levelWidth, levelHeight := rx1-rx0, ry1-ry0
		if levelWidth <= 0 || levelHeight <= 0 {
			continue
		}
		casH, casV := rx0&1, ry0&1

		// A column of ONE sample has no neighbour to lift against, so
		// analyze1D_97_cas leaves it alone -- but the INVERSE does not leave
		// it alone. Synthesize2D_97_WithDims hoists the subband scaling into
		// the copy that fills its column buffer, `v * K` or `v * 2/K` by which
		// half the row falls in, and that copy runs for a one-row resolution
		// like any other. Its own 1-D routine would not have scaled it; the
		// 2-D path is the one a real decode takes, and the forward has to
		// answer the path that is taken.
		//
		// Which factor it is, is decided by cas exactly as it is there: at
		// casV=0 the lone sample is the low band, at casV=1 the low band is
		// empty and it is the high one.
		//
		// The horizontal pass is deliberately NOT given the same treatment:
		// the inverse's horizontal pass delegates to synthesize1D_97_bufs,
		// which returns early for a single sample without scaling it. The
		// asymmetry is the decoder's, and the encoder has to match the decoder
		// rather than tidy it.
		oneRow := float64(0)
		if levelHeight == 1 {
			oneRow = lift97K
			if casV != 0 {
				oneRow = lift97TwoInvK
			}
		}

		// Vertical analysis first (process columns)
		for x := range levelWidth {
			for y := range levelHeight {
				col[y] = coeffs[y][x]
			}
			if oneRow != 0 {
				col[0] /= oneRow
			} else {
				analyze1D_97_cas(col[:levelHeight], casV)
			}
			for y := range levelHeight {
				coeffs[y][x] = col[y]
			}
		}

		// Horizontal analysis second (process rows)
		for y := range levelHeight {
			analyze1D_97_cas(coeffs[y][:levelWidth], casH)
		}
	}
}
