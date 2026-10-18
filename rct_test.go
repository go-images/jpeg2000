package jpeg2000

import (
	"math/rand/v2"
	"slices"
	"testing"
)

// TestRCTScalarMatchesHighway holds the two implementations to the same
// answer, sample for sample, on every build: the scalar one is what ships
// where highway has no vector kernel, and the highway one is what ships on
// arm64, so a difference between them would make a lossless file decode to
// different pixels on different machines.
//
// The samples are signed and reach past 16 bits so the arithmetic shift is
// exercised on negative sums: ITU-T T.800 G.2 defines the RCT with floor, and
// >>2 is floor only because it is an arithmetic shift. The widths are not
// multiples of any vector length so the tail of each row is covered.
func TestRCTScalarMatchesHighway(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 2))
	for _, size := range []struct{ w, h int }{{1, 1}, {3, 2}, {17, 5}, {33, 9}, {130, 7}} {
		plane := func() [][]int32 {
			p := int32Plane(size.w, size.h)
			for _, row := range p {
				for i := range row {
					row[i] = rng.Int32N(1<<18) - 1<<17
				}
			}
			return p
		}
		r, g, b := plane(), plane(), plane()

		ys, cbs, crs := forwardRCTScalar(r, g, b)
		yh, cbh, crh := forwardRCTHighway(r, g, b)
		for name, pair := range map[string][2][][]int32{"Y": {ys, yh}, "Cb": {cbs, cbh}, "Cr": {crs, crh}} {
			if !planesEqual(pair[0], pair[1]) {
				t.Errorf("%dx%d forward %s: scalar and highway differ", size.w, size.h, name)
			}
		}

		rs, gs, bs := applyRCTScalar(ys, cbs, crs)
		rh, gh, bh := applyRCTHighway(ys, cbs, crs)
		for name, pair := range map[string][2][][]int32{"R": {rs, rh}, "G": {gs, gh}, "B": {bs, bh}} {
			if !planesEqual(pair[0], pair[1]) {
				t.Errorf("%dx%d inverse %s: scalar and highway differ", size.w, size.h, name)
			}
		}
		// And the transform is lossless: the round trip gives back the input.
		if !planesEqual(rs, r) || !planesEqual(gs, g) || !planesEqual(bs, b) {
			t.Errorf("%dx%d: scalar round trip is not lossless", size.w, size.h)
		}
	}
}

func TestRCTEmpty(t *testing.T) {
	if y, cb, cr := forwardRCTScalar(nil, nil, nil); y != nil || cb != nil || cr != nil {
		t.Fatal("forward of no rows must be nil")
	}
	if r, g, b := applyRCTScalar(nil, nil, nil); r != nil || g != nil || b != nil {
		t.Fatal("inverse of no rows must be nil")
	}
}

func planesEqual(a, b [][]int32) bool {
	return slices.EqualFunc(a, b, slices.Equal[[]int32])
}
