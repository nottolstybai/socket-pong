package main

import (
	"fmt"
	"math"
	"strings"

	"github.com/nottolstybai/socket-pong/internal/protocol"
	"github.com/nottolstybai/socket-pong/internal/sim"
)

const (
	minCols = 44
	minRows = 13
)

var (
	colorVoid    = hexColor(0x05050C)
	colorFloor   = hexColor(0x0C0B1B)
	colorFloorHi = hexColor(0x15132E)
	colorMark    = hexColor(0x2E2A5A)
	colorWall    = hexColor(0x4F4A8C)
	colorText    = hexColor(0xE9E6FF)
	colorMuted   = hexColor(0x7C78A8)
	colorDim     = hexColor(0x4A4670)
	colorGood    = hexColor(0x5BE38F)
	colorWarn    = hexColor(0xFFC56B)
	colorBad     = hexColor(0xFF5A5A)
)

// layout places the 2:1 court in the terminal. One row is kept for the HUD at
// the top and one at the bottom. Court pixels are aligned to whole pixels so
// that the walls stay crisp.
type layout struct {
	scale          float64 // pixels per world unit
	originX        float64
	originY        float64
	courtW, courtH float64
	firstCol       int
	lastCol        int
}

func computeLayout(cols, rows int) layout {
	availableH := (rows-2)*2 - 2 // the walls need a pixel each
	scale := math.Floor(math.Min(float64(cols)/sim.CourtWidth, float64(availableH)/sim.CourtHeight))
	scale = math.Max(scale, 1)
	courtW := scale * sim.CourtWidth
	courtH := scale * sim.CourtHeight
	originX := math.Floor((float64(cols) - courtW) / 2)
	originY := 2 + math.Floor((float64((rows-2)*2)-courtH)/2)
	firstCol := int(originX)
	return layout{
		scale: scale, originX: originX, originY: originY,
		courtW: courtW, courtH: courtH,
		firstCol: firstCol, lastCol: firstCol + int(courtW) - 1,
	}
}

func (l layout) x(world float64) float64 { return l.originX + world*l.scale }
func (l layout) y(world float64) float64 { return l.originY + world*l.scale }

// buildCourt paints the parts that never change. A static background means
// that only cells near moving objects differ between frames, which keeps
// terminal output small.
func (s *scene) buildCourt() {
	c, l := s.canvas, s.layout

	for i := range c.bg {
		c.bg[i] = colorVoid
	}

	centerX, centerY := l.x(sim.CourtWidth/2), l.y(sim.CourtHeight/2)
	maxDist := math.Hypot(l.courtW/2, l.courtH/2)
	for y := int(l.originY); y < int(l.originY+l.courtH); y++ {
		for x := int(l.originX); x < int(l.originX+l.courtW); x++ {
			fx, fy := float64(x)+0.5, float64(y)+0.5
			light := 1 - math.Hypot(fx-centerX, fy-centerY)/maxDist
			color := colorFloor
			if !s.limitedColor {
				color = colorFloor.mix(colorFloorHi, light*light)
				side := (fx - centerX) / (l.courtW / 2) // -1 at left, 1 at right
				if side < 0 {
					color = color.add(colorLeft.scale(-side * side * 0.028))
				} else {
					color = color.add(colorRight.scale(side * side * 0.028))
				}
			}
			c.bg[y*c.w+x] = color
		}
	}

	// Court markings are rendered into the mask first. That gives every marking
	// pixel a coverage value, used both for the static colour and for how much
	// light it catches from the ball.
	unit := l.scale
	line := math.Max(0.55, 0.0105*unit)

	const dash, gap = 0.05, 0.036
	for y := 0.012; y < sim.CourtHeight; y += dash + gap {
		x := l.x(sim.CourtWidth / 2)
		c.maskCapsule(x, l.y(y), x, l.y(math.Min(y+dash, sim.CourtHeight-0.012)), line/2, 1)
	}

	circle := 0.17 * unit
	steps := int(circle*6) + 16
	for i := range steps {
		a0 := float64(i) / float64(steps) * 2 * math.Pi
		a1 := float64(i+1) / float64(steps) * 2 * math.Pi
		c.maskCapsule(
			centerX+math.Cos(a0)*circle, centerY+math.Sin(a0)*circle,
			centerX+math.Cos(a1)*circle, centerY+math.Sin(a1)*circle,
			line*0.4, 0.8,
		)
	}

	const grid = 0.125
	for gy := grid; gy < sim.CourtHeight; gy += grid {
		for gx := grid; gx < sim.CourtWidth; gx += grid {
			if math.Abs(gx-sim.CourtWidth/2) < 0.01 {
				continue
			}
			c.maskCapsule(l.x(gx), l.y(gy), l.x(gx), l.y(gy), 0.34, 0.5)
		}
	}

	c.lit = c.lit[:0]
	for y := c.maskY0; y < c.maskY1; y++ {
		for x := c.maskX0; x < c.maskX1; x++ {
			weight := c.mask[y*c.w+x]
			if weight <= 0.03 {
				continue
			}
			c.mask[y*c.w+x] = 0
			c.bg[y*c.w+x] = c.bg[y*c.w+x].add(colorMark.scale(float64(weight) * 0.75))
			c.lit = append(c.lit, litPixel{
				index: y*c.w + x, x: float64(x) + 0.5, y: float64(y) + 0.5, weight: weight,
			})
		}
	}
	c.resetMaskBounds()

	for _, wall := range []struct{ top, bottom float64 }{
		{l.originY - 1, l.originY},
		{l.originY + l.courtH, l.originY + l.courtH + 1},
	} {
		for x := int(l.originX); x < int(l.originX+l.courtW); x++ {
			for y := int(wall.top); y < int(wall.bottom); y++ {
				if y >= 0 && y < c.h {
					c.bg[y*c.w+x] = colorWall
				}
			}
		}
	}

	for y := int(l.originY); y < int(l.originY+l.courtH); y += 2 {
		if int(l.originX) >= 0 {
			c.bg[y*c.w+int(l.originX)] = c.bg[y*c.w+int(l.originX)].add(colorLeft.scale(0.14))
		}
		right := int(l.originX + l.courtW - 1)
		if right < c.w {
			c.bg[y*c.w+right] = c.bg[y*c.w+right].add(colorRight.scale(0.14))
		}
	}
}

// scene turns the world into pixels.
type scene struct {
	canvas *canvas
	layout layout
	world  *world
	fx     *effects
	input  *paddleInput

	time float64
	rtt  float64
	err  error

	// limitedColor drops the floor gradient, which turns into visible blotches
	// on terminals without true colour.
	limitedColor bool
}

func (s *scene) resize(cols, rows int) {
	s.canvas.resize(cols, rows)
	if cols >= minCols && rows >= minRows {
		s.layout = computeLayout(cols, rows)
		s.buildCourt()
	}
}

func (s *scene) setLimitedColor(limited bool) {
	if s.limitedColor != limited {
		s.limitedColor = limited
		s.resize(s.canvas.cols, s.canvas.rows)
	}
}

func (s *scene) px(x float64) float64 { return s.layout.x(x) + s.fx.shakeX*s.layout.scale }
func (s *scene) py(y float64) float64 { return s.layout.y(y) + s.fx.shakeY*s.layout.scale }

func (s *scene) render() string {
	c := s.canvas
	c.begin()
	if c.cols < minCols || c.rows < minRows {
		s.drawTooSmall()
		return c.encode()
	}

	l := s.layout
	c.setLimit(int(l.originX), int(l.originY)-1, int(l.originX+l.courtW), int(l.originY+l.courtH)+1)

	state := s.world.state
	s.drawLighting()
	s.drawGoalEffects()
	s.drawScores()
	s.drawTrail()
	s.drawWallPulses()
	s.drawWaves()
	s.drawLights()
	s.drawPaddles()
	if s.world.ballVisible() {
		if state.Phase == protocol.PhaseServe {
			s.drawServe()
		}
		s.drawBall()
	}
	s.drawSparks()

	switch {
	case s.err != nil:
		s.dimCourt(0.4)
		s.drawBanner("OFFLINE", colorBad)
		c.setTextCentered(s.bannerBottomRow()+2, "THE SERVER CLOSED THE CONNECTION", colorMuted, false)
		c.setTextCentered(s.bannerBottomRow()+3, "Q  QUIT", colorText, true)
	case state.Phase == protocol.PhaseWaiting:
		s.drawTitle()
	case state.Phase == protocol.PhaseOver:
		s.drawGameOver()
	}

	s.drawHUD()
	return c.encode()
}

func (s *scene) drawTooSmall() {
	c := s.canvas
	for i := range c.px {
		c.px[i] = colorVoid
	}
	mid := max(c.rows/2-1, 0)
	c.setTextCentered(mid, "SOCKET PONG", colorText, true)
	c.setTextCentered(mid+1, fmt.Sprintf("NEEDS %d×%d", minCols, minRows), colorMuted, false)
}

// drawLighting lets the ball light up nearby court markings, which ties it to
// the floor and makes its position easy to read.
func (s *scene) drawLighting() {
	c, w := s.canvas, s.world
	if !w.ballVisible() {
		return
	}
	bx, by := w.shownBall()
	cx, cy := s.layout.x(bx), s.layout.y(by)
	radius := 0.2 * s.layout.scale
	invSq := 1 / (radius * radius)
	strength := 0.62 + 0.3*sim.Heat(w.ball)
	color := w.tint.mix(colorWhite, 0.25).scale(strength)
	for _, lit := range c.lit {
		dx, dy := lit.x-cx, lit.y-cy
		a := lit.weight / float32(1+(dx*dx+dy*dy)*invSq*(1+(dx*dx+dy*dy)*invSq))
		if a > 0.004 {
			p := &c.px[lit.index]
			p.r += color.r * a
			p.g += color.g * a
			p.b += color.b * a
		}
	}
}

func (s *scene) drawGoalEffects() {
	c, l, fx := s.canvas, s.layout, s.fx

	if fx.tintLevel > 0 {
		tint := fx.courtTint.scale(fx.tintLevel)
		x0, x1 := int(l.originX), int(l.originX+l.courtW)
		for y := int(l.originY); y < int(l.originY+l.courtH); y++ {
			row := c.px[y*c.w+x0 : y*c.w+x1]
			for i := range row {
				row[i].r += tint.r
				row[i].g += tint.g
				row[i].b += tint.b
			}
		}
	}

	if fx.goalLeft > 0 {
		s.drawGoalLine(fx.goalSide, fx.goalLeft/goalFlash, fx.goalColor, fx.goalY, 2.2)
	}

	// A goal line glows while the player defending it is one point from losing.
	state := s.world.state
	if state.Phase == protocol.PhaseOver || state.Phase == protocol.PhaseWaiting {
		return
	}
	beat := 0.5 + 0.5*math.Sin(s.time*5.5)
	if state.LeftScore == sim.WinningScore-1 {
		s.drawGoalLine(sim.Left, 0.1+0.1*beat, colorLeft, sim.CourtHeight/2, 0.3)
	}
	if state.RightScore == sim.WinningScore-1 {
		s.drawGoalLine(sim.Right, 0.1+0.1*beat, colorRight, sim.CourtHeight/2, 0.3)
	}
}

// drawGoalLine lights the goal on the far side from scorer. focus is the height
// light concentrates around, and sharpness how tightly it does.
func (s *scene) drawGoalLine(scorer sim.Side, level float64, color rgb, focus, sharpness float64) {
	c, l := s.canvas, s.layout
	depth := 0.32 * l.scale
	edge := l.originX + l.courtW
	direction := -1.0
	if scorer == sim.Right {
		edge = l.originX
		direction = 1
	}

	for y := int(l.originY); y < int(l.originY+l.courtH); y++ {
		worldY := (float64(y) + 0.5 - l.originY) / l.scale
		spread := math.Exp(-(worldY - focus) * (worldY - focus) * sharpness)
		for d := 0; d < int(depth); d++ {
			x := int(edge + direction*(float64(d)+0.5))
			falloff := 1 - float64(d)/depth
			c.add(x, y, color, float32(level*spread*falloff*falloff*falloff))
		}
	}
}

func (s *scene) drawScores() {
	c, l, state := s.canvas, s.layout, s.world.state
	if state.Phase == protocol.PhaseWaiting {
		return
	}
	scale := max(1, int(math.Round(l.scale*0.24/fontHeight)))
	y := l.y(0) + math.Round(l.scale*0.07)
	height := float64(fontHeight * scale)

	for _, score := range []struct {
		side  sim.Side
		value int
		x     float64
	}{
		{sim.Left, state.LeftScore, l.x(sim.CourtWidth/2 - 0.27)},
		{sim.Right, state.RightScore, l.x(sim.CourtWidth/2 + 0.27)},
	} {
		base := sideColor(score.side)
		pop := s.fx.scorePopLeft[score.side] / scorePop
		c.drawTextCentered(score.x, y, fmt.Sprint(score.value), scale, base.scale(0.2+pop*pop*1.3))
		if pop > 0 {
			c.glow(score.x, y+height/2, height*(1.1+0.5*(1-pop)), base.scale(0.42*pop*pop))
		}
	}
}

func (s *scene) drawTrail() {
	c, w := s.canvas, s.world
	points := w.trail
	if len(points) < 2 {
		return
	}
	radius := sim.BallRadius * s.layout.scale
	hot := sim.Heat(w.ball)
	for i := 1; i < len(points); i++ {
		a, b := points[i-1], points[i]
		age := math.Max(0, math.Min(1, (w.now-b.t)/trailSeconds))
		life := 1 - age
		c.maskCapsule(
			s.px(a.x), s.py(a.y), s.px(b.x), s.py(b.y),
			math.Max(0.28, radius*(0.14+0.72*life)),
			math.Pow(life, 1.7),
		)
	}
	tint := w.tint
	c.flushMask(func(v float32) rgb {
		strength := float64(v)
		color := tint.scale(strength * (0.72 + 0.28*hot))
		return color.add(colorWhite.scale(strength * strength * strength * (0.28 + 0.3*hot)))
	})
}

func (s *scene) drawWallPulses() {
	c, l := s.canvas, s.layout
	const travelSpeed, width = 2.6, 0.2
	for _, pulse := range s.fx.pulses {
		progress := pulse.age / pulse.span
		front := pulse.age * travelSpeed
		level := pulse.strength * (1 - progress) * (1 - progress)

		wallY := int(l.originY - 1)
		inward := 1
		if pulse.bottom {
			wallY = int(l.originY + l.courtH)
			inward = -1
		}
		for x := int(l.originX); x < int(l.originX+l.courtW); x++ {
			worldX := (float64(x) + 0.5 - l.originX) / l.scale
			distance := math.Abs(worldX - pulse.x)
			wave := (distance - front) / width
			at := math.Exp(-wave*wave) * level
			at += math.Exp(-distance*distance*40) * level * 0.8
			if at < 0.006 {
				continue
			}
			c.add(x, wallY, pulse.color, float32(at*1.4))
			offset := inward
			for depth := 1; depth <= 2; depth++ {
				c.add(x, wallY+offset, pulse.color, float32(at*0.32/float64(depth)))
				offset += inward
			}
		}
	}
}

func (s *scene) drawWaves() {
	c, l := s.canvas, s.layout
	for _, wave := range s.fx.waves {
		t := wave.age / wave.span
		eased := 1 - (1-t)*(1-t)*(1-t)
		radius := wave.radius * eased * l.scale
		thickness := math.Max(0.65, wave.thickness*l.scale*(1-0.65*t))
		brightness := wave.brightness * (1 - t) * (1 - t)
		c.ring(s.px(wave.x), s.py(wave.y), radius, thickness, 2*math.Pi, wave.color.scale(brightness))
	}
}

func (s *scene) drawLights() {
	c, l := s.canvas, s.layout
	for _, light := range s.fx.lights {
		t := light.age / light.span
		radius := light.radius * l.scale * (0.65 + 0.35*t)
		c.glow(s.px(light.x), s.py(light.y), radius, light.color.scale((1-t)*(1-t)))
	}
}

func (s *scene) drawPaddles() {
	w := s.world
	own := s.input.y
	opponent := w.remoteShown(w.now)
	if w.state.Phase == protocol.PhaseWaiting {
		s.drawPaddle(w.side, own, 1)
		pulse := 0.16 + 0.1*math.Sin(s.time*3)
		s.drawPaddle(otherSide(w.side), sim.CourtHeight/2, pulse)
		return
	}
	s.drawPaddle(w.side, own, 1)
	s.drawPaddle(otherSide(w.side), opponent, 1)
}

// drawPaddle draws a glowing capsule. A hit flattens it slightly, pushes it back
// and leaves a white-hot spot where the ball landed.
func (s *scene) drawPaddle(side sim.Side, y, presence float64) {
	c, l, fx := s.canvas, s.layout, &s.fx.paddles[side]
	color := sideColor(side).scale(presence)

	flash := fx.flash / paddleFlash * fx.strength
	centerX := s.px(sim.PaddleX(side) + fx.recoil)
	centerY := s.py(y)
	halfW := math.Max(0.8, sim.PaddleHalfWidth*l.scale) * (1 + 0.42*flash)
	halfH := sim.PaddleHeight / 2 * l.scale * (1 + 0.04*flash)
	corner := math.Min(halfW, 1.5)

	halo := math.Max(2.6, 0.075*l.scale) * (1 + 0.5*flash)
	x0, y0, x1, y1 := c.clip(centerX-halfW-halo, centerY-halfH-halo, centerX+halfW+halo, centerY+halfH+halo)
	hotRange := 0.095 * l.scale
	impactY := s.py(fx.impactY)

	for py := y0; py < y1; py++ {
		fy := float64(py) + 0.5 - centerY
		hot := 0.0
		if flash > 0 {
			gap := (float64(py) + 0.5 - impactY) / hotRange
			hot = math.Exp(-gap*gap) * flash
		}
		for px := x0; px < x1; px++ {
			fxp := float64(px) + 0.5 - centerX
			d := roundedBoxDistance(fxp, fy, halfW, halfH, corner)
			if d > halo {
				continue
			}
			if d > 0 {
				falloff := 1 - d/halo
				c.add(px, py, color, float32(falloff*falloff*(0.26+0.45*flash)))
			}
			if a := coverage(d); a > 0 {
				body := color.mix(colorWhite, 0.28*presence).add(colorWhite.scale(hot * 1.6))
				if d < -0.45 {
					body = body.add(colorWhite.scale(0.22 * presence))
				}
				c.add(px, py, body, a)
			}
		}
	}
}

func (s *scene) drawServe() {
	c, l, state := s.canvas, s.layout, s.world.state
	cx, cy := s.px(sim.CourtWidth/2), s.py(sim.CourtHeight/2)

	progress := 1 - state.PhaseLeft/sim.ServeSeconds
	progress = math.Max(0, math.Min(1, progress))
	receiver := sim.Right
	if state.ServeDir < 0 {
		receiver = sim.Left
	}
	color := sideColor(receiver)

	c.ring(cx, cy, 0.09*l.scale, math.Max(0.7, 0.012*l.scale), 2*math.Pi, colorDim.scale(0.5))
	c.ring(cx, cy, 0.09*l.scale, math.Max(0.8, 0.016*l.scale), progress*2*math.Pi, color)

	// Chevrons run towards whoever receives the serve.
	arm := math.Max(1.4, 0.022*l.scale)
	for i := range 3 {
		travel := math.Mod(s.time*1.5+float64(i)/3, 1)
		x := cx + state.ServeDir*(0.15+0.2*travel)*l.scale
		fade := math.Sin(travel * math.Pi)
		chevron := color.scale(fade * 0.8)
		tip := x + state.ServeDir*arm
		c.capsule(x, cy-arm, tip, cy, 0.42, chevron)
		c.capsule(x, cy+arm, tip, cy, 0.42, chevron)
	}

	if state.LeftScore == 0 && state.RightScore == 0 {
		s.drawYouMarker(progress)
	}
}

func (s *scene) drawYouMarker(progress float64) {
	c, l, w := s.canvas, s.layout, s.world
	fade := math.Min(1, (1-progress)*3)
	if fade <= 0 {
		return
	}
	color := sideColor(w.side).scale(fade * 0.9)
	y := s.layout.y(s.input.y) - float64(fontHeight)/2
	width := float64(fontTextWidth("YOU", 1))
	gap := 0.075 * l.scale
	x := l.x(sim.PaddleX(w.side)) + gap
	if w.side == sim.Right {
		x = l.x(sim.PaddleX(w.side)) - gap - width
	}
	c.drawText(x, y, "YOU", 1, color)
}

func (s *scene) drawBall() {
	c, l, w := s.canvas, s.layout, s.world
	x, y := w.shownBall()
	state := w.state
	cx, cy := s.px(x), s.py(y)

	spawn := 1.0
	if state.Phase == protocol.PhaseServe {
		appeared := math.Min(1, (sim.ServeSeconds-state.PhaseLeft)*5)
		spawn = 1 + 2.2*(appeared-1)*(appeared-1)*(appeared-1) + 1.2*(appeared-1)*(appeared-1)
		spawn = math.Max(0, spawn)
	}

	heat := sim.Heat(w.ball)
	radius := sim.BallRadius * l.scale * (1 + 0.42*w.pop) * spawn
	radius = math.Max(radius, 0.85*spawn)

	glowColor := w.tint.mix(colorBall, 0.2)
	c.glow(cx, cy, radius*(6.5+2.5*heat+5*w.pop), glowColor.scale(0.2+0.14*heat+0.3*w.pop))
	c.glow(cx, cy, radius*(3.2+2*w.pop), glowColor.mix(colorWhite, 0.2).scale(0.72+0.8*w.pop))

	// A little motion blur along the velocity, so the fastest shots still read
	// as one continuous object across frames.
	const blurSeconds = 0.0085
	core := colorWhite.mix(w.tint, 0.08*(1-heat)).scale(1.35 + 1.2*w.pop)
	tx := cx - w.ball.VX*blurSeconds*l.scale
	ty := cy - w.ball.VY*blurSeconds*l.scale
	c.capsule(tx, ty, cx, cy, radius, core)
}

func (s *scene) drawSparks() {
	c, l := s.canvas, s.layout
	thin := math.Max(0.32, math.Min(0.85, l.scale*0.0068))
	for _, spark := range s.fx.sparks {
		life := spark.life / spark.span
		color := spark.color.mix(colorWhite, math.Max(0, life-0.55)*1.5)
		// The dying part of a spark falls away to a dim ember rather than fading
		// out uniformly.
		color = color.scale(math.Pow(life, 1.2) * 1.9)
		x, y := s.px(spark.x), s.py(spark.y)
		tailX := s.px(spark.x - spark.vx*spark.streak)
		tailY := s.py(spark.y - spark.vy*spark.streak)
		c.capsule(tailX, tailY, x, y, thin*spark.size, color)
	}
}

func (s *scene) dimCourt(factor float64) {
	c, l := s.canvas, s.layout
	f := float32(factor)
	x0, x1 := int(l.originX), int(l.originX+l.courtW)
	for y := int(l.originY); y < int(l.originY+l.courtH); y++ {
		row := c.px[y*c.w+x0 : y*c.w+x1]
		for i := range row {
			row[i].r *= f
			row[i].g *= f
			row[i].b *= f
		}
	}
}

func (s *scene) bannerScale(text string) int {
	return max(1, min(
		int(s.layout.courtW*0.72)/max(fontTextWidth(text, 1), 1),
		int(s.layout.courtH*0.3)/fontHeight,
	))
}

func (s *scene) bannerBottomRow() int {
	return int(s.layout.y(0.5))/2 + 1
}

func (s *scene) drawBanner(text string, color rgb) {
	scale := s.bannerScale(text)
	cx := s.layout.x(sim.CourtWidth / 2)
	height := float64(fontHeight * scale)
	y := s.layout.y(0.5) - height + float64(scale)
	s.canvas.glow(cx, y+height/2, float64(fontTextWidth(text, scale))*0.72, color.scale(0.22))
	s.canvas.drawTextCentered(cx, y, text, scale, color.mix(colorWhite, 0.2))
}

func (s *scene) drawTitle() {
	c, l := s.canvas, s.layout
	scale := s.bannerScale("SOCKET PONG")
	cx := l.x(sim.CourtWidth / 2)
	height := float64(fontHeight * scale)
	y := l.y(0.5) - height + float64(scale)

	row := s.bannerBottomRow() + 1
	full := float64(fontTextWidth("SOCKET PONG", scale))
	if full <= l.courtW {
		c.glow(cx, y+height/2, full*0.68, colorFloorHi.scale(1.4))
		left := cx - full/2
		c.drawText(left, y, "SOCKET", scale, colorLeft.mix(colorWhite, 0.22))
		c.drawText(left+float64(fontAdvance*7*scale), y, "PONG", scale, colorRight.mix(colorWhite, 0.22))
	} else {
		c.setTextCentered(row-2, "SOCKET PONG", colorText, true)
	}

	dots := strings.Repeat("·", 1+int(s.time*2.5)%3)
	c.setTextCentered(row, "WAITING FOR OPPONENT "+dots+strings.Repeat(" ", 3-len([]rune(dots))), colorText, true)
	side := "LEFT"
	if s.world.side == sim.Right {
		side = "RIGHT"
	}
	c.setTextCentered(row+1, "YOU PLAY "+side, sideColor(s.world.side), false)
	if row+3 < s.hudBottomRow()-1 {
		c.setTextCentered(row+3, "NO ONE AROUND?  go run ./cmd/client -bot", colorMuted, false)
	}
}

func (s *scene) drawGameOver() {
	c, w := s.canvas, s.world
	state := w.state
	won := sideFromName(state.Winner) == w.side
	s.dimCourt(0.62)

	if won {
		s.drawBanner("VICTORY", sideColor(w.side))
	} else {
		s.drawBanner("DEFEAT", colorBad.mix(colorMuted, 0.35))
	}

	row := s.bannerBottomRow() + 1
	c.setTextCentered(row, fmt.Sprintf("%d  :  %d", state.LeftScore, state.RightScore), colorText, true)
	rematch := "R  REMATCH      Q  QUIT"
	color := colorText
	own, other := state.RematchLeft, state.RematchRight
	if w.side == sim.Right {
		own, other = other, own
	}
	switch {
	case own:
		rematch = "WAITING FOR THE OTHER PLAYER"
		color = colorMuted
	case other:
		rematch = "OPPONENT WANTS A REMATCH  ·  R  ACCEPT"
		color = colorWarn
	}
	c.setTextCentered(row+2, rematch, color, false)
}

func (s *scene) drawHUD() {
	c, l, w := s.canvas, s.layout, s.world
	state := w.state

	left, right := "LEFT", "RIGHT"
	if w.side == sim.Left {
		left = "LEFT · YOU"
	} else {
		right = "YOU · RIGHT"
	}
	pips := func(score int) string {
		score = max(0, min(score, sim.WinningScore))
		return strings.Repeat("●", score) + strings.Repeat("○", sim.WinningScore-score)
	}

	leftText := left + "  " + pips(state.LeftScore)
	rightText := pips(state.RightScore) + "  " + right
	firstCol, lastCol := l.firstCol+1, l.lastCol-1
	top := s.hudTopRow()
	c.setText(firstCol, top, leftText, colorLeft, true)
	c.setText(lastCol-len([]rune(rightText))+1, top, rightText, colorRight, true)

	status, color := "SOCKET PONG", colorDim
	switch {
	case s.err != nil:
		status, color = "DISCONNECTED", colorBad
	case state.Phase == protocol.PhaseWaiting:
		status, color = "LOBBY", colorMuted
	case state.Phase == protocol.PhaseOver:
		status, color = "MATCH OVER", colorWarn
	case state.LeftScore == sim.WinningScore-1 || state.RightScore == sim.WinningScore-1:
		status, color = "MATCH POINT", colorWarn.scale(0.75+0.25*math.Sin(s.time*5.5))
	case state.Hits >= 4:
		status, color = fmt.Sprintf("RALLY %d", state.Hits), colorText
	}
	if c.cols-len([]rune(leftText))-len([]rune(rightText)) > len(status)+4 {
		c.setTextCentered(top, status, color, true)
	}

	row := s.hudBottomRow()
	controls := "↑↓ WS MOVE · MOUSE AIM · Q QUIT"
	if !s.input.releaseEvents {
		controls = "↑↓ WS STEP · TAB DRIVE MODE · MOUSE · Q QUIT"
		if s.input.driveMode {
			controls = "↑↓ WS DRIVE · TAB STEP MODE · MOUSE · Q QUIT"
		}
	}
	if s.input.mouseActive {
		controls = "MOUSE · ↑↓ WS KEYS · Q QUIT"
	}
	latency, latencyColor := s.latencyLabel()
	if lastCol-firstCol+1 < len([]rune(controls))+len([]rune(latency))+2 {
		controls = "Q QUIT"
	}
	c.setText(firstCol, row, controls, colorMuted, false)
	c.setText(lastCol-len([]rune(latency))+1, row, latency, latencyColor, false)
}

// The HUD sits directly above and below the court walls, wherever the court
// ended up after letterboxing.
func (s *scene) hudTopRow() int {
	return max(int(s.layout.originY-1)/2-1, 0)
}

func (s *scene) hudBottomRow() int {
	return min(int(s.layout.originY+s.layout.courtH)/2+1, s.canvas.rows-1)
}

func (s *scene) latencyLabel() (string, rgb) {
	ms := int(math.Round(s.rtt * 1000))
	switch {
	case s.err != nil:
		return "● OFFLINE", colorBad
	case s.rtt == 0:
		return "● …", colorMuted
	case ms < 60:
		return fmt.Sprintf("● %d MS", ms), colorGood
	case ms < 130:
		return fmt.Sprintf("● %d MS", ms), colorWarn
	}
	return fmt.Sprintf("● %d MS", ms), colorBad
}
