//go:build arm64

package main

import (
	"fmt"
	"os"
	"sort"
	"time"
)

func eorChain(iters uint64)
func eor2Chain(iters uint64)
func eor3Chain(iters uint64)

// 每次调用的串行指令数
const (
	nEor  = 100
	nEor2 = 200
	nEor3 = 100
)

func timeIt(f func(uint64), iters uint64, ops int, reps int) float64 {
	var s []float64
	for i := 0; i < reps; i++ {
		t := time.Now()
		f(iters)
		d := time.Since(t)
		s = append(s, float64(d.Nanoseconds())/float64(iters*uint64(ops)))
	}
	sort.Float64s(s)
	return s[0] // 取最小值:慢尾是干扰,不是被测代码
}

func main() {
	const iters = 200000
	const reps = 9

	// 预热
	eorChain(iters / 10)
	eor3Chain(iters / 10)

	// 交错测量,逐轮换序
	var e, e2, e3 []float64
	for r := 0; r < reps; r++ {
		switch r % 3 {
		case 0:
			e = append(e, timeIt(eorChain, iters, nEor, 1))
			e2 = append(e2, timeIt(eor2Chain, iters, nEor2, 1))
			e3 = append(e3, timeIt(eor3Chain, iters, nEor3, 1))
		case 1:
			e2 = append(e2, timeIt(eor2Chain, iters, nEor2, 1))
			e3 = append(e3, timeIt(eor3Chain, iters, nEor3, 1))
			e = append(e, timeIt(eorChain, iters, nEor, 1))
		default:
			e3 = append(e3, timeIt(eor3Chain, iters, nEor3, 1))
			e = append(e, timeIt(eorChain, iters, nEor, 1))
			e2 = append(e2, timeIt(eor2Chain, iters, nEor2, 1))
		}
	}
	min := func(v []float64) float64 { sort.Float64s(v); return v[0] }
	ne, ne2, ne3 := min(e), min(e2), min(e3)

	fmt.Printf("VEOR   链长 100 : %.4f ns/op\n", ne)
	fmt.Printf("VEOR   链长 200 : %.4f ns/op   (有效性闸门)\n", ne2)
	fmt.Printf("VEOR3  链长 100 : %.4f ns/op\n", ne3)
	fmt.Println()

	gate := ne2 / ne
	fmt.Printf("闸门 = 200链/100链 每-op 时间比 = %.3f  (须在 0.95–1.05,否则测的不是延迟)\n", gate)
	if gate < 0.95 || gate > 1.05 {
		fmt.Println("❌ 闸门不通过 —— 本次测量作废,不要采用下面的比值")
		os.Exit(1)
	}
	fmt.Println("✅ 闸门通过")
	fmt.Println()

	ratio := ne3 / ne
	fmt.Printf("🔑 VEOR3 / VEOR 延迟比 = %.3f\n", ratio)
	fmt.Println()
	switch {
	case ratio <= 1.15:
		fmt.Println("判读:≈1.0 —— EOR3 与 EOR 同延迟,#6 那条线的前提成立,预计 5–8%")
	case ratio <= 1.6:
		fmt.Println("判读:约 1.5 —— 收益减半到 ~6%,勉强")
	default:
		fmt.Println("判读:≥2.0 —— 收益归零而指令多 25%,纯亏。这条线当场结束")
	}
}
