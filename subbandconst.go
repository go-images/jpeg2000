package jpeg2000

import "math"

// subbandConst holds what decoding a subband's code blocks needs and what does not
// change from one block to the next.
//
// It exists because all of it used to be computed per CODE BLOCK: the quantisation
// tables were selected, a subband-offset switch was written out twice, the bit depth
// and nominal range were derived, and the step size was built with two math.Pow --
// once for every block of every subband of every resolution. A 2533 by 3590 image
// carries some 2 200 blocks in one subband.
//
// offX and offY are the same thing one level further in: the position of a
// coefficient in the tile is a CONSTANT offset plus its position in the subband, and
// the offset was being recomputed by a function call, with two slice bounds checks
// and a switch, for every one of 9.1 million coefficients.
type subbandConst struct {
	mbExponents []int
	guardBits   int
	// mb is the magnitude bit plane count before the per-block adjustment that
	// depends on TotalPasses and ZeroBitPlanes.
	mb       int
	roiShift int
	stepSize float64
	// reversible says the 5/3 integer path applies, which is a property of the
	// codestream rather than of a coefficient.
	reversible bool
	offX, offY int
}

// subbandConstants computes them once for one subband.
func (td *TileDecoder) subbandConstants(sb *Subband, res, comp int) subbandConst {
	h := td.header
	var c subbandConst

	// Which quantisation table speaks for this component: a tile QCD overrides
	// everything, then a per-component QCC, then the main header's.
	c.guardBits = h.GuardBits
	if td.tile.HasTileQCD {
		c.mbExponents = td.tile.TileExponents
		c.guardBits = td.tile.TileGuardBits
	} else if h.CompExponents != nil && comp < len(h.CompExponents) && h.CompExponents[comp] != nil {
		c.mbExponents = h.CompExponents[comp]
	} else if h.OriginalExponents != nil {
		c.mbExponents = h.OriginalExponents
	} else {
		c.mbExponents = h.Exponents
	}

	// The index of this subband in a quantisation table, which the two loops below
	// this used to spell out twice.
	idx := 0
	if res > 0 {
		idx = 1 + 3*(res-1) + subbandOffsetOf(sb.Type)
	}

	c.mb = 8 // the default for eight-bit data without guard bits
	if len(c.mbExponents) > 0 {
		if idx < len(c.mbExponents) {
			c.mb = c.guardBits + c.mbExponents[idx] - 1
		} else if len(c.mbExponents) == 1 {
			c.mb = c.guardBits + derivedExponent(c.mbExponents[0], res) - 1
		}
	}
	if comp < len(td.tile.ROIShift) {
		c.roiShift = td.tile.ROIShift[comp]
	}
	c.mb += c.roiShift

	var exps, mants []int
	switch {
	case td.tile.HasTileQCD:
		exps, mants = td.tile.TileExponents, td.tile.TileMantissas
	case h.CompQuantStyle != nil && comp < len(h.CompQuantStyle) && h.CompQuantStyle[comp] != 255:
		if h.CompExponents != nil && comp < len(h.CompExponents) {
			exps = h.CompExponents[comp]
		}
		if h.CompMantissas != nil && comp < len(h.CompMantissas) {
			mants = h.CompMantissas[comp]
		}
	default:
		exps, mants = h.OriginalExponents, h.OriginalMantissas
		if exps == nil {
			exps = h.Exponents
		}
		if mants == nil {
			mants = h.Mantissas
		}
	}

	bitDepth := 8
	if comp < len(h.BitDepth) {
		bitDepth = h.BitDepth[comp]
	}

	// Rb is the subband's nominal dynamic range, ITU-T T.800 Table E.1:
	// Rb = bitDepth + gain_b. For 9/7 the gain is 0 for ALL subbands
	// (OpenJPEG BUG_WEIRD_TWO_INVK): the DWT uses 2/K instead of 1/K for the
	// high-pass scaling and compensates there.
	rb := bitDepth
	wavelet := td.getWaveletFilter()
	if wavelet == Wavelet53 {
		switch sb.Type {
		case SubbandHL, SubbandLH:
			rb++
		case SubbandHH:
			rb += 2
		}
	}

	// ITU-T T.800 E.1.1: stepSize = (1 + mant/2048) * 2^(Rb - exp).
	c.stepSize = 1.0
	if len(exps) > 0 {
		if idx < len(exps) {
			mant := 0
			if idx < len(mants) {
				mant = mants[idx]
			}
			c.stepSize = (1.0 + float64(mant)/2048.0) * math.Pow(2, float64(rb-exps[idx]))
		} else if len(exps) == 1 {
			mant := 0
			if len(mants) > 0 {
				mant = mants[0]
			}
			c.stepSize = (1.0 + float64(mant)/2048.0) * math.Pow(2, float64(rb-derivedExponent(exps[0], res)))
		}
	}

	quantStyle := h.QuantStyle
	if td.tile.HasTileQCD {
		quantStyle = td.tile.TileQuantStyle
	}
	c.reversible = wavelet == Wavelet53 && quantStyle == 0

	// The quadrant offset, which getCoeffPositionForTile computes one coefficient
	// at a time. Resolution 0 places directly; above it the offset is the PREVIOUS
	// resolution's dimensions, per OpenJPEG t1.c, which is what keeps odd
	// dimensions right.
	if res > 0 {
		prevW, prevH := 0, 0
		if comp < len(td.compResolutions) && res-1 < len(td.compResolutions[comp]) {
			prev := td.compResolutions[comp][res-1]
			prevW, prevH = prev.Width, prev.Height
		}
		switch sb.Type {
		case SubbandHL:
			c.offX = prevW
		case SubbandLH:
			c.offY = prevH
		case SubbandHH:
			c.offX, c.offY = prevW, prevH
		}
	}
	return c
}

// subbandOffsetOf is the order HL, LH, HH takes in a quantisation table.
func subbandOffsetOf(t SubbandType) int {
	switch t {
	case SubbandLH:
		return 1
	case SubbandHH:
		return 2
	}
	return 0
}

// derivedExponent is what a table of one entry means at a given resolution: the
// DERIVED quantisation of ITU-T T.800 E.1.1, Sqcd style 1.
//
// NOTHING IN THIS REPOSITORY EXERCISES IT, and saying so is better than leaving a
// reader to assume the tests cover it. opj_compress writes style 0 for reversible
// and style 2 (expounded) for irreversible and offers no flag for style 1, so the
// fixtures cannot reach this branch; a mutation that returns `base` survives the
// suite. It is unchanged code -- this file only gathered it out of a loop, and the
// corpus of 3 281 documents is byte-identical across that move -- but the branch is
// carried on the conformance data's word, not on a test here. A fixture would need
// an encoder that emits a single-entry QCD.
func derivedExponent(base, res int) int {
	if res == 0 {
		return base
	}
	return base - (res - 1)
}
