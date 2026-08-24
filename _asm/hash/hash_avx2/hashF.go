package main

import (
	. "github.com/mmcloughlin/avo/build"
	. "github.com/mmcloughlin/avo/operand"
	. "github.com/mmcloughlin/avo/reg"
	. "github.com/zeebo/blake3/_asm"
)

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

	// All of the locals share one 32-byte aligned arena, so no vector slot straddles a cache line.
	const (
		arenaMsg     = 0
		arenaSpills  = arenaMsg + 16*32
		arenaCtrLo   = arenaSpills + roundSize
		arenaCtrHi   = arenaCtrLo + 32
		arenaTmp     = arenaCtrHi + 32
		arenaFlags   = arenaTmp + 32
		arenaCounter = arenaFlags + 8
		arenaSize    = arenaCounter + 8
	)

	{
		Comment("Allocate local space and align it")
		local := AllocLocal(arenaSize + 32)
		LEAQ(local.Offset(31), stash)
		ANDQ(I32(^31), stash)
	}

	var (
		msg         = Mem{Base: stash}.Offset(arenaMsg)
		ctr_lo_mem  = Mem{Base: stash}.Offset(arenaCtrLo)
		ctr_hi_mem  = Mem{Base: stash}.Offset(arenaCtrHi)
		tmp         = Mem{Base: stash}.Offset(arenaTmp)
		flags_mem   = Mem{Base: stash}.Offset(arenaFlags)
		counter_mem = Mem{Base: stash}.Offset(arenaCounter)
	)

	alloc := NewAlloc(Mem{Base: stash}.Offset(arenaSpills))
	defer alloc.Free()

	var (
		h_vecs    []*Value
		h_regs    []int
		vs        []*Value
		iv        []*Value
		ctr_low   *Value
		ctr_hi    *Value
		blen_vec  *Value
		flags_vec *Value
	)

	h_vecs = alloc.ValuesWith(8, key)
	blen_vec = alloc.ValueFrom(c.BlockLen)
	flags_vec = alloc.ValueWith(flags_mem)
	iv = alloc.ValuesWith(4, c.IV)
	ctr_low = alloc.ValueFrom(ctr_lo_mem)
	ctr_hi = alloc.ValueFrom(ctr_hi_mem)

	{
		Comment("Skip if the length is zero")
		XORQ(chunks, chunks)
		XORQ(blocks, blocks)
		TESTQ(length, length)
		JZ(LabelRef("skip_compute"))
	}

	{
		Comment("Compute complete chunks and blocks")

		// chunks = (length - 1) / 1024
		SUBQ(U8(1), length)
		MOVQ(length, chunks)
		SHRQ(U8(10), chunks)

		// blocks = (length - 1) % 1024 / 64 * 64
		MOVQ(length, blocks)
		ANDQ(U32(960), blocks)
	}

	Label("skip_compute")

	{
		Comment("Two chunks or fewer do not need all eight lanes")
		CMPQ(chunks, U8(2))
		JAE(LabelRef("eight_lane"))
		emitMixedAxis(c, input, key, out, chain, Mem{Base: stash}, counter, flags, chunks, blocks)
	}

	Label("eight_lane")

	{
		Comment("Load some params into the stack (avo improvment?)")
		MOVL(flags, flags_mem)
		MOVQ(counter, counter_mem)
	}

	{
		Comment("Load IV into vectors")
		h_regs = make([]int, 8)
		for i, v := range h_vecs {
			h_regs[i] = v.Reg()
			_ = v.Get()
		}
	}

	{
		Comment("Build and store counter data on the stack")
		loadCounter(c, alloc, counter_mem, ctr_lo_mem, ctr_hi_mem)
	}

	{
		Comment("Set up block flags and variables for iteration")
		XORQ(loop, loop)
		ORL(U8(flag_chunkStart), flags_mem)
	}

	Label("loop")

	{
		Comment("Include end flags if last block")
		CMPQ(loop, U32(15*64))
		JNE(LabelRef("round_setup"))
		ORL(U8(flag_chunkEnd), flags_mem)
	}

	Label("round_setup")

	{
		Comment("Load and transpose message vectors")
		transposeMsg(c, alloc, loop, input, msg)
	}

	{
		Comment("Load constants for the round")
		for _, v := range h_vecs {
			_ = v.Get()
		}
		_ = blen_vec.Get()
		_ = flags_vec.Get()
		for _, v := range iv {
			_ = v.Get()
		}
		_ = ctr_low.Get()
		_ = ctr_hi.Get()
	}

	{
		Comment("Save state for partial chunk if necessary")
		CMPQ(loop, blocks)
		JNE(LabelRef("begin_rounds"))

		for i, v := range h_vecs {
			tmp32 := GP32()
			VMOVDQU(v.Get(), tmp)
			MOVL(tmp.Idx(chunks, 4), tmp32)
			MOVL(tmp32, chain.Offset(4*i))
		}
	}

	Label("begin_rounds")

	{
		Comment("Perform the rounds")

		vs = []*Value{
			h_vecs[0], h_vecs[1], h_vecs[2], h_vecs[3],
			h_vecs[4], h_vecs[5], h_vecs[6], h_vecs[7],
			iv[0], iv[1], iv[2], iv[3],
			ctr_low, ctr_hi, blen_vec, flags_vec,
		}

		for r := 0; r < 7; r++ {
			Commentf("Round %d", r+1)
			roundF(c, alloc, vs, r, msg)
		}
	}

	{
		Comment("Finalize rounds")
		finalizeRounds(alloc, vs, h_vecs, h_regs)
	}

	{
		Comment("Fix up registers for next iteration")
		for i := 7; i >= 0; i-- {
			h_vecs[i].Become(h_regs[i])
		}
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
		Comment("Increment, reset flags, and loop")
		CMPQ(loop, U32(15*64))
		JEQ(LabelRef("finalize"))

		ADDQ(Imm(64), loop)
		MOVL(flags, flags_mem)
		JMP(LabelRef("loop"))
	}

	Label("finalize")

	{
		Comment("Store result into out")
		for i, v := range h_vecs {
			VMOVDQU(v.Consume(), out.Offset(32*i))
		}
	}

	VZEROUPPER()
	RET()
}

func roundF(c Ctx, alloc *Alloc, vs []*Value, r int, mp Mem) {
	round(c, alloc, vs, r, func(n int) Mem {
		return mp.Offset(n * 32)
	})
}

// emitMixedAxis hashes two chunks or fewer with one chunk per 128-bit lane, so
// the four G columns of a chunk share a lane and the rounds never shuffle across
// one. It runs no lanes it has no chunks for, which is what the eight lane path
// cannot do.
func emitMixedAxis(c Ctx, input, key, out, chain, arena Mem, counter, flags GPVirtual, chunks, blocks GPVirtual) {
	loop := GP64()
	row3 := GP64()

	// This path reuses the front of the arena the eight lane one transposes its
	// messages into, since only one of the two ever runs.
	const (
		mixStart = 0
		mixMid   = mixStart + 32
		mixEnd   = mixMid + 32
		mixChain = mixEnd + 32
	)

	{
		Comment("Build row 3 for the first, middle, and last block of a chunk")
		ctr := GP64()
		bflags := GP32()
		for i := 0; i < 2; i++ {
			LEAQ(Mem{Base: counter, Disp: i}, ctr)
			MOVL(ctr.As32(), arena.Offset(mixStart+16*i))
			SHRQ(U8(32), ctr)
			MOVL(ctr.As32(), arena.Offset(mixStart+16*i+4))
			MOVL(U32(64), arena.Offset(mixStart+16*i+8))
		}
		VMOVDQU(arena.Offset(mixStart), ymmAll[0])
		VMOVDQU(ymmAll[0], arena.Offset(mixMid))
		VMOVDQU(ymmAll[0], arena.Offset(mixEnd))

		MOVL(flags, bflags)
		ORL(U8(flag_chunkStart), bflags)
		for i := 0; i < 2; i++ {
			MOVL(bflags, arena.Offset(mixStart+16*i+12))
		}
		for i := 0; i < 2; i++ {
			MOVL(flags, arena.Offset(mixMid+16*i+12))
		}
		MOVL(flags, bflags)
		ORL(U8(flag_chunkEnd), bflags)
		for i := 0; i < 2; i++ {
			MOVL(bflags, arena.Offset(mixEnd+16*i+12))
		}
	}

	{
		Comment("Load the rotate tables and the key")
		VMOVDQU(c.Rot16, ymmAll[yrot16])
		VMOVDQU(c.Rot8, ymmAll[yrot8])
		VBROADCASTI128(key.Offset(0), ymmAll[yrows])
		VBROADCASTI128(key.Offset(16), ymmAll[yrows+1])
		XORQ(loop, loop)
		LEAQ(arena.Offset(mixStart), row3)
	}

	Label("mix_loop")

	{
		Comment("Include end flags if last block")
		CMPQ(loop, U32(15*64))
		JNE(LabelRef("mix_flags_done"))
		LEAQ(arena.Offset(mixEnd), row3)
	}

	Label("mix_flags_done")

	{
		Comment("Load and group the message words of both chunks")
		for k := 0; k < 4; k++ {
			z := ypoolA + k
			VMOVDQU(input.Idx(loop, 1).Offset(16*k), ymmAll[z].AsX())
			VINSERTI128(U8(1), input.Idx(loop, 1).Offset(1024+16*k), ymmAll[z], ymmAll[z])
		}
	}

	{
		Comment("Build rows 2 and 3 from the IV, counters, and flags")
		VBROADCASTI128(c.IV.Offset(0), ymmAll[yrows+2])
		VMOVDQU(Mem{Base: row3}, ymmAll[yrows+3])
	}

	{
		Comment("Save the chaining value before the partial chunk boundary")
		CMPQ(loop, blocks)
		JNE(LabelRef("mix_chain_done"))

		lane := GP64()
		tmp32 := GP32()
		VMOVDQU(ymmAll[yrows], arena.Offset(mixChain))
		VMOVDQU(ymmAll[yrows+1], arena.Offset(mixChain+32))
		MOVQ(chunks, lane)
		SHLQ(U8(4), lane)
		for i := 0; i < 4; i++ {
			MOVL(arena.Offset(mixChain+4*i).Idx(lane, 1), tmp32)
			MOVL(tmp32, chain.Offset(4*i))
			MOVL(arena.Offset(mixChain+32+4*i).Idx(lane, 1), tmp32)
			MOVL(tmp32, chain.Offset(16+4*i))
		}
	}

	Label("mix_chain_done")

	{
		Comment("Round 1")
		round1MsgF2()
		gvF2(ypoolB, 12)
		gvF2(ypoolB+1, 7)
		diagF2()
		gvF2(ypoolB+2, 12)
		gvF2(ypoolB+3, 7)
		undiagF2()
	}

	for r := 2; r <= 7; r++ {
		Commentf("Round %d", r)
		p, q := ypoolA, ypoolB
		if r%2 == 0 {
			p, q = ypoolB, ypoolA
		}
		permuteMsgF2(p, q)
		gvF2(q, 12)
		gvF2(q+1, 7)
		diagF2()
		gvF2(q+2, 12)
		gvF2(q+3, 7)
		undiagF2()
	}

	{
		Comment("Compute the chaining values for the next block")
		VPXOR(ymmAll[yrows+2], ymmAll[yrows], ymmAll[yrows])
		VPXOR(ymmAll[yrows+3], ymmAll[yrows+1], ymmAll[yrows+1])
	}

	{
		Comment("If we have zero complete chunks, we're done")
		CMPQ(chunks, U8(0))
		JNE(LabelRef("mix_loop_trailer"))
		CMPQ(blocks, loop)
		JEQ(LabelRef("mix_finalize"))
	}

	Label("mix_loop_trailer")

	{
		Comment("Increment, use the middle-block flags, and loop")
		CMPQ(loop, U32(15*64))
		JEQ(LabelRef("mix_finalize"))
		ADDQ(Imm(64), loop)
		LEAQ(arena.Offset(mixMid), row3)
		JMP(LabelRef("mix_loop"))
	}

	Label("mix_finalize")

	{
		Comment("Transpose the chaining values into the word-major out layout")
		// out wants word w of both chunks side by side at out+32*w, and a row
		// holds four words of chunk 0 then four of chunk 1, so one unpack pair
		// per row brings the two chunks of each word together.
		for i := 0; i < 2; i++ {
			r := yrows + i
			VEXTRACTI128(U8(1), ymmAll[r], ymmAll[ytmp].AsX())
			VPUNPCKLDQ(ymmAll[ytmp].AsX(), ymmAll[r].AsX(), ymmAll[ypoolA].AsX())
			VPUNPCKHDQ(ymmAll[ytmp].AsX(), ymmAll[r].AsX(), ymmAll[ypoolA+1].AsX())
			VMOVQ(ymmAll[ypoolA].AsX(), out.Offset(32*(4*i+0)))
			VPEXTRQ(U8(1), ymmAll[ypoolA].AsX(), out.Offset(32*(4*i+1)))
			VMOVQ(ymmAll[ypoolA+1].AsX(), out.Offset(32*(4*i+2)))
			VPEXTRQ(U8(1), ymmAll[ypoolA+1].AsX(), out.Offset(32*(4*i+3)))
		}
	}

	VZEROUPPER()
	RET()
}

// The four state rows, two message pools that swap roles every round so no
// round has to move registers, one scratch register, and the two rotate tables.
const (
	yrows  = 0
	ypoolA = 4
	ypoolB = 8
	ytmp   = 12
	yrot16 = 13
	yrot8  = 14
)

var ymmAll = []VecPhysical{
	Y0, Y1, Y2, Y3, Y4, Y5, Y6, Y7,
	Y8, Y9, Y10, Y11, Y12, Y13, Y14, Y15,
}

func ypack(a, b, c, d int) U8 {
	return U8(a<<6 | b<<4 | c<<2 | d)
}

// yrot rotates every dword right by n. Only 16 and 8 are byte aligned and reach
// the shuffle tables; the others cost a shift pair and an or.
func yrot(dst, n int) {
	switch n {
	case 16:
		VPSHUFB(ymmAll[yrot16], ymmAll[dst], ymmAll[dst])
	case 8:
		VPSHUFB(ymmAll[yrot8], ymmAll[dst], ymmAll[dst])
	default:
		VPSRLD(U8(n), ymmAll[dst], ymmAll[ytmp])
		VPSLLD(U8(32-n), ymmAll[dst], ymmAll[dst])
		VPOR(ymmAll[ytmp], ymmAll[dst], ymmAll[dst])
	}
}

func gvF2(m, rotB int) {
	rotD := 16
	if rotB == 7 {
		rotD = 8
	}
	VPADDD(ymmAll[m], ymmAll[yrows], ymmAll[yrows])
	VPADDD(ymmAll[yrows+1], ymmAll[yrows], ymmAll[yrows])
	VPXOR(ymmAll[yrows], ymmAll[yrows+3], ymmAll[yrows+3])
	yrot(yrows+3, rotD)
	VPADDD(ymmAll[yrows+3], ymmAll[yrows+2], ymmAll[yrows+2])
	VPXOR(ymmAll[yrows+2], ymmAll[yrows+1], ymmAll[yrows+1])
	yrot(yrows+1, rotB)
}

func diagF2() {
	VPSHUFD(ypack(2, 1, 0, 3), ymmAll[yrows], ymmAll[yrows])
	VPSHUFD(ypack(1, 0, 3, 2), ymmAll[yrows+3], ymmAll[yrows+3])
	VPSHUFD(ypack(0, 3, 2, 1), ymmAll[yrows+2], ymmAll[yrows+2])
}

func undiagF2() {
	VPSHUFD(ypack(0, 3, 2, 1), ymmAll[yrows], ymmAll[yrows])
	VPSHUFD(ypack(1, 0, 3, 2), ymmAll[yrows+3], ymmAll[yrows+3])
	VPSHUFD(ypack(2, 1, 0, 3), ymmAll[yrows+2], ymmAll[yrows+2])
}

func round1MsgF2() {
	a, b := ypoolA, ypoolB
	VSHUFPS(ypack(2, 0, 2, 0), ymmAll[a+1], ymmAll[a], ymmAll[b])
	VSHUFPS(ypack(3, 1, 3, 1), ymmAll[a+1], ymmAll[a], ymmAll[b+1])
	VSHUFPS(ypack(2, 0, 2, 0), ymmAll[a+3], ymmAll[a+2], ymmAll[b+2])
	VSHUFPS(ypack(2, 1, 0, 3), ymmAll[b+2], ymmAll[b+2], ymmAll[b+2])
	VSHUFPS(ypack(3, 1, 3, 1), ymmAll[a+3], ymmAll[a+2], ymmAll[b+3])
	VSHUFPS(ypack(2, 1, 0, 3), ymmAll[b+3], ymmAll[b+3], ymmAll[b+3])
}

// permuteMsgF2 applies the blake3 message schedule, reading pool p into pool q.
// The two blends select the same dwords the sse41 generator picks with PBLENDW,
// expressed as VPBLENDD immediates over eight dwords.
func permuteMsgF2(p, q int) {
	VSHUFPS(ypack(3, 1, 1, 2), ymmAll[p+1], ymmAll[p], ymmAll[q])
	VSHUFPS(ypack(0, 3, 2, 1), ymmAll[q], ymmAll[q], ymmAll[q])
	VSHUFPS(ypack(3, 3, 2, 2), ymmAll[p+3], ymmAll[p+2], ymmAll[q+1])
	VPSHUFD(ypack(0, 0, 3, 3), ymmAll[p], ymmAll[ytmp])
	VPBLENDD(U8(0x55), ymmAll[ytmp], ymmAll[q+1], ymmAll[q+1])
	VPUNPCKLDQ(ymmAll[p+1], ymmAll[p+3], ymmAll[q+2])
	VPBLENDD(U8(0x88), ymmAll[p+2], ymmAll[q+2], ymmAll[q+2])
	VSHUFPS(ypack(2, 3, 1, 0), ymmAll[q+2], ymmAll[q+2], ymmAll[q+2])
	VPUNPCKHDQ(ymmAll[p+3], ymmAll[p+1], ymmAll[ytmp])
	VPUNPCKLDQ(ymmAll[ytmp], ymmAll[p+2], ymmAll[q+3])
	VSHUFPS(ypack(0, 1, 3, 2), ymmAll[q+3], ymmAll[q+3], ymmAll[q+3])
}
