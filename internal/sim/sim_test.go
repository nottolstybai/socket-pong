package sim

import (
	"math"
	"testing"
)

func centred() (Paddle, Paddle) {
	return Paddle{Y: 0.5}, Paddle{Y: 0.5}
}

func TestWallBounceIsAMirrorAtBallRadius(t *testing.T) {
	left, right := centred()
	b := Ball{X: 1, Y: 0.1, VX: 0.4, VY: -1}

	events := Advance(&b, 0.2, left, right, nil)

	if len(events) != 1 || events[0].Kind != EventWall {
		t.Fatalf("events = %+v, want a single wall hit", events)
	}
	wantContact := (0.1 - BallRadius) / 1
	if math.Abs(events[0].Elapsed-wantContact) > 1e-12 {
		t.Errorf("contact at %v, want %v", events[0].Elapsed, wantContact)
	}
	if b.VY != 1 || b.VX != 0.4 {
		t.Errorf("velocity = (%v, %v), want a mirrored (0.4, 1)", b.VX, b.VY)
	}
	wantY := BallRadius + (0.2-wantContact)*1
	if math.Abs(b.Y-wantY) > 1e-12 {
		t.Errorf("y = %v, want %v", b.Y, wantY)
	}
}

func TestPaddleContactIsAtTheVisibleFace(t *testing.T) {
	left, right := centred()
	b := Ball{X: 0.6, Y: 0.5, VX: -1, VY: 0}

	events := Advance(&b, 1, left, right, nil)

	if len(events) == 0 || events[0].Kind != EventPaddle || events[0].Side != Left {
		t.Fatalf("events = %+v, want a left paddle hit first", events)
	}
	wantX := PaddleInset + PaddleHalfWidth + BallRadius
	if events[0].X != wantX {
		t.Errorf("contact x = %v, want the paddle face %v", events[0].X, wantX)
	}
	if b.VX <= 0 {
		t.Errorf("vx = %v, want the ball travelling right", b.VX)
	}
}

func TestMissedBallScoresForTheOpponent(t *testing.T) {
	left, right := Paddle{Y: 0.15}, Paddle{Y: 0.5}
	b := Ball{X: 0.6, Y: 0.8, VX: -1, VY: 0}

	events := Advance(&b, 2, left, right, nil)

	if len(events) != 1 || events[0].Kind != EventGoal || events[0].Side != Right {
		t.Fatalf("events = %+v, want a goal for right", events)
	}
	if b.VX != 0 || b.VY != 0 {
		t.Errorf("velocity = (%v, %v), want a stopped ball", b.VX, b.VY)
	}
}

// Slicing time differently must not change the path. This is the property
// client prediction relies on: its frames never line up with server ticks.
func TestResultIsIndependentOfTimeSlicing(t *testing.T) {
	left, right := Paddle{Y: 0.3}, Paddle{Y: 0.7}
	start := Ball{X: 1, Y: 0.5, VX: 1.4, VY: 0.9}

	whole := start
	Advance(&whole, 1.3, left, right, nil)

	sliced := start
	const slice = 1.0 / 97
	for elapsed := 0.0; elapsed < 1.3; elapsed += slice {
		Advance(&sliced, math.Min(slice, 1.3-elapsed), left, right, nil)
	}

	if math.Abs(whole.X-sliced.X) > 1e-9 || math.Abs(whole.Y-sliced.Y) > 1e-9 {
		t.Errorf("whole = %+v, sliced = %+v", whole, sliced)
	}
}

func TestFastBallCannotTunnelThroughAPaddle(t *testing.T) {
	left, right := centred()
	b := Ball{X: 1, Y: 0.5, VX: -MaxBallSpeed, VY: 0}

	events := Advance(&b, 1.0/10, left, right, nil)
	events = Advance(&b, 1, left, right, events)

	if len(events) == 0 || events[0].Kind != EventPaddle {
		t.Fatalf("events = %+v, want a paddle hit first", events)
	}
}

func TestBounceAcceleratesWithoutExceedingMaximum(t *testing.T) {
	right := Paddle{Y: 0.5}
	for _, tc := range []struct {
		name  string
		start Ball
		left  Paddle
	}{
		{"centre", Ball{X: 0.2, Y: 0.5, VX: -1, VY: 0}, Paddle{Y: 0.5}},
		{"edge", Ball{X: 0.2, Y: 0.6, VX: -1, VY: 0}, Paddle{Y: 0.5}},
		{"spin", Ball{X: 0.2, Y: 0.6, VX: -2.6, VY: 0}, Paddle{Y: 0.5, V: PaddleMaxSpeed}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := tc.start
			before := math.Hypot(b.VX, b.VY)
			Advance(&b, 0.09, tc.left, right, nil)

			speed := math.Hypot(b.VX, b.VY)
			if b.VX <= 0 {
				t.Fatalf("ball was not returned: %+v", b)
			}
			if speed <= before {
				t.Errorf("speed %v did not grow from %v", speed, before)
			}
			if speed > MaxBallSpeed+1e-9 {
				t.Errorf("speed %v exceeds maximum %v", speed, MaxBallSpeed)
			}
			if math.Abs(b.VY/speed) > math.Sin(maxSpinAngle)+1e-9 {
				t.Errorf("angle too steep: %+v", b)
			}
		})
	}
}

func TestBallStaysInsideCourtVertically(t *testing.T) {
	left, right := Paddle{Y: 0.5}, Paddle{Y: 0.5}
	b := Ball{X: 1, Y: 0.5, VX: 0.2, VY: 2.5}
	for range 2000 {
		Advance(&b, 1.0/60, left, right, nil)
		if b.Y < BallRadius-1e-9 || b.Y > CourtHeight-BallRadius+1e-9 {
			t.Fatalf("ball left the court: %+v", b)
		}
		if b.VX == 0 && b.VY == 0 {
			break
		}
	}
}
