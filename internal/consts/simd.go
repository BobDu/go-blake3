//go:build goexperiment.simd && amd64 && !purego

package consts

import (
	"os"
	"simd/archsimd"
)

// HasSIMD reports whether the simd/archsimd implementation is usable.
//
// It needs AVX-512 rather than merely AVX2: RotateAllRight on a 256-bit vector
// lowers to AVX-512VL's VPRORD. It is only compiled in at all when building
// with GOEXPERIMENT=simd.
var HasSIMD = archsimd.X86.AVX512() &&
	os.Getenv("BLAKE3_DISABLE_SIMD") == "" &&
	os.Getenv("BLAKE3_PUREGO") == ""
