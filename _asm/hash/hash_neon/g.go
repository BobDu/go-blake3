package main

var gStates = [8][4]int{
	{0, 4, 8, 12}, {1, 5, 9, 13}, {2, 6, 10, 14}, {3, 7, 11, 15},
	{0, 5, 10, 15}, {1, 6, 11, 12}, {2, 7, 8, 13}, {3, 4, 9, 14},
}

type loadMsg func(a *asm, idx, vreg int)

// BLAKE3 rotates right, so the VSHL/VSRI immediates are mirrored relative
// to ChaCha kernels. V31 holds the rotate-right-8 TBL indices.
func emitG(a *asm, va, vb, vc, vd, mx, my int, load loadMsg) {
	load(a, mx, 16)
	a.op("VADD V16.S4, V%d.S4, V%d.S4", va, va)
	a.op("VADD V%d.S4, V%d.S4, V%d.S4", vb, va, va)
	// ror(b,12) 只依赖 b,提出 c 那条链:V19 = ror(b,12)
	a.op("VSHL $20, V%d.S4, V19.S4", vb)
	a.op("VSRI $12, V%d.S4, V19.S4", vb)
	a.op("VEOR V%d.B16, V%d.B16, V%d.B16", va, vd, vd)
	a.op("VREV32 V%d.H8, V%d.H8", vd, vd)
	a.op("VADD V%d.S4, V%d.S4, V%d.S4", vd, vc, vc)
	// ror(b^c,12) = ror(b,12) ^ (c>>12) ^ (c<<20);c 到 b 由 3 层降到 2 层
	a.op("VUSHR $12, V%d.S4, V18.S4", vc)
	a.op("VSHL $20, V%d.S4, V%d.S4", vc, vb)
	a.op("VEOR3 V19.B16, V18.B16, V%d.B16, V%d.B16", vb, vb)
	load(a, my, 17)
	a.op("VADD V17.S4, V%d.S4, V%d.S4", va, va)
	a.op("VADD V%d.S4, V%d.S4, V%d.S4", vb, va, va)
	a.op("VSHL $25, V%d.S4, V19.S4", vb)
	a.op("VSRI $7, V%d.S4, V19.S4", vb)
	a.op("VEOR V%d.B16, V%d.B16, V%d.B16", va, vd, vd)
	a.op("VTBL V31.B16, [V%d.B16], V%d.B16", vd, vd)
	a.op("VADD V%d.S4, V%d.S4, V%d.S4", vd, vc, vc)
	a.op("VUSHR $7, V%d.S4, V18.S4", vc)
	a.op("VSHL $25, V%d.S4, V%d.S4", vc, vb)
	a.op("VEOR3 V19.B16, V18.B16, V%d.B16, V%d.B16", vb, vb)
}

func emitRounds(a *asm, load loadMsg) {
	for r := range msgSchedule {
		a.comment("round %d", r+1)
		s := msgSchedule[r]
		for gi, st := range gStates {
			emitG(a, st[0], st[1], st[2], st[3], s[2*gi], s[2*gi+1], load)
		}
	}
}

func emitKeyBroadcast(a *asm, keyReg string) {
	a.op("VLD1 (%s), [V28.S4, V29.S4]", keyReg)
	for i := 0; i < 8; i++ {
		a.op("VDUP V%d.S[%d], V%d.S4", 28+i/4, i%4, i)
	}
}

func emitFeedForward(a *asm) {
	for i := 0; i < 8; i++ {
		a.op("VEOR V%d.B16, V%d.B16, V%d.B16", 8+i, i, i)
	}
}
