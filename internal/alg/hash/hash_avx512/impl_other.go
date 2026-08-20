//go:build !amd64
// +build !amd64

package hash_avx512

import "github.com/zeebo/blake3/internal/alg/hash/hash_pure"

func HashF(input *[8192]byte, length, counter uint64, flags uint32, key *[8]uint32, out *[64]uint32, chain *[8]uint32) {
	hash_pure.HashF(input, length, counter, flags, key, out, chain)
}

func HashP(left, right *[64]uint32, flags uint32, key *[8]uint32, out *[64]uint32, n int) {
	hash_pure.HashP(left, right, flags, key, out, n)
}

func HashF16(input *[16384]byte, length, counter uint64, flags uint32, key *[8]uint32, outLo, outHi *[64]uint32, chain *[8]uint32) {
	lo, hi := length, uint64(0)
	if length > 8192 {
		lo, hi = 8192, length-8192
	}
	hash_pure.HashF((*[8192]byte)(input[:8192]), lo, counter, flags, key, outLo, chain)
	hash_pure.HashF((*[8192]byte)(input[8192:]), hi, counter+8, flags, key, outHi, chain)
}
