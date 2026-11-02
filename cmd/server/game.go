package main

import (
	"math"
	"math/rand/v2"

	"github.com/nottolstybai/socket-pong/internal/protocol"
	"github.com/nottolstybai/socket-pong/internal/sim"
)

const tickSeconds = 1.0 / sim.TickRate

// paddleSlack lets a paddle catch up after a network hiccup while still
// stopping a modified client from teleporting.
const paddleSlack = 1.5

type paddleState struct {
	y, v    float64
	targetY float64
	targetV float64
}

func (p *paddleState) centre() {
	*p = paddleState{y: sim.CourtHeight / 2, targetY: sim.CourtHeight / 2}
}

func (p *paddleState) step() {
	reach := sim.PaddleMaxSpeed * paddleSlack * tickSeconds
	p.y += math.Max(-reach, math.Min(reach, p.targetY-p.y))
	p.v = p.targetV
}

type game struct {
	time float64

	phase     string
	phaseLeft float64
	serveDir  float64

	ball  sim.Ball
	rally uint64
	hits  int

	left, right paddleState

	leftScore  int
	rightScore int
	winner     string

	rematchLeft  bool
	rematchRight bool

	eventID   uint64
	event     string
	eventSide string
	eventX    float64
	eventY    float64

	eventBuffer []sim.Event
}

func newGame() *game {
	g := &game{phase: protocol.PhaseWaiting, ball: sim.CentreBall()}
	g.left.centre()
	g.right.centre()
	return g
}

// start begins a fresh match once both players are connected.
func (g *game) start() {
	g.serveDir = 1
	if rand.IntN(2) == 0 {
		g.serveDir = -1
	}
	g.resetMatch()
}

// stop returns to the lobby when a player disconnects.
func (g *game) stop() {
	g.phase = protocol.PhaseWaiting
	g.leftScore, g.rightScore = 0, 0
	g.winner = ""
	g.rematchLeft, g.rematchRight = false, false
	g.placeBall()
}

// join puts a newly connected player's paddle in the middle. Afterwards the
// paddle only ever moves on that player's input, so client prediction is never
// contradicted by a server-side reset.
func (g *game) join(side string) {
	if side == protocol.SideLeft {
		g.left.centre()
	} else {
		g.right.centre()
	}
}

func (g *game) resetMatch() {
	g.leftScore, g.rightScore = 0, 0
	g.winner = ""
	g.rematchLeft, g.rematchRight = false, false
	g.beginServe()
}

func (g *game) setPaddle(side string, y, v float64) {
	y = sim.ClampPaddle(y)
	v = math.Max(-sim.PaddleMaxSpeed, math.Min(sim.PaddleMaxSpeed, v))
	if side == protocol.SideLeft {
		g.left.targetY, g.left.targetV = y, v
	} else {
		g.right.targetY, g.right.targetV = y, v
	}
}

func (g *game) requestRematch(side string) {
	if g.phase != protocol.PhaseOver {
		return
	}
	if side == protocol.SideLeft {
		g.rematchLeft = true
	} else {
		g.rematchRight = true
	}
	if g.rematchLeft && g.rematchRight {
		g.resetMatch()
	}
}

// step advances the simulation by exactly one tick.
func (g *game) step() {
	g.time += tickSeconds
	g.left.step()
	g.right.step()

	switch g.phase {
	case protocol.PhaseServe:
		g.phaseLeft -= tickSeconds
		if g.phaseLeft <= 0 {
			g.launchBall()
		}
	case protocol.PhasePoint:
		g.phaseLeft -= tickSeconds
		if g.phaseLeft <= 0 {
			g.beginServe()
		}
	case protocol.PhasePlay:
		g.stepBall()
	}
}

func (g *game) stepBall() {
	g.eventBuffer = sim.Advance(&g.ball, tickSeconds,
		sim.Paddle{Y: g.left.y, V: g.left.v},
		sim.Paddle{Y: g.right.y, V: g.right.v},
		g.eventBuffer[:0])

	for _, event := range g.eventBuffer {
		switch event.Kind {
		case sim.EventWall:
			g.emit(protocol.EventWall, "", event.X, event.Y)
		case sim.EventPaddle:
			g.hits++
			g.emit(protocol.EventPaddle, sideName(event.Side), event.X, event.Y)
		case sim.EventGoal:
			g.score(event)
		}
	}
}

func (g *game) score(event sim.Event) {
	scorer := sideName(event.Side)
	g.emit(protocol.EventGoal, scorer, event.X, event.Y)

	if event.Side == sim.Left {
		g.leftScore++
		g.serveDir = 1 // the player who conceded receives the serve
	} else {
		g.rightScore++
		g.serveDir = -1
	}

	if g.leftScore >= sim.WinningScore || g.rightScore >= sim.WinningScore {
		g.phase = protocol.PhaseOver
		g.phaseLeft = 0
		g.winner = scorer
		return
	}
	g.phase = protocol.PhasePoint
	g.phaseLeft = sim.PointSeconds
}

func (g *game) placeBall() {
	g.ball = sim.CentreBall()
	g.rally++
	g.hits = 0
}

func (g *game) beginServe() {
	g.placeBall()
	g.phase = protocol.PhaseServe
	g.phaseLeft = sim.ServeSeconds
}

func (g *game) launchBall() {
	g.ball.VX, g.ball.VY = sim.Serve(g.serveDir, rand.Float64()*2-1)
	g.phase = protocol.PhasePlay
	g.phaseLeft = 0
}

func (g *game) emit(kind, side string, x, y float64) {
	g.eventID++
	g.event = kind
	g.eventSide = side
	g.eventX = x
	g.eventY = y
}

func (g *game) snapshot() protocol.State {
	return protocol.State{
		Time:         g.time,
		Phase:        g.phase,
		PhaseLeft:    g.phaseLeft,
		ServeDir:     g.serveDir,
		Rally:        g.rally,
		Hits:         g.hits,
		BallX:        g.ball.X,
		BallY:        g.ball.Y,
		BallVX:       g.ball.VX,
		BallVY:       g.ball.VY,
		LeftY:        g.left.y,
		LeftV:        g.left.v,
		RightY:       g.right.y,
		RightV:       g.right.v,
		LeftScore:    g.leftScore,
		RightScore:   g.rightScore,
		Winner:       g.winner,
		RematchLeft:  g.rematchLeft,
		RematchRight: g.rematchRight,
		EventID:      g.eventID,
		Event:        g.event,
		EventSide:    g.eventSide,
		EventX:       g.eventX,
		EventY:       g.eventY,
	}
}

func sideName(side sim.Side) string {
	if side == sim.Left {
		return protocol.SideLeft
	}
	return protocol.SideRight
}
