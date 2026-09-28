package jpeg2000

import (
	"bytes"
	"os"
	"runtime"
	"testing"
)

// multiBlockImage is the picture testdata/multiblock_256x256.j2k holds, written
// out by the formula it was generated from rather than committed as a second file.
//
// The checkerboard matters: its squares are the code block size, so a block placed
// in the wrong cell changes the picture instead of blurring it. The diagonal ramp
// underneath means two blocks are never identical, so a swap shows too.
func multiBlockImage() []byte {
	const w, h = 256, 256
	px := make([]byte, w*h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			v := ((x*3 + y*5) / 7) % 256
			if (x/32+y/32)%2 == 0 {
				v = (v + 128) % 256
			}
			px[y*w+x] = byte(v)
		}
	}
	return px
}

// decodeMultiBlock reads a fixture and returns one byte per pixel.
func decodeMultiBlock(t *testing.T) []byte {
	return decodeFixture(t, "testdata/multiblock_256x256.j2k")
}

func decodeFixture(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	img, err := Decode(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	bo := img.Bounds()
	if bo.Dx() != 256 || bo.Dy() != 256 {
		t.Fatalf("decoded %dx%d, want 256x256", bo.Dx(), bo.Dy())
	}
	out := make([]byte, 0, 256*256)
	for y := bo.Min.Y; y < bo.Max.Y; y++ {
		for x := bo.Min.X; x < bo.Max.X; x++ {
			r, _, _, _ := img.At(x, y).RGBA()
			out = append(out, byte(r>>8))
		}
	}
	return out
}

// TestAMultiBlockImageDecodesExactly is the first test in this package to read an
// image with more than one code block. The fixture is 256 by 256 with 32 by 32 code
// blocks over three resolutions -- 16 blocks in a subband of the top resolution --
// and it is LOSSLESS, so the answer is known exactly rather than to a tolerance.
//
// It was checked against OpenJPEG 2.5.4 when it was made: zero differing pixels,
// both against opj_decompress's output and against the source the encoder was given.
func TestAMultiBlockImageDecodesExactly(t *testing.T) {
	got := decodeMultiBlock(t)
	want := multiBlockImage()
	if len(got) != len(want) {
		t.Fatalf("decoded %d bytes, want %d", len(got), len(want))
	}
	diff, worst, firstAt := 0, 0, -1
	for i := range want {
		d := int(got[i]) - int(want[i])
		if d < 0 {
			d = -d
		}
		if d != 0 {
			diff++
			if firstAt < 0 {
				firstAt = i
			}
		}
		if d > worst {
			worst = d
		}
	}
	if diff != 0 {
		t.Errorf("%d of %d pixels differ, worst %d levels; first at (%d,%d): got %d want %d",
			diff, len(want), worst, firstAt%256, firstAt/256, got[firstAt], want[firstAt])
	}
}

// TestCodeBlocksDecodedInParallelGiveTheSamePicture.
//
// Code blocks are independent by construction -- ITU-T T.800 makes that a property
// of the format -- so they are decoded on several goroutines, each with its own
// EBCOT decoder. What that must not change is the picture, and the failure it could
// cause is not a crash: two blocks writing each other's rectangles would produce a
// picture that is wrong in patches, on some runs and not others.
//
// GOMAXPROCS is the knob because it is the one the decoder asks about. At one, the
// subband loop takes its serial path; restored, it fans out.
func TestCodeBlocksDecodedInParallelGiveTheSamePicture(t *testing.T) {
	was := runtime.GOMAXPROCS(0)
	t.Cleanup(func() { runtime.GOMAXPROCS(was) })

	// BOTH wavelets, because they take different halves of the loop that places a
	// block's coefficients: 5/3 writes the int32 plane and 9/7 the float64 one.
	// A fixture of one wavelet leaves the other half of that loop unexercised, and
	// this change rewrote both.
	for _, tc := range []struct{ name, path string }{
		{"5/3 reversible", "testdata/multiblock_256x256.j2k"},
		{"9/7 irreversible", "testdata/multiblock97_256x256.j2k"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			runtime.GOMAXPROCS(1)
			serial := decodeFixture(t, tc.path)
			runtime.GOMAXPROCS(was)
			if runtime.GOMAXPROCS(0) < 2 {
				t.Skip("one processor: there is no parallel path to compare against")
			}
			// Several times, because a race that shows on one run may not on the next.
			for i := 0; i < 8; i++ {
				parallel := decodeFixture(t, tc.path)
				if !bytes.Equal(serial, parallel) {
					n, first := 0, -1
					for j := range serial {
						if serial[j] != parallel[j] {
							n++
							if first < 0 {
								first = j
							}
						}
					}
					t.Fatalf("run %d: %d pixels differ between the serial and parallel decode, first at (%d,%d)",
						i, n, first%256, first/256)
				}
			}
		})
	}
}

// TestTheWorkerCountIsBoundedAndNeverZero. A decoder that asked for zero workers
// would hand a subband to nobody, and one that asked for a thousand would put a
// thousand 22 KB state arrays on a page that has no use for them.
func TestTheWorkerCountIsBoundedAndNeverZero(t *testing.T) {
	was := runtime.GOMAXPROCS(0)
	t.Cleanup(func() { runtime.GOMAXPROCS(was) })
	for _, procs := range []int{1, 2, 3, 16, 64} {
		runtime.GOMAXPROCS(procs)
		n := ebcotWorkers()
		if n < 1 {
			t.Errorf("GOMAXPROCS %d gave %d workers", procs, n)
		}
		if n > maxEBCOTWorkers {
			t.Errorf("GOMAXPROCS %d gave %d workers, past the cap of %d", procs, n, maxEBCOTWorkers)
		}
		if procs < maxEBCOTWorkers && n != procs {
			t.Errorf("GOMAXPROCS %d gave %d workers; below the cap it should ask for what it was given", procs, n)
		}
	}
}

// TestEachWorkerGetsItsOwnDecoder, since sharing one would be the race.
func TestEachWorkerGetsItsOwnDecoder(t *testing.T) {
	ds := newEBCOTDecoders(4, 64, 64)
	if len(ds) != 4 {
		t.Fatalf("made %d decoders, want 4", len(ds))
	}
	seen := map[*ebcotDecoder]bool{}
	for _, d := range ds {
		if d == nil {
			t.Fatal("a nil decoder in the set")
		}
		if seen[d] {
			t.Error("the same decoder was handed out twice")
		}
		seen[d] = true
	}
	// And their state arrays must not be shared either.
	ds[0].state[1][1] = 0xAB
	if ds[1].state[1][1] == 0xAB {
		t.Error("two decoders share a state array")
	}
	// A count below one is still one decoder, not none.
	if got := len(newEBCOTDecoders(0, 8, 8)); got != 1 {
		t.Errorf("asking for 0 decoders gave %d, want 1", got)
	}
}

// TestASubsampledComponentIsReadTheWayGetComponentSampleReadIt.
//
// newSampler resolves subsampling once per row where getComponentSample resolved it
// per pixel, and the two must agree exactly -- a nearest-neighbour map is easy to
// shift by one and a shifted chroma plane is a picture with coloured edges, not a
// crash.
func TestASubsampledComponentIsReadTheWayGetComponentSampleReadIt(t *testing.T) {
	// Every shape that matters: full size, halved in each direction, halved in
	// both, an odd output, and a component LARGER than the output.
	for _, tc := range []struct{ compW, compH, outW, outH int }{
		{8, 8, 8, 8},
		{4, 8, 8, 8},
		{8, 4, 8, 8},
		{4, 4, 8, 8},
		{3, 5, 7, 11},
		{16, 16, 8, 8},
		{1, 1, 5, 5},
	} {
		comp := make([][]int32, tc.compH)
		for y := range comp {
			comp[y] = make([]int32, tc.compW)
			for x := range comp[y] {
				comp[y][x] = int32(y*tc.compW + x)
			}
		}
		s := newSampler(comp, tc.outW, tc.outH)
		for y := 0; y < tc.outH; y++ {
			row := s.row(y)
			for x := 0; x < tc.outW; x++ {
				want := getComponentSample(comp, x, y, tc.outW, tc.outH)
				if got := s.at(row, x); got != want {
					t.Fatalf("comp %dx%d into %dx%d at (%d,%d): sampler %d, getComponentSample %d",
						tc.compW, tc.compH, tc.outW, tc.outH, x, y, got, want)
				}
			}
		}
	}
}

// TestAnEmptyComponentReadsAsZero, which is what getComponentSample answered and
// what a nil row has to keep answering.
func TestAnEmptyComponentReadsAsZero(t *testing.T) {
	for _, comp := range [][][]int32{nil, {}, {{}}} {
		s := newSampler(comp, 4, 4)
		row := s.row(0)
		if row != nil {
			t.Errorf("an empty component gave a row of %d", len(row))
		}
		if got := s.at(row, 2); got != 0 {
			t.Errorf("an empty component read %d, want 0", got)
		}
		if want := getComponentSample(comp, 2, 0, 4, 4); want != 0 {
			t.Errorf("getComponentSample says %d for an empty component", want)
		}
	}
}

// TestARowReadTwiceIsExpandedOnce. The expansion buffer is reused, so asking for
// the same row again must give the same answer rather than a half-written one.
func TestARowReadTwiceIsExpandedOnce(t *testing.T) {
	comp := [][]int32{{10, 20}, {30, 40}}
	s := newSampler(comp, 4, 4)
	first := append([]int32(nil), s.row(1)...)
	second := s.row(1)
	for i := range first {
		if first[i] != second[i] {
			t.Fatalf("row 1 read twice gave %v then %v", first, second)
		}
	}
	// And a different row must not return the previous one.
	other := s.row(3)
	if len(other) == len(first) && other[0] == first[0] && comp[1][0] != comp[0][0] {
		// rows 1 and 3 map to component rows 0 and 1, so they differ
		t.Errorf("row 3 gave row 1's contents %v", other)
	}
}

// TestARaggedComponentDoesNotPanic. A component's rows all have the same length in
// any file this decoder writes, and compW is read from the first of them -- so a
// file whose later rows are shorter would index past the end. getComponentSample
// clamped against the FIRST row's width and would have panicked; at() asks the row
// it was given.
func TestARaggedComponentDoesNotPanic(t *testing.T) {
	comp := [][]int32{
		{1, 2, 3, 4},
		{5, 6}, // short
		{7, 8, 9, 10},
		{}, // empty
	}
	s := newSampler(comp, 4, 4)
	for y := 0; y < 4; y++ {
		row := s.row(y)
		for x := 0; x < 4; x++ {
			s.at(row, x) // must not panic
		}
	}
	// And the short row reads its last sample rather than something else's.
	if got := s.at(s.row(1), 3); got != 6 {
		t.Errorf("past the end of a short row gave %d, want its last sample 6", got)
	}
	// An empty row inside a non-empty component gives zero rather than panicking.
	if got := s.at(s.row(3), 0); got != 0 {
		t.Errorf("an empty row gave %d, want 0", got)
	}
}
