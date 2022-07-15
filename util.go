package main

// InRange checks whether v is in bounds for length.
func InRange(v int, length int) bool {
	return 0 <= v && v < length
}
