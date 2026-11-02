package main

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func testCanvas(cols, rows int) *canvas {
	c := newCanvas()
	c.resize(cols, rows)
	for i := range c.bg {
		c.bg[i] = hexColor(0x101020)
	}
	c.begin()
	return c
}

func TestEncodeFillsEveryCell(t *testing.T) {
	c := testCanvas(20, 6)
	c.disc(10, 6, 3, rgb{1, 0.5, 0.2})
	c.setText(2, 1, "HI · ●", rgb{1, 1, 1}, true)

	lines := strings.Split(c.encode(), "\n")
	if len(lines) != 6 {
		t.Fatalf("got %d rows, want 6", len(lines))
	}
	for i, line := range lines {
		if got := ansi.StringWidth(line); got != 20 {
			t.Errorf("row %d is %d cells wide, want 20", i, got)
		}
	}
}

func TestBoldDoesNotLeakPastText(t *testing.T) {
	c := testCanvas(6, 1)
	c.setText(0, 0, "A", rgb{1, 1, 1}, true)
	out := c.encode()

	bold, reset := strings.Index(out, "\x1b[1m"), strings.Index(out, "\x1b[22m")
	if bold < 0 || reset < bold || reset > strings.Index(out, "A")+2 {
		t.Errorf("bold not closed right after the text: %q", out)
	}
}

func TestSettleSkipsInvisibleChangesButLeavesNoGhost(t *testing.T) {
	c := testCanvas(1, 1)
	c.encode()
	resting := c.settle(0)

	// A strong change goes through.
	c.begin()
	c.px[0] = rgb{0.5, 0.5, 0.5}
	bright := c.settle(0)
	if bright == resting {
		t.Fatal("a large change was ignored")
	}

	// A change of one level does not.
	c.px[0] = rgb{0.504, 0.504, 0.504}
	if got := c.settle(0); got != bright {
		t.Errorf("tiny change redrawn: %v -> %v", bright, got)
	}

	// Close to resting, but still lit: holds its colour.
	c.px[0] = c.bg[0].add(rgb{0.004, 0.004, 0.004})
	c.settle(0)
	// Back to the static court: lands exactly.
	c.begin()
	if got := c.settle(0); got != resting {
		t.Errorf("ghost left behind: %v, want %v", got, resting)
	}
}

func TestDrawingStaysInsideTheLimit(t *testing.T) {
	c := testCanvas(10, 5)
	c.setLimit(2, 2, 8, 8)
	c.glow(5, 5, 40, rgb{1, 1, 1})
	c.disc(0, 0, 3, rgb{1, 1, 1})

	for y := range c.h {
		for x := range c.w {
			inside := x >= 2 && x < 8 && y >= 2 && y < 8
			if !inside && c.px[y*c.w+x] != c.bg[y*c.w+x] {
				t.Fatalf("pixel (%d, %d) was painted outside the limit", x, y)
			}
		}
	}
}

func TestMoreOfASoftCircleIsLitAsItCovers(t *testing.T) {
	// Sub-pixel motion only reads as smooth if coverage changes gradually.
	c := testCanvas(10, 5)
	base := c.px[5*c.w+5]
	c.disc(5.5, 5.5, 0.5, rgb{1, 1, 1})
	full := c.px[5*c.w+5].r - base.r

	c.begin()
	c.disc(5.5+0.5, 5.5, 0.5, rgb{1, 1, 1})
	half := c.px[5*c.w+5].r - base.r

	if !(half > 0.1 && half < full) {
		t.Errorf("coverage: full %v, half-offset %v", full, half)
	}
}
