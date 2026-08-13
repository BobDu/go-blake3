package main

import (
	"github.com/mmcloughlin/avo/build"
)

func main() {
	c := NewCtx()

	HashF(c)

	build.Generate()
}
