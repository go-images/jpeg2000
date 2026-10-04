# jpeg2000

> **A fork of [ajroetker/go-jpeg2000](https://github.com/ajroetker/go-jpeg2000)**,
> under the same Apache-2.0 licence. It exists so that a fix can be used
> without waiting on an upstream that has not published since February.
> The import path is `github.com/go-images/jpeg2000`. Every change made here
> is stated in [NOTICE](NOTICE), as Apache-2.0 section 4(b) requires.
>
> **What differs today**, with every change stated in full in
> [NOTICE](NOTICE):
>
> - A four-component picture whose JP2 header declares the enumerated colour
>   space **CMYK** decodes to an `*image.CMYK` instead of an `*image.RGBA`
>   built from its first three components — which discarded the black plate.
>   **255 levels of disagreement on every pixel became at most 2**, over
>   2 440 044 samples.
> - **A multi-tile encode round-trips.** Eight of sixteen tile grids did not
>   survive a lossless round trip; none fails now, and six were read back by
>   OpenJPEG's `opj_decompress` as well.
> - **A decode holds a quarter of the memory**: 19.8 → **8.6 bytes a pixel** on
>   a grey page, 76.4 → **20.2** on a colour one. A one-component picture comes
>   back as one byte a pixel.
> - A codestream whose component origin is odd no longer crashes, and a tile
>   whose coefficients all quantise away is written and read rather than
>   refused.
> - The tests run on eight architectures and three operating systems, where
>   upstream only ever cross-compiled.


[![Go](https://github.com/go-images/jpeg2000/actions/workflows/go.yml/badge.svg)](https://github.com/go-images/jpeg2000/actions/workflows/go.yml)

A pure Go JPEG2000 codec. Decode and encode JPEG2000 codestreams (.j2k/.j2c) and JP2 files (.jp2) with zero C dependencies.

## Requirements

- Go 1.25+

## Installation

```bash
go get github.com/go-images/jpeg2000
```

## Quick Start

### Decoding

```go
package main

import (
	"image/png"
	"log"
	"os"

	jpeg2000 "github.com/go-images/jpeg2000"
)

func main() {
	f, err := os.Open("input.jp2")
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()

	img, err := jpeg2000.Decode(f)
	if err != nil {
		log.Fatal(err)
	}

	out, _ := os.Create("output.png")
	defer out.Close()
	png.Encode(out, img)
}
```

### Encoding

```go
package main

import (
	"image"
	"image/png"
	"log"
	"os"

	jpeg2000 "github.com/go-images/jpeg2000"
)

func main() {
	f, err := os.Open("input.png")
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()

	img, _, err := image.Decode(f)
	if err != nil {
		log.Fatal(err)
	}

	out, _ := os.Create("output.j2k")
	defer out.Close()

	// Lossless encoding
	err = jpeg2000.Encode(out, img, &jpeg2000.EncodeOptions{
		Lossless: true,
	})
	if err != nil {
		log.Fatal(err)
	}
}
```

### Automatic Format Detection

```go
import (
	"image"
	_ "github.com/go-images/jpeg2000" // register format
)

img, format, err := image.Decode(reader) // format = "jpeg2000"
```

## Features

| Feature | Status |
|---------|--------|
| Decode J2K codestreams | Supported |
| Decode JP2 files | Supported |
| Encode J2K codestreams | Supported |
| Encode JP2 files | Supported |
| 5/3 reversible wavelet (lossless) | Supported |
| 9/7 irreversible wavelet (lossy) | Supported |
| Multi-component color transform (RCT/ICT) | Supported |
| EBCOT Tier-1 (MQ arithmetic coding) | Supported |
| EBCOT Tier-2 (packet assembly) | Supported |
| Rate control (PCRD-opt) | Supported |
| Quality layers | Supported |
| Grayscale and RGB images | Supported |
| Multi-tile images | Supported — **every one of sixteen tile grids survives a lossless round trip**, which `tilegrid_test.go` asserts as a gate |
| A one-pixel tile, lossy | **Not** — the grid that leaves a 1×1 corner tile loses that tile's single coefficient and decodes it to 128, the DC level shift. Pre-existing (the same at v0.12.2 and v0.13.0); reported by `tilegrid_test.go` rather than asserted, so that pinning it does not make it permanent |

## Encode Options

```go
type EncodeOptions struct {
	Lossless       bool      // true = 5/3 + RCT, false = 9/7 + ICT
	Quality        float64   // 0.0-1.0 (lossy only, default: 0.8)
	TargetSize     int       // target file size in bytes (overrides Quality)
	NumLayers      int       // quality layers (default: 1)
	TileWidth      int       // 0 = single tile
	TileHeight     int       // 0 = single tile
	NumResolutions int       // decomposition levels + 1 (default: 6)
	CodeBlockWidth int       // default: 64
	CodeBlockHeight int      // default: 64
	FileFormat     FileFormat // FormatJ2K or FormatJP2
}
```

## Building

```bash
go build ./...
go test ./...
go test -bench=. -benchmem ./...
```

## License

Apache 2.0
