package main

import (
	"github.com/mmcloughlin/avo/build"
)

func main() {
	c := NewCtx()

	HashF(c)
	HashP(c)
	HashF16(c)

	build.Generate()
}
