package main

//go:noinline
func sumInts(xs []int) int {
	total := 0
	for _, x := range xs {
		total += x
	}
	return total
}

func main() {
	println(sumInts([]int{1, 2, 3}))
}
