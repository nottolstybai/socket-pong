package main

import (
	"math"
	"math/rand/v2"
	"testing"

	"github.com/nottolstybai/socket-pong/internal/protocol"
	"github.com/nottolstybai/socket-pong/internal/sim"
)

// flightState is a snapshot of a ball in free flight at time t.
func flightState(start sim.Ball, t float64) protocol.State {
	return protocol.State{
		Time:   t,
		Phase:  protocol.PhasePlay,
		Rally:  1,
		BallX:  start.X + start.VX*t,
		BallY:  start.Y + start.VY*t,
		BallVX: start.VX,
		BallVY: start.VY,
		LeftY:  0.5,
		RightY: 0.5,
	}
}

// The complaint this guards against: a ball that shakes and seems to change
// direction in mid-air. Snapshots arrive late by a random amount and frames are
// unevenly spaced, as on a real system. The drawn ball must stay on the exact
// line regardless, and never step backwards along it.
func TestBallStaysOnItsLineDespiteJitter(t *testing.T) {
	start := sim.Ball{X: 0.3, Y: 0.35, VX: 1.2, VY: 0.2}
	rng := rand.New(rand.NewPCG(1, 2))

	w := newWorld(sim.Left, flightState(start, 0), &effects{})
	own := sim.Paddle{Y: 0.5}

	type pending struct {
		arrives float64
		state   protocol.State
	}
	var queue []pending
	nextSnapshot := 1.0 / sim.SnapshotRate

	now, lastX := 0.0, start.X
	for now < 0.9 {
		dt := (12 + rng.Float64()*10) / 1000
		now += dt

		for nextSnapshot <= now {
			queue = append(queue, pending{
				arrives: nextSnapshot + rng.Float64()*0.03,
				state:   flightState(start, nextSnapshot),
			})
			nextSnapshot += 1.0 / sim.SnapshotRate
		}
		var delivered []protocol.State
		remaining := queue[:0]
		for _, p := range queue {
			if p.arrives <= now {
				delivered = append(delivered, p.state)
			} else {
				remaining = append(remaining, p)
			}
		}
		queue = remaining

		w.update(now, dt, own, delivered)

		x, y := w.shownBall()
		if x <= lastX {
			t.Fatalf("t=%.3f: ball moved backwards from x=%v to x=%v", now, lastX, x)
		}
		lastX = x
		wantY := start.Y + (x-start.X)/start.VX*start.VY
		if math.Abs(y-wantY) > 1e-9 {
			t.Fatalf("t=%.3f: ball left its line by %v", now, y-wantY)
		}
	}
}

func TestPredictedBounceIsNotReplayedFromOlderSnapshots(t *testing.T) {
	// Heading into the left paddle, which then moves away after the hit. A late
	// snapshot from before the bounce must not re-aim the ball.
	start := sim.Ball{X: 0.3, Y: 0.5, VX: -1.5, VY: 0}
	fx := &effects{}
	w := newWorld(sim.Left, flightState(start, 0), fx)

	now := 0.16
	w.update(now, now, sim.Paddle{Y: 0.5}, nil)
	if w.ball.VX <= 0 {
		t.Fatalf("expected a predicted bounce, got %+v", w.ball)
	}
	bounced := w.ball
	beforeSparks := len(fx.sparks)
	if beforeSparks == 0 {
		t.Fatal("the predicted bounce fired no effect")
	}

	stale := flightState(start, 0.05)
	now += 1.0 / 60
	w.update(now, 1.0/60, sim.Paddle{Y: 0.9, V: 2}, []protocol.State{stale})

	if w.ball.VY != bounced.VY || w.ball.VX != bounced.VX {
		t.Errorf("stale snapshot changed the rebound: %+v -> %+v", bounced, w.ball)
	}
}

func TestServerConfirmationDoesNotPlayABounceTwice(t *testing.T) {
	start := sim.Ball{X: 0.3, Y: 0.5, VX: -1.5, VY: 0}
	fx := &effects{}
	w := newWorld(sim.Left, flightState(start, 0), fx)

	now := 0.16
	w.update(now, now, sim.Paddle{Y: 0.5}, nil)
	sparks := len(fx.sparks)
	waves := len(fx.waves)

	confirmed := flightState(start, 0.15)
	confirmed.BallX, confirmed.BallVX = w.ball.X, w.ball.VX
	confirmed.BallY, confirmed.BallVY = w.ball.Y, w.ball.VY
	confirmed.EventID = 1
	confirmed.Event = protocol.EventPaddle
	confirmed.EventSide = protocol.SideLeft
	confirmed.EventX = sim.PaddleInset + sim.PaddleHalfWidth + sim.BallRadius
	confirmed.EventY = 0.5

	now += 1.0 / 60
	w.update(now, 1.0/60, sim.Paddle{Y: 0.5}, []protocol.State{confirmed})

	if len(fx.sparks) != sparks || len(fx.waves) != waves {
		t.Errorf("effect fired twice: sparks %d -> %d, waves %d -> %d", sparks, len(fx.sparks), waves, len(fx.waves))
	}
}

func TestMispredictionBlendsInsteadOfJumping(t *testing.T) {
	start := sim.Ball{X: 0.3, Y: 0.5, VX: -1.5, VY: 0}
	w := newWorld(sim.Left, flightState(start, 0), &effects{})

	const dt = 1.0 / 60
	now := 0.0
	own := sim.Paddle{Y: 0.5}
	for range 3 {
		now += dt
		w.update(now, dt, own, nil)
	}
	shownX, shownY := w.shownBall()

	corrected := flightState(start, now)
	corrected.BallY += 0.03
	now += dt
	w.update(now, dt, own, []protocol.State{corrected})

	x, y := w.shownBall()
	if jump := math.Hypot(x-shownX, y-shownY); jump > 0.04 {
		t.Errorf("ball jumped by %v in one frame", jump)
	}
	for range 30 {
		now += dt
		corrected = flightState(start, now)
		corrected.BallY += 0.03
		w.update(now, dt, own, []protocol.State{corrected})
	}
	if _, y := w.shownBall(); math.Abs(y-(0.5+0.03)) > 0.001 {
		t.Errorf("correction did not converge, y = %v", y)
	}
}

func TestNewRallySnapsWithoutBlending(t *testing.T) {
	start := sim.Ball{X: 1.7, Y: 0.8, VX: 1.5, VY: 0}
	w := newWorld(sim.Left, flightState(start, 0), &effects{})

	serve := protocol.State{
		Time: 1.0 / 60, Phase: protocol.PhaseServe, PhaseLeft: 1, Rally: 2,
		BallX: sim.CourtWidth / 2, BallY: sim.CourtHeight / 2, LeftY: 0.5, RightY: 0.5,
	}
	w.update(1.0/60, 1.0/60, sim.Paddle{Y: 0.5}, []protocol.State{serve})

	x, y := w.shownBall()
	if x != serve.BallX || y != serve.BallY {
		t.Errorf("ball at (%v, %v), want the centre", x, y)
	}
}
