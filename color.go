package jpeg2000

import (
	"image"

	hwyimage "github.com/ajroetker/go-highway/hwy/contrib/image"
)

// applyRCT applies inverse Reversible Color Transform (lossless)
// Converts from YCbCr to RGB using integer arithmetic
// Y = (R + 2G + B) / 4  =>  G = Y - (Cb + Cr) / 4
// Cb = B - G            =>  B = Cb + G
// Cr = R - G            =>  R = Cr + G
func applyRCT(y, cb, cr [][]int32) (r, g, b [][]int32) {
	height := len(y)
	if height == 0 {
		return nil, nil, nil
	}
	width := len(y[0])

	buf := getInt32Buf(width, height)
	defer putInt32Buf(buf)

	// Copy inputs into pooled images
	slicesToImageInPlace(y, buf.imgs[0])
	slicesToImageInPlace(cb, buf.imgs[1])
	slicesToImageInPlace(cr, buf.imgs[2])

	// Apply SIMD inverse RCT
	hwyimage.InverseRCT(buf.imgs[0], buf.imgs[1], buf.imgs[2], buf.imgs[3], buf.imgs[4], buf.imgs[5])

	// Convert back to slices
	r = imageToSlices(buf.imgs[3])
	g = imageToSlices(buf.imgs[4])
	b = imageToSlices(buf.imgs[5])

	return r, g, b
}

// applyICT applies inverse Irreversible Color Transform (lossy)
// Standard YCbCr to RGB conversion with JPEG2000 coefficients
// R = Y + 1.402 * Cr
// G = Y - 0.344136 * Cb - 0.714136 * Cr
// B = Y + 1.772 * Cb
func applyICT(y, cb, cr [][]float64) (r, g, b [][]float64) {
	height := len(y)
	if height == 0 {
		return nil, nil, nil
	}
	width := len(y[0])

	buf := getFloat64Buf(width, height)
	defer putFloat64Buf(buf)

	// Copy inputs into pooled images
	slicesToImageInPlace(y, buf.imgs[0])
	slicesToImageInPlace(cb, buf.imgs[1])
	slicesToImageInPlace(cr, buf.imgs[2])

	// Apply SIMD inverse ICT
	hwyimage.InverseICT(buf.imgs[0], buf.imgs[1], buf.imgs[2], buf.imgs[3], buf.imgs[4], buf.imgs[5])

	// Convert back to slices
	r = imageToSlices(buf.imgs[3])
	g = imageToSlices(buf.imgs[4])
	b = imageToSlices(buf.imgs[5])

	return r, g, b
}

// forwardRCT applies the forward Reversible Color Transform (lossless).
// Converts from RGB to YCbCr using integer arithmetic per ITU-T T.800 G.2:
//
//	Y  = floor((R + 2G + B) / 4)
//	Cb = B - G
//	Cr = R - G
//
// This is the exact inverse of applyRCT: forwardRCT followed by applyRCT
// yields the original RGB values (lossless round-trip).
func forwardRCT(r, g, b [][]int32) (y, cb, cr [][]int32) {
	height := len(r)
	if height == 0 {
		return nil, nil, nil
	}
	width := len(r[0])

	buf := getInt32Buf(width, height)
	defer putInt32Buf(buf)

	// Copy inputs into pooled images
	slicesToImageInPlace(r, buf.imgs[0])
	slicesToImageInPlace(g, buf.imgs[1])
	slicesToImageInPlace(b, buf.imgs[2])

	// Apply SIMD forward RCT
	hwyimage.ForwardRCT(buf.imgs[0], buf.imgs[1], buf.imgs[2], buf.imgs[3], buf.imgs[4], buf.imgs[5])

	// Convert back to slices
	y = imageToSlices(buf.imgs[3])
	cb = imageToSlices(buf.imgs[4])
	cr = imageToSlices(buf.imgs[5])

	return y, cb, cr
}

// forwardICT applies the forward Irreversible Color Transform (lossy).
// Converts from RGB to YCbCr using the standard JPEG2000 coefficients
// per ITU-T T.800 G.2:
//
//	Y  =  0.299   * R + 0.587   * G + 0.114   * B
//	Cb = -0.16875 * R - 0.33126 * G + 0.5     * B
//	Cr =  0.5     * R - 0.41869 * G - 0.08131 * B
//
// The inverse of this transform is applyICT. Note that because this uses
// floating-point arithmetic, the round-trip is not perfectly lossless.
func forwardICT(r, g, b [][]float64) (y, cb, cr [][]float64) {
	height := len(r)
	if height == 0 {
		return nil, nil, nil
	}
	width := len(r[0])

	buf := getFloat64Buf(width, height)
	defer putFloat64Buf(buf)

	// Copy inputs into pooled images
	slicesToImageInPlace(r, buf.imgs[0])
	slicesToImageInPlace(g, buf.imgs[1])
	slicesToImageInPlace(b, buf.imgs[2])

	// Apply SIMD forward ICT
	hwyimage.ForwardICT(buf.imgs[0], buf.imgs[1], buf.imgs[2], buf.imgs[3], buf.imgs[4], buf.imgs[5])

	// Convert back to slices
	y = imageToSlices(buf.imgs[3])
	cb = imageToSlices(buf.imgs[4])
	cr = imageToSlices(buf.imgs[5])

	return y, cb, cr
}

// clampToUint8 clamps a value to [0, 255] range
func clampToUint8(v int32) uint8 {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return uint8(v)
}

// clampFloat clamps a float to [0, 255] and converts to uint8
func clampFloat(v float64) uint8 {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return uint8(v + 0.5) // Round to nearest
}

// getComponentSample safely gets a sample from a component, handling subsampling.
// If the component is smaller than the output dimensions, uses nearest-neighbor upsampling.
func getComponentSample(comp [][]int32, x, y, outWidth, outHeight int) int32 {
	compHeight := len(comp)
	if compHeight == 0 {
		return 0
	}
	compWidth := len(comp[0])
	if compWidth == 0 {
		return 0
	}

	// Scale coordinates if component is subsampled
	// Use nearest-neighbor interpolation
	compY := y
	compX := x
	if compHeight < outHeight {
		compY = y * compHeight / outHeight
	}
	if compWidth < outWidth {
		compX = x * compWidth / outWidth
	}

	// Bounds check
	if compY >= compHeight {
		compY = compHeight - 1
	}
	if compX >= compWidth {
		compX = compWidth - 1
	}

	return comp[compY][compX]
}

// convertToRGBA converts decoded components to image.RGBA
// components: slice of 2D coefficient arrays (1 for grayscale, 3+ for RGB/RGBA)
// bitDepths: per-component bits per sample (typically 8)
// signed: per-component signedness
// reversible: true for RCT, false for ICT
// Note: Components may have different dimensions due to subsampling (e.g., YCbCr 4:2:0)
func convertToRGBA(components [][][]int32, width, height int,
	bitDepths []int, signed []bool, reversible bool) *image.RGBA {

	img := image.NewRGBA(image.Rect(0, 0, width, height))

	// Helper to get bit depth for a component (defaults to 8)
	getBitDepth := func(c int) int {
		if c < len(bitDepths) && bitDepths[c] > 0 {
			return bitDepths[c]
		}
		return 8
	}

	// Display mapping offset: always add 2^(bitDepth-1) to convert to unsigned range.
	// For unsigned data: undoes the encoder's DC level shift (ITU-T T.800 G.1.2).
	// For signed data: converts from signed range [-2^(B-1), 2^(B-1)-1] to
	// unsigned range [0, 2^B-1] for display. OpenJPEG's opj_decompress does the
	// same when outputting to unsigned formats (PGM/PPM/PNG).
	getOffset := func(c int) int32 {
		bitDepth := getBitDepth(c)
		return int32(1 << (bitDepth - 1))
	}

	// Grayscale
	if len(components) == 1 {
		gray := components[0]
		bitDepth := getBitDepth(0)
		offset := getOffset(0)
		// Calculate proper scaling factor
		// For n-bit to 8-bit: scale by 255 / (2^n - 1)
		// This maps [0, 2^n-1] to [0, 255] correctly
		maxVal := int32((1 << bitDepth) - 1)
		for y := range height {
			for x := range width {
				val := getComponentSample(gray, x, y, width, height) + offset
				// Scale to 8-bit using proper factor
				// For example: 4-bit uses * 255 / 15 = * 17
				if bitDepth != 8 {
					val = (val*255 + maxVal/2) / maxVal
				}
				g := clampToUint8(val)
				idx := img.PixOffset(x, y)
				img.Pix[idx+0] = g   // R
				img.Pix[idx+1] = g   // G
				img.Pix[idx+2] = g   // B
				img.Pix[idx+3] = 255 // A
			}
		}
		return img
	}

	// 2-component (grayscale + alpha or similar) - use first component as grayscale
	// This matches OpenJPEG's behavior of outputting only the first component for PGM
	if len(components) == 2 {
		gray := components[0]
		bitDepth := getBitDepth(0)
		offset := getOffset(0)
		maxVal := int32((1 << bitDepth) - 1)
		for y := range height {
			for x := range width {
				val := getComponentSample(gray, x, y, width, height) + offset
				if bitDepth != 8 {
					val = (val*255 + maxVal/2) / maxVal
				}
				g := clampToUint8(val)
				idx := img.PixOffset(x, y)
				img.Pix[idx+0] = g   // R
				img.Pix[idx+1] = g   // G
				img.Pix[idx+2] = g   // B
				img.Pix[idx+3] = 255 // A
			}
		}
		return img
	}

	// RGB/RGBA with optional color transform
	if len(components) >= 3 {
		var rComp, gComp, bComp [][]int32

		if reversible {
			// Apply RCT - requires all components to have same dimensions
			// Check if components have matching dimensions
			if len(components[0]) == len(components[1]) && len(components[0]) == len(components[2]) &&
				len(components[0]) > 0 && len(components[0][0]) == len(components[1][0]) &&
				len(components[0][0]) == len(components[2][0]) {
				rComp, gComp, bComp = applyRCT(components[0], components[1], components[2])
			} else {
				// Components have different sizes, skip RCT
				rComp = components[0]
				gComp = components[1]
				bComp = components[2]
			}
		} else {
			// No color transform for integer components in ICT mode
			// (ICT uses float components, handled by convertToRGBAFloat)
			rComp = components[0]
			gComp = components[1]
			bComp = components[2]
		}

		// Calculate per-component offsets, bit depths, and max values
		rBitDepth := getBitDepth(0)
		gBitDepth := getBitDepth(1)
		bBitDepth := getBitDepth(2)
		rOffset := getOffset(0)
		gOffset := getOffset(1)
		bOffset := getOffset(2)
		rMaxVal := int32((1 << rBitDepth) - 1)
		gMaxVal := int32((1 << gBitDepth) - 1)
		bMaxVal := int32((1 << bBitDepth) - 1)

		for y := range height {
			for x := range width {
				// Use safe accessor that handles subsampling
				// Apply per-component DC offset
				rVal := getComponentSample(rComp, x, y, width, height) + rOffset
				gVal := getComponentSample(gComp, x, y, width, height) + gOffset
				bVal := getComponentSample(bComp, x, y, width, height) + bOffset

				// Scale each component to 8-bit using proper linear scaling
				// For n-bit to 8-bit: output = (val * 255 + maxVal/2) / maxVal
				// This maps [0, 2^n-1] to [0, 255] correctly
				if rBitDepth != 8 {
					rVal = (rVal * 255 + rMaxVal/2) / rMaxVal
				}
				if gBitDepth != 8 {
					gVal = (gVal * 255 + gMaxVal/2) / gMaxVal
				}
				if bBitDepth != 8 {
					bVal = (bVal * 255 + bMaxVal/2) / bMaxVal
				}

				idx := img.PixOffset(x, y)
				img.Pix[idx+0] = clampToUint8(rVal)
				img.Pix[idx+1] = clampToUint8(gVal)
				img.Pix[idx+2] = clampToUint8(bVal)
				img.Pix[idx+3] = 255
			}
		}
		return img
	}

	return img
}

// convertToRGBAFloat converts decoded float components to image.RGBA
// Used for lossy (ICT) decoding
func convertToRGBAFloat(components [][][]float64, width, height int,
	bitDepths []int, signed []bool) *image.RGBA {

	img := image.NewRGBA(image.Rect(0, 0, width, height))

	// Helper to get bit depth for a component (defaults to 8)
	getBitDepth := func(c int) int {
		if c < len(bitDepths) && bitDepths[c] > 0 {
			return bitDepths[c]
		}
		return 8
	}

	// Calculate display offset and scale for a component.
	// Always add 2^(bitDepth-1) to convert to unsigned range for display.
	getOffsetAndScale := func(c int) (float64, float64) {
		bitDepth := getBitDepth(c)
		offset := float64(uint(1) << (bitDepth - 1))
		scale := 1.0
		if bitDepth != 8 {
			scale = 255.0 / float64((uint(1)<<bitDepth)-1)
		}
		return offset, scale
	}

	// Grayscale
	if len(components) == 1 {
		gray := components[0]
		offset, scale := getOffsetAndScale(0)
		for y := range height {
			for x := range width {
				val := (gray[y][x] + offset) * scale
				g := clampFloat(val)
				idx := img.PixOffset(x, y)
				img.Pix[idx+0] = g
				img.Pix[idx+1] = g
				img.Pix[idx+2] = g
				img.Pix[idx+3] = 255
			}
		}
		return img
	}

	// RGB with ICT
	if len(components) >= 3 {
		// For ICT (irreversible color transform), the DC level shift was applied to the
		// Y component BEFORE forward ICT in the encoder. Per ITU-T T.800:
		// - Forward: RGB -> YCbCr, then Y -= 2^(B-1) for DC level shift (unsigned only)
		// - Inverse: Y += 2^(B-1), then YCbCr -> RGB (unsigned only)
		// The Y component must have DC offset added BEFORE inverse ICT, not after.
		// Cb and Cr are already centered at 0 and don't need offset.
		yComp := components[0]
		cbComp := components[1]
		crComp := components[2]

		// Add offset to Y component before ICT.
		// For unsigned: undoes encoder DC level shift. For signed: converts to unsigned range.
		bitDepth := getBitDepth(0)
		yOffset := float64(uint(1) << (bitDepth - 1)) // 2^(bitDepth-1) = 128 for 8-bit

		yShifted := make([][]float64, len(yComp))
		for i := range yComp {
			yShifted[i] = make([]float64, len(yComp[i]))
			for j := range yComp[i] {
				yShifted[i][j] = yComp[i][j] + yOffset
			}
		}

		// Now apply inverse ICT with properly shifted Y
		rComp, gComp, bComp := applyICT(yShifted, cbComp, crComp)

		// Calculate scale for non-8-bit depths
		rScale := 1.0
		gScale := 1.0
		bScale := 1.0
		if getBitDepth(0) != 8 {
			rScale = 255.0 / float64((uint(1)<<getBitDepth(0))-1)
		}
		if getBitDepth(1) != 8 {
			gScale = 255.0 / float64((uint(1)<<getBitDepth(1))-1)
		}
		if getBitDepth(2) != 8 {
			bScale = 255.0 / float64((uint(1)<<getBitDepth(2))-1)
		}

		for y := range height {
			for x := range width {
				// RGB values from ICT are already in [0, 2^B - 1] range
				// Only scale if bit depth != 8
				rVal := rComp[y][x] * rScale
				gVal := gComp[y][x] * gScale
				bVal := bComp[y][x] * bScale

				idx := img.PixOffset(x, y)
				img.Pix[idx+0] = clampFloat(rVal)
				img.Pix[idx+1] = clampFloat(gVal)
				img.Pix[idx+2] = clampFloat(bVal)
				img.Pix[idx+3] = 255
			}
		}
		return img
	}

	return img
}

// convertToCMYK converts four decoded components to an image.CMYK.
//
// A JPEG 2000 file says what its components mean, in the enumerated colour
// space of its own JP2 header. EnumCS 12 is CMYK, and its four planes are
// amounts of ink with nought meaning none -- image.CMYK's convention too, so
// the samples map straight across once the display offset of ITU-T T.800
// G.1.2 and any n-bit to 8-bit scaling are applied.
//
// A multi-component transform, when the codestream signals one, was applied to
// the FIRST THREE components. RCT and ICT are defined over three correlated
// planes and say nothing about what those planes mean, so for a CMYK picture
// they carry cyan, magenta and yellow. The fourth is black and is never part
// of the transform. Undoing it is therefore the same arithmetic as for a
// colour picture; what differs is that the fourth plane is KEPT.
//
// Reading the four planes as red, green, blue and nothing is what this
// replaces, and it does not lose a rounding: it loses most of the ink on a
// printed page.
func convertToCMYK(components [][][]int32, width, height int,
	bitDepths []int, signed []bool, reversible bool) *image.CMYK {

	img := image.NewCMYK(image.Rect(0, 0, width, height))

	getBitDepth := func(c int) int {
		if c < len(bitDepths) && bitDepths[c] > 0 {
			return bitDepths[c]
		}
		return 8
	}

	planes := [4][][]int32{components[0], components[1], components[2], components[3]}
	if reversible {
		if sameShape(components[0], components[1]) && sameShape(components[0], components[2]) {
			planes[0], planes[1], planes[2] = applyRCT(components[0], components[1], components[2])
		}
	}

	for c := range 4 {
		bitDepth := getBitDepth(c)
		offset := int32(1) << (bitDepth - 1)
		maxVal := int32(1)<<bitDepth - 1
		plane := planes[c]
		for y := range height {
			for x := range width {
				val := getComponentSample(plane, x, y, width, height) + offset
				if bitDepth != 8 {
					val = (val*255 + maxVal/2) / maxVal
				}
				img.Pix[img.PixOffset(x, y)+c] = clampToUint8(val)
			}
		}
	}
	return img
}

// convertToCMYKFloat is convertToCMYK for the irreversible path, where the
// transform is the ICT and the components arrive as floats.
//
// The display offset goes on the FIRST plane BEFORE the inverse transform, as
// it does in convertToRGBAFloat and for the same reason: the encoder applied
// the level shift after the forward transform, so undoing them in the other
// order would move every colour. The two chroma-like planes are already
// centred on nought. The fourth plane is outside the transform and takes its
// own offset afterwards.
func convertToCMYKFloat(components [][][]float64, width, height int,
	bitDepths []int, signed []bool) *image.CMYK {

	img := image.NewCMYK(image.Rect(0, 0, width, height))

	getBitDepth := func(c int) int {
		if c < len(bitDepths) && bitDepths[c] > 0 {
			return bitDepths[c]
		}
		return 8
	}
	scaleOf := func(c int) float64 {
		if getBitDepth(c) == 8 {
			return 1.0
		}
		return 255.0 / float64(uint(1)<<getBitDepth(c)-1)
	}

	first := components[0]
	firstOffset := float64(uint(1) << (getBitDepth(0) - 1))
	shifted := make([][]float64, len(first))
	for i := range first {
		shifted[i] = make([]float64, len(first[i]))
		for j := range first[i] {
			shifted[i][j] = first[i][j] + firstOffset
		}
	}
	c0, c1, c2 := applyICT(shifted, components[1], components[2])

	kOffset := float64(uint(1) << (getBitDepth(3) - 1))
	kScale := scaleOf(3)
	s0, s1, s2 := scaleOf(0), scaleOf(1), scaleOf(2)
	k := components[3]

	for y := range height {
		for x := range width {
			idx := img.PixOffset(x, y)
			img.Pix[idx+0] = clampFloat(c0[y][x] * s0)
			img.Pix[idx+1] = clampFloat(c1[y][x] * s1)
			img.Pix[idx+2] = clampFloat(c2[y][x] * s2)
			img.Pix[idx+3] = clampFloat((k[y][x] + kOffset) * kScale)
		}
	}
	return img
}

// sameShape reports two planes a transform may be applied across.
func sameShape(a, b [][]int32) bool {
	if len(a) != len(b) || len(a) == 0 {
		return false
	}
	return len(a[0]) == len(b[0])
}
