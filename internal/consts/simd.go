//go:build goexperiment.simd && amd64 && !purego

package consts

import (
	"os"
	"simd/archsimd"
)

// HasSIMD reports whether the simd/archsimd implementation is usable.
//
// AVX2 is enough. The vector width used is 256 bits, and no operation the
// implementation reaches for needs EVEX encoding: RotateAllRight in particular
// has no amd64 intrinsic in Go 1.27 at any width, so it lowers to a shift pair
// and an or rather than to AVX-512VL's VPRORD.
//
// This file only exists when building with GOEXPERIMENT=simd.
var HasSIMD = archsimd.X86.AVX2() &&
	os.Getenv("BLAKE3_DISABLE_SIMD") == "" &&
	os.Getenv("BLAKE3_PUREGO") == ""
