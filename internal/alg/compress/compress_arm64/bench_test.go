package compress_arm64_test

import (
	"math/bits"
	"testing"

	"github.com/zeebo/assert"
	"github.com/zeebo/pcg"

	"github.com/zeebo/blake3/internal/alg/compress/compress_arm64"
	"github.com/zeebo/blake3/internal/alg/compress/compress_pure"
	"github.com/zeebo/blake3/internal/consts"
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

// The reassociated form of the pure Go round function, kept here rather than in
// the package so the shipping code stays as it is upstream. Measuring it next
// to the real one shows whether the reordering that the assembly needs is worth
// anything to the compiler as well.
func gReassoc(a, b, c, d, mx, my uint32) (uint32, uint32, uint32, uint32) {
	a += mx
	a += b
	d = bits.RotateLeft32(d^a, -16)
	c += d
	b = bits.RotateLeft32(b^c, -12)
	a += my
	a += b
	d = bits.RotateLeft32(d^a, -8)
	c += d
	b = bits.RotateLeft32(b^c, -7)
	return a, b, c, d
}

func rcompressReassoc(s *[16]uint32, m *[16]uint32) {
	const (
		a = 10
		b = 11
		c = 12
		d = 13
		e = 14
		f = 15
	)

	s0, s1, s2, s3 := s[0+0], s[0+1], s[0+2], s[0+3]
	s4, s5, s6, s7 := s[0+4], s[0+5], s[0+6], s[0+7]
	s8, s9, sa, sb := s[8+0], s[8+1], s[8+2], s[8+3]
	sc, sd, se, sf := s[8+4], s[8+5], s[8+6], s[8+7]

	s0, s4, s8, sc = gReassoc(s0, s4, s8, sc, m[0], m[1])
	s1, s5, s9, sd = gReassoc(s1, s5, s9, sd, m[2], m[3])
	s2, s6, sa, se = gReassoc(s2, s6, sa, se, m[4], m[5])
	s3, s7, sb, sf = gReassoc(s3, s7, sb, sf, m[6], m[7])
	s0, s5, sa, sf = gReassoc(s0, s5, sa, sf, m[8], m[9])
	s1, s6, sb, sc = gReassoc(s1, s6, sb, sc, m[a], m[b])
	s2, s7, s8, sd = gReassoc(s2, s7, s8, sd, m[c], m[d])
	s3, s4, s9, se = gReassoc(s3, s4, s9, se, m[e], m[f])

	s0, s4, s8, sc = gReassoc(s0, s4, s8, sc, m[2], m[6])
	s1, s5, s9, sd = gReassoc(s1, s5, s9, sd, m[3], m[a])
	s2, s6, sa, se = gReassoc(s2, s6, sa, se, m[7], m[0])
	s3, s7, sb, sf = gReassoc(s3, s7, sb, sf, m[4], m[d])
	s0, s5, sa, sf = gReassoc(s0, s5, sa, sf, m[1], m[b])
	s1, s6, sb, sc = gReassoc(s1, s6, sb, sc, m[c], m[5])
	s2, s7, s8, sd = gReassoc(s2, s7, s8, sd, m[9], m[e])
	s3, s4, s9, se = gReassoc(s3, s4, s9, se, m[f], m[8])

	s0, s4, s8, sc = gReassoc(s0, s4, s8, sc, m[3], m[4])
	s1, s5, s9, sd = gReassoc(s1, s5, s9, sd, m[a], m[c])
	s2, s6, sa, se = gReassoc(s2, s6, sa, se, m[d], m[2])
	s3, s7, sb, sf = gReassoc(s3, s7, sb, sf, m[7], m[e])
	s0, s5, sa, sf = gReassoc(s0, s5, sa, sf, m[6], m[5])
	s1, s6, sb, sc = gReassoc(s1, s6, sb, sc, m[9], m[0])
	s2, s7, s8, sd = gReassoc(s2, s7, s8, sd, m[b], m[f])
	s3, s4, s9, se = gReassoc(s3, s4, s9, se, m[8], m[1])

	s0, s4, s8, sc = gReassoc(s0, s4, s8, sc, m[a], m[7])
	s1, s5, s9, sd = gReassoc(s1, s5, s9, sd, m[c], m[9])
	s2, s6, sa, se = gReassoc(s2, s6, sa, se, m[e], m[3])
	s3, s7, sb, sf = gReassoc(s3, s7, sb, sf, m[d], m[f])
	s0, s5, sa, sf = gReassoc(s0, s5, sa, sf, m[4], m[0])
	s1, s6, sb, sc = gReassoc(s1, s6, sb, sc, m[b], m[2])
	s2, s7, s8, sd = gReassoc(s2, s7, s8, sd, m[5], m[8])
	s3, s4, s9, se = gReassoc(s3, s4, s9, se, m[1], m[6])

	s0, s4, s8, sc = gReassoc(s0, s4, s8, sc, m[c], m[d])
	s1, s5, s9, sd = gReassoc(s1, s5, s9, sd, m[9], m[b])
	s2, s6, sa, se = gReassoc(s2, s6, sa, se, m[f], m[a])
	s3, s7, sb, sf = gReassoc(s3, s7, sb, sf, m[e], m[8])
	s0, s5, sa, sf = gReassoc(s0, s5, sa, sf, m[7], m[2])
	s1, s6, sb, sc = gReassoc(s1, s6, sb, sc, m[5], m[3])
	s2, s7, s8, sd = gReassoc(s2, s7, s8, sd, m[0], m[1])
	s3, s4, s9, se = gReassoc(s3, s4, s9, se, m[6], m[4])

	s0, s4, s8, sc = gReassoc(s0, s4, s8, sc, m[9], m[e])
	s1, s5, s9, sd = gReassoc(s1, s5, s9, sd, m[b], m[5])
	s2, s6, sa, se = gReassoc(s2, s6, sa, se, m[8], m[c])
	s3, s7, sb, sf = gReassoc(s3, s7, sb, sf, m[f], m[1])
	s0, s5, sa, sf = gReassoc(s0, s5, sa, sf, m[d], m[3])
	s1, s6, sb, sc = gReassoc(s1, s6, sb, sc, m[0], m[a])
	s2, s7, s8, sd = gReassoc(s2, s7, s8, sd, m[2], m[6])
	s3, s4, s9, se = gReassoc(s3, s4, s9, se, m[4], m[7])

	s0, s4, s8, sc = gReassoc(s0, s4, s8, sc, m[b], m[f])
	s1, s5, s9, sd = gReassoc(s1, s5, s9, sd, m[5], m[0])
	s2, s6, sa, se = gReassoc(s2, s6, sa, se, m[1], m[9])
	s3, s7, sb, sf = gReassoc(s3, s7, sb, sf, m[8], m[6])
	s0, s5, sa, sf = gReassoc(s0, s5, sa, sf, m[e], m[a])
	s1, s6, sb, sc = gReassoc(s1, s6, sb, sc, m[2], m[c])
	s2, s7, s8, sd = gReassoc(s2, s7, s8, sd, m[3], m[4])
	s3, s4, s9, se = gReassoc(s3, s4, s9, se, m[7], m[d])

	s[8+0] = s8 ^ s[0]
	s[8+1] = s9 ^ s[1]
	s[8+2] = sa ^ s[2]
	s[8+3] = sb ^ s[3]
	s[8+4] = sc ^ s[4]
	s[8+5] = sd ^ s[5]
	s[8+6] = se ^ s[6]
	s[8+7] = sf ^ s[7]

	s[0] = s0 ^ s8
	s[1] = s1 ^ s9
	s[2] = s2 ^ sa
	s[3] = s3 ^ sb
	s[4] = s4 ^ sc
	s[5] = s5 ^ sd
	s[6] = s6 ^ se
	s[7] = s7 ^ sf
}

func compressPureReassoc(chain *[8]uint32, block *[16]uint32, counter uint64, blen uint32, flags uint32, out *[16]uint32) {
	*out = [16]uint32{
		chain[0], chain[1], chain[2], chain[3],
		chain[4], chain[5], chain[6], chain[7],
		consts.IV0, consts.IV1, consts.IV2, consts.IV3,
		uint32(counter), uint32(counter >> 32), blen, flags,
	}
	rcompressReassoc(out, block)
}

func BenchmarkCompress1bPureGoReassoc(b *testing.B) { benchSerial(b, compressPureReassoc) }

// TestPureReassocSame keeps the local copy honest: if it ever drifts from the
// real one the benchmark comparing them stops meaning anything.
func TestPureReassocSame(t *testing.T) {
	var chain [8]uint32
	var block [16]uint32

	for i := 0; i < 1e4; i++ {
		var want, got [16]uint32

		counter, blen, flags := pcg.Uint64(), pcg.Uint32(), pcg.Uint32()
		for i := range &chain {
			chain[i] = pcg.Uint32()
		}
		for i := range &block {
			block[i] = pcg.Uint32()
		}

		compress_pure.Compress(&chain, &block, counter, blen, flags, &want)
		compressPureReassoc(&chain, &block, counter, blen, flags, &got)

		assert.Equal(t, got, want)
	}
}

// gReassocLate and the function pair below it are byte-for-byte the same as
// gReassoc, placed further along in the file. If placement mattered, the two
// would not measure the same, so comparing them says whether the difference the
// reordering shows is the reordering or where the code landed.
func gReassocLate(a, b, c, d, mx, my uint32) (uint32, uint32, uint32, uint32) {
	a += mx
	a += b
	d = bits.RotateLeft32(d^a, -16)
	c += d
	b = bits.RotateLeft32(b^c, -12)
	a += my
	a += b
	d = bits.RotateLeft32(d^a, -8)
	c += d
	b = bits.RotateLeft32(b^c, -7)
	return a, b, c, d
}
func rcompressReassocLate(s *[16]uint32, m *[16]uint32) {
	const (
		a = 10
		b = 11
		c = 12
		d = 13
		e = 14
		f = 15
	)

	s0, s1, s2, s3 := s[0+0], s[0+1], s[0+2], s[0+3]
	s4, s5, s6, s7 := s[0+4], s[0+5], s[0+6], s[0+7]
	s8, s9, sa, sb := s[8+0], s[8+1], s[8+2], s[8+3]
	sc, sd, se, sf := s[8+4], s[8+5], s[8+6], s[8+7]

	s0, s4, s8, sc = gReassocLate(s0, s4, s8, sc, m[0], m[1])
	s1, s5, s9, sd = gReassocLate(s1, s5, s9, sd, m[2], m[3])
	s2, s6, sa, se = gReassocLate(s2, s6, sa, se, m[4], m[5])
	s3, s7, sb, sf = gReassocLate(s3, s7, sb, sf, m[6], m[7])
	s0, s5, sa, sf = gReassocLate(s0, s5, sa, sf, m[8], m[9])
	s1, s6, sb, sc = gReassocLate(s1, s6, sb, sc, m[a], m[b])
	s2, s7, s8, sd = gReassocLate(s2, s7, s8, sd, m[c], m[d])
	s3, s4, s9, se = gReassocLate(s3, s4, s9, se, m[e], m[f])

	s0, s4, s8, sc = gReassocLate(s0, s4, s8, sc, m[2], m[6])
	s1, s5, s9, sd = gReassocLate(s1, s5, s9, sd, m[3], m[a])
	s2, s6, sa, se = gReassocLate(s2, s6, sa, se, m[7], m[0])
	s3, s7, sb, sf = gReassocLate(s3, s7, sb, sf, m[4], m[d])
	s0, s5, sa, sf = gReassocLate(s0, s5, sa, sf, m[1], m[b])
	s1, s6, sb, sc = gReassocLate(s1, s6, sb, sc, m[c], m[5])
	s2, s7, s8, sd = gReassocLate(s2, s7, s8, sd, m[9], m[e])
	s3, s4, s9, se = gReassocLate(s3, s4, s9, se, m[f], m[8])

	s0, s4, s8, sc = gReassocLate(s0, s4, s8, sc, m[3], m[4])
	s1, s5, s9, sd = gReassocLate(s1, s5, s9, sd, m[a], m[c])
	s2, s6, sa, se = gReassocLate(s2, s6, sa, se, m[d], m[2])
	s3, s7, sb, sf = gReassocLate(s3, s7, sb, sf, m[7], m[e])
	s0, s5, sa, sf = gReassocLate(s0, s5, sa, sf, m[6], m[5])
	s1, s6, sb, sc = gReassocLate(s1, s6, sb, sc, m[9], m[0])
	s2, s7, s8, sd = gReassocLate(s2, s7, s8, sd, m[b], m[f])
	s3, s4, s9, se = gReassocLate(s3, s4, s9, se, m[8], m[1])

	s0, s4, s8, sc = gReassocLate(s0, s4, s8, sc, m[a], m[7])
	s1, s5, s9, sd = gReassocLate(s1, s5, s9, sd, m[c], m[9])
	s2, s6, sa, se = gReassocLate(s2, s6, sa, se, m[e], m[3])
	s3, s7, sb, sf = gReassocLate(s3, s7, sb, sf, m[d], m[f])
	s0, s5, sa, sf = gReassocLate(s0, s5, sa, sf, m[4], m[0])
	s1, s6, sb, sc = gReassocLate(s1, s6, sb, sc, m[b], m[2])
	s2, s7, s8, sd = gReassocLate(s2, s7, s8, sd, m[5], m[8])
	s3, s4, s9, se = gReassocLate(s3, s4, s9, se, m[1], m[6])

	s0, s4, s8, sc = gReassocLate(s0, s4, s8, sc, m[c], m[d])
	s1, s5, s9, sd = gReassocLate(s1, s5, s9, sd, m[9], m[b])
	s2, s6, sa, se = gReassocLate(s2, s6, sa, se, m[f], m[a])
	s3, s7, sb, sf = gReassocLate(s3, s7, sb, sf, m[e], m[8])
	s0, s5, sa, sf = gReassocLate(s0, s5, sa, sf, m[7], m[2])
	s1, s6, sb, sc = gReassocLate(s1, s6, sb, sc, m[5], m[3])
	s2, s7, s8, sd = gReassocLate(s2, s7, s8, sd, m[0], m[1])
	s3, s4, s9, se = gReassocLate(s3, s4, s9, se, m[6], m[4])

	s0, s4, s8, sc = gReassocLate(s0, s4, s8, sc, m[9], m[e])
	s1, s5, s9, sd = gReassocLate(s1, s5, s9, sd, m[b], m[5])
	s2, s6, sa, se = gReassocLate(s2, s6, sa, se, m[8], m[c])
	s3, s7, sb, sf = gReassocLate(s3, s7, sb, sf, m[f], m[1])
	s0, s5, sa, sf = gReassocLate(s0, s5, sa, sf, m[d], m[3])
	s1, s6, sb, sc = gReassocLate(s1, s6, sb, sc, m[0], m[a])
	s2, s7, s8, sd = gReassocLate(s2, s7, s8, sd, m[2], m[6])
	s3, s4, s9, se = gReassocLate(s3, s4, s9, se, m[4], m[7])

	s0, s4, s8, sc = gReassocLate(s0, s4, s8, sc, m[b], m[f])
	s1, s5, s9, sd = gReassocLate(s1, s5, s9, sd, m[5], m[0])
	s2, s6, sa, se = gReassocLate(s2, s6, sa, se, m[1], m[9])
	s3, s7, sb, sf = gReassocLate(s3, s7, sb, sf, m[8], m[6])
	s0, s5, sa, sf = gReassocLate(s0, s5, sa, sf, m[e], m[a])
	s1, s6, sb, sc = gReassocLate(s1, s6, sb, sc, m[2], m[c])
	s2, s7, s8, sd = gReassocLate(s2, s7, s8, sd, m[3], m[4])
	s3, s4, s9, se = gReassocLate(s3, s4, s9, se, m[7], m[d])

	s[8+0] = s8 ^ s[0]
	s[8+1] = s9 ^ s[1]
	s[8+2] = sa ^ s[2]
	s[8+3] = sb ^ s[3]
	s[8+4] = sc ^ s[4]
	s[8+5] = sd ^ s[5]
	s[8+6] = se ^ s[6]
	s[8+7] = sf ^ s[7]

	s[0] = s0 ^ s8
	s[1] = s1 ^ s9
	s[2] = s2 ^ sa
	s[3] = s3 ^ sb
	s[4] = s4 ^ sc
	s[5] = s5 ^ sd
	s[6] = s6 ^ se
	s[7] = s7 ^ sf
}

func compressPureReassocLate(chain *[8]uint32, block *[16]uint32, counter uint64, blen uint32, flags uint32, out *[16]uint32) {
	*out = [16]uint32{
		chain[0], chain[1], chain[2], chain[3],
		chain[4], chain[5], chain[6], chain[7],
		consts.IV0, consts.IV1, consts.IV2, consts.IV3,
		uint32(counter), uint32(counter >> 32), blen, flags,
	}
	rcompressReassocLate(out, block)
}

func BenchmarkCompress1cPureGoReassocLate(b *testing.B) { benchSerial(b, compressPureReassocLate) }
