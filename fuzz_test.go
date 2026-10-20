package jpeg2000

import (
	"bytes"
	"image"
	"image/color"
	"testing"
)

// fuzzSeeds are real codestreams from this package's own encoder, so the
// fuzzer starts from inputs that get deep into the decoder: lossless and
// lossy, grey and colour, one tile and several, J2K and JP2.
func fuzzSeeds(f *testing.F) {
	f.Helper()
	rgb := image.NewRGBA(image.Rect(0, 0, 33, 17))
	grey := image.NewGray(image.Rect(0, 0, 19, 23))
	for y := range 23 {
		for x := range 33 {
			rgb.Set(x, y, color.RGBA{uint8(x * 7), uint8(y * 11), uint8(x ^ y), 255})
			grey.Set(x, y, color.Gray{uint8(x*y + 3)})
		}
	}
	for _, src := range []image.Image{rgb, grey} {
		for _, opts := range []EncodeOptions{
			{Lossless: true},
			{Lossless: false, Quality: 0.5},
			{Lossless: true, TileWidth: 16, TileHeight: 8},
			{Lossless: true, NumResolutions: 2, FileFormat: FormatJP2},
		} {
			var buf bytes.Buffer
			if err := Encode(&buf, src, &opts); err != nil {
				f.Fatalf("seed encode %+v: %v", opts, err)
			}
			f.Add(buf.Bytes())
		}
	}
	f.Add([]byte{})
	f.Add([]byte{0xff, 0x4f, 0xff, 0x51}) // SOC, SIZ marker, nothing after
}

// fuzzMaxPixels bounds what a fuzzed header may make the decoder allocate. A
// caller that decodes untrusted files does the same through DecodeConfig
// (go-pdfkit/render does), and without it the fuzzer only rediscovers that a
// header can claim 65535 x 65535 samples.
const fuzzMaxPixels = 1 << 20

// FuzzDecode holds the decoder to one property on any input: it returns an
// image or an error, and never panics. A decoder of untrusted bytes that can
// be made to panic takes down the process that called it.
func FuzzDecode(f *testing.F) {
	fuzzSeeds(f)
	f.Fuzz(func(t *testing.T, data []byte) {
		cfg, err := DecodeConfig(bytes.NewReader(data))
		if err != nil {
			return
		}
		if cfg.Width <= 0 || cfg.Height <= 0 || int64(cfg.Width)*int64(cfg.Height) > fuzzMaxPixels {
			return
		}
		img, err := Decode(bytes.NewReader(data))
		if err != nil {
			return
		}
		if img == nil {
			t.Fatal("Decode returned neither an image nor an error")
		}
	})
}

// FuzzDecodeConfig: reading the header alone must not panic either, and is
// what a caller runs FIRST to decide whether a decode is affordable.
func FuzzDecodeConfig(f *testing.F) {
	fuzzSeeds(f)
	f.Fuzz(func(t *testing.T, data []byte) {
		cfg, err := DecodeConfig(bytes.NewReader(data))
		if err == nil && (cfg.Width < 0 || cfg.Height < 0) {
			t.Fatalf("negative size %dx%d reported without an error", cfg.Width, cfg.Height)
		}
	})
}
