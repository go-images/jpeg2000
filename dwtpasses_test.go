package jpeg2000

import (
	"math"
	"math/rand"
	"testing"

	"github.com/ajroetker/go-highway/hwy/contrib/wavelet"
)

// reference97 is the 1D synthesis as it was written before the scaling was
// folded into the read-out: copy, then ScaleSlice, then lift, then interleave.
func reference97(data, low, high []float64, cas int) {
	sn, dn := len(low), len(high)
	copy(low, data[:sn])
	copy(high, data[sn:sn+dn])
	wavelet.ScaleSlice(low, sn, lift97K)
	wavelet.ScaleSlice(high, dn, lift97TwoInvK)
	wavelet.LiftStep97(low, sn, high, dn, lift97Delta, 1-cas)
	wavelet.LiftStep97(high, dn, low, sn, lift97Gamma, cas)
	wavelet.LiftStep97(low, sn, high, dn, lift97Beta, 1-cas)
	wavelet.LiftStep97(high, dn, low, sn, lift97Alpha, cas)
	wavelet.Interleave(data, low, sn, high, dn, cas)
}

// referenceSynthesize2D_97 is the 2D vertical pass as it was written before the
// interleave was folded into the scatter: gather, synthesise each column with
// the 1D routine, interleave into the column buffer, scatter.
func referenceSynthesize2D_97(coeffs [][]float64, resDims []ResBounds) {
	levels := len(resDims) - 1
	if levels < 1 {
		return
	}
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
	cols := make([]float64, colBlock*maxDim)
	for level := levels; level >= 1; level-- {
		resIdx := levels - level + 1
		levelWidth := resDims[resIdx].Width
		levelHeight := resDims[resIdx].Height
		casH := resDims[resIdx].X0 % 2
		casV := resDims[resIdx].Y0 % 2
		for y := range levelHeight {
			d := coeffs[y][:levelWidth]
			n := len(d)
			if n > 1 {
				sn, dn := (n+1)/2, n/2
				if casH != 0 {
					sn, dn = dn, sn
				}
				reference97(d, bufs.low[:sn], bufs.high[:dn], casH)
			}
		}
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
				col := cols[j*maxDim : j*maxDim+levelHeight]
				if levelHeight > 1 {
					sn, dn := (levelHeight+1)/2, levelHeight/2
					if casV != 0 {
						sn, dn = dn, sn
					}
					reference97(col, bufs.low[:sn], bufs.high[:dn], casV)
				}
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

// TestFewerPassesIsBitIdentical is the whole argument for the change: removing
// two passes must not move a single bit. It compares the current 2D synthesis
// against the shape it had before, on the same coefficients.
func TestFewerPassesIsBitIdentical(t *testing.T) {
	r := rand.New(rand.NewSource(20260926))
	cases := []struct{ w, h, levels, x0, y0 int }{
		{8, 8, 1, 0, 0}, {8, 8, 1, 1, 1}, {9, 7, 1, 0, 1}, {7, 9, 1, 1, 0},
		{16, 16, 2, 0, 0}, {17, 15, 2, 1, 1}, {31, 33, 3, 0, 1},
		{64, 48, 3, 1, 0}, {65, 65, 3, 1, 1}, {128, 96, 4, 0, 0},
		{200, 137, 4, 1, 1}, {256, 256, 5, 0, 1},
	}
	for _, c := range cases {
		dims := make([]ResBounds, c.levels+1)
		for i := range dims {
			shift := c.levels - i
			dims[i] = ResBounds{
				Width:  (c.w + (1 << shift) - 1) >> shift,
				Height: (c.h + (1 << shift) - 1) >> shift,
				X0:     c.x0, Y0: c.y0,
			}
		}
		a := make([][]float64, c.h)
		b := make([][]float64, c.h)
		for y := range a {
			a[y] = make([]float64, c.w)
			b[y] = make([]float64, c.w)
			for x := range a[y] {
				v := (r.Float64() - 0.5) * 4096
				a[y][x], b[y][x] = v, v
			}
		}
		Synthesize2D_97_WithDims(a, dims)
		referenceSynthesize2D_97(b, dims)

		for y := range a {
			for x := range a[y] {
				if math.Float64bits(a[y][x]) != math.Float64bits(b[y][x]) {
					t.Fatalf("%dx%d levels=%d origin=(%d,%d): at (%d,%d) got %v (%#016x), reference %v (%#016x)",
						c.w, c.h, c.levels, c.x0, c.y0, x, y,
						a[y][x], math.Float64bits(a[y][x]), b[y][x], math.Float64bits(b[y][x]))
				}
			}
		}
	}
}
