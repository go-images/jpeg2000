package jpeg2000

import "image"

// greyPicture is a one-component eight-bit picture as an *image.Gray, which is
// ONE BYTE A PIXEL where convertToRGBA's answer is four.
//
// Most of a scanned corpus is exactly this shape, and the four-byte form is what
// puts the largest pages over a renderer's ceiling: 9 449 by 13 701 is 123 MB here
// and 494 MB there, and go-pdfkit refuses five pages of its corpus for that reason
// alone. The decoder's own peak drops with it -- the RGBA it no longer builds was
// the largest single allocation of a grey decode.
//
// It answers nil rather than guessing whenever the picture is not plainly that
// shape: one component, eight bits, unsigned, no subsampling to resolve, and rows
// of the width the caller asked for. convertToRGBA handles every one of those, and
// this is a fast path beside it, not a replacement for it.
//
// The value written is the one convertToRGBA writes for the same pixel: the sample
// plus the display offset of 2^(B-1), clamped. At eight bits the scaling it applies
// is 255/255, so there is none to apply here -- see the `bitDepth != 8` branch it
// takes, which this shape never reaches.
func greyPicture(components [][][]int32, width, height int, bitDepths []int, signed []bool) *image.Gray {
	if len(components) != 1 || width <= 0 || height <= 0 {
		return nil
	}
	if len(bitDepths) > 0 && bitDepths[0] != 8 {
		return nil
	}
	if len(signed) > 0 && signed[0] {
		return nil
	}
	plane := components[0]
	if len(plane) != height {
		return nil
	}
	for _, row := range plane {
		if len(row) != width {
			return nil
		}
	}
	img := image.NewGray(image.Rect(0, 0, width, height))
	const offset = 1 << 7 // 2^(bitDepth-1), the display offset of ITU-T T.800 G.1.2
	for y := 0; y < height; y++ {
		src := plane[y]
		dst := img.Pix[y*img.Stride : y*img.Stride+width]
		for x, v := range src {
			dst[x] = clampToUint8(v + offset)
		}
	}
	return img
}
