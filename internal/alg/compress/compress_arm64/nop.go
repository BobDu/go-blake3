//go:build arm64

package compress_arm64

//go:noescape
func CallOverhead(chain *[8]uint32, block *[16]uint32, counter uint64, blen uint32, flags uint32, out *[16]uint32)
