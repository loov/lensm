package codeview

type bounds struct{ Min, Max float32 }

func boundsWidth(min, width int) bounds {
	return bounds{Min: float32(min), Max: float32(min + width)}
}

func (b bounds) Width() float32 { return b.Max - b.Min }

func (b bounds) Lerp(p float32) float32 {
	return b.Min + p*(b.Max-b.Min)
}

func (b bounds) Contains(v float32) bool {
	return b.Min <= v && v <= b.Max
}
