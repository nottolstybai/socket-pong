package main

import (
	"math"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/nottolstybai/socket-pong/internal/protocol"
	"github.com/nottolstybai/socket-pong/internal/sim"
)

const (
	frameInterval = time.Second / 60
	maxFrameStep  = 0.05 // seconds, longer pauses do not fast-forward effects
	minKeyRedraw  = 4 * time.Millisecond
)

type frameMsg time.Time

// model is the Bubble Tea program. Everything is advanced and drawn in frame.
// View only returns the finished frame: Bubble Tea calls it after every message,
// so doing the work there would make drawing cost scale with input and network
// traffic and hold up the event loop.
type model struct {
	link   *link
	ticker *time.Ticker

	input paddleInput
	world *world
	fx    *effects
	scene *scene

	sentY, sentV float64

	lastFrame time.Time
	clockTime float64
	content   string
	mouseRow  int
}

func newModel(l *link, side string, initial protocol.State) *model {
	simSide := sideFromName(side)
	fx := &effects{}
	world := newWorld(simSide, initial, fx)

	m := &model{
		link:     l,
		ticker:   time.NewTicker(frameInterval),
		world:    world,
		fx:       fx,
		mouseRow: -1,
	}
	m.input = newPaddleInput(world.own.Y)
	m.sentY = world.own.Y
	m.scene = &scene{canvas: newCanvas(), world: world, fx: fx, input: &m.input}
	m.scene.resize(80, 24)
	return m
}

func (m *model) Init() tea.Cmd {
	return m.nextFrame()
}

// nextFrame waits on a shared ticker. Unlike a fresh timer per frame it does not
// accumulate drift, so frames stay evenly spaced.
func (m *model) nextFrame() tea.Cmd {
	ticks := m.ticker.C
	return func() tea.Msg {
		return frameMsg(<-ticks)
	}
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	now := time.Now()

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.scene.resize(msg.Width, msg.Height)
		m.frame(now)

	case tea.ColorProfileMsg:
		m.scene.setLimitedColor(msg.Profile != colorprofile.TrueColor)

	case tea.KeyboardEnhancementsMsg:
		if msg.SupportsEventTypes() {
			m.input.enableReleaseEvents()
		}

	case tea.KeyPressMsg:
		key := msg.Key()
		switch actionFor(key) {
		case actionQuit:
			return m, tea.Quit
		case actionUp:
			m.input.press(-1, key.IsRepeat, now)
			m.redrawAfterKey(now)
		case actionDown:
			m.input.press(1, key.IsRepeat, now)
			m.redrawAfterKey(now)
		case actionRematch:
			if m.world.state.Phase == protocol.PhaseOver {
				m.link.sendRematch()
			}
		case actionToggleDrive:
			m.input.toggleDrive()
		}

	case tea.KeyReleaseMsg:
		switch actionFor(msg.Key()) {
		case actionUp:
			m.input.release(-1)
		case actionDown:
			m.input.release(1)
		}

	case tea.MouseMotionMsg:
		mouse := msg.Mouse()
		if mouse.Y != m.mouseRow {
			m.mouseRow = mouse.Y
			layout := m.scene.layout
			if layout.scale > 0 {
				centre := float64(mouse.Y*2 + 1)
				m.input.mouse((centre - layout.originY) / layout.scale)
			}
		}

	case tea.BlurMsg:
		m.input.stop()

	case frameMsg:
		m.frame(now)
		return m, m.nextFrame()
	}

	return m, nil
}

// redrawAfterKey shows the paddle's reaction now rather than up to a frame later.
func (m *model) redrawAfterKey(now time.Time) {
	if now.Sub(m.lastFrame) >= minKeyRedraw {
		m.frame(now)
	}
}

func (m *model) frame(now time.Time) {
	dt := frameInterval.Seconds()
	if !m.lastFrame.IsZero() {
		dt = math.Min(now.Sub(m.lastFrame).Seconds(), maxFrameStep)
	}
	m.lastFrame = now
	m.clockTime += dt

	states, serverNow, rtt, err := m.link.poll(now, dt)
	m.input.advance(dt, now)
	if m.input.y != m.sentY || m.input.v != m.sentV {
		m.link.sendPaddle(m.input.y, m.input.v)
		m.sentY, m.sentV = m.input.y, m.input.v
	}

	m.world.update(serverNow, dt, sim.Paddle{Y: m.input.y, V: m.input.v}, states)
	m.fx.advance(dt, m.clockTime)

	m.scene.time = m.clockTime
	m.scene.rtt = rtt
	m.scene.err = err
	m.content = m.scene.render()
}

func (m *model) View() tea.View {
	view := tea.NewView(m.content)
	view.AltScreen = true
	view.ReportFocus = true
	view.MouseMode = tea.MouseModeAllMotion
	view.KeyboardEnhancements.ReportEventTypes = true
	view.WindowTitle = "Socket Pong"
	return view
}
