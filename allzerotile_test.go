package jpeg2000

import (
	"bytes"
	"image"
	"image/color"
	"testing"
)

// TestATileWhoseCoefficientsAllQuantiseAwayStillDecodes pins a stream this
// decoder used to REFUSE.
//
// A 255x255 tile grid over a 256x256 picture leaves a 1x1 tile at the corner.
// At anything below the top quality its single coefficient quantises to zero,
// so every code block of that tile is excluded in every layer -- and the
// decoder read that as a parsing failure: "no packet data read: parsed 1 bytes
// from bitstream but got 0 bytes of data", and gave up on the whole page.
//
// Nothing was wrong with the file. OpenJPEG decodes it without complaint, and
// renders that corner pixel as 128 -- the DC level shift and nothing else --
// which is exactly what this decoder produces now. The inference "nothing
// included means a misparse" was the defect.
//
// The qualities are swept because the defect appeared BELOW the top one and
// the top one alone would have missed it.
func TestATileWhoseCoefficientsAllQuantiseAwayStillDecodes(t *testing.T) {
	const n = 256
	src := image.NewGray(image.Rect(0, 0, n, n))
	for y := range n {
		for x := range n {
			src.SetGray(x, y, color.Gray{Y: uint8((x*7 + y*3) % 256)})
		}
	}
	for _, q := range []float64{0.2, 0.5, 0.8, 1.0} {
		var buf bytes.Buffer
		if err := Encode(&buf, src, &EncodeOptions{
			Quality: q, TileWidth: 255, TileHeight: 255}); err != nil {
			t.Fatalf("quality %.1f: encode: %v", q, err)
		}
		img, err := Decode(bytes.NewReader(buf.Bytes()))
		if err != nil {
			t.Errorf("quality %.1f: %v", q, err)
			continue
		}
		// The page is DRAWN, not merely accepted. The control is deliberately
		// not a fidelity check: at quality 0.2 an interior pixel is 45 levels
		// out and that is what quality 0.2 means. What must not happen is a
		// page that comes back as the DC level shift and nothing else, which
		// is what every pixel would be if no coefficient anywhere had been
		// read -- the shape the removed guard was trying to catch.
		levels := map[uint8]int{}
		for y := 0; y < n; y += 8 {
			for x := 0; x < n; x += 8 {
				g, _, _, _ := img.At(x, y).RGBA()
				levels[uint8(g>>8)]++
			}
		}
		if len(levels) < 16 {
			t.Errorf("quality %.1f: the page came back in %d distinct levels, "+
				"which is not a drawn page", q, len(levels))
		}
	}
}
