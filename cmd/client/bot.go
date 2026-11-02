package main

import (
	"math"
	"math/rand/v2"
	"time"

	"github.com/nottolstybai/socket-pong/internal/protocol"
	"github.com/nottolstybai/socket-pong/internal/sim"
)

// runBot plays one side without a terminal, so the game can be tried alone. It
// reads the ball the way a person does, guessing where it will land with an
// error that grows with ball speed, which keeps it beatable.
func runBot(l *link, side string, initial protocol.State) error {
	own := sideFromName(side)
	input := newPaddleInput(sim.CourtHeight / 2)
	if own == sim.Left {
		input.y = initial.LeftY
	} else {
		input.y = initial.RightY
	}

	ticker := time.NewTicker(frameInterval)
	defer ticker.Stop()

	state := initial
	var lastHits int
	aimError := 0.0
	rematchSent := false
	last := time.Now()

	for now := range ticker.C {
		dt := now.Sub(last).Seconds()
		last = now

		states, _, _, err := l.poll(now, dt)
		if err != nil {
			return err
		}
		if len(states) > 0 {
			state = states[len(states)-1]
		}

		ball := sim.Ball{X: state.BallX, Y: state.BallY, VX: state.BallVX, VY: state.BallVY}
		if state.Hits != lastHits || state.Phase != protocol.PhasePlay {
			lastHits = state.Hits
			spread := 0.07 + 0.13*sim.Heat(ball)
			aimError = (rand.Float64()*2 - 1) * spread
		}

		switch {
		case state.Phase == protocol.PhaseOver:
			if !rematchSent {
				l.sendRematch()
				rematchSent = true
			}
		case state.Phase == protocol.PhasePlay:
			rematchSent = false
			approaching := (own == sim.Left && ball.VX < 0) || (own == sim.Right && ball.VX > 0)
			if approaching {
				input.mouse(landingY(ball, own) + aimError)
			} else {
				input.mouse(sim.CourtHeight / 2)
			}
		default:
			input.mouse(sim.CourtHeight / 2)
		}

		// The bot moves slower than a person may, otherwise it never misses.
		before := input.y
		input.advance(dt*0.7, now)
		velocity := 0.0
		if dt > 0 {
			velocity = (input.y - before) / dt
		}
		l.sendPaddle(input.y, velocity)
	}
	return nil
}

// landingY follows the ball's reflections to where it reaches the paddle's line.
func landingY(ball sim.Ball, own sim.Side) float64 {
	face := sim.PaddleX(own)
	if ball.VX == 0 {
		return ball.Y
	}
	seconds := math.Abs((face - ball.X) / ball.VX)

	span := sim.CourtHeight - 2*sim.BallRadius
	travel := math.Mod(ball.Y-sim.BallRadius+ball.VY*seconds, 2*span)
	if travel < 0 {
		travel += 2 * span
	}
	if travel > span {
		travel = 2*span - travel
	}
	return travel + sim.BallRadius
}
