package codeview

import (
	"image/color"
	"math"
)

// hsla returns the colour for hue, saturation, lightness and alpha, all
// in 0..1. Jump lines take their hue from the instruction's pc.
func hsla(h, s, l, a float32) color.NRGBA {
	if s == 0 {
		return color.NRGBA{R: sat8(l), G: sat8(l), B: sat8(l), A: sat8(a)}
	}
	h = float32(math.Mod(float64(h), 1))
	var v2 float32
	if l < 0.5 {
		v2 = l * (1 + s)
	} else {
		v2 = (l + s) - s*l
	}
	v1 := 2*l - v2
	return color.NRGBA{
		R: sat8(hue(v1, v2, h+1.0/3.0)),
		G: sat8(hue(v1, v2, h)),
		B: sat8(hue(v1, v2, h-1.0/3.0)),
		A: sat8(a),
	}
}

func hue(v1, v2, h float32) float32 {
	if h < 0 {
		h += 1
	}
	if h > 1 {
		h -= 1
	}
	switch {
	case 6*h < 1:
		return v1 + (v2-v1)*6*h
	case 2*h < 1:
		return v2
	case 3*h < 2:
		return v1 + (v2-v1)*(2.0/3.0-h)*6
	}
	return v1
}

// sat8 converts 0..1 to 0..255.
func sat8(v float32) uint8 {
	return uint8(min(max(v*255, 0), 255))
}
