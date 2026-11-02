// Package sim holds the Pong physics shared by the server and the client.
//
// The server uses it as the source of truth. The client runs the very same
// code to predict the ball between snapshots, which is what keeps the rendered
// path perfectly straight between bounces.
//
// The court has a fixed 2:1 shape, so a ball leaves a surface at the same
// visible angle on every terminal. Half-block pixels are roughly square, so
// world units map uniformly to pixels. Collisions are solved analytically and
// account for the ball radius, so the ball visibly touches what it bounces off.
package sim

import "math"

const (
	CourtWidth  = 2.0
	CourtHeight = 1.0

	TickRate     = 120
	SnapshotRate = 60
	WinningScore = 5

	PaddleHeight    = 0.24
	PaddleHalfWidth = 0.016
	PaddleInset     = 0.075 // court edge to paddle centre line
	PaddleMaxSpeed  = 2.2
	paddleGrace     = 0.012 // forgiving margin past the paddle ends

	BallRadius   = 0.026
	ServeSpeed   = 1.05
	MaxBallSpeed = 2.7

	ballSpeedGain = 1.055
	spinTransfer  = 0.22

	maxBounceAngle = 55 * math.Pi / 180
	maxSpinAngle   = 66 * math.Pi / 180
	maxServeAngle  = 24 * math.Pi / 180

	ServeSeconds = 1.25
	PointSeconds = 0.75
)

// Side identifies a player's half of the court.
type Side int

const (
	Left Side = iota
	Right
)

// Ball is the ball's position and velocity in world units.
type Ball struct {
	X, Y, VX, VY float64
}

// Paddle is a paddle's centre position and vertical velocity.
type Paddle struct {
	Y, V float64
}

// EventKind classifies what the ball did during an Advance.
type EventKind int

const (
	EventWall EventKind = iota
	EventPaddle
	EventGoal
)

// Event describes a collision. Elapsed is the time since the start of the
// Advance call, X and Y are where the ball was at that moment. Side is the
// paddle that was hit, or the player who scored for EventGoal.
type Event struct {
	Kind    EventKind
	Side    Side
	X, Y    float64
	Elapsed float64
}

// CentreBall returns a motionless ball in the middle of the court.
func CentreBall() Ball {
	return Ball{X: CourtWidth / 2, Y: CourtHeight / 2}
}

// ClampPaddle limits a paddle centre to the playable range.
func ClampPaddle(y float64) float64 {
	return clamp(y, PaddleHeight/2, CourtHeight-PaddleHeight/2)
}

// PaddleX returns the x coordinate of a paddle's centre line.
func PaddleX(side Side) float64 {
	if side == Left {
		return PaddleInset
	}
	return CourtWidth - PaddleInset
}

// Heat maps ball speed to 0..1: 0 at serve speed, 1 at maximum speed.
func Heat(b Ball) float64 {
	speed := math.Hypot(b.VX, b.VY)
	return clamp((speed-ServeSpeed)/(MaxBallSpeed-ServeSpeed), 0, 1)
}

// Serve returns the launch velocity. direction is -1 (towards left) or 1, and
// bias in -1..1 picks the launch angle.
func Serve(direction, bias float64) (vx, vy float64) {
	angle := clamp(bias, -1, 1) * maxServeAngle
	return math.Cos(angle) * ServeSpeed * direction, math.Sin(angle) * ServeSpeed
}

// Advance moves the ball forward by dt seconds, resolving every collision on
// the way at its exact time of impact. Results do not depend on how dt is
// sliced, as long as the paddles do not move. Events are appended to events.
//
// After a goal the ball stays where it crossed the line with zero velocity.
func Advance(b *Ball, dt float64, left, right Paddle, events []Event) []Event {
	const (
		hitNone = iota
		hitTop
		hitBottom
		hitLeft
		hitRight
		hitGoalLeft
		hitGoalRight
	)

	var elapsed float64
	for range 32 {
		remaining := dt - elapsed
		if remaining <= 0 || (b.VX == 0 && b.VY == 0) {
			return events
		}

		toi := remaining
		hit := hitNone
		consider := func(t float64, kind int) {
			t = math.Max(t, 0)
			if t <= toi {
				toi = t
				hit = kind
			}
		}

		switch {
		case b.VY < 0:
			consider((BallRadius-b.Y)/b.VY, hitTop)
		case b.VY > 0:
			consider((CourtHeight-BallRadius-b.Y)/b.VY, hitBottom)
		}

		leftFace := PaddleInset + PaddleHalfWidth + BallRadius
		rightFace := CourtWidth - leftFace
		switch {
		case b.VX < 0:
			if b.X >= leftFace {
				consider((leftFace-b.X)/b.VX, hitLeft)
			}
			consider((-BallRadius-b.X)/b.VX, hitGoalLeft)
		case b.VX > 0:
			if b.X <= rightFace {
				consider((rightFace-b.X)/b.VX, hitRight)
			}
			consider((CourtWidth+BallRadius-b.X)/b.VX, hitGoalRight)
		}

		b.X += b.VX * toi
		b.Y += b.VY * toi
		elapsed += toi

		switch hit {
		case hitTop:
			b.Y, b.VY = BallRadius, math.Abs(b.VY)
			events = append(events, Event{Kind: EventWall, X: b.X, Y: 0, Elapsed: elapsed})
		case hitBottom:
			b.Y, b.VY = CourtHeight-BallRadius, -math.Abs(b.VY)
			events = append(events, Event{Kind: EventWall, X: b.X, Y: CourtHeight, Elapsed: elapsed})
		case hitLeft:
			if overPaddle(b.Y, left) {
				b.X = leftFace
				bounce(b, left, 1)
				events = append(events, Event{Kind: EventPaddle, Side: Left, X: b.X, Y: b.Y, Elapsed: elapsed})
			} else {
				b.X = math.Nextafter(leftFace, -1) // past the face: no second test
			}
		case hitRight:
			if overPaddle(b.Y, right) {
				b.X = rightFace
				bounce(b, right, -1)
				events = append(events, Event{Kind: EventPaddle, Side: Right, X: b.X, Y: b.Y, Elapsed: elapsed})
			} else {
				b.X = math.Nextafter(rightFace, CourtWidth+1)
			}
		case hitGoalLeft, hitGoalRight:
			scorer := Right
			if hit == hitGoalRight {
				scorer = Left
			}
			b.VX, b.VY = 0, 0
			return append(events, Event{Kind: EventGoal, Side: scorer, X: b.X, Y: b.Y, Elapsed: elapsed})
		}
	}
	return events
}

func overPaddle(ballY float64, p Paddle) bool {
	reach := PaddleHeight/2 + BallRadius + paddleGrace
	return math.Abs(ballY-p.Y) <= reach
}

// bounce sends the ball off a paddle. The further from the centre it lands the
// steeper it leaves, and a moving paddle adds spin. Speed is applied after the
// spin, so the ball never appears to slow down on a hit.
func bounce(b *Ball, p Paddle, directionX float64) {
	relative := clamp((b.Y-p.Y)/(PaddleHeight/2+BallRadius), -1, 1)
	speed := math.Min(MaxBallSpeed, math.Hypot(b.VX, b.VY)*ballSpeedGain)

	vy := math.Sin(relative*maxBounceAngle)*speed + p.V*spinTransfer
	limit := math.Sin(maxSpinAngle) * speed
	vy = clamp(vy, -limit, limit)

	b.VY = vy
	b.VX = math.Sqrt(speed*speed-vy*vy) * directionX
}

func clamp(value, low, high float64) float64 {
	return math.Max(low, math.Min(high, value))
}
