package ui

import (
	"fmt"
	"image/color"
	"math"

	"charm.land/lipgloss/v2"
)

// rgb is a truecolor triple we can interpolate before handing to lipgloss.
type rgb struct{ r, g, b float64 }

func (c rgb) color() color.Color {
	clamp := func(v float64) int {
		if v < 0 {
			v = 0
		}
		if v > 255 {
			v = 255
		}
		return int(v + 0.5)
	}
	return lipgloss.Color(fmt.Sprintf("#%02x%02x%02x", clamp(c.r), clamp(c.g), clamp(c.b)))
}

func (c rgb) lerp(o rgb, t float64) rgb {
	return rgb{
		r: c.r + (o.r-c.r)*t,
		g: c.g + (o.g-c.g)*t,
		b: c.b + (o.b-c.b)*t,
	}
}

// hsl converts HSL (h in degrees, s/l in [0,1]) to rgb.
func hsl(h, s, l float64) rgb {
	h = math.Mod(math.Mod(h, 360)+360, 360)
	c := (1 - math.Abs(2*l-1)) * s
	x := c * (1 - math.Abs(math.Mod(h/60, 2)-1))
	m := l - c/2
	var r, g, b float64
	switch {
	case h < 60:
		r, g, b = c, x, 0
	case h < 120:
		r, g, b = x, c, 0
	case h < 180:
		r, g, b = 0, c, x
	case h < 240:
		r, g, b = 0, x, c
	case h < 300:
		r, g, b = x, 0, c
	default:
		r, g, b = c, 0, x
	}
	return rgb{(r + m) * 255, (g + m) * 255, (b + m) * 255}
}

// tapeHue returns the decorative base hue for the tile at board index i. It is
// deliberately NOT tied to the track's theme — that would spoil the grouping.
// Slowly drifting `shift` (driven by the animation frame) makes the whole deck
// shimmer through the spectrum.
func tapeHue(i, shift int) float64 {
	return float64(i)*23.0 + float64(shift)
}

// tapeColors gives the two endpoints of a tile's reel gradient.
func tapeColors(i, shift int, bright float64) (rgb, rgb) {
	h := tapeHue(i, shift)
	a := hsl(h, 0.70, 0.55*bright)
	b := hsl(h+38, 0.85, 0.62*bright)
	return a, b
}

// gradientText colors each rune of s along the a→b gradient. Pure ANSI eye
// candy for the tape reels and headers.
func gradientText(s string, a, b rgb) string {
	r := []rune(s)
	if len(r) == 0 {
		return s
	}
	var out string
	for i, ch := range r {
		t := 0.0
		if len(r) > 1 {
			t = float64(i) / float64(len(r)-1)
		}
		col := a.lerp(b, t).color()
		out += lipgloss.NewStyle().Foreground(col).Render(string(ch))
	}
	return out
}

// reelBar renders the tape as a gradient waveform on the tile's bottom line —
// a gentle resting ripple when idle, a livelier bounce while a preview plays.
func reelBar(i, frame, width int, playing bool) string {
	bright := 0.8
	if playing {
		bright = 1.2
	}
	a, b := tapeColors(i, frame/3, bright)
	glyphs := []rune("▁▂▃▄▅▆▇█")
	out := make([]rune, width)
	for x := 0; x < width; x++ {
		var v float64
		if playing {
			// Lively VU bounce, scrolling left as the tape runs.
			v = (math.Sin(float64(frame)/1.6+float64(x)*0.8) + 1) / 2
		} else {
			// Calm analog idle so a resting tape still breathes a little.
			v = (math.Sin(float64(x)*0.6+float64(i)+float64(frame)/8) + 1) / 2 * 0.45
		}
		out[x] = glyphs[int(v*float64(len(glyphs)-1))]
	}
	return gradientText(string(out), a, b)
}

// mutedRGB is the artist-line gray, as an interpolatable triple.
var mutedRGB = rgb{138, 141, 153}

// cassetteBrands flavor the factory stickers that cover each tape.
var cassetteBrands = []string{"TDK", "BASF", "MAXELL", "SONY", "MEMOREX", "AIWA", "DENON", "FUJI"}

// stickerLabel is the blank factory label printed on tape i before you peel it.
func stickerLabel(i int) string {
	return "▦ " + cassetteBrands[i%len(cassetteBrands)] + " C-60 ▦"
}

// peelEdgeRune is the lifting-edge curl drawn at the seam between the peeled
// cover and the song revealed underneath.
var peelEdgeRune = []rune("⟫")[0]

func padRunes(s string, width int) []rune {
	r := []rune(s)
	for len(r) < width {
		r = append(r, ' ')
	}
	return r[:width]
}

// peelReveal renders a tape label mid-peel. As p goes 0→1 the cover lifts away
// from the left and the song (`under`) is revealed beneath it; a bright curl
// marks the seam. p=0 is fully covered, p=1 fully revealed. Revealed text gets
// the tape's gradient; the cover sits dim; the seam flares amber.
func peelReveal(under, cover string, width int, p float64, a, b rgb) string {
	u := padRunes(under, width)
	c := padRunes(cover, width)
	lifted := int(p*float64(width) + 0.5)

	coverCol := lipgloss.NewStyle().Foreground(colMuted)
	edge := lipgloss.NewStyle().Foreground(colAmber).Bold(true)

	var out string
	for x := 0; x < width; x++ {
		switch {
		case x < lifted:
			t := 0.0
			if width > 1 {
				t = float64(x) / float64(width-1)
			}
			out += lipgloss.NewStyle().Foreground(a.lerp(b, t).color()).Render(string(u[x]))
		case x == lifted && p > 0 && p < 1:
			out += edge.Render(string(peelEdgeRune))
		default:
			out += coverCol.Render(string(c[x]))
		}
	}
	return out
}

// marquee returns a `width`-wide view of s, scrolling it when it overflows.
func marquee(s string, width, pos int) string {
	if width <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= width {
		// Pad to a stable width so the box doesn't jitter.
		for len(r) < width {
			r = append(r, ' ')
		}
		return string(r)
	}
	full := append([]rune(s), []rune("   •   ")...)
	off := pos % len(full)
	win := make([]rune, width)
	for x := 0; x < width; x++ {
		win[x] = full[(off+x)%len(full)]
	}
	return string(win)
}
