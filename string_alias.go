//go:build !gopherjs

package blake3

// GopherJS strings are not backed by linear memory, so the zero-copy
// string-to-array view in updateString is only safe elsewhere.
const optimizeStringAlias = true
