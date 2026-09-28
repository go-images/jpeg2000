package jpeg2000

import (
	"image"
	"testing"

	hwyimage "github.com/ajroetker/go-highway/hwy/contrib/image"
)

// wholePageICT is convertYCbCrInt32ToRGBA as it was before the band: six SIMD
// images the size of the PAGE, one call to the kernel, one pass out.
//
// It is kept here and not deleted because it is what the banded form is
// MEASURED AGAINST. A test that only checked the banded output against a
// fixture would pin what it draws; only the pair says it draws what the form it
// replaced drew, which is the whole claim -- the transform is pixel-wise, so
// the rows held at a time are free to choose.
func wholePageICT(components [][][]int32, width, height int, bitDepths []int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	bitDepth := func(c int) int {
		if c < len(bitDepths) && bitDepths[c] > 0 {
			return bitDepths[c]
		}
		return 8
	}
	scale := func(c int) float64 {
		if bitDepth(c) == 8 {
			return 1.0
		}
		return 255.0 / float64((uint(1)<<bitDepth(c))-1)
	}
	buf := getFloat64Buf(width, height)
	defer putFloat64Buf(buf)

	yOffset := float64(uint(1) << (bitDepth(0) - 1))
	for y := range height {
		row := buf.imgs[0].Row(y)
		for x, v := range components[0][y] {
			row[x] = float64(v) + yOffset
		}
	}
	for c := 1; c <= 2; c++ {
		for y := range height {
			row := buf.imgs[c].Row(y)
			for x, v := range components[c][y] {
				row[x] = float64(v)
			}
		}
	}
	hwyimage.InverseICT(buf.imgs[0], buf.imgs[1], buf.imgs[2],
		buf.imgs[3], buf.imgs[4], buf.imgs[5])

	rScale, gScale, bScale := scale(0), scale(1), scale(2)
	for y := range height {
		rRow, gRow, bRow := buf.imgs[3].Row(y), buf.imgs[4].Row(y), buf.imgs[5].Row(y)
		idx := img.PixOffset(0, y)
		for x := range width {
			img.Pix[idx+0] = clampFloat(rRow[x] * rScale)
			img.Pix[idx+1] = clampFloat(gRow[x] * gScale)
			img.Pix[idx+2] = clampFloat(bRow[x] * bScale)
			img.Pix[idx+3] = 255
			idx += 4
		}
	}
	return img
}

// ycbcrComponents makes three planes whose values sweep the range and vary down
// the page, so a band that reads the wrong rows, or writes them to the wrong
// place, shows rather than hides.
func ycbcrComponents(width, height int) [][][]int32 {
	comps := make([][][]int32, 3)
	for c := range comps {
		comps[c] = make([][]int32, height)
		for y := range height {
			comps[c][y] = make([]int32, width)
			for x := range width {
				switch c {
				case 0:
					comps[c][y][x] = int32((y*7+x*3)%256) - 128
				case 1:
					comps[c][y][x] = int32((x*5+y)%256) - 128
				default:
					comps[c][y][x] = int32((x+y*11)%256) - 128
				}
			}
		}
	}
	return comps
}

// TestBandingTheICTChangesNoPixel is the whole claim. The heights are chosen
// around the band: one exactly on it, one a single row over -- where the tail
// band is one row and every off-by-one lives -- one just under, and one many
// bands deep with a ragged tail.
func TestBandingTheICTChangesNoPixel(t *testing.T) {
	const width = 37 // not a multiple of any vector width either
	for _, height := range []int{1, ictBand - 1, ictBand, ictBand + 1, 3*ictBand + 17} {
		comps := ycbcrComponents(width, height)
		want := wholePageICT(comps, width, height, []int{8, 8, 8})
		got := convertYCbCrInt32ToRGBA(comps, width, height, []int{8, 8, 8})
		if len(got.Pix) != len(want.Pix) {
			t.Fatalf("height %d: %d bytes against %d", height, len(got.Pix), len(want.Pix))
		}
		// Without this the comparison is also satisfied by two blank images.
		var ink int
		for _, v := range want.Pix {
			if v != 0 && v != 255 {
				ink++
			}
		}
		if ink == 0 {
			t.Fatalf("height %d: the reference drew nothing, so identity says nothing", height)
		}
		for i := range got.Pix {
			if got.Pix[i] != want.Pix[i] {
				t.Fatalf("height %d, byte %d (row %d): banded %d, whole page %d",
					height, i, i/(width*4), got.Pix[i], want.Pix[i])
			}
		}
	}
}

// TestBandingHoldsBandRowsNotPageRows pins the reason the band exists. The test
// above would pass just as well if ictBand were the height of the page, which
// is exactly the change being undone.
func TestBandingHoldsBandRowsNotPageRows(t *testing.T) {
	if ictBand <= 0 {
		t.Fatalf("ictBand is %d", ictBand)
	}
	const width, height = 64, 4096
	comps := ycbcrComponents(width, height)
	convertYCbCrInt32ToRGBA(comps, width, height, []int{8, 8, 8})

	// The pool holds what the last call asked for. If the transform had taken
	// the page in one piece, the pooled images would be page-high.
	buf := getFloat64Buf(width, ictBand)
	defer putFloat64Buf(buf)
	if buf.h != ictBand {
		t.Errorf("the pooled images are %d rows high, want %d", buf.h, ictBand)
	}
	if buf.h >= height {
		t.Errorf("the transform is holding %d rows of a %d-row page", buf.h, height)
	}
}
