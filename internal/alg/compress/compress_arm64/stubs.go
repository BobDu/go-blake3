//go:build arm64

package compress_arm64

//go:noescape
func Compress(chain *[8]uint32, block *[16]uint32, counter uint64, blen uint32, flags uint32, out *[16]uint32)

// CompressBaseline adds the second row before the message word. It is here so
// the two orders can be measured against each other in one binary.
//
//go:noescape
func CompressBaseline(chain *[8]uint32, block *[16]uint32, counter uint64, blen uint32, flags uint32, out *[16]uint32)
