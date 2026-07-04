package main

type Bounds struct{ Min, Max float32 }

func BoundsWidth(min, width int) Bounds {
	return Bounds{Min: float32(min), Max: float32(min + width)}
}

func (b Bounds) Width() float32 { return b.Max - b.Min }

func (b Bounds) Lerp(p float32) float32 {
	return b.Min + p*(b.Max-b.Min)
}

func (b Bounds) Contains(v float32) bool {
	return b.Min <= v && v <= b.Max
}

// InRange checks whether v is in bounds for length.
func InRange(v int, length int) bool {
	return 0 <= v && v < length
}
