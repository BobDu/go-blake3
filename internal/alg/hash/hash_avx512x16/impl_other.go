//go:build !amd64
// +build !amd64

package hash_avx512x16

func HashF16(input *[16384]byte, length, counter uint64, flags uint32, key *[8]uint32, outLo, outHi *[64]uint32, chain *[8]uint32) {
	panic("HashF16 requires amd64")
}
