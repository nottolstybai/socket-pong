package main

import (
	"math"
	"strconv"
)

// A terminal cell can show two independently coloured pixels with the upper
// half block: foreground on top, background below. That gives a roughly square
// full-colour framebuffer. Every shape below is anti-aliased with a signed
// distance, so objects move in fractions of a pixel instead of jumping between
// cells, which is what makes motion look smooth at this resolution.

type rgb struct {
	r, g, b float32
}

func hexColor(value uint32) rgb {
	return rgb{
		r: float32(value>>16&0xff) / 255,
		g: float32(value>>8&0xff) / 255,
		b: float32(value&0xff) / 255,
	}
}

func (c rgb) scale(k float64) rgb {
	f := float32(k)
	return rgb{c.r * f, c.g * f, c.b * f}
}

func (c rgb) add(o rgb) rgb {
	return rgb{c.r + o.r, c.g + o.g, c.b + o.b}
}

func (c rgb) mix(o rgb, t float64) rgb {
	f := float32(t)
	return rgb{c.r + (o.r-c.r)*f, c.g + (o.g-c.g)*f, c.b + (o.b-c.b)*f}
}

// litPixel is a court marking that catches light from the ball.
type litPixel struct {
	index  int
	x, y   float64
	weight float32
}

type glyph struct {
	r    rune
	fg   rgb
	bold bool
}

type canvas struct {
	cols, rows int
	w, h       int

	bg   []rgb // static court, rebuilt on resize
	px   []rgb // this frame, values above 1 are allowed and read as overexposure
	text []glyph
	lit  []litPixel

	// Drawing is confined to this rectangle, which keeps effects out of the HUD.
	limitX0, limitY0 int
	limitX1, limitY1 int

	shown []uint8 // what each pixel last displayed, see settle

	mask                   []float32 // single-channel layer for the ball trail
	maskX0, maskY0         int
	maskX1, maskY1         int
	out                    []byte
	lastFG, lastBG         [3]uint8
	hasFG, hasBG, lastBold bool
}

func newCanvas() *canvas {
	return &canvas{}
}

func (c *canvas) resize(cols, rows int) {
	c.cols, c.rows = cols, rows
	c.w, c.h = cols, rows*2
	n := c.w * c.h
	c.bg = make([]rgb, n)
	c.px = make([]rgb, n)
	c.mask = make([]float32, n)
	c.shown = make([]uint8, n*3)
	c.text = make([]glyph, cols*rows)
	c.lit = c.lit[:0]
	c.setLimit(0, 0, c.w, c.h)
	c.resetMaskBounds()
}

func (c *canvas) setLimit(x0, y0, x1, y1 int) {
	c.limitX0, c.limitY0 = max(x0, 0), max(y0, 0)
	c.limitX1, c.limitY1 = min(x1, c.w), min(y1, c.h)
}

func (c *canvas) begin() {
	copy(c.px, c.bg)
	clear(c.text)
}

func (c *canvas) add(x, y int, color rgb, alpha float32) {
	if x < 0 || y < 0 || x >= c.w || y >= c.h {
		return
	}
	p := &c.px[y*c.w+x]
	p.r += color.r * alpha
	p.g += color.g * alpha
	p.b += color.b * alpha
}

func (c *canvas) clip(x0, y0, x1, y1 float64) (int, int, int, int) {
	return max(int(math.Floor(x0)), c.limitX0), max(int(math.Floor(y0)), c.limitY0),
		min(int(math.Ceil(x1)), c.limitX1), min(int(math.Ceil(y1)), c.limitY1)
}

func coverage(distance float64) float32 {
	return float32(math.Max(0, math.Min(1, 0.5-distance)))
}

// disc draws an anti-aliased filled circle.
func (c *canvas) disc(cx, cy, radius float64, color rgb) {
	x0, y0, x1, y1 := c.clip(cx-radius-1, cy-radius-1, cx+radius+1, cy+radius+1)
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			d := math.Hypot(float64(x)+0.5-cx, float64(y)+0.5-cy) - radius
			if a := coverage(d); a > 0 {
				c.add(x, y, color, a)
			}
		}
	}
}

// glow draws a soft light with a smooth falloff that reaches zero at radius.
func (c *canvas) glow(cx, cy, radius float64, color rgb) {
	if radius <= 0 {
		return
	}
	x0, y0, x1, y1 := c.clip(cx-radius, cy-radius, cx+radius, cy+radius)
	inv := 1 / (radius * radius)
	for y := y0; y < y1; y++ {
		dy := float64(y) + 0.5 - cy
		for x := x0; x < x1; x++ {
			dx := float64(x) + 0.5 - cx
			t := 1 - (dx*dx+dy*dy)*inv
			if t > 0 {
				c.add(x, y, color, float32(t*t))
			}
		}
	}
}

func segmentDistance(px, py, ax, ay, bx, by float64) float64 {
	abx, aby := bx-ax, by-ay
	apx, apy := px-ax, py-ay
	lengthSq := abx*abx + aby*aby
	t := 0.0
	if lengthSq > 1e-12 {
		t = math.Max(0, math.Min(1, (apx*abx+apy*aby)/lengthSq))
	}
	return math.Hypot(apx-abx*t, apy-aby*t)
}

// capsule draws a line with round caps, used for sparks and motion blur.
func (c *canvas) capsule(ax, ay, bx, by, radius float64, color rgb) {
	x0, y0, x1, y1 := c.clip(
		math.Min(ax, bx)-radius-1, math.Min(ay, by)-radius-1,
		math.Max(ax, bx)+radius+1, math.Max(ay, by)+radius+1,
	)
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			d := segmentDistance(float64(x)+0.5, float64(y)+0.5, ax, ay, bx, by) - radius
			if a := coverage(d); a > 0 {
				c.add(x, y, color, a)
			}
		}
	}
}

// ring draws a circle outline. If sweep is below 2π only an arc is drawn,
// starting at the top and running clockwise.
func (c *canvas) ring(cx, cy, radius, thickness, sweep float64, color rgb) {
	outer := radius + thickness/2 + 1
	x0, y0, x1, y1 := c.clip(cx-outer, cy-outer, cx+outer, cy+outer)
	full := sweep >= 2*math.Pi
	for y := y0; y < y1; y++ {
		dy := float64(y) + 0.5 - cy
		for x := x0; x < x1; x++ {
			dx := float64(x) + 0.5 - cx
			d := math.Abs(math.Hypot(dx, dy)-radius) - thickness/2
			a := coverage(d)
			if a <= 0 {
				continue
			}
			if !full {
				angle := math.Atan2(dx, -dy)
				if angle < 0 {
					angle += 2 * math.Pi
				}
				if angle > sweep {
					continue
				}
			}
			c.add(x, y, color, a)
		}
	}
}

// roundedBoxDistance returns the signed distance to a rounded rectangle.
func roundedBoxDistance(dx, dy, halfW, halfH, radius float64) float64 {
	qx := math.Abs(dx) - halfW + radius
	qy := math.Abs(dy) - halfH + radius
	outside := math.Hypot(math.Max(qx, 0), math.Max(qy, 0))
	return outside + math.Min(math.Max(qx, qy), 0) - radius
}

// rect adds a box with exact per-pixel area coverage, for crisp lines.
func (c *canvas) rect(x0f, y0f, x1f, y1f float64, color rgb) {
	x0, y0, x1, y1 := c.clip(x0f, y0f, x1f, y1f)
	for y := y0; y < y1; y++ {
		cy := overlap(float64(y), y0f, y1f)
		for x := x0; x < x1; x++ {
			c.add(x, y, color, cy*overlap(float64(x), x0f, x1f))
		}
	}
}

func overlap(pixel, low, high float64) float32 {
	return float32(math.Max(0, math.Min(pixel+1, high)-math.Max(pixel, low)))
}

// Trail mask: overlapping segments combine with max, not addition, so the joints
// of the ball trail do not show up as bright beads.

func (c *canvas) resetMaskBounds() {
	c.maskX0, c.maskY0 = c.w, c.h
	c.maskX1, c.maskY1 = 0, 0
}

func (c *canvas) maskCapsule(ax, ay, bx, by, radius, intensity float64) {
	x0, y0, x1, y1 := c.clip(
		math.Min(ax, bx)-radius-1, math.Min(ay, by)-radius-1,
		math.Max(ax, bx)+radius+1, math.Max(ay, by)+radius+1,
	)
	if x0 >= x1 || y0 >= y1 {
		return
	}
	c.maskX0, c.maskY0 = min(c.maskX0, x0), min(c.maskY0, y0)
	c.maskX1, c.maskY1 = max(c.maskX1, x1), max(c.maskY1, y1)
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			d := segmentDistance(float64(x)+0.5, float64(y)+0.5, ax, ay, bx, by) - radius
			if a := coverage(d) * float32(intensity); a > c.mask[y*c.w+x] {
				c.mask[y*c.w+x] = a
			}
		}
	}
}

// flushMask resolves the mask into the frame through a colour ramp and clears it.
func (c *canvas) flushMask(ramp func(float32) rgb) {
	for y := c.maskY0; y < c.maskY1; y++ {
		row := y * c.w
		for x := c.maskX0; x < c.maskX1; x++ {
			if v := c.mask[row+x]; v > 0 {
				color := ramp(v)
				p := &c.px[row+x]
				p.r += color.r
				p.g += color.g
				p.b += color.b
				c.mask[row+x] = 0
			}
		}
	}
	c.resetMaskBounds()
}

func (c *canvas) setText(col, row int, text string, fg rgb, bold bool) {
	for _, r := range text {
		if col >= 0 && row >= 0 && col < c.cols && row < c.rows {
			c.text[row*c.cols+col] = glyph{r: r, fg: fg, bold: bold}
		}
		col++
	}
}

func (c *canvas) setTextCentered(row int, text string, fg rgb, bold bool) {
	c.setText((c.cols-len([]rune(text)))/2, row, text, fg, bold)
}

// toneMap converts a light value to a display byte. Bright light rolls off
// instead of clipping hard.
func toneMap(v float32) uint8 {
	switch {
	case v <= 0:
		return 0
	case v < 0.75:
		return uint8(v*255 + 0.5)
	}
	rolled := 0.75 + 0.25*(1-float32(math.Exp(-float64(v-0.75)*2.5)))
	return uint8(rolled*255 + 0.5)
}

// display resolves a pixel. Light far beyond a channel's range spills into the
// others, so overexposed colours burn towards white like a real light source.
func display(p rgb) [3]uint8 {
	peak := max(p.r, p.g, p.b)
	if peak > 1 {
		spill := (peak - 1) * 0.55
		p.r += spill
		p.g += spill
		p.b += spill
	}
	return [3]uint8{toneMap(p.r), toneMap(p.g), toneMap(p.b)}
}

// settleTolerance is the per-channel change, out of 255, not worth redrawing.
const settleTolerance = 3

// settle returns the colour to display for a pixel. A soft glow sliding across
// the court changes hundreds of cells by a level or two each frame, which nobody
// can see but which costs real terminal output. Such changes are skipped, unless
// the pixel has returned to the static court: that always lands exactly, so
// nothing leaves a ghost behind.
func (c *canvas) settle(index int) [3]uint8 {
	next := display(c.px[index])
	shown := c.shown[index*3 : index*3+3 : index*3+3]
	if c.px[index] != c.bg[index] &&
		near(next[0], shown[0]) && near(next[1], shown[1]) && near(next[2], shown[2]) {
		return [3]uint8{shown[0], shown[1], shown[2]}
	}
	copy(shown, next[:])
	return next
}

func near(a, b uint8) bool {
	return int(a)-int(b) <= settleTolerance && int(b)-int(a) <= settleTolerance
}

// byteDigits holds decimal strings for fast escape-code assembly.
var byteDigits = func() [256]string {
	var table [256]string
	for i := range table {
		table[i] = strconv.Itoa(i)
	}
	return table
}()

func (c *canvas) setFG(color [3]uint8) {
	if c.hasFG && c.lastFG == color {
		return
	}
	c.out = append(c.out, "\x1b[38;2;"...)
	c.appendColor(color)
	c.lastFG, c.hasFG = color, true
}

func (c *canvas) setBG(color [3]uint8) {
	if c.hasBG && c.lastBG == color {
		return
	}
	c.out = append(c.out, "\x1b[48;2;"...)
	c.appendColor(color)
	c.lastBG, c.hasBG = color, true
}

func (c *canvas) setBold(bold bool) {
	if bold == c.lastBold {
		return
	}
	if bold {
		c.out = append(c.out, "\x1b[1m"...)
	} else {
		c.out = append(c.out, "\x1b[22m"...)
	}
	c.lastBold = bold
}

func (c *canvas) appendColor(color [3]uint8) {
	c.out = append(c.out, byteDigits[color[0]]...)
	c.out = append(c.out, ';')
	c.out = append(c.out, byteDigits[color[1]]...)
	c.out = append(c.out, ';')
	c.out = append(c.out, byteDigits[color[2]]...)
	c.out = append(c.out, 'm')
}

// encode turns the frame into a styled string for Bubble Tea.
func (c *canvas) encode() string {
	c.out = c.out[:0]
	c.hasFG, c.hasBG, c.lastBold = false, false, false

	for row := range c.rows {
		for col := range c.cols {
			upper := c.settle(row*2*c.w + col)
			lower := c.settle((row*2+1)*c.w + col)
			g := &c.text[row*c.cols+col]
			switch {
			case g.r != 0:
				c.setBG([3]uint8{
					uint8((int(upper[0]) + int(lower[0])) / 2),
					uint8((int(upper[1]) + int(lower[1])) / 2),
					uint8((int(upper[2]) + int(lower[2])) / 2),
				})
				c.setFG(display(g.fg))
				c.setBold(g.bold)
				c.out = append(c.out, string(g.r)...)
			case upper == lower:
				c.setBold(false)
				c.setBG(lower)
				c.out = append(c.out, ' ')
			default:
				c.setBold(false)
				c.setFG(upper)
				c.setBG(lower)
				c.out = append(c.out, "▀"...)
			}
		}
		if row < c.rows-1 {
			c.out = append(c.out, '\n')
		}
	}
	c.out = append(c.out, "\x1b[0m"...)
	return string(c.out)
}
