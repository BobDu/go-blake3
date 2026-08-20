package main

import (
	. "github.com/mmcloughlin/avo/build"
	. "github.com/mmcloughlin/avo/operand"
	. "github.com/mmcloughlin/avo/reg"
)

// HashF hashes up to 8 chunks in parallel with a mixed-axis layout: each Zmm
// vector holds one state row of four chunks (4 chunks x 4 lanes), so the four
// G columns of a chunk live in one 128-bit group and every shuffle is a cheap
// in-group operation. Up to 4 chunks run in a single stream; 5 to 8 chunks run
// as two independent streams inside one loop body so their dependency chains
// interleave in the out-of-order core.
//
// Layout per stream (Zmm registers):
//   rows:    base+0 .. base+3   (row 2 and 3 are rebuilt from IV/state every block)
//   message: two pools of four vectors that swap roles every round, so the
//            per-round message permutation writes into the other pool and no
//            register moves are needed.

type stream struct {
	rows  int // rows[0..3] at zs[rows..rows+3]
	poolA int // message pool A
	poolB int // message pool B
	tmp   int // scratch register
	msg   int // byte offset of this stream's chunks in the input
	ivf   int // byte offset of this stream's row3 data in the arena
}

var zs = []VecPhysical{
	Z0, Z1, Z2, Z3, Z4, Z5, Z6, Z7, Z8, Z9, Z10, Z11, Z12, Z13, Z14, Z15,
	Z16, Z17, Z18, Z19, Z20, Z21, Z22, Z23, Z24, Z25, Z26, Z27, Z28, Z29, Z30, Z31,
}
var ysv = []VecPhysical{
	Y0, Y1, Y2, Y3, Y4, Y5, Y6, Y7, Y8, Y9, Y10, Y11, Y12, Y13, Y14, Y15,
	Y16, Y17, Y18, Y19, Y20, Y21, Y22, Y23, Y24, Y25, Y26, Y27, Y28, Y29, Y30, Y31,
}
var xsv = []VecPhysical{
	X0, X1, X2, X3, X4, X5, X6, X7, X8, X9, X10, X11, X12, X13, X14, X15,
	X16, X17, X18, X19, X20, X21, X22, X23, X24, X25, X26, X27, X28, X29, X30, X31,
}

func pack(a, b, c, d int) U8 { return U8(a<<6 | b<<4 | c<<2 | d) }

// gvF emits half of the G function for all four columns of the stream.
func gvF(s stream, m, n int) {
	r := s.rows
	first := 16
	if n == 7 {
		first = 8
	}
	VPADDD(zs[m], zs[r], zs[r])
	VPADDD(zs[r+1], zs[r], zs[r])
	VPXORD(zs[r], zs[r+3], zs[r+3])
	VPRORD(U8(first), zs[r+3], zs[r+3])
	VPADDD(zs[r+3], zs[r+2], zs[r+2])
	VPXORD(zs[r+2], zs[r+1], zs[r+1])
	VPRORD(U8(n), zs[r+1], zs[r+1])
}

func diagF(s stream) {
	r := s.rows
	VPSHUFD(pack(2, 1, 0, 3), zs[r], zs[r])
	VPSHUFD(pack(1, 0, 3, 2), zs[r+3], zs[r+3])
	VPSHUFD(pack(0, 3, 2, 1), zs[r+2], zs[r+2])
}

func undiagF(s stream) {
	r := s.rows
	VPSHUFD(pack(0, 3, 2, 1), zs[r], zs[r])
	VPSHUFD(pack(1, 0, 3, 2), zs[r+3], zs[r+3])
	VPSHUFD(pack(2, 1, 0, 3), zs[r+2], zs[r+2])
}

// round1MsgF builds the first round's message vectors in pool B from the raw
// block words in pool A.
func round1MsgF(s stream) {
	a, b := s.poolA, s.poolB
	VSHUFPS(pack(2, 0, 2, 0), zs[a+1], zs[a], zs[b])
	VSHUFPS(pack(3, 1, 3, 1), zs[a+1], zs[a], zs[b+1])
	VSHUFPS(pack(2, 0, 2, 0), zs[a+3], zs[a+2], zs[b+2])
	VSHUFPS(pack(2, 1, 0, 3), zs[b+2], zs[b+2], zs[b+2])
	VSHUFPS(pack(3, 1, 3, 1), zs[a+3], zs[a+2], zs[b+3])
	VSHUFPS(pack(2, 1, 0, 3), zs[b+3], zs[b+3], zs[b+3])
}

// permuteMsgF advances the message vectors one round: reads pool p, writes
// pool q, following the fixed blake3 message schedule.
func permuteMsgF(s stream, p, q int) {
	t := s.tmp
	VSHUFPS(pack(3, 1, 1, 2), zs[p+1], zs[p], zs[q])
	VSHUFPS(pack(0, 3, 2, 1), zs[q], zs[q], zs[q])
	VSHUFPS(pack(3, 3, 2, 2), zs[p+3], zs[p+2], zs[q+1])
	VPSHUFD(pack(0, 0, 3, 3), zs[p], zs[t])
	VPBLENDMD(zs[t], zs[q+1], K1, zs[q+1])
	VPUNPCKLDQ(zs[p+1], zs[p+3], zs[q+2])
	VPBLENDMD(zs[p+2], zs[q+2], K2, zs[q+2])
	VSHUFPS(pack(2, 3, 1, 0), zs[q+2], zs[q+2], zs[q+2])
	VPUNPCKHDQ(zs[p+3], zs[p+1], zs[t])
	VPUNPCKLDQ(zs[t], zs[p+2], zs[q+3])
	VSHUFPS(pack(0, 1, 3, 2), zs[q+3], zs[q+3], zs[q+3])
}

func HashF(c Ctx) {
	TEXT("HashF", 0, `func(
		input *[8192]byte,
		length uint64,
		counter uint64,
		flags uint32,
		key *[8]uint32,
		out *[64]uint32,
		chain *[8]uint32,
	)`)

	transIdx := GLOBL("transpose_idx", RODATA|NOPTR)
	for w := 0; w < 4; w++ {
		for i := 0; i < 4; i++ {
			DATA(64*w+4*i, U32(w+4*i))
		}
		for i := 0; i < 4; i++ {
			DATA(64*w+16+4*i, U32(16+w+4*i))
		}
		for i := 8; i < 16; i++ {
			DATA(64*w+4*i, U32(0))
		}
	}

	var (
		input   = Mem{Base: Load(Param("input"), GP64())}
		length  = Load(Param("length"), GP64()).(GPVirtual)
		counter = Load(Param("counter"), GP64()).(GPVirtual)
		flags   = Load(Param("flags"), GP32()).(GPVirtual)
		key     = Mem{Base: Load(Param("key"), GP64())}
		out     = Mem{Base: Load(Param("out"), GP64())}
		chain   = Mem{Base: Load(Param("chain"), GP64())}
	)

	chunks := GP64()
	blocks := GP64()
	stash := GP64()

	// The arena holds three row-3 images (chunk counters, block length, and
	// the flags for the first, middle, and last block of a chunk) plus room
	// to spill the running chaining values when the partial chunk boundary
	// is crossed.
	const (
		arenaStart = 0
		arenaMid   = arenaStart + 128
		arenaEnd   = arenaMid + 128
		arenaChain = arenaEnd + 128
		arenaSize  = arenaChain + 256
	)

	{
		Comment("Allocate local space and align it")
		local := AllocLocal(arenaSize + 64)
		LEAQ(local.Offset(63), stash)
		ANDQ(I32(^63), stash)
	}

	arena := Mem{Base: stash}

	{
		Comment("Compute complete chunks and blocks")
		XORQ(chunks, chunks)
		XORQ(blocks, blocks)
		TESTQ(length, length)
		JZ(LabelRef("skip_compute"))

		// chunks = (length - 1) / 1024, blocks = (length - 1) % 1024 / 64 * 64
		SUBQ(U8(1), length)
		MOVQ(length, chunks)
		SHRQ(U8(10), chunks)
		MOVQ(length, blocks)
		ANDQ(U32(960), blocks)
	}

	Label("skip_compute")

	{
		Comment("Build the three row-3 images: counters and block length ...")
		ctr := GP64()
		for i := 0; i < 8; i++ {
			LEAQ(Mem{Base: counter, Disp: i}, ctr)
			MOVL(ctr.As32(), arena.Offset(arenaStart+16*i))
			SHRQ(U8(32), ctr)
			MOVL(ctr.As32(), arena.Offset(arenaStart+16*i+4))
			MOVL(U32(64), arena.Offset(arenaStart+16*i+8))
		}
		VMOVDQU32(arena.Offset(arenaStart), zs[0])
		VMOVDQU32(arena.Offset(arenaStart+64), zs[1])
		VMOVDQU32(zs[0], arena.Offset(arenaMid))
		VMOVDQU32(zs[1], arena.Offset(arenaMid+64))
		VMOVDQU32(zs[0], arena.Offset(arenaEnd))
		VMOVDQU32(zs[1], arena.Offset(arenaEnd+64))

		Comment("... and the first, middle, and last block flags")
		fl := GP32()
		MOVL(flags, fl)
		ORL(U8(flag_chunkStart), fl)
		for i := 0; i < 8; i++ {
			MOVL(fl, arena.Offset(arenaStart+16*i+12))
		}
		for i := 0; i < 8; i++ {
			MOVL(flags, arena.Offset(arenaMid+16*i+12))
		}
		MOVL(flags, fl)
		ORL(U8(flag_chunkEnd), fl)
		for i := 0; i < 8; i++ {
			MOVL(fl, arena.Offset(arenaEnd+16*i+12))
		}
	}

	{
		Comment("Set up the blend masks for the message permutation")
		msk := GP32()
		MOVL(U32(0x5555), msk)
		KMOVW(msk, K1)
		MOVL(U32(0x8888), msk)
		KMOVW(msk, K2)
	}

	{
		Comment("Choose the kernel: one stream covers up to 4 chunks")
		CMPQ(chunks, U8(4))
		JB(LabelRef("single_stream"))
	}

	s1 := stream{rows: 0, poolA: 4, poolB: 8, tmp: 12, msg: 0, ivf: 0}
	s2 := stream{rows: 13, poolA: 17, poolB: 21, tmp: 25, msg: 4096, ivf: 64}

	emitPath := func(prefix string, streams []stream) {
		loop := GP64()
		ivfp := GP64()

		{
			Comment("Load the key into the chaining value rows of every chunk")
			for _, s := range streams {
				VBROADCASTI32X4(key.Offset(0), zs[s.rows])
				VBROADCASTI32X4(key.Offset(16), zs[s.rows+1])
			}
			XORQ(loop, loop)
			LEAQ(arena.Offset(arenaStart), ivfp)
		}

		Label(prefix + "_loop")

		{
			Comment("Use the last-block flags for block 16")
			CMPQ(loop, U32(15*64))
			JNE(LabelRef(prefix + "_flags_done"))
			LEAQ(arena.Offset(arenaEnd), ivfp)
		}

		Label(prefix + "_flags_done")

		{
			Comment("Load and group the message words of every chunk")
			for _, s := range streams {
				for k := 0; k < 4; k++ {
					z := s.poolA + k
					VMOVDQU32(input.Idx(loop, 1).Offset(s.msg+16*k), xsv[z])
					for l := 1; l < 4; l++ {
						VINSERTI32X4(U8(l), input.Idx(loop, 1).Offset(s.msg+1024*l+16*k), zs[z], zs[z])
					}
				}
			}
		}

		{
			Comment("Build rows 2 and 3 from the IV, counters, and flags")
			for _, s := range streams {
				VBROADCASTI32X4(c.IV.Offset(0), zs[s.rows+2])
				VMOVDQU32(Mem{Base: ivfp}.Offset(s.ivf), zs[s.rows+3])
			}
		}

		{
			Comment("Save the chaining value before the partial chunk boundary")
			CMPQ(loop, blocks)
			JNE(LabelRef(prefix + "_chain_done"))

			for _, s := range streams {
				VMOVDQU32(zs[s.rows], arena.Offset(arenaChain+s.ivf*2))
				VMOVDQU32(zs[s.rows+1], arena.Offset(arenaChain+s.ivf*2+64))
			}
			lane := GP64()
			group := GP64()
			tmp32 := GP32()
			MOVQ(chunks, lane)
			ANDQ(U8(3), lane)
			SHLQ(U8(4), lane)
			MOVQ(chunks, group)
			ANDQ(U8(4), group)
			SHLQ(U8(5), group)
			ADDQ(group, lane)
			for i := 0; i < 4; i++ {
				MOVL(arena.Offset(arenaChain+4*i).Idx(lane, 1), tmp32)
				MOVL(tmp32, chain.Offset(4*i))
				MOVL(arena.Offset(arenaChain+64+4*i).Idx(lane, 1), tmp32)
				MOVL(tmp32, chain.Offset(16+4*i))
			}
		}

		Label(prefix + "_chain_done")

		{
			Comment("Round 1")
			for _, s := range streams {
				round1MsgF(s)
			}
			for _, s := range streams {
				gvF(s, s.poolB, 12)
				gvF(s, s.poolB+1, 7)
				diagF(s)
				gvF(s, s.poolB+2, 12)
				gvF(s, s.poolB+3, 7)
				undiagF(s)
			}
		}

		for r := 2; r <= 7; r++ {
			Commentf("Round %d", r)
			for _, s := range streams {
				p, q := s.poolA, s.poolB
				if r%2 == 0 {
					p, q = s.poolB, s.poolA
				}
				permuteMsgF(s, p, q)
				gvF(s, q, 12)
				gvF(s, q+1, 7)
				diagF(s)
				gvF(s, q+2, 12)
				gvF(s, q+3, 7)
				undiagF(s)
			}
		}

		{
			Comment("Compute the chaining values for the next block")
			for _, s := range streams {
				VPXORD(zs[s.rows+2], zs[s.rows], zs[s.rows])
				VPXORD(zs[s.rows+3], zs[s.rows+1], zs[s.rows+1])
			}
		}

		{
			Comment("If we have zero complete chunks, we're done")
			CMPQ(chunks, U8(0))
			JNE(LabelRef(prefix + "_trailer"))
			CMPQ(blocks, loop)
			JEQ(LabelRef(prefix + "_finalize"))
		}

		Label(prefix + "_trailer")

		{
			Comment("Increment, use the middle-block flags, and loop")
			CMPQ(loop, U32(15*64))
			JEQ(LabelRef(prefix + "_finalize"))
			ADDQ(Imm(64), loop)
			LEAQ(arena.Offset(arenaMid), ivfp)
			JMP(LabelRef(prefix + "_loop"))
		}

		Label(prefix + "_finalize")

		{
			Comment("Transpose the chaining values into the word-major out layout")
			second := streams[0]
			if len(streams) == 2 {
				second = streams[1]
			}
			for w := 0; w < 4; w++ {
				VMOVDQU32(Mem{Symbol: transIdx.Symbol, Base: StaticBase}.Offset(64*w), zs[s1.tmp])
				VMOVDQA32(zs[streams[0].rows], zs[s1.poolA])
				VPERMT2D(zs[second.rows], zs[s1.tmp], zs[s1.poolA])
				VMOVDQU(ysv[s1.poolA], out.Offset(32*w))
				VMOVDQA32(zs[streams[0].rows+1], zs[s1.poolA+1])
				VPERMT2D(zs[second.rows+1], zs[s1.tmp], zs[s1.poolA+1])
				VMOVDQU(ysv[s1.poolA+1], out.Offset(32*(4+w)))
			}
		}

		VZEROUPPER()
		RET()
	}

	Label("dual_stream")
	emitPath("d", []stream{s1, s2})

	Label("single_stream")
	emitPath("s", []stream{s1})
}
