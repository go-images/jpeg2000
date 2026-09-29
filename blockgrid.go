package jpeg2000

// ceilShift is ceil(x / 2^n) for an x that may be negative, which T.800's
// subband equations need: tbx0 = ceil((tcx0 - 2^(nb-1)*xob) / 2^nb) subtracts
// before it divides, and the subtraction can go below zero for a tile at the
// image origin.
func ceilShift(x, n int) int {
	d := 1 << n
	if x >= 0 {
		return (x + d - 1) / d
	}
	return -((-x) / d)
}

// subbandOrigin is where a subband begins on the REFERENCE GRID, per ITU-T
// T.800 B-15, as against subbandBounds which says where it begins inside the
// tile's own coefficient array.
//
// The two are the same only for a tile at the image origin, and the difference
// is not cosmetic: code-blocks sit on a grid anchored at the reference grid's
// origin (B.7), so where a subband BEGINS on that grid decides how many blocks
// it is cut into and where their edges fall. A subband at [48,96) with 64-wide
// blocks spans two of them; the same subband measured only by its width of 48
// looks like one.
func subbandOrigin(sbIdx, numLevels, tcx0, tcy0 int) (int, int) {
	if sbIdx == 0 {
		return ceilShift(tcx0, numLevels), ceilShift(tcy0, numLevels)
	}
	detailIdx := sbIdx - 1
	level := numLevels - detailIdx/3 // 1-indexed from the finest
	var xob, yob int
	switch detailIdx % 3 {
	case 0: // LH
		xob, yob = 0, 1
	case 1: // HL
		xob, yob = 1, 0
	default: // HH
		xob, yob = 1, 1
	}
	half := 1 << (level - 1)
	return ceilShift(tcx0-half*xob, level), ceilShift(tcy0-half*yob, level)
}

// blockGrid cuts a subband into code-blocks the way the DECODER reads them:
// on a grid anchored at the reference grid's origin, so the first block is
// PARTIAL whenever the subband does not begin on a multiple of the block size.
//
// It returns the number of blocks and a function giving each one's extent
// inside the subband. The encoder used to cut from the subband's own start,
// which agrees only when absX0 is already on the grid -- and when it does not
// agree, every packet after the first carries the wrong number of blocks.
func blockGrid(absX0, size, cb int) (n int, at func(i int) (lo, hi int)) {
	if size <= 0 {
		return 0, func(int) (int, int) { return 0, 0 }
	}
	grid0 := absX0 / cb
	n = (absX0+size-1)/cb - grid0 + 1
	return n, func(i int) (int, int) {
		lo := max((grid0+i)*cb-absX0, 0)
		hi := min((grid0+i+1)*cb-absX0, size)
		return lo, hi
	}
}
