package jpeg2000

import (
	"bytes"
	"encoding/binary"
	"errors"
	"runtime"
	"testing"
)

// manyComponents builds a codestream declaring w x h with c 8-bit components
// and NO packet data: SOC, SIZ, COD (no decomposition), QCD (no quantisation),
// one tile-part holding only SOT+SOD, EOC. A component no tile writes is still
// allocated at its declared size, so this is c planes of w*h int32 asked for by
// a header of about 3c bytes.
func manyComponents(w, h, c int) []byte { return manyComponentsTiled(w, h, w, h, c) }

// manyComponentsTiled is manyComponents with tiles of tw x th; only tile 0
// has a tile-part.
func manyComponentsTiled(w, h, tw, th, c int) []byte {
	be := binary.BigEndian
	var b bytes.Buffer
	put16 := func(v int) { var x [2]byte; be.PutUint16(x[:], uint16(v)); b.Write(x[:]) }
	put32 := func(v int) { var x [4]byte; be.PutUint32(x[:], uint32(v)); b.Write(x[:]) }
	put16(0xFF4F) // SOC
	put16(0xFF51) // SIZ
	put16(38 + 3*c)
	put16(0)  // Rsiz
	put32(w)  // Xsiz
	put32(h)  // Ysiz
	put32(0)  // XOsiz
	put32(0)  // YOsiz
	put32(tw) // XTsiz
	put32(th) // YTsiz
	put32(0)  // XTOsiz
	put32(0)  // YTOsiz
	put16(c)  // Csiz
	for i := 0; i < c; i++ {
		b.Write([]byte{7, 1, 1}) // Ssiz 8-bit unsigned, XRsiz, YRsiz
	}
	put16(0xFF52) // COD
	put16(12)
	b.Write([]byte{0, 0})          // Scod, progression LRCP
	put16(1)                       // layers
	b.Write([]byte{0})             // MCT off
	b.Write([]byte{0, 4, 4, 0, 1}) // levels 0, 64x64 code-blocks, style 0, 5/3
	put16(0xFF5C)                  // QCD
	put16(4)
	b.Write([]byte{0x40, 0x48}) // no quantisation, 2 guard bits; one subband
	put16(0xFF90)               // SOT
	put16(10)
	put16(0)  // Isot
	put32(14) // Psot: SOT (12) + SOD (2)
	b.Write([]byte{0, 1})
	put16(0xFF93) // SOD
	put16(0xFFD9) // EOC
	return b.Bytes()
}

// Measured before the ceiling: this 846-byte codestream allocated 277.5 MB and
// decoded without error. Every entry point must now refuse it before the
// planes exist.
func TestManyComponentsAreRefusedBeforeAllocating(t *testing.T) {
	data := manyComponents(4096, 4096, 64) // 2^30 samples, 4 GiB of int32
	entries := map[string]func() error{
		"Decode": func() error { _, err := Decode(bytes.NewReader(data)); return err },
		"DecodeWithOptions": func() error {
			_, err := DecodeWithOptions(bytes.NewReader(data), DecodeOptions{})
			return err
		},
		"DecodeWithUpsampling": func() error { _, err := DecodeWithUpsampling(bytes.NewReader(data)); return err },
	}
	for name, decode := range entries {
		t.Run(name, func(t *testing.T) {
			var before, after runtime.MemStats
			runtime.GC()
			runtime.ReadMemStats(&before)
			err := decode()
			runtime.ReadMemStats(&after)
			if !errors.Is(err, ErrTooLarge) {
				t.Fatalf("err = %v, want ErrTooLarge", err)
			}
			if grew := after.TotalAlloc - before.TotalAlloc; grew > 4<<20 {
				t.Fatalf("allocated %d bytes before refusing", grew)
			}
		})
	}
}

// A subsampled component is stored small and upsampled to the full image:
// 3 components of 32768x32768 at XRsiz=YRsiz=255 are 129x129 each in the
// file, but upsampling asks for 3 x 2^30 samples. Refused before allocating.
func TestSubsampledComponentsCountAtTheirUpsampledSize(t *testing.T) {
	data := manyComponents(32768, 32768, 3)
	// Patch every component's XRsiz/YRsiz (SIZ starts at offset 2; the
	// per-component triples start 2+2+38 = 42 bytes in).
	for c := 0; c < 3; c++ {
		data[42+3*c+1], data[42+3*c+2] = 255, 255
	}
	for name, decode := range map[string]func() error{
		"Decode":               func() error { _, err := Decode(bytes.NewReader(data)); return err },
		"DecodeWithUpsampling": func() error { _, err := DecodeWithUpsampling(bytes.NewReader(data)); return err },
	} {
		t.Run(name, func(t *testing.T) {
			var before, after runtime.MemStats
			runtime.GC()
			runtime.ReadMemStats(&before)
			err := decode()
			runtime.ReadMemStats(&after)
			if !errors.Is(err, ErrTooLarge) {
				t.Fatalf("err = %v, want ErrTooLarge", err)
			}
			if grew := after.TotalAlloc - before.TotalAlloc; grew > 4<<20 {
				t.Fatalf("allocated %d bytes before refusing", grew)
			}
		})
	}
}

// The ceiling is a ceiling, not a guess below it: exactly MaxSamples decodes,
// one more is refused, and Reduce counts (a halved image holds a quarter).
func TestTheSampleCeilingIsExact(t *testing.T) {
	data := manyComponents(64, 64, 4) // 16384 samples
	cases := []struct {
		opts DecodeOptions
		ok   bool
	}{
		{DecodeOptions{MaxSamples: 16384}, true},
		{DecodeOptions{MaxSamples: 16383}, false},
	}
	for _, c := range cases {
		_, err := DecodeWithOptions(bytes.NewReader(data), c.opts)
		if c.ok != (err == nil) {
			t.Fatalf("MaxSamples %d: err = %v", c.opts.MaxSamples, err)
		}
		if !c.ok && !errors.Is(err, ErrTooLarge) {
			t.Fatalf("MaxSamples %d: err = %v, want ErrTooLarge", c.opts.MaxSamples, err)
		}
	}
	header, _, err := parseCodestream(data)
	if err != nil {
		t.Fatal(err)
	}
	d := &Decoder{header: header, opts: DecodeOptions{Reduce: 1, MaxSamples: 4096}}
	if err := d.checkSampleBudget(); err != nil {
		t.Fatalf("at Reduce 1 the image holds 4096 samples: %v", err)
	}
}
