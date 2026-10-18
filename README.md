# jpeg2000

> **A fork of [ajroetker/go-jpeg2000](https://github.com/ajroetker/go-jpeg2000)**,
> under the same Apache-2.0 licence. It exists so that a fix can be used
> without waiting on an upstream that has not published since February.
> The import path is `github.com/go-images/jpeg2000`. Every change made here
> is stated in [NOTICE](NOTICE), as Apache-2.0 section 4(b) requires.
>
> **What differs today** (the full list, with the files, is in NOTICE):
>
> - a four-component picture whose JP2 header declares the enumerated colour
>   space **CMYK** decodes to an `*image.CMYK`, not an `*image.RGBA` built from
>   its first three components, which discarded the black plate. Against
>   poppler on the reference picture, **255 levels of disagreement on every
>   pixel became at most 2**, over 2 440 044 samples;
> - six decoder and encoder corrections: an odd origin no longer crashes the
>   9/7 synthesis; 9/7 coefficients are carried as float32, as OpenJPEG
>   carries them; tiled encodes place their code blocks and tiles where the
>   decoder and OpenJPEG read them; a tile whose blocks all quantise away is
>   accepted;
> - the SIMD dependency is [go-simd/highway](https://github.com/go-simd/highway),
>   a fork of go-highway that consumers can `go mod tidy` against;
> - the lossless colour transform is a plain loop, **22.8× faster on amd64
>   and 2.1× on arm64** than the highway path it replaced for a 4K frame
>   (6 allocations instead of 12.45 million on amd64);
> - many decode-speed and memory changes that leave the output identical.

[![Go](https://github.com/go-images/jpeg2000/actions/workflows/go.yml/badge.svg)](https://github.com/go-images/jpeg2000/actions/workflows/go.yml)

A pure Go JPEG2000 codec. Decode and encode JPEG2000 codestreams (.j2k/.j2c) and JP2 files (.jp2) with zero C dependencies.

## Requirements

- Go 1.27+

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

	"github.com/go-images/jpeg2000"
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

	"github.com/go-images/jpeg2000"
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
| Multi-tile images | Supported |

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
