//go:build arm64

package compress_arm64

//go:noescape
func Compress(chain *[8]uint32, block *[16]uint32, counter uint64, blen uint32, flags uint32, out *[16]uint32)

// CompressBaseline adds the second row before the message word. It is here so
// the two orders can be measured against each other in one binary.
//
//go:noescape
func CompressBaseline(chain *[8]uint32, block *[16]uint32, counter uint64, blen uint32, flags uint32, out *[16]uint32)

// CompressHoisted loads a half round's eight message words before the first of
// them is needed, so the load latency is covered. The other two forms load four
// at a time and the first add sits three instructions behind its load.
//
//go:noescape
func CompressHoisted(chain *[8]uint32, block *[16]uint32, counter uint64, blen uint32, flags uint32, out *[16]uint32)
