package jpeg2000

import (
	"math"

	"github.com/go-simd/highway/hwy/contrib/wavelet"
)

// WaveletType indicates the wavelet filter used
type WaveletType int

const (
	Wavelet53 WaveletType = iota // 5/3 reversible (lossless)
	Wavelet97                    // 9/7 irreversible (lossy)
)

// Lifting coefficients for 9/7 irreversible filter (ITU-T T.800 Table F.4)
const (
	// Untyped, so that the same names serve the float32 synthesis and the
	// float64 analysis: a typed constant would force one of the two to
	// convert, and a conversion written at the use is a place to get it wrong.
	lift97Alpha = -1.586134342059924
	lift97Beta  = -0.052980118572961
	lift97Gamma = 0.882911075530934
	lift97Delta = 0.443506852043971
	lift97K     = 1.230174104914001
	// BUG_WEIRD_TWO_INVK: OpenJPEG uses 2/K instead of 1/K for high-pass scaling,
	// compensated by using gain=0 for all subbands in 9/7 decode (see tcd.c).
	// This avoids numerical issues when n=1 DWT levels cause no-op pass-throughs
	// that would leave standard gain factors uncompensated.
	lift97TwoInvK = 2.0 / 1.230174104914001
)

// dwtBufs97 holds reusable working buffers for 9/7 wavelet transforms.
// Allocated once per 2D function call, reused across all rows/columns.
type dwtBufs97 struct {
	low, high []float32
}

// ensure grows the internal buffers to accommodate a signal of length n.
func (b *dwtBufs97) ensure(n int) {
	half := (n + 1) / 2
	if cap(b.low) < half {
		b.low = make([]float32, half)
	}
	if cap(b.high) < half {
		b.high = make([]float32, half)
	}
}

// The inverse 5/3 transform is plain Go, not a highway call, for the reason
// the RCT is (see applyRCT). Without GOEXPERIMENT=simd, which nothing in the
// fleet sets, highway's 5/3 lifting on amd64 runs its generic Vec[T]. That
// type allocates in every Add, Sub and ShiftRight. Decoding one lossless
// 1920x1080 RGB picture made 14.55 million allocations on amd64, against
// 93 891 on arm64, and 97% of them came from this transform.
//
// The arithmetic is ITU-T T.800 F.3.8 in OpenJPEG's form (opj_dwt_decode_1_):
// samples [low | high] on entry, interleaved on exit, with every neighbour
// index CLAMPED into its subband. Clamping is the whole symmetric extension,
// and it reproduces each boundary case highway special-cases.
//
//	update   low[i]  -= (high[a] + high[b] + 2) >> 2   a, b = i-1, i  (cas 0)  or  i, i+1  (cas 1)
//	predict  high[i] += (low[a]  + low[b])      >> 1   a, b = i, i+1  (cas 0)  or  i-1, i  (cas 1)
//
// dwt53_test.go holds this to highway's result, sample for sample.

// split53 returns how many low and high samples a signal of n has.
func split53(n, cas int) (sn, dn int) {
	if cas == 0 {
		return (n + 1) / 2, n / 2
	}
	return n / 2, (n + 1) / 2
}

// clampIndex keeps i in [0, n-1].
func clampIndex(i, n int) int {
	if i < 0 {
		return 0
	}
	if i >= n {
		return n - 1
	}
	return i
}

// synthesize1D_53 performs the 5/3 inverse transform using pre-allocated
// buffers; low and high each need room for (len(data)+1)/2 samples.
func synthesize1D_53(data []int32, low, high []int32, cas int) {
	n := len(data)
	if n <= 1 {
		if n == 1 && cas == 1 {
			data[0] /= 2
		}
		return
	}
	sn, dn := split53(n, cas)
	low, high = low[:sn], high[:dn]
	copy(low, data[:sn])
	copy(high, data[sn:n])
	for i := range low {
		a, b := i-1+cas, i+cas
		low[i] -= (high[clampIndex(a, dn)] + high[clampIndex(b, dn)] + 2) >> 2
	}
	for i := range high {
		a, b := i-cas, i+1-cas
		high[i] += (low[clampIndex(a, sn)] + low[clampIndex(b, sn)]) >> 1
	}
	if cas == 0 {
		for i, v := range low {
			data[2*i] = v
		}
		for i, v := range high {
			data[2*i+1] = v
		}
		return
	}
	for i, v := range high {
		data[2*i] = v
	}
	for i, v := range low {
		data[2*i+1] = v
	}
}

// synthesizeRows53 is the vertical 5/3 pass done a whole ROW at a time:
// rows [0, sn) of coeffs are the low subband and [sn, n) the high one, and
// each lifting step adds one row to another. That reads memory in the order
// it is laid out, where a column at a time reads one value per row.
//
// The lifting needs no copy: each step writes one subband and reads the
// other, so it runs on the rows where they are. What remains is the
// interleave, which only puts rows in another order. That is done by
// following the permutation's cycles with one spare row (spare, width
// samples) and n marks (seen). A first version copied the whole plane into a
// scratch buffer instead, and that cost 24.6 MB on a 1920x1080 RGB decode.
func synthesizeRows53(coeffs [][]int32, width, n, cas int, spare []int32, seen []bool) {
	if n <= 1 {
		if n == 1 && cas == 1 {
			for x := range coeffs[0][:width] {
				coeffs[0][x] /= 2
			}
		}
		return
	}
	sn, dn := split53(n, cas)
	low := func(i int) []int32 { return coeffs[i][:width] }
	high := func(i int) []int32 { return coeffs[sn+i][:width] }
	for i := range sn {
		l := low(i)
		ha, hb := high(clampIndex(i-1+cas, dn)), high(clampIndex(i+cas, dn))
		for x := range l {
			l[x] -= (ha[x] + hb[x] + 2) >> 2
		}
	}
	for i := range dn {
		h := high(i)
		la, lb := low(clampIndex(i-cas, sn)), low(clampIndex(i+1-cas, sn))
		for x := range h {
			h[x] += (la[x] + lb[x]) >> 1
		}
	}

	// Output row d comes from low row (d-cas)/2 when d has the parity of cas,
	// and from high row (d-1+cas)/2 otherwise.
	from := func(d int) int {
		if (d-cas)%2 == 0 {
			return (d - cas) / 2
		}
		return sn + (d-1+cas)/2
	}
	seen = seen[:n]
	clear(seen)
	spare = spare[:width]
	for start := range n {
		if seen[start] {
			continue
		}
		seen[start] = true
		if from(start) == start {
			continue
		}
		copy(spare, coeffs[start][:width])
		d := start
		for {
			s := from(d)
			if s == start {
				copy(coeffs[d][:width], spare)
				break
			}
			copy(coeffs[d][:width], coeffs[s][:width])
			seen[s] = true
			d = s
		}
	}
}

// synthesize2D53 runs the levels coarsest first: rows, then columns, for
// each resolution in turn.
func synthesize2D53(coeffs [][]int32, levels []ResBounds) {
	maxW, maxH := 0, 0
	for _, rd := range levels {
		maxW, maxH = max(maxW, rd.Width), max(maxH, rd.Height)
	}
	half := (max(maxW, maxH) + 1) / 2
	low, high := make([]int32, half), make([]int32, half)
	spare, seen := make([]int32, maxW), make([]bool, maxH)
	for _, rd := range levels {
		casH, casV := rd.X0%2, rd.Y0%2
		for y := range rd.Height {
			synthesize1D_53(coeffs[y][:rd.Width], low, high, casH)
		}
		synthesizeRows53(coeffs, rd.Width, rd.Height, casV, spare, seen)
	}
}

// synthesize1D_97 performs in-place 1D inverse 9/7 wavelet transform
// Input: coefficients [L0, L1, ..., H0, H1, ...] where first half is low-pass, second half is high-pass
// Output: reconstructed signal in same array
func synthesize1D_97(data []float32) {
	synthesize1D_97_cas(data, 0)
}

// synthesize1D_97_cas performs 1D inverse 9/7 transform with parity control
// cas=0: first output sample is at even position (standard case)
// cas=1: first output sample is at odd position (for tiles starting at odd coordinates)
// Uses float32 SIMD primitives for lifting, scaling, and interleaving while
// preserving JPEG2000's non-standard 2/K high-pass scaling (BUG_WEIRD_TWO_INVK).
func synthesize1D_97_cas(data []float32, cas int) {
	n := len(data)
	if n <= 1 {
		return
	}
	sn := (n + 1) / 2
	dn := n / 2
	if cas != 0 {
		sn, dn = dn, sn
	}
	low := make([]float32, sn)
	high := make([]float32, dn)
	doSynthesize97(data, low, high, cas)
}

// synthesize1D_97_bufs performs the 9/7 inverse transform using pre-allocated buffers.
// The caller must ensure bufs has been sized via bufs.ensure(len(data)).
func synthesize1D_97_bufs(data []float32, bufs *dwtBufs97, cas int) {
	n := len(data)
	if n <= 1 {
		return
	}
	sn := (n + 1) / 2
	dn := n / 2
	if cas != 0 {
		sn, dn = dn, sn
	}
	doSynthesize97(data, bufs.low[:sn], bufs.high[:dn], cas)
}

// lift97 applies the four inverse 9/7 lifting steps to two subbands that have
// already been scaled. low and high must not overlap; each step writes one and
// reads the other.
//
// For update steps (target=low) the phase is 1-cas; for predict steps
// (target=high) it is cas.
func lift97(low []float32, sn int, high []float32, dn int, cas int) {
	wavelet.LiftStep97(low, sn, high, dn, lift97Delta, 1-cas)
	wavelet.LiftStep97(high, dn, low, sn, lift97Gamma, cas)
	wavelet.LiftStep97(low, sn, high, dn, lift97Beta, 1-cas)
	wavelet.LiftStep97(high, dn, low, sn, lift97Alpha, cas)
}

// doSynthesize97 is the core 9/7 inverse transform implementation.
// Operates entirely on float32 -- the precision the reference uses, where a
// tile-component buffer is reinterpreted as OPJ_FLOAT32 for 9/7 (openjpeg
// dwt.c) -- with SIMD-accelerated lifting and interleaving.
// low and high are separate buffers (not aliases of data), so Interleave can
// write directly into data without an intermediate out buffer.
//
// The inverse scaling is applied AS THE SUBBANDS ARE READ OUT rather than by a
// pass of its own: a copy followed by `*= K` is the same single multiply as
// `= data[i] * K`, so this is one pass over the data where it used to be two,
// bit for bit. Profiled on the heaviest page of the corpus, the two copies alone
// were 0.30s of runtime.memmove -- 5.6% of the page -- against 0.45s for all the
// lifting arithmetic.
//
// Scaling per OpenJPEG's BUG_WEIRD_TWO_INVK approach: low-pass *= K, high-pass
// *= 2/K rather than the standard 1/K.
func doSynthesize97(data, low, high []float32, cas int) {
	sn := len(low)
	dn := len(high)

	for i := range low {
		low[i] = data[i] * lift97K
	}
	for i := range high {
		high[i] = data[sn+i] * lift97TwoInvK
	}

	lift97(low, sn, high, dn, cas)

	// Interleave directly into data (safe: low/high are separate copies)
	wavelet.Interleave(data, low, sn, high, dn, cas)
}

// Synthesize2D_53 performs full 2D inverse 5/3 wavelet transform
// coeffs: coefficient array organized as [LL, LH; HL, HH] subbands per level
// JPEG2000 standard: forward is V→H (columns then rows), inverse is H→V (rows then columns)
// width, height: dimensions of the full image
// levels: number of decomposition levels
func Synthesize2D_53(coeffs [][]int32, width, height, levels int) {
	if levels < 1 {
		return
	}
	dims := make([]ResBounds, 0, levels)
	for level := levels; level >= 1; level-- {
		dims = append(dims, ResBounds{
			Width:  (width + (1 << (level - 1)) - 1) >> (level - 1),
			Height: (height + (1 << (level - 1)) - 1) >> (level - 1),
		})
	}
	synthesize2D53(coeffs, dims)
}

// ResBounds holds resolution-level bounds for DWT synthesis
// X0, Y0 are the image-space origin (trX0, trY0) used for parity calculation
type ResBounds struct {
	Width, Height int
	X0, Y0        int
}

// Synthesize2D_53_WithDims performs 2D inverse 5/3 wavelet transform
// using explicit resolution dimensions instead of computing them.
// resDims[i] contains bounds for resolution level i (0=coarsest)
// The X0/Y0 values are used to compute DWT parity (cas) for tiles at odd positions.
func Synthesize2D_53_WithDims(coeffs [][]int32, resDims []ResBounds) {
	if len(resDims) < 2 {
		return
	}
	// resDims[0] is the coarsest resolution, which has nothing to undo; each
	// later one is synthesised from it in turn.
	synthesize2D53(coeffs, resDims[1:])
}

// Synthesize2D_97 performs full 2D inverse 9/7 wavelet transform
// coeffs: coefficient array organized as [LL, LH; HL, HH] subbands per level
// JPEG2000 standard: forward is V→H (columns then rows), inverse is H→V (rows then columns)
// width, height: dimensions of the full image
// levels: number of decomposition levels
// colBlock is how many columns the vertical pass of the 9/7 synthesis takes at
// a time. A column of a [][]float32 is one value out of each row, so taking
// columns one at a time reads a whole cache line per row and uses four bytes
// of it.
//
// Eight was chosen when the coefficients were float64 and eight of them were
// one 64-byte line. At float32 SIXTEEN are, so this block now covers half a
// line rather than all of it. It is left at eight because the figure that
// matters is measured, not derived, and no measurement has been taken at
// sixteen: the block also bounds the scratch buffer, which is colBlock*maxDim
// values, and doubling it doubles that. Worth trying, with a number to show
// for it.
const colBlock = 8

func Synthesize2D_97(coeffs [][]float32, width, height, levels int) {
	if levels < 1 {
		return
	}

	// Pre-allocate reusable buffers sized for the largest level (level=1 = full image).
	var bufs dwtBufs97
	maxDim := max(width, height)
	bufs.ensure(maxDim)
	cols := make([]float32, colBlock*maxDim)

	// Process from coarsest to finest level
	for level := levels; level >= 1; level-- {
		// Compute dimensions at this level
		levelWidth := (width + (1 << (level - 1)) - 1) >> (level - 1)
		levelHeight := (height + (1 << (level - 1)) - 1) >> (level - 1)

		// Horizontal synthesis first (process rows) - matches OpenJPEG inverse order
		// Undoes the last step of forward transform (which did horizontal second)
		for y := range levelHeight {
			synthesize1D_97_bufs(coeffs[y][:levelWidth], &bufs, 0)
		}

		// Vertical synthesis second (process columns)
		// Undoes the first step of forward transform (which did vertical first)
		for x0 := 0; x0 < levelWidth; x0 += colBlock {
			n := levelWidth - x0
			if n > colBlock {
				n = colBlock
			}
			for y := range levelHeight {
				row := coeffs[y][x0 : x0+n]
				for j, v := range row {
					cols[j*maxDim+y] = v
				}
			}
			for j := range n {
				synthesize1D_97_bufs(cols[j*maxDim:j*maxDim+levelHeight], &bufs, 0)
			}
			for y := range levelHeight {
				row := coeffs[y][x0 : x0+n]
				for j := range row {
					row[j] = cols[j*maxDim+y]
				}
			}
		}
	}
}

// Synthesize2D_97_WithDims performs 2D inverse 9/7 wavelet transform
// using explicit resolution dimensions instead of computing them.
// resDims[i] contains bounds for resolution level i (0=coarsest)
// The X0/Y0 values are used to compute DWT parity (cas) for tiles at odd positions.
func Synthesize2D_97_WithDims(coeffs [][]float32, resDims []ResBounds) {
	levels := len(resDims) - 1
	if levels < 1 {
		return
	}

	// Pre-allocate reusable buffers sized for the largest resolution level.
	maxDim := 0
	for _, rd := range resDims {
		if rd.Width > maxDim {
			maxDim = rd.Width
		}
		if rd.Height > maxDim {
			maxDim = rd.Height
		}
	}
	var bufs dwtBufs97
	bufs.ensure(maxDim)
	cols := make([]float32, colBlock*maxDim)

	for level := levels; level >= 1; level-- {
		resIdx := levels - level + 1
		levelWidth := resDims[resIdx].Width
		levelHeight := resDims[resIdx].Height

		// Compute parity (cas) based on resolution origin
		casH := resDims[resIdx].X0 % 2
		casV := resDims[resIdx].Y0 % 2

		for y := range levelHeight {
			synthesize1D_97_bufs(coeffs[y][:levelWidth], &bufs, casH)
		}

		// The vertical pass gathers a block of columns so that each one is
		// contiguous, which is what lets the lifting kernels vectorise along a
		// column. Two passes that used to sit between the gather and the scatter
		// are folded into them:
		//
		//   the scaling, applied as each value is gathered, and
		//   the interleave, applied as each value is scattered.
		//
		// Neither changes an arithmetic operation: the scaling is the same single
		// multiply, and the interleave is a choice of which element to read.
		// Profiled on the heaviest page of the corpus, those two passes cost
		// 0.30s of memmove plus 0.40s in BaseInterleave -- against 0.45s for all
		// the lifting. The interleave was running the package's pure-Go FALLBACK
		// on arm64: the dispatcher there assigns every operation to the fallback
		// and a later init replaces only the ones that have a C-derived assembly
		// routine, which Interleave does not.
		snV := (levelHeight + 1) / 2
		dnV := levelHeight / 2
		if casV != 0 {
			snV, dnV = dnV, snV
		}
		for x0 := 0; x0 < levelWidth; x0 += colBlock {
			n := levelWidth - x0
			if n > colBlock {
				n = colBlock
			}
			for y := range levelHeight {
				var scale float32 = lift97K
				if y >= snV {
					scale = lift97TwoInvK
				}
				row := coeffs[y][x0 : x0+n]
				for j, v := range row {
					cols[j*maxDim+y] = v * scale
				}
			}
			for j := range n {
				col := cols[j*maxDim : j*maxDim+levelHeight]
				lift97(col[:snV], snV, col[snV:levelHeight], dnV, casV)
			}
			// Destination row y takes low[y/2] when its parity matches the phase
			// and high[y/2] otherwise, which is exactly what Interleave would
			// have written into the column buffer first.
			for y := range levelHeight {
				off := y / 2
				if (y%2 == 0) != (casV == 0) {
					off += snV
				}
				row := coeffs[y][x0 : x0+n]
				for j := range row {
					row[j] = cols[j*maxDim+off]
				}
			}
		}
	}
}

// analyze1D_53 performs forward 1D 5/3 wavelet transform (for testing)
// Input: signal [s0, s1, s2, s3, ...]
// Output: [L0, L1, ..., H0, H1, ...] (low-pass first, then high-pass)
func analyze1D_53(data []int32) {
	maxHalf := (len(data) + 1) / 2
	low := make([]int32, maxHalf)
	high := make([]int32, maxHalf)
	wavelet.Analyze53(data, 0, low, high)
}

// analyze1D_97 performs forward 1D 9/7 wavelet transform (for testing)
// Uses float64 SIMD primitives while preserving JPEG2000's non-standard scaling.
func analyze1D_97(data []float64) { analyze1D_97_cas(data, 0) }

// analyze1D_97_cas is the forward 9/7 transform of a signal whose first sample
// sits at an EVEN position (cas=0) or an odd one (cas=1).
//
// It mirrors synthesize1D_97_cas: the lifting steps run in the reverse order
// with negated coefficients, and the phase each step is given follows the same
// rule -- cas where the synthesis writes the high band from the low, 1-cas the
// other way. The subband lengths swap with cas for the same reason they do
// there: which of the two bands the first sample belongs to is what cas says.
func analyze1D_97_cas(data []float64, cas int) {
	n := len(data)
	if n <= 1 {
		return
	}

	half := (n + 1) / 2
	dn := n - half
	if cas != 0 {
		half, dn = dn, half
	}

	low := make([]float64, half)
	high := make([]float64, dn)
	wavelet.Deinterleave(data, low, half, high, dn, cas)

	// Forward lifting steps (SIMD-accelerated on float64)
	// Forward uses += coeff*(neighbors), LiftStep97 does -= coeff*(neighbors),
	// so negate coefficients for forward direction.
	wavelet.LiftStep97(high, dn, low, half, -lift97Alpha, cas)
	wavelet.LiftStep97(low, half, high, dn, -lift97Beta, 1-cas)
	wavelet.LiftStep97(high, dn, low, half, -lift97Gamma, cas)
	wavelet.LiftStep97(low, half, high, dn, -lift97Delta, 1-cas)

	// JPEG2000 scaling: low /= K, high *= K/2
	wavelet.ScaleSlice(low, half, 1.0/lift97K)
	wavelet.ScaleSlice(high, dn, lift97K/2.0)

	// Pack [low | high] back
	copy(data[:half], low)
	copy(data[half:], high)
}

// roundTripError computes max absolute error after forward+inverse transform
func roundTripError_53(original []int32) int32 {
	data := make([]int32, len(original))
	copy(data, original)

	analyze1D_53(data)
	maxHalf := (len(data) + 1) / 2
	low := make([]int32, maxHalf)
	high := make([]int32, maxHalf)
	synthesize1D_53(data, low, high, 0)

	var maxErr int32
	for i := range original {
		err := data[i] - original[i]
		if err < 0 {
			err = -err
		}
		if err > maxErr {
			maxErr = err
		}
	}
	return maxErr
}

// roundTripError_97 computes max absolute error after forward+inverse transform
func roundTripError_97(original []float64) float64 {
	data := make([]float64, len(original))
	copy(data, original)

	analyze1D_97(data)
	// The synthesis works at the reference's precision now, so the round trip
	// crosses it and comes back.
	f32 := make([]float32, len(data))
	for i, v := range data {
		f32[i] = float32(v)
	}
	synthesize1D_97(f32)
	for i, v := range f32 {
		data[i] = float64(v)
	}

	var maxErr float64
	for i := range original {
		err := math.Abs(data[i] - original[i])
		if err > maxErr {
			maxErr = err
		}
	}
	return maxErr
}
