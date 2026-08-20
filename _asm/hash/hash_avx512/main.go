package main

import (
	"github.com/mmcloughlin/avo/build"
)

func main() {
	c := NewCtx()

	HashF(c)
	HashF16(c)
	HashP(c)

	build.Generate()
}
