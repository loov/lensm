package f32color

import (
	"image/color"
	"math"
)

var (
	White  = color.NRGBA{0xFF, 0xFF, 0xFF, 0xFF}
	Black  = color.NRGBA{0x00, 0x00, 0x00, 0xFF}
	Red    = color.NRGBA{0xFF, 0x00, 0x00, 0xFF}
	Green  = color.NRGBA{0x00, 0xFF, 0x00, 0xFF}
	Blue   = color.NRGBA{0x00, 0x00, 0xFF, 0xFF}
	Yellow = color.NRGBA{0xFF, 0xFF, 0x00, 0xFF}

	Transparent = color.NRGBA{0xFF, 0xFF, 0xFF, 0x00}
)

func NRGBAHex(hex uint32) color.NRGBA {
	return color.NRGBA{
		R: uint8(hex >> 24),
		G: uint8(hex >> 16),
		B: uint8(hex >> 8),
		A: uint8(hex >> 0),
	}
}

// RGB returns color based on RGB in range 0..1
func RGB(r, g, b float32) color.NRGBA {
	return color.NRGBA{R: sat8(r), G: sat8(g), B: sat8(b), A: 0xFF}
}

// RGBA returns color based on RGBA in range 0..1
func RGBA(r, g, b, a float32) color.NRGBA {
	return color.NRGBA{R: sat8(r), G: sat8(g), B: sat8(b), A: sat8(a)}
}

// HSLA returns color based on HSLA in range 0..1
func HSLA(h, s, l, a float32) color.NRGBA { return RGBA(hsla(h, s, l, a)) }

// HSL returns color based on HSL in range 0..1
func HSL(h, s, l float32) color.NRGBA { return HSLA(h, s, l, 1) }

// RGBAFloat returns RGBA scaled to 0..1
func RGBAFloat(c color.NRGBA) (r, g, b, a float32) {
	return float32(c.R) / 0xFF, float32(c.G) / 0xFF, float32(c.B) / 0xFF, float32(c.A) / 0xFF
}

// Lerp linearly interpolates each RGBA component separately
func RGBALerp(a, b color.NRGBA, p float32) color.NRGBA {
	ar, ag, ab, aa := RGBAFloat(a)
	br, bg, bb, ba := RGBAFloat(b)
	return RGBA(
		lerpClamp(ar, br, p),
		lerpClamp(ag, bg, p),
		lerpClamp(ab, bb, p),
		lerpClamp(aa, ba, p),
	)
}

func hue(v1, v2, h float32) float32 {
	if h < 0 {
		h += 1
	}
	if h > 1 {
		h -= 1
	}
	if 6*h < 1 {
		return v1 + (v2-v1)*6*h
	} else if 2*h < 1 {
		return v2
	} else if 3*h < 2 {
		return v1 + (v2-v1)*(2.0/3.0-h)*6
	}

	return v1
}

func hsla(h, s, l, a float32) (r, g, b, ra float32) {
	if s == 0 {
		return l, l, l, a
	}

	h = mod32(h, 1)

	var v2 float32
	if l < 0.5 {
		v2 = l * (1 + s)
	} else {
		v2 = (l + s) - s*l
	}

	v1 := 2*l - v2
	r = hue(v1, v2, h+1.0/3.0)
	g = hue(v1, v2, h)
	b = hue(v1, v2, h-1.0/3.0)
	ra = a

	return
}

// sat8 converts 0..1 float to 0..255 uint8
func sat8(v float32) uint8 {
	v *= 255.0
	if v >= 255 {
		return 255
	} else if v <= 0 {
		return 0
	}
	return uint8(v)
}

func lerpClamp(p, min, max float32) float32 {
	if p < 0 {
		return min
	} else if p > 1 {
		return max
	}
	return min + (max-min)*p
}

func mod32(x, y float32) float32 { return float32(math.Mod(float64(x), float64(y))) }
