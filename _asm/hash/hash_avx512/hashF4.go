package main

import (
	. "github.com/mmcloughlin/avo/build"
	. "github.com/mmcloughlin/avo/operand"
	. "github.com/mmcloughlin/avo/reg"
	. "github.com/zeebo/blake3/_asm"
)

// The four state rows, and two message pools that swap roles every round so no
// round has to move registers.
const (
	rows  = 0
	poolA = 4
	poolB = 8
	tmp   = 12
)

func pack(a, b, c, d int) U8 {
	return U8(a<<6 | b<<4 | c<<2 | d)
}

func gvF(m, rotB int) {
	rotD := 16
	if rotB == 7 {
		rotD = 8
	}
	VPADDD(ZmmRegs[m], ZmmRegs[rows], ZmmRegs[rows])
	VPADDD(ZmmRegs[rows+1], ZmmRegs[rows], ZmmRegs[rows])
	VPXORD(ZmmRegs[rows], ZmmRegs[rows+3], ZmmRegs[rows+3])
	VPRORD(U8(rotD), ZmmRegs[rows+3], ZmmRegs[rows+3])
	VPADDD(ZmmRegs[rows+3], ZmmRegs[rows+2], ZmmRegs[rows+2])
	VPXORD(ZmmRegs[rows+2], ZmmRegs[rows+1], ZmmRegs[rows+1])
	VPRORD(U8(rotB), ZmmRegs[rows+1], ZmmRegs[rows+1])
}

func diagF() {
	VPSHUFD(pack(2, 1, 0, 3), ZmmRegs[rows], ZmmRegs[rows])
	VPSHUFD(pack(1, 0, 3, 2), ZmmRegs[rows+3], ZmmRegs[rows+3])
	VPSHUFD(pack(0, 3, 2, 1), ZmmRegs[rows+2], ZmmRegs[rows+2])
}

func undiagF() {
	VPSHUFD(pack(0, 3, 2, 1), ZmmRegs[rows], ZmmRegs[rows])
	VPSHUFD(pack(1, 0, 3, 2), ZmmRegs[rows+3], ZmmRegs[rows+3])
	VPSHUFD(pack(2, 1, 0, 3), ZmmRegs[rows+2], ZmmRegs[rows+2])
}

func round1MsgF() {
	VSHUFPS(pack(2, 0, 2, 0), ZmmRegs[poolA+1], ZmmRegs[poolA], ZmmRegs[poolB])
	VSHUFPS(pack(3, 1, 3, 1), ZmmRegs[poolA+1], ZmmRegs[poolA], ZmmRegs[poolB+1])
	VSHUFPS(pack(2, 0, 2, 0), ZmmRegs[poolA+3], ZmmRegs[poolA+2], ZmmRegs[poolB+2])
	VSHUFPS(pack(2, 1, 0, 3), ZmmRegs[poolB+2], ZmmRegs[poolB+2], ZmmRegs[poolB+2])
	VSHUFPS(pack(3, 1, 3, 1), ZmmRegs[poolA+3], ZmmRegs[poolA+2], ZmmRegs[poolB+3])
	VSHUFPS(pack(2, 1, 0, 3), ZmmRegs[poolB+3], ZmmRegs[poolB+3], ZmmRegs[poolB+3])
}

func permuteMsgF(p, q int) {
	VSHUFPS(pack(3, 1, 1, 2), ZmmRegs[p+1], ZmmRegs[p], ZmmRegs[q])
	VSHUFPS(pack(0, 3, 2, 1), ZmmRegs[q], ZmmRegs[q], ZmmRegs[q])
	VSHUFPS(pack(3, 3, 2, 2), ZmmRegs[p+3], ZmmRegs[p+2], ZmmRegs[q+1])
	VPSHUFD(pack(0, 0, 3, 3), ZmmRegs[p], ZmmRegs[tmp])
	VPBLENDMD(ZmmRegs[tmp], ZmmRegs[q+1], K1, ZmmRegs[q+1])
	VPUNPCKLDQ(ZmmRegs[p+1], ZmmRegs[p+3], ZmmRegs[q+2])
	VPBLENDMD(ZmmRegs[p+2], ZmmRegs[q+2], K2, ZmmRegs[q+2])
	VSHUFPS(pack(2, 3, 1, 0), ZmmRegs[q+2], ZmmRegs[q+2], ZmmRegs[q+2])
	VPUNPCKHDQ(ZmmRegs[p+3], ZmmRegs[p+1], ZmmRegs[tmp])
	VPUNPCKLDQ(ZmmRegs[tmp], ZmmRegs[p+2], ZmmRegs[q+3])
	VSHUFPS(pack(0, 1, 3, 2), ZmmRegs[q+3], ZmmRegs[q+3], ZmmRegs[q+3])
}

// HashF4 hashes up to 4 chunks in parallel. Every vector holds one state row of
// all four, one chunk per 128-bit lane, so a chunk's four G columns share a lane
// and the rounds never shuffle across one.
func HashF4(c Ctx) {
	TEXT("HashF4", 0, `func(
		input *[4096]byte,
		length uint64,
		counter uint64,
		flags uint32,
		key *[8]uint32,
		out *[64]uint32,
		chain *[8]uint32,
	)`)

	var (
		input   = Mem{Base: Load(Param("input"), GP64())}
		length  = Load(Param("length"), GP64()).(GPVirtual)
		counter = Load(Param("counter"), GP64()).(GPVirtual)
		flags   = Load(Param("flags"), GP32()).(GPVirtual)
		key     = Mem{Base: Load(Param("key"), GP64())}
		out     = Mem{Base: Load(Param("out"), GP64())}
		chain   = Mem{Base: Load(Param("chain"), GP64())}
	)

	loop := GP64()
	chunks := GP64()
	blocks := GP64()
	stash := GP64()
	row3 := GP64()

	// All of the locals share one 64-byte aligned arena, so no vector slot straddles a cache line.
	const (
		arenaStart = 0
		arenaMid   = arenaStart + 64
		arenaEnd   = arenaMid + 64
		arenaChain = arenaEnd + 64
		arenaSize  = arenaChain + 128
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
		Comment("Build row 3 for the first, middle, and last block of a chunk")
		ctr := GP64()
		bflags := GP32()
		for i := 0; i < 4; i++ {
			LEAQ(Mem{Base: counter, Disp: i}, ctr)
			MOVL(ctr.As32(), arena.Offset(arenaStart+16*i))
			SHRQ(U8(32), ctr)
			MOVL(ctr.As32(), arena.Offset(arenaStart+16*i+4))
			MOVL(U32(64), arena.Offset(arenaStart+16*i+8))
		}
		VMOVDQU32(arena.Offset(arenaStart), ZmmRegs[0])
		VMOVDQU32(ZmmRegs[0], arena.Offset(arenaMid))
		VMOVDQU32(ZmmRegs[0], arena.Offset(arenaEnd))

		MOVL(flags, bflags)
		ORL(U8(flag_chunkStart), bflags)
		for i := 0; i < 4; i++ {
			MOVL(bflags, arena.Offset(arenaStart+16*i+12))
		}
		for i := 0; i < 4; i++ {
			MOVL(flags, arena.Offset(arenaMid+16*i+12))
		}
		MOVL(flags, bflags)
		ORL(U8(flag_chunkEnd), bflags)
		for i := 0; i < 4; i++ {
			MOVL(bflags, arena.Offset(arenaEnd+16*i+12))
		}
	}

	{
		Comment("Set up the blend masks for the message permutation")
		mask := GP32()
		MOVL(U32(0x5555), mask)
		KMOVW(mask, K1)
		MOVL(U32(0x8888), mask)
		KMOVW(mask, K2)
	}

	{
		Comment("Load the key into the chaining value rows of every chunk")
		VBROADCASTI32X4(key.Offset(0), ZmmRegs[rows])
		VBROADCASTI32X4(key.Offset(16), ZmmRegs[rows+1])
		XORQ(loop, loop)
		LEAQ(arena.Offset(arenaStart), row3)
	}

	Label("loop")

	{
		Comment("Include end flags if last block")
		CMPQ(loop, U32(15*64))
		JNE(LabelRef("flags_done"))
		LEAQ(arena.Offset(arenaEnd), row3)
	}

	Label("flags_done")

	{
		Comment("Load and group the message words of every chunk")
		for k := 0; k < 4; k++ {
			z := poolA + k
			VMOVDQU32(input.Idx(loop, 1).Offset(16*k), ZmmRegs[z].AsX())
			for l := 1; l < 4; l++ {
				VINSERTI32X4(U8(l), input.Idx(loop, 1).Offset(1024*l+16*k), ZmmRegs[z], ZmmRegs[z])
			}
		}
	}

	{
		Comment("Build rows 2 and 3 from the IV, counters, and flags")
		VBROADCASTI32X4(c.IV.Offset(0), ZmmRegs[rows+2])
		VMOVDQU32(Mem{Base: row3}, ZmmRegs[rows+3])
	}

	{
		Comment("Save the chaining value before the partial chunk boundary")
		CMPQ(loop, blocks)
		JNE(LabelRef("chain_done"))

		lane := GP64()
		tmp32 := GP32()
		VMOVDQU32(ZmmRegs[rows], arena.Offset(arenaChain))
		VMOVDQU32(ZmmRegs[rows+1], arena.Offset(arenaChain+64))
		MOVQ(chunks, lane)
		SHLQ(U8(4), lane)
		for i := 0; i < 4; i++ {
			MOVL(arena.Offset(arenaChain+4*i).Idx(lane, 1), tmp32)
			MOVL(tmp32, chain.Offset(4*i))
			MOVL(arena.Offset(arenaChain+64+4*i).Idx(lane, 1), tmp32)
			MOVL(tmp32, chain.Offset(16+4*i))
		}
	}

	Label("chain_done")

	{
		Comment("Round 1")
		round1MsgF()
		gvF(poolB, 12)
		gvF(poolB+1, 7)
		diagF()
		gvF(poolB+2, 12)
		gvF(poolB+3, 7)
		undiagF()
	}

	for r := 2; r <= 7; r++ {
		Commentf("Round %d", r)
		p, q := poolA, poolB
		if r%2 == 0 {
			p, q = poolB, poolA
		}
		permuteMsgF(p, q)
		gvF(q, 12)
		gvF(q+1, 7)
		diagF()
		gvF(q+2, 12)
		gvF(q+3, 7)
		undiagF()
	}

	{
		Comment("Compute the chaining values for the next block")
		VPXORD(ZmmRegs[rows+2], ZmmRegs[rows], ZmmRegs[rows])
		VPXORD(ZmmRegs[rows+3], ZmmRegs[rows+1], ZmmRegs[rows+1])
	}

	{
		Comment("If we have zero complete chunks, we're done")
		CMPQ(chunks, U8(0))
		JNE(LabelRef("loop_trailer"))
		CMPQ(blocks, loop)
		JEQ(LabelRef("finalize"))
	}

	Label("loop_trailer")

	{
		Comment("Increment, use the middle-block flags, and loop")
		CMPQ(loop, U32(15*64))
		JEQ(LabelRef("finalize"))
		ADDQ(Imm(64), loop)
		LEAQ(arena.Offset(arenaMid), row3)
		JMP(LabelRef("loop"))
	}

	Label("finalize")

	{
		// VMOVDQU is VEX encoded and cannot reach Y16 and above, so the
		// transpose lands in the message pool, free by now.
		Comment("Transpose the chaining values into the word-major out layout")
		for w := 0; w < 4; w++ {
			VMOVDQU32(c.Transpose.Offset(64*w), ZmmRegs[tmp])
			VMOVDQA32(ZmmRegs[rows], ZmmRegs[poolA])
			VPERMT2D(ZmmRegs[rows], ZmmRegs[tmp], ZmmRegs[poolA])
			VMOVDQU(ZmmRegs[poolA].AsY(), out.Offset(32*w))
			VMOVDQA32(ZmmRegs[rows+1], ZmmRegs[poolA+1])
			VPERMT2D(ZmmRegs[rows+1], ZmmRegs[tmp], ZmmRegs[poolA+1])
			VMOVDQU(ZmmRegs[poolA+1].AsY(), out.Offset(32*(4+w)))
		}
	}

	VZEROUPPER()
	RET()
}
