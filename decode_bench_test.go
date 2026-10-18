package jpeg2000

import (
	"bytes"
	"image"
	"image/color"
	"testing"
)

// BenchmarkDecodeLossless1080p is a whole decode of one lossless (5/3 + RCT)
// 1920x1080 RGB picture: what a caller pays, conversions included. The
// per-kernel benchmarks cannot show that: the RCT kernel alone was fast on
// arm64 while the path the decoder ran was twice as slow as a plain loop.
func BenchmarkDecodeLossless1080p(b *testing.B) {
	const w, h = 1920, 1080
	src := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			src.Set(x, y, color.RGBA{uint8(x), uint8(y), uint8(x ^ y), 255})
		}
	}
	var buf bytes.Buffer
	if err := Encode(&buf, src, &EncodeOptions{Lossless: true}); err != nil {
		b.Fatal(err)
	}
	data := buf.Bytes()
	b.SetBytes(int64(w * h * 3))
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if _, err := Decode(bytes.NewReader(data)); err != nil {
			b.Fatal(err)
		}
	}
}
