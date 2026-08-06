package blake3

import (
	"encoding/binary"
	"fmt"
	"io"

	"github.com/zeebo/blake3/internal/alg"
	"github.com/zeebo/blake3/internal/consts"
	"github.com/zeebo/blake3/internal/utils"
)

// Digest captures the state of a Hasher allowing reading and seeking through
// the output stream.
type Digest struct {
	counter uint64
	chain   [8]uint32
	block   [16]uint32
	blen    uint32
	flags   uint32
	buf     [16]uint32
	bufn    int
}

// Read reads data from the hasher into out. It always fills the entire buffer and
// never errors. The stream will wrap around when reading past 2^64 bytes.
func (d *Digest) Read(p []byte) (n int, err error) {
	n = len(p)

	if d.bufn > 0 {
		n := d.slowCopy(p)
		p = p[n:]
		d.bufn -= n
	}

	for len(p) >= 64 {
		d.fillBuf()

		utils.WordsToBytes(&d.buf, p)

		p = p[64:]
		d.bufn = 0
	}

	if len(p) == 0 {
		return n, nil
	}

	d.fillBuf()
	d.bufn -= d.slowCopy(p)

	return n, nil
}

// Seek sets the position to the provided location. Only SeekStart and
// SeekCurrent are allowed.
func (d *Digest) Seek(offset int64, whence int) (int64, error) {
	switch whence {
	case io.SeekStart:
	case io.SeekEnd:
		return 0, fmt.Errorf("seek from end not supported")
	case io.SeekCurrent:
		offset += int64(consts.BlockLen*d.counter) - int64(d.bufn)
	default:
		return 0, fmt.Errorf("invalid whence: %d", whence)
	}
	if offset < 0 {
		return 0, fmt.Errorf("seek before start")
	}
	d.setPosition(uint64(offset))
	return offset, nil
}

func (d *Digest) setPosition(pos uint64) {
	d.counter = pos / consts.BlockLen
	d.fillBuf()
	d.bufn -= int(pos % consts.BlockLen)
}

func (d *Digest) slowCopy(p []byte) (n int) {
	off := uint(consts.BlockLen-d.bufn) % consts.BlockLen
	end := off + uint(len(p))
	if end > consts.BlockLen {
		end = consts.BlockLen
	}
	// Convert only the words the read touches. Converting the whole buffer
	// made one-byte reads pay for sixty-four.
	var tmp [consts.BlockLen]byte
	for w := off / 4; w < (end+3)/4; w++ {
		binary.LittleEndian.PutUint32(tmp[4*w:4*w+4], d.buf[w])
	}
	return copy(p, tmp[off:end])
}

func (d *Digest) fillBuf() {
	alg.Compress(&d.chain, &d.block, d.counter, d.blen, d.flags, &d.buf)
	d.counter++
	d.bufn = consts.BlockLen
}
