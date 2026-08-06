package utils

import (
	"encoding/binary"
)

// ChainFromBytes reads the first eight little-endian words of a compression
// output as a chaining value.
func ChainFromBytes(out *[64]byte) [8]uint32 {
	return [8]uint32{
		binary.LittleEndian.Uint32(out[0:]),
		binary.LittleEndian.Uint32(out[4:]),
		binary.LittleEndian.Uint32(out[8:]),
		binary.LittleEndian.Uint32(out[12:]),
		binary.LittleEndian.Uint32(out[16:]),
		binary.LittleEndian.Uint32(out[20:]),
		binary.LittleEndian.Uint32(out[24:]),
		binary.LittleEndian.Uint32(out[28:]),
	}
}

// ChainToBytes writes the eight chaining-value words as little-endian bytes.
func ChainToBytes(chain *[8]uint32, dst *[32]byte) {
	binary.LittleEndian.PutUint32(dst[0:], chain[0])
	binary.LittleEndian.PutUint32(dst[4:], chain[1])
	binary.LittleEndian.PutUint32(dst[8:], chain[2])
	binary.LittleEndian.PutUint32(dst[12:], chain[3])
	binary.LittleEndian.PutUint32(dst[16:], chain[4])
	binary.LittleEndian.PutUint32(dst[20:], chain[5])
	binary.LittleEndian.PutUint32(dst[24:], chain[6])
	binary.LittleEndian.PutUint32(dst[28:], chain[7])
}

func KeyFromBytes(key []byte, out *[8]uint32) {
	key = key[:32]
	out[0] = binary.LittleEndian.Uint32(key[0:])
	out[1] = binary.LittleEndian.Uint32(key[4:])
	out[2] = binary.LittleEndian.Uint32(key[8:])
	out[3] = binary.LittleEndian.Uint32(key[12:])
	out[4] = binary.LittleEndian.Uint32(key[16:])
	out[5] = binary.LittleEndian.Uint32(key[20:])
	out[6] = binary.LittleEndian.Uint32(key[24:])
	out[7] = binary.LittleEndian.Uint32(key[28:])
}
