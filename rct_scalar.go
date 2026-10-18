//go:build !arm64 && !(amd64 && goexperiment.simd)

package jpeg2000

// rctHighway is false wherever highway's dispatch falls back to its generic
// Vec[T]. That type emulates a vector with a heap-allocated slice, and every
// Load, Add, Mul, Sub and ShiftRight calls make(). For the integer RCT this
// came to 12.4 million allocations for one 4K frame on amd64, which made it
// 6.6x SLOWER than the plain loop: 402.8 ms against 61.3 ms forward, and
// 339.1 ms against 54.2 ms inverse (GitHub ubuntu-latest, Go 1.27.2, n=10).
//
// This is the build the fleet ships, since nothing sets GOEXPERIMENT=simd,
// and it covers amd64 as well as every architecture that is neither arm64
// nor amd64. The float ICT is not affected: its fallback does not allocate.
const rctHighway = false
