package jpeg2000

import "unsafe"

// float32View is the same memory read as float32 rather than int32.
//
// It exists so that a 9/7 decode holds ONE buffer a component where it held
// two: the coefficients are dequantized as float32, synthesized as float32, and
// converted back to int32 in place. Both are four bytes and both are aligned to
// four, on every architecture this builds for, so the view is a reinterpretation
// and not a reinterpretation-with-padding.
//
// This is the reference's own arrangement rather than a trick of ours. OpenJPEG
// gives a tile-component a single OPJ_INT32* (tcd.h), casts it to OPJ_FLOAT32*
// for the 9/7 synthesis -- "Where void* is a OPJ_INT32* for 5x3 and
// OPJ_FLOAT32* for 9x7" (dwt.c) -- and then walks it once more reading a float
// out of a slot and writing an int back into the same slot
// (opj_tcd_dc_level_shift_decode, tcd.c).
//
// WHAT MAKES IT SOUND HERE, and each of these is tested:
//
//   - Nothing writes the two views at once. decodeSubband writes the integer
//     row only when inv.reversible, which is `wavelet == Wavelet53 &&
//     quantStyle == 0` -- false exactly when a float plane exists.
//   - The conversion back reads index i and writes index i, in increasing
//     order, so no slot is read after it has been overwritten.
//   - The returned slice points into ints, so ints stays reachable for as long
//     as the view does; the garbage collector needs no help.
//
// An empty component has no first element to take the address of, and no
// coefficients either, so it gets a nil view.
func float32View(ints []int32) []float32 {
	if len(ints) == 0 {
		return nil
	}
	return unsafe.Slice((*float32)(unsafe.Pointer(&ints[0])), len(ints))
}
