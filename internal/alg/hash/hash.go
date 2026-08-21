package hash

import (
	"github.com/zeebo/blake3/internal/alg/hash/hash_avx2"
	"github.com/zeebo/blake3/internal/alg/hash/hash_avx512"
	"github.com/zeebo/blake3/internal/alg/hash/hash_neon"
	"github.com/zeebo/blake3/internal/alg/hash/hash_pure"
	"github.com/zeebo/blake3/internal/alg/hash/hash_sve2"
	"github.com/zeebo/blake3/internal/consts"
)

func HashF(input *[8192]byte, length, counter uint64, flags uint32, key *[8]uint32, out *[64]uint32, chain *[8]uint32) {
	if consts.HasAVX512 && length > 2*consts.ChunkLen {
		// Four chunks or fewer come out ahead in the mixed-axis kernel, which
		// runs no lanes it has no chunks for. Beyond that the eight lane kernel
		// wins: filling a Zmm costs 512-bit instructions, and the licence-based
		// downclocking they trigger outweighs the wider vectors on Skylake and
		// Ice Lake server parts.
		if length <= 4*consts.ChunkLen {
			hash_avx512.HashF4((*[4096]byte)(input[:4096]), length, counter, flags, key, out, chain)
		} else {
			hash_avx512.HashF(input, length, counter, flags, key, out, chain)
		}
	} else if consts.HasAVX2 && length > 2*consts.ChunkLen {
		hash_avx2.HashF(input, length, counter, flags, key, out, chain)
	} else if consts.HasSVE2 && length > 2*consts.ChunkLen {
		hash_sve2.HashF(input, length, counter, flags, key, out, chain)
	} else if consts.HasNEON && length > 2*consts.ChunkLen {
		hash_neon.HashF(input, length, counter, flags, key, out, chain)
	} else {
		hash_pure.HashF(input, length, counter, flags, key, out, chain)
	}
}

func HashP(left, right *[64]uint32, flags uint32, key *[8]uint32, out *[64]uint32, n int) {
	if consts.HasAVX512 && n >= 2 {
		hash_avx512.HashP(left, right, flags, key, out, n)
	} else if consts.HasAVX2 && n >= 2 {
		hash_avx2.HashP(left, right, flags, key, out, n)
	} else if consts.HasSVE2 && n >= 2 {
		hash_sve2.HashP(left, right, flags, key, out, n)
	} else if consts.HasNEON && n >= 2 {
		hash_neon.HashP(left, right, flags, key, out, n)
	} else {
		hash_pure.HashP(left, right, flags, key, out, n)
	}
}
