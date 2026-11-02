package main

import (
	"math"

	"github.com/nottolstybai/socket-pong/internal/protocol"
	"github.com/nottolstybai/socket-pong/internal/sim"
)

const (
	trailSeconds    = 0.34
	remoteDelay     = 0.034 // opponent paddle plays back slightly in the past
	correctionTime  = 0.055 // seconds for a prediction error to blend away
	correctionLimit = 0.45  // larger errors snap, blending them would look worse
	demoSpeed       = 0.72
	effectDedupe    = 0.3
)

var (
	colorLeft  = hexColor(0x22E6DC)
	colorRight = hexColor(0xFF3D81)
	colorBall  = hexColor(0xFFC56B)
	colorWhite = rgb{1, 1, 1}
)

func sideColor(side sim.Side) rgb {
	if side == sim.Left {
		return colorLeft
	}
	return colorRight
}

func sideFromName(name string) sim.Side {
	if name == protocol.SideRight {
		return sim.Right
	}
	return sim.Left
}

type trailPoint struct {
	x, y, t float64
}

type remoteSample struct {
	t, y, v float64
}

type firedEffect struct {
	kind sim.EventKind
	x, y float64
	at   float64
}

// world is the client's view of the match.
//
// The ball is not drawn from snapshots. It is simulated locally against the
// server's clock with the same physics as the server, so between collisions its
// path is mathematically straight and network jitter cannot touch it. Snapshots
// only correct it, and any correction blends in over a few frames.
type world struct {
	side  sim.Side
	state protocol.State

	ball    sim.Ball
	ballAt  float64
	offsetX float64 // shown position minus simulated position
	offsetY float64

	// A prediction made after the last paddle bounce must not be redone from an
	// older snapshot, or the rebound would wobble as the paddle keeps moving.
	bounceAt float64

	remote []remoteSample
	own    sim.Paddle

	trail []trailPoint
	tint  rgb
	pop   float64 // 1 right after a bounce, decays to 0
	now   float64

	lastEvent uint64
	fired     []firedEffect
	events    []sim.Event

	fx *effects
}

func newWorld(side sim.Side, initial protocol.State, fx *effects) *world {
	w := &world{
		side:      side,
		state:     initial,
		tint:      colorBall,
		lastEvent: initial.EventID,
		fx:        fx,
	}
	w.ball = ballFromState(initial)
	w.ballAt = initial.Time
	w.own = sim.Paddle{Y: w.paddleFromState(side, initial).Y}
	w.remote = append(w.remote, remoteSample{
		t: initial.Time,
		y: w.paddleFromState(otherSide(side), initial).Y,
	})
	if initial.Phase == protocol.PhaseWaiting {
		w.resetDemo()
	}
	return w
}

func otherSide(side sim.Side) sim.Side {
	if side == sim.Left {
		return sim.Right
	}
	return sim.Left
}

func ballFromState(s protocol.State) sim.Ball {
	return sim.Ball{X: s.BallX, Y: s.BallY, VX: s.BallVX, VY: s.BallVY}
}

func (w *world) paddleFromState(side sim.Side, s protocol.State) sim.Paddle {
	if side == sim.Left {
		return sim.Paddle{Y: s.LeftY, V: s.LeftV}
	}
	return sim.Paddle{Y: s.RightY, V: s.RightV}
}

// update brings the world to server time now. own is the local paddle at that
// moment, states are the snapshots received since the previous frame.
func (w *world) update(now, dt float64, own sim.Paddle, states []protocol.State) {
	w.now = now
	w.own = own

	for _, s := range states {
		w.applySnapshot(s)
	}

	if w.state.Phase == protocol.PhaseWaiting {
		w.advanceDemo(dt)
	} else if w.state.Phase == protocol.PhasePlay {
		w.predict(now)
	}

	if len(states) > 0 {
		w.reconcile(states[len(states)-1], now)
	}

	fade := math.Exp(-dt / correctionTime)
	w.offsetX *= fade
	w.offsetY *= fade
	w.pop *= math.Exp(-13 * dt)

	w.pushTrail(now)
}

// shownBall returns where to draw the ball.
func (w *world) shownBall() (x, y float64) {
	return w.ball.X + w.offsetX, w.ball.Y + w.offsetY
}

func (w *world) ballVisible() bool {
	switch w.state.Phase {
	case protocol.PhasePoint, protocol.PhaseOver:
		return false
	case protocol.PhasePlay:
		return w.ball.VX != 0 || w.ball.VY != 0
	}
	return true
}

func (w *world) applySnapshot(s protocol.State) {
	previous := w.state
	w.state = s

	opponent := w.paddleFromState(otherSide(w.side), s)
	w.remote = append(w.remote, remoteSample{t: s.Time, y: opponent.Y, v: opponent.V})
	if len(w.remote) > 16 {
		w.remote = w.remote[len(w.remote)-16:]
	}

	if s.EventID != w.lastEvent {
		w.lastEvent = s.EventID
		w.applyServerEvent(s)
	}

	switch {
	case previous.Phase != s.Phase && s.Phase == protocol.PhaseWaiting:
		w.resetDemo()
		w.fx.reset()
	case previous.Phase == protocol.PhaseServe && s.Phase == protocol.PhasePlay:
		w.fx.serveBurst(w.tint)
	case previous.Phase != protocol.PhaseOver && s.Phase == protocol.PhaseOver:
		if sideFromName(s.Winner) == w.side {
			w.fx.startFireworks(sideColor(w.side))
		}
	case previous.Phase == protocol.PhaseOver && s.Phase != protocol.PhaseOver:
		w.fx.reset()
	}

	if s.Rally != previous.Rally {
		w.tint = colorBall
		w.trail = w.trail[:0]
		w.offsetX, w.offsetY = 0, 0
		w.bounceAt = 0
	}
}

// predict runs the ball forward with the paddles as best known right now.
func (w *world) predict(now float64) {
	step := now - w.ballAt
	if step <= 0 {
		return
	}
	left, right := w.paddles(now)
	heat := sim.Heat(w.ball)
	vy := w.ball.VY
	w.events = sim.Advance(&w.ball, step, left, right, w.events[:0])
	for _, event := range w.events {
		w.onCollision(event, vy, heat, w.ballAt+event.Elapsed)
	}
	w.ballAt = now
}

func (w *world) paddles(now float64) (left, right sim.Paddle) {
	opponent := w.remoteNow(now)
	if w.side == sim.Left {
		return w.own, opponent
	}
	return opponent, w.own
}

// remoteNow extrapolates the opponent's paddle to the present for collisions.
func (w *world) remoteNow(now float64) sim.Paddle {
	last := w.remote[len(w.remote)-1]
	ahead := math.Max(0, math.Min(now-last.t, 0.1))
	return sim.Paddle{Y: sim.ClampPaddle(last.y + last.v*ahead), V: last.v}
}

// remoteShown plays the opponent's paddle slightly in the past, interpolating
// between snapshots, so its motion stays smooth however packets arrive.
func (w *world) remoteShown(now float64) float64 {
	t := now - remoteDelay
	for i := len(w.remote) - 1; i > 0; i-- {
		a, b := w.remote[i-1], w.remote[i]
		if t >= a.t && t <= b.t && b.t > a.t {
			return a.y + (b.y-a.y)*(t-a.t)/(b.t-a.t)
		}
	}
	return w.remote[len(w.remote)-1].y
}

// reconcile compares the prediction with the newest snapshot, carried forward
// to the present by the same physics. In free flight the two agree exactly.
func (w *world) reconcile(s protocol.State, now float64) {
	authoritative := ballFromState(s)
	if s.Phase != protocol.PhasePlay {
		w.ball = authoritative
		w.ballAt = now
		w.offsetX, w.offsetY = 0, 0
		return
	}
	if s.Time < w.bounceAt {
		return // the snapshot predates a paddle bounce already played out here
	}

	left, right := w.paddles(now)
	w.events = sim.Advance(&authoritative, math.Max(0, math.Min(now-s.Time, 0.3)), left, right, w.events[:0])

	dx := w.ball.X + w.offsetX - authoritative.X
	dy := w.ball.Y + w.offsetY - authoritative.Y
	if math.Hypot(dx, dy) > correctionLimit {
		dx, dy = 0, 0
	}
	w.ball = authoritative
	w.ballAt = now
	w.offsetX, w.offsetY = dx, dy
}

// onCollision handles a bounce found by the local prediction.
func (w *world) onCollision(event sim.Event, ballVY, heat, at float64) {
	switch event.Kind {
	case sim.EventWall:
		y := math.Max(sim.BallRadius, math.Min(sim.CourtHeight-sim.BallRadius, event.Y))
		w.trail = append(w.trail, trailPoint{x: event.X, y: y, t: at})
		w.pop = math.Max(w.pop, 0.6)
		w.fireWall(event.X, event.Y, heat)
	case sim.EventPaddle:
		w.bounceAt = at
		w.trail = append(w.trail, trailPoint{x: event.X, y: event.Y, t: at})
		w.pop = 1
		w.firePaddle(event.Side, event.X, event.Y, ballVY, heat)
	}
}

// applyServerEvent fires whatever a prediction did not, and always handles goals.
func (w *world) applyServerEvent(s protocol.State) {
	heat := sim.Heat(w.ball)
	switch s.Event {
	case protocol.EventWall:
		w.fireWall(s.EventX, s.EventY, heat)
	case protocol.EventPaddle:
		w.firePaddle(sideFromName(s.EventSide), s.EventX, s.EventY, s.BallVY, heat)
	case protocol.EventGoal:
		scorer := sideFromName(s.EventSide)
		w.fx.goal(scorer, s.EventX, s.EventY, sideColor(scorer))
	}
}

func (w *world) fireWall(x, y, heat float64) {
	if w.alreadyFired(sim.EventWall, x, y) {
		return
	}
	w.fx.wallHit(x, y, heat, w.tint.mix(colorWhite, 0.25))
}

func (w *world) firePaddle(side sim.Side, x, y, vy, heat float64) {
	w.tint = sideColor(side)
	if w.alreadyFired(sim.EventPaddle, x, y) {
		return
	}
	w.fx.paddleHit(side, x, y, vy, heat, sideColor(side))
}

// alreadyFired prevents one collision from playing twice when both the
// prediction and the server report it.
func (w *world) alreadyFired(kind sim.EventKind, x, y float64) bool {
	alive := w.fired[:0]
	found := false
	for _, f := range w.fired {
		if w.now-f.at > effectDedupe {
			continue
		}
		alive = append(alive, f)
		if f.kind == kind && math.Hypot(f.x-x, f.y-y) < 0.25 {
			found = true
		}
	}
	w.fired = alive
	if !found {
		w.fired = append(w.fired, firedEffect{kind: kind, x: x, y: y, at: w.now})
	}
	return found
}

func (w *world) pushTrail(now float64) {
	if w.state.Phase == protocol.PhaseServe || !w.ballVisible() {
		w.trimTrail(now)
		return
	}
	x, y := w.shownBall()
	w.trail = append(w.trail, trailPoint{x: x, y: y, t: now})
	w.trimTrail(now)
}

func (w *world) trimTrail(now float64) {
	drop := 0
	for drop < len(w.trail) && now-w.trail[drop].t > trailSeconds {
		drop++
	}
	if drop > 0 {
		w.trail = append(w.trail[:0], w.trail[drop:]...)
	}
}

// While waiting for an opponent the ball drifts around the court as a demo. It
// is purely local and bounces as if both paddles were in the right place.

func (w *world) resetDemo() {
	vx, vy := sim.Serve(1, 0.9)
	w.ball = sim.Ball{X: sim.CourtWidth / 2, Y: sim.CourtHeight / 2, VX: vx * demoSpeed, VY: vy * demoSpeed}
	w.trail = w.trail[:0]
	w.tint = colorBall
	w.offsetX, w.offsetY = 0, 0
}

func (w *world) advanceDemo(dt float64) {
	follow := sim.Paddle{Y: w.ball.Y}
	w.events = sim.Advance(&w.ball, dt, follow, follow, w.events[:0])
	for _, event := range w.events {
		y := math.Max(sim.BallRadius, math.Min(sim.CourtHeight-sim.BallRadius, event.Y))
		w.trail = append(w.trail, trailPoint{x: event.X, y: y, t: w.now})
		if event.Kind == sim.EventPaddle {
			w.tint = sideColor(event.Side)
		}
	}
	// Bounces speed a real ball up. The demo keeps its pace.
	if speed := math.Hypot(w.ball.VX, w.ball.VY); speed > 0 {
		scale := sim.ServeSpeed * demoSpeed / speed
		w.ball.VX *= scale
		w.ball.VY *= scale
	}
}
