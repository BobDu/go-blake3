//go:build !(goexperiment.simd && amd64)

package hash_simd

import "github.com/zeebo/blake3/internal/alg/hash/hash_pure"

// Enabled reports whether the simd/archsimd implementation is compiled in.
const Enabled = false

func HashF(input *[8192]byte, length, counter uint64, flags uint32, key *[8]uint32, out *[64]uint32, chain *[8]uint32) {
	hash_pure.HashF(input, length, counter, flags, key, out, chain)
}

func HashP(left, right *[64]uint32, flags uint32, key *[8]uint32, out *[64]uint32, n int) {
	hash_pure.HashP(left, right, flags, key, out, n)
}
