package main

import (
	"testing"

	"github.com/nottolstybai/socket-pong/internal/protocol"
	"github.com/nottolstybai/socket-pong/internal/sim"
)

func stepFor(g *game, seconds float64) {
	for range int(seconds * sim.TickRate) {
		g.step()
	}
}

func startedGame() *game {
	g := newGame()
	g.join(protocol.SideLeft)
	g.join(protocol.SideRight)
	g.start()
	return g
}

func TestServeLaunchesTowardsTheReceiver(t *testing.T) {
	g := startedGame()
	g.serveDir = -1
	if g.phase != protocol.PhaseServe {
		t.Fatalf("phase = %s, want serve", g.phase)
	}
	stepFor(g, sim.ServeSeconds+0.02)
	if g.phase != protocol.PhasePlay || g.ball.VX >= 0 {
		t.Errorf("phase = %s, ball = %+v, want a ball moving left", g.phase, g.ball)
	}
}

func TestPaddleSpeedIsLimited(t *testing.T) {
	g := startedGame()
	g.setPaddle(protocol.SideLeft, 0.1, 0)

	g.step()

	moved := 0.5 - g.left.y
	limit := sim.PaddleMaxSpeed * paddleSlack * tickSeconds
	if moved <= 0 || moved > limit+1e-12 {
		t.Errorf("paddle moved %v in one tick, limit is %v", moved, limit)
	}
}

func TestPaddleInputIsClamped(t *testing.T) {
	g := startedGame()
	g.setPaddle(protocol.SideRight, 99, 99)
	stepFor(g, 1)

	if want := sim.CourtHeight - sim.PaddleHeight/2; g.right.y != want {
		t.Errorf("y = %v, want %v", g.right.y, want)
	}
	if g.right.v != sim.PaddleMaxSpeed {
		t.Errorf("v = %v, want clamped to %v", g.right.v, sim.PaddleMaxSpeed)
	}
}

func TestMissGoesThroughPointPauseToNextServe(t *testing.T) {
	g := startedGame()
	g.serveDir = 1
	g.setPaddle(protocol.SideRight, sim.PaddleHeight/2, 0) // out of the way
	stepFor(g, 0.5)                                        // paddle travels
	stepFor(g, sim.ServeSeconds)

	rally := g.rally
	for range 10 * sim.TickRate {
		g.ball.VY = 0
		g.ball.Y = sim.CourtHeight - 0.1
		g.step()
		if g.phase != protocol.PhasePlay {
			break
		}
	}
	if g.phase != protocol.PhasePoint || g.leftScore != 1 {
		t.Fatalf("phase = %s, left score = %d", g.phase, g.leftScore)
	}
	if g.event != protocol.EventGoal || g.eventSide != protocol.SideLeft {
		t.Errorf("event = %s for %s", g.event, g.eventSide)
	}
	if g.serveDir != 1 {
		t.Errorf("serveDir = %v, the conceding right player should receive", g.serveDir)
	}

	stepFor(g, sim.PointSeconds+0.02)
	if g.phase != protocol.PhaseServe || g.rally == rally {
		t.Errorf("phase = %s, rally %d -> %d", g.phase, rally, g.rally)
	}
	if g.ball != sim.CentreBall() {
		t.Errorf("ball = %+v, want it back in the centre", g.ball)
	}
}

func TestMatchEndsAndRematchNeedsBothPlayers(t *testing.T) {
	g := startedGame()
	g.phase = protocol.PhasePlay
	g.leftScore = sim.WinningScore - 1
	g.ball = sim.Ball{X: sim.CourtWidth - 0.01, Y: 0.95, VX: 2}
	g.setPaddle(protocol.SideRight, sim.PaddleHeight/2, 0)
	stepFor(g, 0.5)

	if g.phase != protocol.PhaseOver || g.winner != protocol.SideLeft {
		t.Fatalf("phase = %s, winner = %q", g.phase, g.winner)
	}

	g.requestRematch(protocol.SideLeft)
	if g.phase != protocol.PhaseOver || !g.rematchLeft {
		t.Errorf("one vote should not restart: phase = %s", g.phase)
	}
	g.requestRematch(protocol.SideRight)
	if g.phase != protocol.PhaseServe || g.leftScore != 0 || g.rematchLeft {
		t.Errorf("rematch did not reset: phase = %s, score = %d", g.phase, g.leftScore)
	}
}

func TestRematchRequestsOutsideGameOverAreIgnored(t *testing.T) {
	g := startedGame()
	g.requestRematch(protocol.SideLeft)
	if g.rematchLeft {
		t.Error("rematch vote accepted during play")
	}
}

func TestDisconnectReturnsToLobby(t *testing.T) {
	g := startedGame()
	g.leftScore = 3
	stepFor(g, sim.ServeSeconds+0.1)

	g.stop()
	stepFor(g, 0.5)

	if g.phase != protocol.PhaseWaiting || g.leftScore != 0 {
		t.Errorf("phase = %s, score = %d", g.phase, g.leftScore)
	}
	if g.ball != sim.CentreBall() {
		t.Errorf("ball = %+v kept moving in the lobby", g.ball)
	}
}

func TestSlowPlayerAlwaysGetsNewestSnapshot(t *testing.T) {
	p := newPlayer(nil, protocol.SideLeft)
	p.pushState(protocol.State{Time: 1})
	p.pushState(protocol.State{Time: 2})
	p.pushState(protocol.State{Time: 3})

	if got := <-p.states; got.Time != 3 {
		t.Errorf("got snapshot from t=%v, want the newest", got.Time)
	}
}
