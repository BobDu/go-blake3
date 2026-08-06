package utils

import (
	"testing"

	"github.com/zeebo/assert"
)

func TestBytesToWords(t *testing.T) {
	var bytes [64]uint8
	for i := range bytes {
		bytes[i] = byte(i)
	}

	var words [16]uint32
	BytesToWords(&bytes, &words)

	for i, w := range words {
		b := 4 * uint32(i)
		assert.Equal(t, b|(b+1)<<8|(b+2)<<16|(b+3)<<24, w)
	}
}

func TestWordsToBytes(t *testing.T) {
	var words [16]uint32
	for i := range words {
		b := 4 * uint32(i)
		words[i] = b | (b+1)<<8 | (b+2)<<16 | (b+3)<<24
	}

	var bytes [64]uint8
	WordsToBytes(&words, bytes[:])

	for i, v := range bytes {
		assert.Equal(t, byte(i), v)
	}
}
