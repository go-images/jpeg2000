//go:build arm64 || (amd64 && goexperiment.simd)

package jpeg2000

// rctHighway reports whether highway has a real vector kernel for the
// integer colour transform on this build: NEON assembly on arm64, AVX2 or
// AVX-512 on amd64 when GOEXPERIMENT=simd is set. See rct_scalar.go for
// every other build.
const rctHighway = true
