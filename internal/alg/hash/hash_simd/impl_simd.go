//go:build goexperiment.simd && amd64

// Package hash_simd implements degree-8 BLAKE3 chunk and parent hashing using
// the standard library's simd/archsimd intrinsics.
//
// It is a drop-in replacement for hash_avx2, which is 61 KB of avo-generated
// assembly produced by a 4.5 KB generator.
//
// Only AVX2 is needed. The generated code contains no EVEX-encoded instruction,
// no zmm register and no mask register: as of Go 1.27, RotateAllRight on a
// 256-bit vector lowers to a shift pair rather than AVX-512VL's VPRORD.
//
// Four things about the style here are deliberate, and each was arrived at by
// reading the generated code rather than by taste:
//
//   - The rounds are written out flat, one variable per state word, rather than
//     as loops over arrays. archsimd's documentation says not to "put [a vector]
//     in an aggregate type"; a version using [16]Uint32x8 ran 9x slower, because
//     the array forced every state word through memory, the message schedule
//     became a bounds-checked runtime lookup, and the round function grew past
//     the inliner's budget.
//
//   - The message words stay in memory and reach g as pointers. AVX2 has 16
//     vector registers but a round needs 16 state plus 16 message words live at
//     once, so holding messages in registers forces spills. Loading at the point
//     of use lets the compiler fold the load into the operand, which is what the
//     avo generator arranges by hand.
//
//   - Scalars reach the lanes through archsimd.BroadcastUint32x8, not by
//     filling an eight-word array and loading it back. compress needs four
//     broadcasts per block, and the array version cost a 32-byte store
//     followed by a dependent load each time: 4.8x on HashF_8K, for a 4%
//     change in instruction count.
//
//   - The two byte-aligned rotations go through a byte shuffle instead of
//     RotateAllRight, which costs three instructions per rotation. See rotr16.
package hash_simd

import (
	"simd/archsimd"
	"unsafe"

	"github.com/zeebo/blake3/internal/consts"
)

type vec = archsimd.Uint32x8

// words is one message word broadcast across the eight lanes. It is kept in
// memory so that the rounds can use it as a memory operand.
type words = [8]uint32

// Two of BLAKE3's four rotations are by whole bytes, so they are permutations
// of the bytes within each 32-bit word and lower to a single shuffle instead of
// RotateAllRight's shift-shift-or. A little-endian word holds its bytes in the
// order [b0(LSB), b1, b2, b3(MSB)], so rotating right by 16 selects
// [b2, b3, b0, b1] and by 8 selects [b1, b2, b3, b0]. The tables are int8
// because the shuffle treats a negative index as "write zero"; every index here
// is in range, so nothing is zeroed.
var (
	rotr16Bytes = [32]int8{
		2, 3, 0, 1, 6, 7, 4, 5, 10, 11, 8, 9, 14, 15, 12, 13,
		2, 3, 0, 1, 6, 7, 4, 5, 10, 11, 8, 9, 14, 15, 12, 13,
	}
	rotr8Bytes = [32]int8{
		1, 2, 3, 0, 5, 6, 7, 4, 9, 10, 11, 8, 13, 14, 15, 12,
		1, 2, 3, 0, 5, 6, 7, 4, 9, 10, 11, 8, 13, 14, 15, 12,
	}
)

func rotr16(x vec) vec {
	i := archsimd.LoadInt8x32Array(&rotr16Bytes)
	return x.AsUint8x32().PermuteOrZeroGrouped(i).AsUint32x8()
}

func rotr8(x vec) vec {
	i := archsimd.LoadInt8x32Array(&rotr8Bytes)
	return x.AsUint8x32().PermuteOrZeroGrouped(i).AsUint32x8()
}

// g is the BLAKE3 quarter-round pair applied to eight independent states.
func g(a, b, c, d vec, mx, my *words) (vec, vec, vec, vec) {
	a = a.Add(b).Add(archsimd.LoadUint32x8Array(mx))
	d = rotr16(d.Xor(a))
	c = c.Add(d)
	b = b.Xor(c).RotateAllRight(12)
	a = a.Add(b).Add(archsimd.LoadUint32x8Array(my))
	d = rotr8(d.Xor(a))
	c = c.Add(d)
	b = b.Xor(c).RotateAllRight(7)
	return a, b, c, d
}

// compress runs the seven rounds over eight lanes and returns the eight
// chaining values.
func compress(
	h0, h1, h2, h3, h4, h5, h6, h7 vec,
	m *[16]words,
	ctrLo, ctrHi, blockLen, flags vec,
) (vec, vec, vec, vec, vec, vec, vec, vec) {
	s0, s1, s2, s3 := h0, h1, h2, h3
	s4, s5, s6, s7 := h4, h5, h6, h7
	s8 := archsimd.BroadcastUint32x8(consts.IV0)
	s9 := archsimd.BroadcastUint32x8(consts.IV1)
	sa := archsimd.BroadcastUint32x8(consts.IV2)
	sb := archsimd.BroadcastUint32x8(consts.IV3)
	sc, sd, se, sf := ctrLo, ctrHi, blockLen, flags

	// round 1
	s0, s4, s8, sc = g(s0, s4, s8, sc, &m[0], &m[1])
	s1, s5, s9, sd = g(s1, s5, s9, sd, &m[2], &m[3])
	s2, s6, sa, se = g(s2, s6, sa, se, &m[4], &m[5])
	s3, s7, sb, sf = g(s3, s7, sb, sf, &m[6], &m[7])
	s0, s5, sa, sf = g(s0, s5, sa, sf, &m[8], &m[9])
	s1, s6, sb, sc = g(s1, s6, sb, sc, &m[10], &m[11])
	s2, s7, s8, sd = g(s2, s7, s8, sd, &m[12], &m[13])
	s3, s4, s9, se = g(s3, s4, s9, se, &m[14], &m[15])

	// round 2
	s0, s4, s8, sc = g(s0, s4, s8, sc, &m[2], &m[6])
	s1, s5, s9, sd = g(s1, s5, s9, sd, &m[3], &m[10])
	s2, s6, sa, se = g(s2, s6, sa, se, &m[7], &m[0])
	s3, s7, sb, sf = g(s3, s7, sb, sf, &m[4], &m[13])
	s0, s5, sa, sf = g(s0, s5, sa, sf, &m[1], &m[11])
	s1, s6, sb, sc = g(s1, s6, sb, sc, &m[12], &m[5])
	s2, s7, s8, sd = g(s2, s7, s8, sd, &m[9], &m[14])
	s3, s4, s9, se = g(s3, s4, s9, se, &m[15], &m[8])

	// round 3
	s0, s4, s8, sc = g(s0, s4, s8, sc, &m[3], &m[4])
	s1, s5, s9, sd = g(s1, s5, s9, sd, &m[10], &m[12])
	s2, s6, sa, se = g(s2, s6, sa, se, &m[13], &m[2])
	s3, s7, sb, sf = g(s3, s7, sb, sf, &m[7], &m[14])
	s0, s5, sa, sf = g(s0, s5, sa, sf, &m[6], &m[5])
	s1, s6, sb, sc = g(s1, s6, sb, sc, &m[9], &m[0])
	s2, s7, s8, sd = g(s2, s7, s8, sd, &m[11], &m[15])
	s3, s4, s9, se = g(s3, s4, s9, se, &m[8], &m[1])

	// round 4
	s0, s4, s8, sc = g(s0, s4, s8, sc, &m[10], &m[7])
	s1, s5, s9, sd = g(s1, s5, s9, sd, &m[12], &m[9])
	s2, s6, sa, se = g(s2, s6, sa, se, &m[14], &m[3])
	s3, s7, sb, sf = g(s3, s7, sb, sf, &m[13], &m[15])
	s0, s5, sa, sf = g(s0, s5, sa, sf, &m[4], &m[0])
	s1, s6, sb, sc = g(s1, s6, sb, sc, &m[11], &m[2])
	s2, s7, s8, sd = g(s2, s7, s8, sd, &m[5], &m[8])
	s3, s4, s9, se = g(s3, s4, s9, se, &m[1], &m[6])

	// round 5
	s0, s4, s8, sc = g(s0, s4, s8, sc, &m[12], &m[13])
	s1, s5, s9, sd = g(s1, s5, s9, sd, &m[9], &m[11])
	s2, s6, sa, se = g(s2, s6, sa, se, &m[15], &m[10])
	s3, s7, sb, sf = g(s3, s7, sb, sf, &m[14], &m[8])
	s0, s5, sa, sf = g(s0, s5, sa, sf, &m[7], &m[2])
	s1, s6, sb, sc = g(s1, s6, sb, sc, &m[5], &m[3])
	s2, s7, s8, sd = g(s2, s7, s8, sd, &m[0], &m[1])
	s3, s4, s9, se = g(s3, s4, s9, se, &m[6], &m[4])

	// round 6
	s0, s4, s8, sc = g(s0, s4, s8, sc, &m[9], &m[14])
	s1, s5, s9, sd = g(s1, s5, s9, sd, &m[11], &m[5])
	s2, s6, sa, se = g(s2, s6, sa, se, &m[8], &m[12])
	s3, s7, sb, sf = g(s3, s7, sb, sf, &m[15], &m[1])
	s0, s5, sa, sf = g(s0, s5, sa, sf, &m[13], &m[3])
	s1, s6, sb, sc = g(s1, s6, sb, sc, &m[0], &m[10])
	s2, s7, s8, sd = g(s2, s7, s8, sd, &m[2], &m[6])
	s3, s4, s9, se = g(s3, s4, s9, se, &m[4], &m[7])

	// round 7
	s0, s4, s8, sc = g(s0, s4, s8, sc, &m[11], &m[15])
	s1, s5, s9, sd = g(s1, s5, s9, sd, &m[5], &m[0])
	s2, s6, sa, se = g(s2, s6, sa, se, &m[1], &m[9])
	s3, s7, sb, sf = g(s3, s7, sb, sf, &m[8], &m[6])
	s0, s5, sa, sf = g(s0, s5, sa, sf, &m[14], &m[10])
	s1, s6, sb, sc = g(s1, s6, sb, sc, &m[2], &m[12])
	s2, s7, s8, sd = g(s2, s7, s8, sd, &m[3], &m[4])
	s3, s4, s9, se = g(s3, s4, s9, se, &m[7], &m[13])

	return s0.Xor(s8), s1.Xor(s9), s2.Xor(sa), s3.Xor(sb),
		s4.Xor(sc), s5.Xor(sd), s6.Xor(se), s7.Xor(sf)
}

// transpose8 exchanges "which lane" with "which word" across eight vectors.
func transpose8(v0, v1, v2, v3, v4, v5, v6, v7 vec) (vec, vec, vec, vec, vec, vec, vec, vec) {
	a0, a1 := v0.InterleaveLoGrouped(v1), v0.InterleaveHiGrouped(v1)
	a2, a3 := v2.InterleaveLoGrouped(v3), v2.InterleaveHiGrouped(v3)
	a4, a5 := v4.InterleaveLoGrouped(v5), v4.InterleaveHiGrouped(v5)
	a6, a7 := v6.InterleaveLoGrouped(v7), v6.InterleaveHiGrouped(v7)

	q0, q2 := a0.AsUint64x4(), a2.AsUint64x4()
	q1, q3 := a1.AsUint64x4(), a3.AsUint64x4()
	q4, q6 := a4.AsUint64x4(), a6.AsUint64x4()
	q5, q7 := a5.AsUint64x4(), a7.AsUint64x4()

	b0, b1 := q0.InterleaveLoGrouped(q2).AsUint32x8(), q0.InterleaveHiGrouped(q2).AsUint32x8()
	b2, b3 := q1.InterleaveLoGrouped(q3).AsUint32x8(), q1.InterleaveHiGrouped(q3).AsUint32x8()
	b4, b5 := q4.InterleaveLoGrouped(q6).AsUint32x8(), q4.InterleaveHiGrouped(q6).AsUint32x8()
	b6, b7 := q5.InterleaveLoGrouped(q7).AsUint32x8(), q5.InterleaveHiGrouped(q7).AsUint32x8()

	return b0.SetHi(b4.GetLo()), b1.SetHi(b5.GetLo()), b2.SetHi(b6.GetLo()), b3.SetHi(b7.GetLo()),
		b4.SetLo(b0.GetHi()), b5.SetLo(b1.GetHi()), b6.SetLo(b2.GetHi()), b7.SetLo(b3.GetHi())
}

// loadBlock reads block n from all eight chunks, transposes it, and leaves the
// result in m so the rounds can use it as memory operands.
func loadBlock(input *[8192]byte, n int, m *[16]words) {
	const cl, bl = consts.ChunkLen, consts.BlockLen
	// at returns block n of the given chunk as a word array, so that it can be
	// loaded without a cast at each of the sixteen call sites below.
	at := func(chunk, half int) *words {
		return (*words)(unsafe.Pointer(&input[chunk*cl+n*bl+half*32]))
	}
	for half := 0; half < 2; half++ {
		t0, t1, t2, t3, t4, t5, t6, t7 := transpose8(
			archsimd.LoadUint32x8Array(at(0, half)), archsimd.LoadUint32x8Array(at(1, half)),
			archsimd.LoadUint32x8Array(at(2, half)), archsimd.LoadUint32x8Array(at(3, half)),
			archsimd.LoadUint32x8Array(at(4, half)), archsimd.LoadUint32x8Array(at(5, half)),
			archsimd.LoadUint32x8Array(at(6, half)), archsimd.LoadUint32x8Array(at(7, half)))
		t0.StoreArray(&m[half*8+0])
		t1.StoreArray(&m[half*8+1])
		t2.StoreArray(&m[half*8+2])
		t3.StoreArray(&m[half*8+3])
		t4.StoreArray(&m[half*8+4])
		t5.StoreArray(&m[half*8+5])
		t6.StoreArray(&m[half*8+6])
		t7.StoreArray(&m[half*8+7])
	}
}

func store(h0, h1, h2, h3, h4, h5, h6, h7 vec, out *[64]uint32) {
	h0.Store(out[0:8])
	h1.Store(out[8:16])
	h2.Store(out[16:24])
	h3.Store(out[24:32])
	h4.Store(out[32:40])
	h5.Store(out[40:48])
	h6.Store(out[48:56])
	h7.Store(out[56:64])
}

func HashF(input *[8192]byte, length, counter uint64, flags uint32, key *[8]uint32, out *[64]uint32, chain *[8]uint32) {
	if length == 0 {
		return
	}

	h0 := archsimd.BroadcastUint32x8(key[0])
	h1 := archsimd.BroadcastUint32x8(key[1])
	h2 := archsimd.BroadcastUint32x8(key[2])
	h3 := archsimd.BroadcastUint32x8(key[3])
	h4 := archsimd.BroadcastUint32x8(key[4])
	h5 := archsimd.BroadcastUint32x8(key[5])
	h6 := archsimd.BroadcastUint32x8(key[6])
	h7 := archsimd.BroadcastUint32x8(key[7])

	var lo, hi [8]uint32
	for i := range &lo {
		c := counter + uint64(i)
		lo[i], hi[i] = uint32(c), uint32(c>>32)
	}
	ctrLo := archsimd.LoadUint32x8Array(&lo)
	ctrHi := archsimd.LoadUint32x8Array(&hi)
	blockLen := archsimd.BroadcastUint32x8(consts.BlockLen)

	// The chunk and block that the input ends in. chain is the chaining value
	// as it stands just before that block is compressed.
	lastChunk := int((length - 1) / consts.ChunkLen)
	lastBlock := int((length - 1) % consts.ChunkLen / consts.BlockLen)

	var m [16]words

	for n := 0; n < 16; n++ {
		bflags := flags
		if n == 0 {
			bflags |= consts.Flag_ChunkStart
		}
		if n == 15 {
			bflags |= consts.Flag_ChunkEnd
		}

		if n == lastBlock {
			var t [64]uint32
			store(h0, h1, h2, h3, h4, h5, h6, h7, &t)
			for w := range chain {
				chain[w] = t[8*w+lastChunk]
			}
		}

		loadBlock(input, n, &m)
		h0, h1, h2, h3, h4, h5, h6, h7 = compress(
			h0, h1, h2, h3, h4, h5, h6, h7, &m,
			ctrLo, ctrHi, blockLen, archsimd.BroadcastUint32x8(bflags))
	}

	store(h0, h1, h2, h3, h4, h5, h6, h7, out)
}

func HashP(left, right *[64]uint32, flags uint32, key *[8]uint32, out *[64]uint32, n int) {
	h0 := archsimd.BroadcastUint32x8(key[0])
	h1 := archsimd.BroadcastUint32x8(key[1])
	h2 := archsimd.BroadcastUint32x8(key[2])
	h3 := archsimd.BroadcastUint32x8(key[3])
	h4 := archsimd.BroadcastUint32x8(key[4])
	h5 := archsimd.BroadcastUint32x8(key[5])
	h6 := archsimd.BroadcastUint32x8(key[6])
	h7 := archsimd.BroadcastUint32x8(key[7])

	// left and right are already lane-major and adjacent in meaning, so the
	// message block is the two of them read in place: no copy is needed, and
	// compress uses them straight out of memory.
	// left and right are already lane-major, so building the message block is
	// eight vector moves rather than a transpose.
	var m [16]words
	lm := (*[8]words)(unsafe.Pointer(left))
	rm := (*[8]words)(unsafe.Pointer(right))
	for i := 0; i < 8; i++ {
		archsimd.LoadUint32x8Array(&lm[i]).StoreArray(&m[i])
		archsimd.LoadUint32x8Array(&rm[i]).StoreArray(&m[i+8])
	}

	zero := archsimd.BroadcastUint32x8(0)
	h0, h1, h2, h3, h4, h5, h6, h7 = compress(
		h0, h1, h2, h3, h4, h5, h6, h7, &m,
		zero, zero, archsimd.BroadcastUint32x8(consts.BlockLen), archsimd.BroadcastUint32x8(flags))

	store(h0, h1, h2, h3, h4, h5, h6, h7, out)
}
