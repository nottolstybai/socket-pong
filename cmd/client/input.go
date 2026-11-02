package main

import (
	"math"
	"time"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"github.com/nottolstybai/socket-pong/internal/sim"
)

// The local paddle is moved here and shown immediately, without waiting for the
// server. The server receives the resulting position and only checks that it
// could have been reached.
//
// Keyboard input comes in two flavours, because many terminals only report key
// presses, and repeat them after a long pause (often around 500 ms):
//
//   - With release events (kitty protocol, Windows Terminal) the paddle moves
//     exactly while a key is held.
//   - Without them a press moves one fixed step, which makes taps precise and
//     repeatable, and an auto-repeat stream moves continuously. Tab switches to
//     "drive", where a press keeps the paddle moving until the opposite key.
//
// The mouse needs neither: the paddle simply follows the pointer.

const (
	keyStartSpeed   = 0.55
	keyAcceleration = 13.0
	keyStep         = 0.095

	// Presses closer together than this are the terminal's auto-repeat.
	repeatGap = 85 * time.Millisecond
	// How long an auto-repeat stream may pause before the key counts as released.
	repeatHold = 95 * time.Millisecond
)

type action int

const (
	actionNone action = iota
	actionUp
	actionDown
	actionQuit
	actionRematch
	actionToggleDrive
)

// actionFor maps a key to a game action, on Latin and Russian layouts alike.
func actionFor(key tea.Key) action {
	if key.Mod.Contains(tea.ModCtrl) {
		if key.Code == 'c' || key.BaseCode == 'c' {
			return actionQuit
		}
		return actionNone
	}

	switch key.Code {
	case tea.KeyUp:
		return actionUp
	case tea.KeyDown:
		return actionDown
	case tea.KeyTab:
		return actionToggleDrive
	}

	code := key.BaseCode
	if code == 0 {
		code = key.Code
	}
	switch unicode.ToLower(code) {
	case 'w', 'k', 'ц', 'л':
		return actionUp
	case 's', 'j', 'ы', 'о':
		return actionDown
	case 'q', 'й':
		return actionQuit
	case 'r', 'к':
		return actionRematch
	}
	return actionNone
}

type paddleInput struct {
	y, v  float64
	speed float64

	// Release-event mode: which keys are physically down.
	releaseEvents bool
	upHeld        bool
	downHeld      bool
	lastDirection float64

	// Press-only mode.
	driveMode  bool
	lastPress  time.Time
	lastDir    float64
	holdDir    float64
	holdUntil  time.Time
	stepping   bool
	stepTarget float64
	driveDir   float64

	mouseActive bool
	mouseTarget float64
}

func newPaddleInput(y float64) paddleInput {
	return paddleInput{y: y}
}

func (p *paddleInput) press(direction float64, isRepeat bool, now time.Time) {
	p.mouseActive = false

	if p.releaseEvents {
		if direction < 0 {
			p.upHeld = true
		} else {
			p.downHeld = true
		}
		if !isRepeat && p.lastDirection != direction {
			p.speed = keyStartSpeed
		}
		p.lastDirection = direction
		return
	}

	stream := direction == p.lastDir && now.Sub(p.lastPress) < repeatGap
	p.lastPress, p.lastDir = now, direction

	if p.driveMode {
		switch {
		case stream:
			p.driveDir = direction
			p.holdDir, p.holdUntil = direction, now.Add(repeatHold)
		case p.driveDir == -direction:
			p.driveDir = 0 // the opposite key brakes first
		default:
			p.driveDir = direction
			p.holdDir = 0
		}
		return
	}

	if stream {
		p.stepping = false
		p.holdDir, p.holdUntil = direction, now.Add(repeatHold)
		return
	}

	// A new tap. It stacks on a step still in progress in the same direction,
	// so quick taps add up exactly, and it cancels movement the other way.
	base := p.y
	if p.stepping && (p.stepTarget-p.y)*direction > 0 {
		base = p.stepTarget
	}
	p.holdDir = 0
	p.stepping = true
	p.stepTarget = sim.ClampPaddle(base + direction*keyStep)
}

func (p *paddleInput) release(direction float64) {
	// Seeing a release proves that the terminal reports them.
	if !p.releaseEvents {
		p.enableReleaseEvents()
	}
	if direction < 0 {
		p.upHeld = false
	} else {
		p.downHeld = false
	}
}

func (p *paddleInput) enableReleaseEvents() {
	p.releaseEvents = true
	p.stepping = false
	p.holdDir = 0
	p.driveDir = 0
}

func (p *paddleInput) mouse(y float64) {
	p.mouseActive = true
	p.mouseTarget = sim.ClampPaddle(y)
	p.stepping = false
	p.holdDir = 0
	p.driveDir = 0
	p.upHeld, p.downHeld = false, false
}

// stop halts the paddle, for example when the terminal loses focus and key
// releases can no longer be trusted.
func (p *paddleInput) stop() {
	p.upHeld, p.downHeld = false, false
	p.holdDir, p.driveDir = 0, 0
	p.stepping = false
	p.mouseActive = false
	p.speed = 0
	p.v = 0
}

func (p *paddleInput) toggleDrive() {
	if p.releaseEvents {
		return
	}
	p.stop()
	p.driveMode = !p.driveMode
}

// direction resolves what the keys are asking for right now.
func (p *paddleInput) direction(now time.Time) float64 {
	if p.releaseEvents {
		switch {
		case p.upHeld && p.downHeld:
			return p.lastDirection // the latest key wins
		case p.upHeld:
			return -1
		case p.downHeld:
			return 1
		}
		return 0
	}

	if p.holdDir != 0 {
		if now.After(p.holdUntil) {
			if p.driveMode {
				p.driveDir = 0 // the key was held and has now been let go
			}
			p.holdDir = 0
		} else {
			return p.holdDir
		}
	}
	if p.driveMode {
		return p.driveDir
	}
	return 0
}

// seek moves towards an exact position as fast as allowed and lands on it.
func (p *paddleInput) seek(target, dt float64) {
	reach := sim.PaddleMaxSpeed * dt
	delta := target - p.y
	next := target
	if math.Abs(delta) > reach {
		next = p.y + math.Copysign(reach, delta)
	}
	p.v = 0
	if dt > 0 {
		p.v = (next - p.y) / dt
	}
	p.y = next
}

func (p *paddleInput) advance(dt float64, now time.Time) {
	switch {
	case p.mouseActive:
		p.seek(p.mouseTarget, dt)
		return
	case p.stepping:
		p.seek(p.stepTarget, dt)
		if p.y == p.stepTarget {
			p.stepping = false
		}
		return
	}

	direction := p.direction(now)
	if direction == 0 {
		p.v = 0
		p.speed = 0
		return
	}

	if p.speed == 0 || p.v*direction < 0 {
		p.speed = keyStartSpeed
	}
	p.speed = math.Min(sim.PaddleMaxSpeed, p.speed+keyAcceleration*dt)

	next := sim.ClampPaddle(p.y + direction*p.speed*dt)
	p.v = 0
	if dt > 0 {
		p.v = (next - p.y) / dt
	}
	p.y = next
}
