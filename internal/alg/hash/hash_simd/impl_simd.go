//go:build goexperiment.simd && amd64

// Package hash_simd implements degree-8 BLAKE3 chunk and parent hashing using
// the standard library's simd/archsimd intrinsics.
//
// It is a drop-in replacement for hash_avx2, which is 61 KB of avo-generated
// assembly produced by a 4.5 KB generator.
//
// Style note: the rounds are written out flat with one variable per state and
// message word, rather than as loops over arrays. That is not a stylistic
// choice. archsimd's documentation says not to "put [a vector] in an aggregate
// type", and an earlier version of this file that used [16]Uint32x8 arrays ran
// 9x slower: the array forced every state word through memory, the message
// schedule became a bounds-checked runtime lookup, and the round function grew
// past the inliner's budget.
package hash_simd

import (
	"simd/archsimd"
	"unsafe"

	"github.com/zeebo/blake3/internal/consts"
)

type vec = archsimd.Uint32x8

func splat(x uint32) vec {
	a := [8]uint32{x, x, x, x, x, x, x, x}
	return archsimd.LoadUint32x8Array(&a)
}

func load(p unsafe.Pointer) vec { return archsimd.LoadUint32x8Array((*[8]uint32)(p)) }

// g is the BLAKE3 quarter-round pair applied to eight independent states.
func g(a, b, c, d, mx, my vec) (vec, vec, vec, vec) {
	a = a.Add(b).Add(mx)
	d = d.Xor(a).RotateAllRight(16)
	c = c.Add(d)
	b = b.Xor(c).RotateAllRight(12)
	a = a.Add(b).Add(my)
	d = d.Xor(a).RotateAllRight(8)
	c = c.Add(d)
	b = b.Xor(c).RotateAllRight(7)
	return a, b, c, d
}

// compress runs the seven rounds over eight lanes and returns the eight
// chaining values.
func compress(
	h0, h1, h2, h3, h4, h5, h6, h7 vec,
	m0, m1, m2, m3, m4, m5, m6, m7, m8, m9, ma, mb, mc, md, me, mf vec,
	ctrLo, ctrHi, blockLen, flags vec,
) (vec, vec, vec, vec, vec, vec, vec, vec) {
	s0, s1, s2, s3 := h0, h1, h2, h3
	s4, s5, s6, s7 := h4, h5, h6, h7
	s8, s9, sa, sb := splat(consts.IV0), splat(consts.IV1), splat(consts.IV2), splat(consts.IV3)
	sc, sd, se, sf := ctrLo, ctrHi, blockLen, flags

	// round 1
	s0, s4, s8, sc = g(s0, s4, s8, sc, m0, m1)
	s1, s5, s9, sd = g(s1, s5, s9, sd, m2, m3)
	s2, s6, sa, se = g(s2, s6, sa, se, m4, m5)
	s3, s7, sb, sf = g(s3, s7, sb, sf, m6, m7)
	s0, s5, sa, sf = g(s0, s5, sa, sf, m8, m9)
	s1, s6, sb, sc = g(s1, s6, sb, sc, ma, mb)
	s2, s7, s8, sd = g(s2, s7, s8, sd, mc, md)
	s3, s4, s9, se = g(s3, s4, s9, se, me, mf)

	// round 2
	s0, s4, s8, sc = g(s0, s4, s8, sc, m2, m6)
	s1, s5, s9, sd = g(s1, s5, s9, sd, m3, ma)
	s2, s6, sa, se = g(s2, s6, sa, se, m7, m0)
	s3, s7, sb, sf = g(s3, s7, sb, sf, m4, md)
	s0, s5, sa, sf = g(s0, s5, sa, sf, m1, mb)
	s1, s6, sb, sc = g(s1, s6, sb, sc, mc, m5)
	s2, s7, s8, sd = g(s2, s7, s8, sd, m9, me)
	s3, s4, s9, se = g(s3, s4, s9, se, mf, m8)

	// round 3
	s0, s4, s8, sc = g(s0, s4, s8, sc, m3, m4)
	s1, s5, s9, sd = g(s1, s5, s9, sd, ma, mc)
	s2, s6, sa, se = g(s2, s6, sa, se, md, m2)
	s3, s7, sb, sf = g(s3, s7, sb, sf, m7, me)
	s0, s5, sa, sf = g(s0, s5, sa, sf, m6, m5)
	s1, s6, sb, sc = g(s1, s6, sb, sc, m9, m0)
	s2, s7, s8, sd = g(s2, s7, s8, sd, mb, mf)
	s3, s4, s9, se = g(s3, s4, s9, se, m8, m1)

	// round 4
	s0, s4, s8, sc = g(s0, s4, s8, sc, ma, m7)
	s1, s5, s9, sd = g(s1, s5, s9, sd, mc, m9)
	s2, s6, sa, se = g(s2, s6, sa, se, me, m3)
	s3, s7, sb, sf = g(s3, s7, sb, sf, md, mf)
	s0, s5, sa, sf = g(s0, s5, sa, sf, m4, m0)
	s1, s6, sb, sc = g(s1, s6, sb, sc, mb, m2)
	s2, s7, s8, sd = g(s2, s7, s8, sd, m5, m8)
	s3, s4, s9, se = g(s3, s4, s9, se, m1, m6)

	// round 5
	s0, s4, s8, sc = g(s0, s4, s8, sc, mc, md)
	s1, s5, s9, sd = g(s1, s5, s9, sd, m9, mb)
	s2, s6, sa, se = g(s2, s6, sa, se, mf, ma)
	s3, s7, sb, sf = g(s3, s7, sb, sf, me, m8)
	s0, s5, sa, sf = g(s0, s5, sa, sf, m7, m2)
	s1, s6, sb, sc = g(s1, s6, sb, sc, m5, m3)
	s2, s7, s8, sd = g(s2, s7, s8, sd, m0, m1)
	s3, s4, s9, se = g(s3, s4, s9, se, m6, m4)

	// round 6
	s0, s4, s8, sc = g(s0, s4, s8, sc, m9, me)
	s1, s5, s9, sd = g(s1, s5, s9, sd, mb, m5)
	s2, s6, sa, se = g(s2, s6, sa, se, m8, mc)
	s3, s7, sb, sf = g(s3, s7, sb, sf, mf, m1)
	s0, s5, sa, sf = g(s0, s5, sa, sf, md, m3)
	s1, s6, sb, sc = g(s1, s6, sb, sc, m0, ma)
	s2, s7, s8, sd = g(s2, s7, s8, sd, m2, m6)
	s3, s4, s9, se = g(s3, s4, s9, se, m4, m7)

	// round 7
	s0, s4, s8, sc = g(s0, s4, s8, sc, mb, mf)
	s1, s5, s9, sd = g(s1, s5, s9, sd, m5, m0)
	s2, s6, sa, se = g(s2, s6, sa, se, m1, m9)
	s3, s7, sb, sf = g(s3, s7, sb, sf, m8, m6)
	s0, s5, sa, sf = g(s0, s5, sa, sf, me, ma)
	s1, s6, sb, sc = g(s1, s6, sb, sc, m2, mc)
	s2, s7, s8, sd = g(s2, s7, s8, sd, m3, m4)
	s3, s4, s9, se = g(s3, s4, s9, se, m7, md)

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

// loadBlock reads block n from all eight chunks and transposes it, so each
// returned vector holds one message word across all eight lanes.
func loadBlock(input *[8192]byte, n int) (
	m0, m1, m2, m3, m4, m5, m6, m7, m8, m9, ma, mb, mc, md, me, mf vec,
) {
	const cl, bl = consts.ChunkLen, consts.BlockLen
	p := func(chunk, half int) unsafe.Pointer {
		return unsafe.Pointer(&input[chunk*cl+n*bl+half*32])
	}
	m0, m1, m2, m3, m4, m5, m6, m7 = transpose8(
		load(p(0, 0)), load(p(1, 0)), load(p(2, 0)), load(p(3, 0)),
		load(p(4, 0)), load(p(5, 0)), load(p(6, 0)), load(p(7, 0)))
	m8, m9, ma, mb, mc, md, me, mf = transpose8(
		load(p(0, 1)), load(p(1, 1)), load(p(2, 1)), load(p(3, 1)),
		load(p(4, 1)), load(p(5, 1)), load(p(6, 1)), load(p(7, 1)))
	return
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

	h0, h1, h2, h3 := splat(key[0]), splat(key[1]), splat(key[2]), splat(key[3])
	h4, h5, h6, h7 := splat(key[4]), splat(key[5]), splat(key[6]), splat(key[7])

	var lo, hi [8]uint32
	for i := range &lo {
		c := counter + uint64(i)
		lo[i], hi[i] = uint32(c), uint32(c>>32)
	}
	ctrLo := archsimd.LoadUint32x8Array(&lo)
	ctrHi := archsimd.LoadUint32x8Array(&hi)
	blockLen := splat(consts.BlockLen)

	// The chunk and block that the input ends in. chain is the chaining value
	// as it stands just before that block is compressed.
	lastChunk := int((length - 1) / consts.ChunkLen)
	lastBlock := int((length - 1) % consts.ChunkLen / consts.BlockLen)

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

		m0, m1, m2, m3, m4, m5, m6, m7, m8, m9, ma, mb, mc, md, me, mf := loadBlock(input, n)
		h0, h1, h2, h3, h4, h5, h6, h7 = compress(
			h0, h1, h2, h3, h4, h5, h6, h7,
			m0, m1, m2, m3, m4, m5, m6, m7, m8, m9, ma, mb, mc, md, me, mf,
			ctrLo, ctrHi, blockLen, splat(bflags))
	}

	store(h0, h1, h2, h3, h4, h5, h6, h7, out)
}

func HashP(left, right *[64]uint32, flags uint32, key *[8]uint32, out *[64]uint32, n int) {
	h0, h1, h2, h3 := splat(key[0]), splat(key[1]), splat(key[2]), splat(key[3])
	h4, h5, h6, h7 := splat(key[4]), splat(key[5]), splat(key[6]), splat(key[7])

	// left and right are already lane-major, so each message word is a plain
	// eight-word load.
	l := func(i int) vec { return archsimd.LoadUint32x8(left[8*i : 8*i+8]) }
	r := func(i int) vec { return archsimd.LoadUint32x8(right[8*i : 8*i+8]) }

	zero := splat(0)
	h0, h1, h2, h3, h4, h5, h6, h7 = compress(
		h0, h1, h2, h3, h4, h5, h6, h7,
		l(0), l(1), l(2), l(3), l(4), l(5), l(6), l(7),
		r(0), r(1), r(2), r(3), r(4), r(5), r(6), r(7),
		zero, zero, splat(consts.BlockLen), splat(flags))

	store(h0, h1, h2, h3, h4, h5, h6, h7, out)
}
