package main

import (
	"github.com/mmcloughlin/avo/build"
)

func main() {
	c := NewCtx()

	HashF(c)
	HashF4(c)
	HashP(c)

	build.Generate()
}
