//go:build goexperiment.simd && amd64 && !purego

package consts

import (
	"os"
	"simd/archsimd"
)

// HasSIMD reports whether the simd/archsimd implementation is usable.
//
// AVX2 is enough. The vector width used is 256 bits and, as of Go 1.27,
// RotateAllRight on a 256-bit vector lowers to a shift pair rather than
// AVX-512VL's VPRORD, so nothing in the generated code needs EVEX encoding.
//
// This file only exists when building with GOEXPERIMENT=simd.
var HasSIMD = archsimd.X86.AVX2() &&
	os.Getenv("BLAKE3_DISABLE_SIMD") == "" &&
	os.Getenv("BLAKE3_PUREGO") == ""
