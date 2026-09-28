package jpeg2000

// sampler reads one component as rows the OUTPUT can index directly.
//
// getComponentSample answers the same question one pixel at a time, and answering
// it that way costs a function call, two length loads, two comparisons and -- when
// the component is subsampled -- two integer DIVISIONS, for every pixel of the
// picture. A 2533 by 3590 scan has 9.1 million of them and carries two such images.
//
// Almost no component is subsampled: a scanned page is one plane at full size, and
// then compY is y and compX is x and the whole function is comp[y][x]. So the
// subsampling is resolved ONCE, here, and a caller gets a slice.
//
// When a component IS subsampled the row is expanded into a buffer that is reused
// across rows, which trades width divisions for width copies and still leaves every
// call site a plain index. The buffer belongs to the sampler, so two components
// never share one.
type sampler struct {
	comp         [][]int32
	compW, compH int
	outW, outH   int
	subsampledX  bool
	subsampledY  bool
	buf          []int32
	// lastY is which output row buf holds, so a caller that reads a row twice does
	// not expand it twice. -1 means nothing.
	lastY int
}

// newSampler resolves how a component maps onto an output of outW by outH.
func newSampler(comp [][]int32, outW, outH int) *sampler {
	s := &sampler{comp: comp, outW: outW, outH: outH, lastY: -1}
	s.compH = len(comp)
	if s.compH > 0 {
		s.compW = len(comp[0])
	}
	s.subsampledY = s.compH < outH
	s.subsampledX = s.compW < outW
	if s.subsampledX {
		s.buf = make([]int32, outW)
	}
	return s
}

// row returns output row y as a slice indexable by output x, or nil when the
// component is empty -- which getComponentSample reported as a zero sample, and a
// nil row gives the caller the same answer without a branch per pixel.
func (s *sampler) row(y int) []int32 {
	if s.compH == 0 || s.compW == 0 {
		return nil
	}
	cy := y
	if s.subsampledY {
		cy = y * s.compH / s.outH
	}
	// No clamp on cy: y is below outH, and cy is either y with compH >= outH or
	// y*compH/outH which is below compH. Both are below compH, so a clamp here
	// would be a branch no test could tell from its absence. getComponentSample
	// carried one; it was unreachable there too.
	if !s.subsampledX {
		// The component's own row. A row longer than the output is fine: the
		// caller indexes it by output x, which stays inside it.
		return s.comp[cy]
	}
	if s.lastY == y {
		return s.buf
	}
	src := s.comp[cy]
	for x := 0; x < s.outW; x++ {
		cx := x * s.compW / s.outW
		if cx >= s.compW {
			cx = s.compW - 1
		}
		s.buf[x] = src[cx]
	}
	s.lastY = y
	return s.buf
}

// at reads one sample from a row row() returned.
//
// The clamp is NOT dead, and it guards something the old accessor did not: a
// component whose rows are not all the same length. compW is read from comp[0], so
// a later row that is shorter would be indexed past its end -- getComponentSample
// clamped against comp[0]'s width and would have panicked. A malformed file must
// not take the decoder down, so the clamp asks the ROW how long it is.
func (s *sampler) at(row []int32, x int) int32 {
	if len(row) == 0 {
		// Nil for an empty component, and EMPTY for a ragged one: clamping to
		// len(row)-1 here gave -1 and took the decoder down, which the test that
		// found it now pins.
		return 0
	}
	if x >= len(row) {
		x = len(row) - 1
	}
	return row[x]
}
