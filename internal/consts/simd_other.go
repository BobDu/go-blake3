//go:build !(goexperiment.simd && amd64 && !purego)

package consts

const HasSIMD = false
