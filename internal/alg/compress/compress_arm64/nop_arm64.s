#include "textflag.h"

// func CallOverhead(chain *[8]uint32, block *[16]uint32, counter uint64, blen uint32, flags uint32, out *[16]uint32)
//
// Does nothing. Calling it measures what reaching an assembly function costs:
// Go wraps every one of them so the caller's register arguments get spilled to
// a stack frame and a real call is made, none of which a Go function with the
// same signature pays.
TEXT ·CallOverhead(SB), NOSPLIT|NOFRAME, $0-40
	RET
