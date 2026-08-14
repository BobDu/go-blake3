package hash_avx512_test

import (
	"testing"

	"github.com/zeebo/assert"
	"github.com/zeebo/blake3/internal/alg/hash/hash_avx512"
	"github.com/zeebo/blake3/internal/alg/hash/hash_pure"
	"github.com/zeebo/blake3/internal/consts"
	"github.com/zeebo/pcg"
)

func TestHashF(t *testing.T) {
	if !consts.HasAVX512 {
		t.SkipNow()
	}

	var input [8192]byte
	var key [8]uint32

	for n := 0; n <= 8192; n++ {
		var c1, c2 [8]uint32
		var o1, o2 [64]uint32

		ctr, flags := pcg.Uint64(), pcg.Uint32()
		for i := range &key {
			key[i] = pcg.Uint32()
		}
		for i := 0; i < n; i++ {
			input[i] = byte(i+1) % 251
		}

		hash_avx512.HashF(&input, uint64(n), ctr, flags, &key, &o1, &c1)
		hash_pure.HashF(&input, uint64(n), ctr, flags, &key, &o2, &c2)

		for i := 0; (i+1)*1024 <= n; i++ {
			for j := 0; j < 8; j++ {
				assert.Equal(t, o1[i+8*j], o2[i+8*j])
			}
		}
		if n%1024 != 0 {
			assert.Equal(t, c1, c2)
		}
	}
}

func TestHashP(t *testing.T) {
	if !consts.HasAVX512 {
		t.SkipNow()
	}

	var key [8]uint32
	var left, right [64]uint32

	for i := 0; i < 64; i++ {
		left[i] = uint32(i+1) % 251
		right[i] = uint32(i+2) % 251
	}

	for n := 1; n <= 8; n++ {
		var o1, o2 [64]uint32

		for i := range &key {
			key[i] = pcg.Uint32()
		}

		hash_avx512.HashP(&left, &right, 0, &key, &o1, n)
		hash_pure.HashP(&left, &right, 0, &key, &o2, n)

		for i := 0; i < n; i++ {
			for j := 0; j < 8; j++ {
				assert.Equal(t, o1[i+8*j], o2[i+8*j])
			}
		}
	}
}

func TestHashF16(t *testing.T) {
	if !consts.HasAVX512 {
		t.SkipNow()
	}

	var input [16384]byte
	var key [8]uint32

	lo := (*[8192]byte)(input[:8192])
	hi := (*[8192]byte)(input[8192:])

	check := func(t *testing.T, n int) {
		var c1, c2, c3 [8]uint32
		var oLo, oHi, o2, o3 [64]uint32

		ctr, flags := pcg.Uint64(), pcg.Uint32()
		for i := range &key {
			key[i] = pcg.Uint32()
		}
		for i := 0; i < n; i++ {
			input[i] = byte(i+1) % 251
		}

		nLo, nHi := n, 0
		if n > 8192 {
			nLo, nHi = 8192, n-8192
		}

		hash_avx512.HashF16(&input, uint64(n), ctr, flags, &key, &oLo, &oHi, &c1)
		hash_pure.HashF(lo, uint64(nLo), ctr, flags, &key, &o2, &c2)
		hash_pure.HashF(hi, uint64(nHi), ctr+8, flags, &key, &o3, &c3)

		for i := 0; (i+1)*1024 <= nLo; i++ {
			for j := 0; j < 8; j++ {
				assert.Equal(t, oLo[i+8*j], o2[i+8*j])
			}
		}
		for i := 0; (i+1)*1024 <= nHi; i++ {
			for j := 0; j < 8; j++ {
				assert.Equal(t, oHi[i+8*j], o3[i+8*j])
			}
		}

		// The chain carries the partial state of whichever chunk the input
		// stopped inside of, so it comes from the group that chunk lands in.
		if n%1024 != 0 {
			if n > 8192 {
				assert.Equal(t, c1, c3)
			} else {
				assert.Equal(t, c1, c2)
			}
		}
	}

	for n := 0; n <= 16384; n++ {
		check(t, n)
	}
}
