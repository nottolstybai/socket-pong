package main

import (
	"math"
	"math/rand/v2"

	"github.com/nottolstybai/socket-pong/internal/sim"
)

// Visual effects live in world units and fade on their own. Nothing here
// affects the game.

const (
	maxParticles = 420
	paddleFlash  = 0.22
	goalFlash    = 0.75
	scorePop     = 0.7
)

type spark struct {
	x, y, vx, vy float64
	life, span   float64
	size         float64
	drag         float64
	gravity      float64
	color        rgb
	streak       float64
}

type shockwave struct {
	x, y       float64
	age, span  float64
	radius     float64
	thickness  float64
	color      rgb
	brightness float64
}

// wallPulse is light running along a wall away from the point of impact.
type wallPulse struct {
	x         float64
	bottom    bool
	age, span float64
	strength  float64
	color     rgb
}

type burstLight struct {
	x, y      float64
	age, span float64
	radius    float64
	color     rgb
}

type paddleFX struct {
	flash    float64 // remaining seconds of the hit flash
	impactY  float64
	strength float64
	recoil   float64 // current sideways offset, world units
	recoilV  float64
}

type effects struct {
	sparks []spark
	waves  []shockwave
	pulses []wallPulse
	lights []burstLight

	paddles [2]paddleFX

	shake     float64
	shakeX    float64
	shakeY    float64
	courtTint rgb
	tintLevel float64

	goalSide  sim.Side
	goalY     float64
	goalLeft  float64
	goalColor rgb

	scorePopLeft [2]float64
	fireworks    float64
	fireworkWait float64
	fireColor    rgb
}

func randRange(low, high float64) float64 {
	return low + rand.Float64()*(high-low)
}

func (fx *effects) addSpark(s spark) {
	if len(fx.sparks) >= maxParticles {
		copy(fx.sparks, fx.sparks[1:])
		fx.sparks = fx.sparks[:len(fx.sparks)-1]
	}
	s.span = s.life
	fx.sparks = append(fx.sparks, s)
}

func (fx *effects) addWave(x, y, span, radius, thickness float64, color rgb, brightness float64) {
	fx.waves = append(fx.waves, shockwave{
		x: x, y: y, span: span, radius: radius,
		thickness: thickness, color: color, brightness: brightness,
	})
}

func (fx *effects) addLight(x, y, span, radius float64, color rgb) {
	fx.lights = append(fx.lights, burstLight{x: x, y: y, span: span, radius: radius, color: color})
}

func (fx *effects) addShake(amount float64) {
	fx.shake = math.Max(fx.shake, amount)
}

// paddleHit fires sparks in a cone around the surface normal. heat (0..1) is
// how fast the ball was going, so the effect grows through a rally.
func (fx *effects) paddleHit(side sim.Side, x, y, ballVY, heat float64, color rgb) {
	normal := 1.0
	if side == sim.Right {
		normal = -1
	}

	count := 14 + int(26*heat)
	base := math.Atan2(ballVY*0.4, normal)
	for range count {
		angle := base + randRange(-1.15, 1.15)
		speed := randRange(0.35, 1.3+0.9*heat)
		fx.addSpark(spark{
			x: x, y: y,
			vx: math.Cos(angle) * speed, vy: math.Sin(angle) * speed,
			life: randRange(0.16, 0.42+0.15*heat),
			size: randRange(0.6, 1.05),
			drag: randRange(3.5, 6), gravity: 0.25,
			color: color, streak: 0.022,
		})
	}

	fx.addWave(x, y, 0.3, 0.24+0.14*heat, 0.016, color, 1.0)
	fx.addLight(x, y, 0.16, 0.3+0.14*heat, color.mix(rgb{1, 1, 1}, 0.45).scale(0.9+0.5*heat))

	p := &fx.paddles[side]
	p.flash = paddleFlash
	p.impactY = y
	p.strength = 0.65 + 0.35*heat
	p.recoilV = -normal * (0.42 + 0.3*heat)

	fx.addShake(0.004 + 0.01*heat)
}

func (fx *effects) wallHit(x, y, heat float64, color rgb) {
	normal := 1.0
	if y > sim.CourtHeight/2 {
		normal = -1
	}
	for range 7 + int(8*heat) {
		fx.addSpark(spark{
			x: x, y: y,
			vx: randRange(-0.85, 0.85), vy: normal * randRange(0.1, 0.65),
			life: randRange(0.12, 0.3),
			size: randRange(0.5, 0.85),
			drag: 5, gravity: 0,
			color: color, streak: 0.018,
		})
	}
	fx.pulses = append(fx.pulses, wallPulse{
		x: x, bottom: y > sim.CourtHeight/2,
		span: 0.42, strength: 0.75 + 0.5*heat, color: color,
	})
	fx.addWave(x, y, 0.2, 0.11+0.05*heat, 0.012, color, 0.55)
	fx.addLight(x, y, 0.1, 0.16, color.scale(0.75))
}

// goal is the big moment: the goal line flashes, a wave sweeps the whole court
// and the ball shatters into sparks.
func (fx *effects) goal(scorer sim.Side, x, y float64, color rgb) {
	inward := 1.0
	lineX := 0.0
	if scorer == sim.Left { // the ball left through the right side
		inward = -1
		lineX = sim.CourtWidth
	}

	for range 110 {
		angle := math.Atan2(randRange(-1.4, 1.4), inward)
		speed := randRange(0.25, 2.4)
		hot := color
		if rand.Float64() < 0.22 {
			hot = rgb{1, 1, 1}
		}
		fx.addSpark(spark{
			x: lineX, y: y + randRange(-0.07, 0.07),
			vx: math.Cos(angle) * speed, vy: math.Sin(angle) * speed,
			life: randRange(0.3, 0.95),
			size: randRange(0.6, 1.2),
			drag: randRange(2, 4.5), gravity: 0.3,
			color: hot, streak: 0.028,
		})
	}

	fx.addWave(lineX, y, 0.85, 2.3, 0.032, color, 0.95)
	fx.addWave(lineX, y, 0.55, 1.1, 0.025, rgb{1, 1, 1}, 0.8)
	fx.addLight(lineX, y, 0.4, 0.75, color.mix(rgb{1, 1, 1}, 0.3).scale(1.6))

	fx.goalSide = scorer
	fx.goalY = y
	fx.goalLeft = goalFlash
	fx.goalColor = color
	fx.courtTint = color
	fx.tintLevel = 0.11
	fx.scorePopLeft[scorer] = scorePop

	fx.addShake(0.04)
}

// serveBurst marks the moment the ball launches.
func (fx *effects) serveBurst(color rgb) {
	x, y := sim.CourtWidth/2, sim.CourtHeight/2
	fx.addWave(x, y, 0.3, 0.2, 0.014, color, 0.8)
	fx.addLight(x, y, 0.16, 0.26, color.scale(0.8))
}

func (fx *effects) startFireworks(color rgb) {
	fx.fireworks = 3.2
	fx.fireworkWait = 0
	fx.fireColor = color
}

func (fx *effects) launchFirework() {
	x := randRange(0.25, sim.CourtWidth-0.25)
	y := randRange(0.12, sim.CourtHeight-0.12)
	color := fx.fireColor
	if rand.Float64() < 0.3 {
		color = color.mix(rgb{1, 1, 1}, 0.6)
	}
	for range 46 {
		angle := randRange(0, 2*math.Pi)
		speed := randRange(0.12, 0.8)
		fx.addSpark(spark{
			x: x, y: y,
			vx: math.Cos(angle) * speed, vy: math.Sin(angle) * speed,
			life: randRange(0.45, 1.15),
			size: randRange(0.55, 0.95),
			drag: 2.2, gravity: 0.32,
			color: color, streak: 0.016,
		})
	}
	fx.addWave(x, y, 0.4, 0.3, 0.012, color, 0.6)
	fx.addLight(x, y, 0.22, 0.34, color.scale(0.9))
}

func (fx *effects) advance(dt float64, t float64) {
	alive := fx.sparks[:0]
	for _, s := range fx.sparks {
		s.life -= dt
		if s.life <= 0 {
			continue
		}
		damping := math.Exp(-s.drag * dt)
		s.vx *= damping
		s.vy = s.vy*damping + s.gravity*dt
		s.x += s.vx * dt
		s.y += s.vy * dt
		if s.y < 0 {
			s.y, s.vy = -s.y, -s.vy*0.55
		} else if s.y > sim.CourtHeight {
			s.y, s.vy = 2*sim.CourtHeight-s.y, -s.vy*0.55
		}
		alive = append(alive, s)
	}
	fx.sparks = alive

	waves := fx.waves[:0]
	for _, w := range fx.waves {
		w.age += dt
		if w.age < w.span {
			waves = append(waves, w)
		}
	}
	fx.waves = waves

	pulses := fx.pulses[:0]
	for _, p := range fx.pulses {
		p.age += dt
		if p.age < p.span {
			pulses = append(pulses, p)
		}
	}
	fx.pulses = pulses

	lights := fx.lights[:0]
	for _, l := range fx.lights {
		l.age += dt
		if l.age < l.span {
			lights = append(lights, l)
		}
	}
	fx.lights = lights

	// Paddle recoil is a stiff damped spring, so it snaps back with a slight
	// overshoot instead of easing limply.
	for i := range fx.paddles {
		p := &fx.paddles[i]
		p.flash = math.Max(0, p.flash-dt)
		const stiffness, damping = 520.0, 30.0
		for range 2 { // two substeps keep the spring stable at low frame rates
			step := dt / 2
			p.recoilV += (-stiffness*p.recoil - damping*p.recoilV) * step
			p.recoil += p.recoilV * step
		}
		fx.scorePopLeft[i] = math.Max(0, fx.scorePopLeft[i]-dt)
	}

	fx.shake *= math.Exp(-11 * dt)
	if fx.shake < 0.0004 {
		fx.shake = 0
	}
	fx.shakeX = math.Sin(t*83) * fx.shake
	fx.shakeY = math.Cos(t*71) * fx.shake

	fx.goalLeft = math.Max(0, fx.goalLeft-dt)
	fx.tintLevel *= math.Exp(-5.5 * dt)
	if fx.tintLevel < 0.003 {
		fx.tintLevel = 0
	}

	if fx.fireworks > 0 {
		fx.fireworks -= dt
		fx.fireworkWait -= dt
		if fx.fireworkWait <= 0 {
			fx.launchFirework()
			fx.fireworkWait = randRange(0.22, 0.5)
		}
	}
}

func (fx *effects) reset() {
	fx.fireworks = 0
}
