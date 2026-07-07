// Command testprog is a small dependency-free fixture for exercising the
// disassembler and code view by hand or from tests. It cross-compiles to any
// Go target, e.g.:
//
//	GOOS=linux GOARCH=amd64 go build -o testprog_amd64.exe ./testdata/testprog
//	GOOS=linux GOARCH=arm64 go build -o testprog_arm64.exe ./testdata/testprog
//
// The functions are noinline and their results printed, so each survives as a
// distinct symbol with a varied instruction mix: integer and floating-point
// arithmetic, memory traffic, bit tricks, branches, and calls.
package main

import (
	"math"
	"math/bits"
)

//go:noinline
func sumInts(xs []int) int {
	total := 0
	for _, x := range xs {
		total += x
	}
	return total
}

//go:noinline
func dot(a, b []float64) float64 {
	total := 0.0
	for i := range a {
		total += a[i] * b[i]
	}
	return total
}

//go:noinline
func popcountAll(xs []uint64) int {
	total := 0
	for _, x := range xs {
		total += bits.OnesCount64(x)
	}
	return total
}

//go:noinline
func collatzSteps(n int) int {
	steps := 0
	for n != 1 {
		if n%2 == 0 {
			n /= 2
		} else {
			n = 3*n + 1
		}
		steps++
	}
	return steps
}

//go:noinline
func mat4Mul(a, b *[16]float64) [16]float64 {
	var out [16]float64
	for row := range 4 {
		for col := range 4 {
			for k := range 4 {
				out[row*4+col] += a[row*4+k] * b[k*4+col]
			}
		}
	}
	return out
}

//go:noinline
func normalize(v []float64) {
	length := 0.0
	for _, x := range v {
		length += x * x
	}
	length = math.Sqrt(length)
	for i := range v {
		v[i] /= length
	}
}

func main() {
	ints := []int{3, 1, 4, 1, 5, 9, 2, 6}
	floats := []float64{2.7, 1.8, 2.8, 1.8}
	words := []uint64{0xdeadbeef, 0xcafebabe, 0x0ddba11}

	var a, b [16]float64
	for i := range a {
		a[i] = float64(i)
		b[i] = float64(15 - i)
	}
	m := mat4Mul(&a, &b)
	normalize(floats)

	println(sumInts(ints), dot(floats, floats), popcountAll(words), collatzSteps(27), m[5])
}
