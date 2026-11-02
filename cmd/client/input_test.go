package main

import (
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/nottolstybai/socket-pong/internal/sim"
)

const testStep = time.Second / 60

// runFrames advances the paddle for a duration, as frames would.
func runFrames(p *paddleInput, now *time.Time, duration time.Duration) {
	for elapsed := time.Duration(0); elapsed < duration; elapsed += testStep {
		*now = now.Add(testStep)
		p.advance(testStep.Seconds(), *now)
	}
}

func TestPaddleMovesOnTheVeryFirstFrame(t *testing.T) {
	for _, tc := range []struct {
		name    string
		release bool
	}{{"release events", true}, {"press only", false}} {
		t.Run(tc.name, func(t *testing.T) {
			now := time.Now()
			p := newPaddleInput(0.5)
			p.releaseEvents = tc.release

			p.press(-1, false, now)
			runFrames(&p, &now, testStep)

			if p.y >= 0.5 {
				t.Errorf("paddle did not move in the first frame: y = %v", p.y)
			}
		})
	}
}

func TestHeldKeyStopsTheFrameItIsReleased(t *testing.T) {
	now := time.Now()
	p := newPaddleInput(0.5)
	p.releaseEvents = true

	p.press(1, false, now)
	runFrames(&p, &now, 100*time.Millisecond)
	p.release(1)
	runFrames(&p, &now, testStep)
	stopped := p.y
	runFrames(&p, &now, 100*time.Millisecond)

	if p.y != stopped || p.v != 0 {
		t.Errorf("paddle kept coasting: %v -> %v, v = %v", stopped, p.y, p.v)
	}
}

func TestLatestKeyWinsWhenBothAreHeld(t *testing.T) {
	now := time.Now()
	p := newPaddleInput(0.5)
	p.releaseEvents = true

	p.press(1, false, now)
	runFrames(&p, &now, 50*time.Millisecond)
	p.press(-1, false, now)
	before := p.y
	runFrames(&p, &now, 50*time.Millisecond)
	if p.y >= before {
		t.Errorf("newest key ignored: %v -> %v", before, p.y)
	}
	p.release(-1)
	before = p.y
	runFrames(&p, &now, 50*time.Millisecond)
	if p.y <= before {
		t.Errorf("still-held key not resumed: %v -> %v", before, p.y)
	}
}

func TestTapMovesExactlyOneStepWithoutReleaseEvents(t *testing.T) {
	now := time.Now()
	p := newPaddleInput(0.5)

	p.press(1, false, now)
	runFrames(&p, &now, 300*time.Millisecond)

	if want := 0.5 + keyStep; p.y != want {
		t.Errorf("y = %v after a tap, want exactly %v", p.y, want)
	}
	if p.v != 0 {
		t.Errorf("paddle still moving after a tap: v = %v", p.v)
	}
}

func TestQuickTapsAddUp(t *testing.T) {
	now := time.Now()
	p := newPaddleInput(0.5)

	p.press(-1, false, now)
	runFrames(&p, &now, 100*time.Millisecond) // slower than auto-repeat
	p.press(-1, false, now)
	runFrames(&p, &now, 300*time.Millisecond)

	want := 0.5 - 2*keyStep
	if diff := p.y - want; diff > 1e-12 || diff < -1e-12 {
		t.Errorf("y = %v, want %v", p.y, want)
	}
}

func TestAutoRepeatStreamMovesContinuouslyAndEnds(t *testing.T) {
	now := time.Now()
	p := newPaddleInput(0.7)

	p.press(-1, false, now)
	runFrames(&p, &now, 400*time.Millisecond) // the terminal's initial repeat delay
	afterTap := p.y

	for range 8 {
		p.press(-1, false, now)
		runFrames(&p, &now, 33*time.Millisecond)
	}
	if p.y >= afterTap-keyStep {
		t.Errorf("auto-repeat barely moved the paddle: %v -> %v", afterTap, p.y)
	}

	runFrames(&p, &now, repeatHold+2*testStep)
	stopped := p.y
	runFrames(&p, &now, 100*time.Millisecond)
	if p.y != stopped {
		t.Errorf("paddle kept moving after the stream ended: %v -> %v", stopped, p.y)
	}
}

func TestDriveModeKeepsMovingUntilTheOppositeKey(t *testing.T) {
	now := time.Now()
	p := newPaddleInput(0.5)
	p.toggleDrive()

	p.press(1, false, now)
	runFrames(&p, &now, 60*time.Millisecond)
	moving := p.y
	runFrames(&p, &now, 60*time.Millisecond)
	if p.y <= moving {
		t.Fatalf("drive stopped by itself: %v -> %v", moving, p.y)
	}

	p.press(-1, false, now)
	runFrames(&p, &now, testStep)
	braked := p.y
	runFrames(&p, &now, 60*time.Millisecond)
	if p.y != braked {
		t.Errorf("opposite key did not brake: %v -> %v", braked, p.y)
	}
}

func TestMouseFollowsPointerWithoutExceedingSpeedLimit(t *testing.T) {
	now := time.Now()
	p := newPaddleInput(0.5)
	p.mouse(0.8)

	runFrames(&p, &now, testStep)
	if p.y-0.5 > sim.PaddleMaxSpeed*testStep.Seconds()+1e-9 {
		t.Errorf("paddle teleported to %v", p.y)
	}
	runFrames(&p, &now, 300*time.Millisecond)
	if p.y != 0.8 || p.v != 0 {
		t.Errorf("paddle did not settle on the pointer: y = %v, v = %v", p.y, p.v)
	}
}

func TestPaddleStaysInCourt(t *testing.T) {
	now := time.Now()
	p := newPaddleInput(0.5)
	p.releaseEvents = true
	p.press(1, false, now)
	runFrames(&p, &now, time.Second)
	if want := sim.CourtHeight - sim.PaddleHeight/2; p.y != want || p.v != 0 {
		t.Errorf("y = %v, v = %v, want y = %v and v = 0", p.y, p.v, want)
	}
}

func TestKeysWorkOnEveryLayout(t *testing.T) {
	for _, tc := range []struct {
		name string
		key  tea.Key
		want action
	}{
		{"arrow", tea.Key{Code: tea.KeyUp}, actionUp},
		{"latin", tea.Key{Code: 'w'}, actionUp},
		{"caps lock", tea.Key{Code: 'S'}, actionDown},
		{"russian typed", tea.Key{Code: 'ц'}, actionUp},
		{"russian quit", tea.Key{Code: 'й'}, actionQuit},
		{"russian rematch", tea.Key{Code: 'к'}, actionRematch},
		{"physical key", tea.Key{Code: 'ы', BaseCode: 's'}, actionDown},
		{"ctrl+c", tea.Key{Code: 'c', Mod: tea.ModCtrl}, actionQuit},
		{"plain c", tea.Key{Code: 'c'}, actionNone},
		{"tab", tea.Key{Code: tea.KeyTab}, actionToggleDrive},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := actionFor(tc.key); got != tc.want {
				t.Errorf("actionFor(%+v) = %v, want %v", tc.key, got, tc.want)
			}
		})
	}
}
