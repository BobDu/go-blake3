package main

import (
	. "github.com/mmcloughlin/avo/build"
	. "github.com/mmcloughlin/avo/operand"
	. "github.com/mmcloughlin/avo/reg"
	. "github.com/zeebo/blake3/avo"
)

// zmmSlot is the message and spill slot size the sixteen lane kernel uses,
// one 512 bit register per word.
const zmmSlot = 64

func HashF16(c Ctx) {
	TEXT("HashF16", 0, `func(
		input *[16384]byte,
		length uint64,
		counter uint64,
		flags uint32,
		key *[8]uint32,
		outLo *[32]uint32,
		outHi *[32]uint32,
		chain *[8]uint32,
	)`)

	var (
		input   = Mem{Base: Load(Param("input"), GP64())}
		length  = Load(Param("length"), GP64()).(GPVirtual)
		counter = Load(Param("counter"), GP64()).(GPVirtual)
		flags   = Load(Param("flags"), GP32()).(GPVirtual)
		key     = Mem{Base: Load(Param("key"), GP64())}
		outLo   = Mem{Base: Load(Param("outLo"), GP64())}
		outHi   = Mem{Base: Load(Param("outHi"), GP64())}
		chain   = Mem{Base: Load(Param("chain"), GP64())}
	)

	loop := GP64()
	chunks := GP64()
	blocks := GP64()
	stash := GP64()

	// All of the locals share one 32-byte aligned arena, so no vector slot straddles a cache line.
	const (
		arenaMsg       = 0
		arenaSpills    = arenaMsg + 16*zmmSlot
		arenaYmmSpills = arenaSpills + 16*zmmSlot
		arenaCtrLo     = arenaYmmSpills + 16*32
		arenaCtrHi     = arenaCtrLo + 64
		arenaTmp       = arenaCtrHi + 64
		arenaFlags     = arenaTmp + 64
		arenaCounter   = arenaFlags + 8
		arenaSize      = arenaCounter + 8
	)

	{
		Comment("Allocate local space and align it")
		local := AllocLocal(arenaSize + 64)
		LEAQ(local.Offset(63), stash)
		ANDQ(I32(^63), stash)
	}

	var (
		msg         = Mem{Base: stash}.Offset(arenaMsg)
		ctr_lo_mem  = Mem{Base: stash}.Offset(arenaCtrLo)
		ctr_hi_mem  = Mem{Base: stash}.Offset(arenaCtrHi)
		tmp         = Mem{Base: stash}.Offset(arenaTmp)
		flags_mem   = Mem{Base: stash}.Offset(arenaFlags)
		counter_mem = Mem{Base: stash}.Offset(arenaCounter)
	)

	alloc := NewAllocZMM(Mem{Base: stash}.Offset(arenaSpills))
	defer alloc.Free()

	// The transpose and the counter build stay 256-bit and use Y0-Y15, because the
	// shuffles they need are VEX only and VEX cannot encode Y16-Y31. The zmm state
	// lives in Z16-Z31, so the two never alias.
	ymmAlloc := NewAlloc(Mem{Base: stash}.Offset(arenaYmmSpills))
	defer ymmAlloc.Free()

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
		loadCounter(c, ymmAlloc, 16, counter_mem, ctr_lo_mem, ctr_hi_mem)
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
		transposeMsg(c, ymmAlloc, 16, loop, input, msg)
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
			VMOVDQU64(v.Get(), tmp)
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
			roundF(c, alloc, 16, vs, r, msg)
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
		Comment("Store result into the two output halves")
		scratch := ymmAlloc.Value()
		for i, v := range h_vecs {
			z := v.Get()
			VEXTRACTI64X4(Imm(0), z, scratch.Get())
			VMOVDQU(scratch.Get(), outLo.Offset(32*i))
			VEXTRACTI64X4(Imm(1), z, scratch.Get())
			VMOVDQU(scratch.Get(), outHi.Offset(32*i))
			v.Free()
		}
		scratch.Free()
	}

	VZEROUPPER()
	RET()
}
