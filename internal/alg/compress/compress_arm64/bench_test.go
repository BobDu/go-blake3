package compress_arm64_test

import (
	"testing"

	"github.com/zeebo/blake3/internal/alg/compress/compress_arm64"
	"github.com/zeebo/blake3/internal/alg/compress/compress_pure"
)

// The shape here is the one compressBlocks uses: each block's chaining value
// feeds the next, so the measurement sees the serial dependency rather than
// throughput over independent blocks.
func benchSerial(b *testing.B, f func(*[8]uint32, *[16]uint32, uint64, uint32, uint32, *[16]uint32)) {
	var chain [8]uint32
	var block [16]uint32
	var out [16]uint32
	b.SetBytes(64)
	for i := 0; i < b.N; i++ {
		f(&chain, &block, uint64(i), 64, 0, &out)
		copy(chain[:], out[:8])
	}
}

func BenchmarkCompress1PureGo(b *testing.B)   { benchSerial(b, compress_pure.Compress) }
func BenchmarkCompress2Baseline(b *testing.B) { benchSerial(b, compress_arm64.CompressBaseline) }
func BenchmarkCompress3Reassoc(b *testing.B)  { benchSerial(b, compress_arm64.Compress) }
func BenchmarkCompress4Hoisted(b *testing.B)  { benchSerial(b, compress_arm64.CompressHoisted) }

// BenchmarkCompress0CallOverhead runs the same loop over a function that
// returns immediately, so the difference between it and the others is what the
// compression itself costs rather than what reaching it costs.
func BenchmarkCompress0CallOverhead(b *testing.B) { benchSerial(b, compress_arm64.CallOverhead) }

// nopGo is the Go-side counterpart of CallOverhead: subtracting each from the
// measurement it belongs to leaves the compression itself, without the cost of
// reaching it, which differs between a Go function and an assembly one.
//
//go:noinline
func nopGo(chain *[8]uint32, block *[16]uint32, counter uint64, blen uint32, flags uint32, out *[16]uint32) {
}

func BenchmarkCompress0NopGo(b *testing.B) { benchSerial(b, nopGo) }
