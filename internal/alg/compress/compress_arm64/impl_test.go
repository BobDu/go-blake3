package compress_arm64_test

import (
	"testing"

	"github.com/zeebo/assert"
	"github.com/zeebo/blake3/internal/alg/compress/compress_arm64"
	"github.com/zeebo/blake3/internal/alg/compress/compress_pure"
	"github.com/zeebo/blake3/internal/consts"
	"github.com/zeebo/pcg"
)

func TestCompress(t *testing.T) {
	if !consts.HasNEON {
		t.SkipNow()
	}

	var chain [8]uint32
	var block [16]uint32

	for i := 0; i < 1e5; i++ {
		var want, got, base, hoist [16]uint32

		counter, blen, flags := pcg.Uint64(), pcg.Uint32(), pcg.Uint32()
		for i := range &chain {
			chain[i] = pcg.Uint32()
		}
		for i := range &block {
			block[i] = pcg.Uint32()
		}

		compress_pure.Compress(&chain, &block, counter, blen, flags, &want)
		compress_arm64.Compress(&chain, &block, counter, blen, flags, &got)
		compress_arm64.CompressBaseline(&chain, &block, counter, blen, flags, &base)
		compress_arm64.CompressHoisted(&chain, &block, counter, blen, flags, &hoist)

		assert.Equal(t, got, want)
		assert.Equal(t, base, want)
		assert.Equal(t, hoist, want)
	}
}
