package main

import (
	"log"
	"maps"
	"slices"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/nottolstybai/socket-pong/internal/protocol"
)

const (
	fieldHeight   = 12
	paddleHeight  = 4
	fieldWidth    = 40
	frameInterval = 80 * time.Millisecond
	winningScore  = 5
)

type Player struct {
	conn    *websocket.Conn
	side    string
	writeMu sync.Mutex
}

func (p *Player) WriteJSON(v any) error {
	p.writeMu.Lock()
	defer p.writeMu.Unlock()

	return p.conn.WriteJSON(v)
}

type Room struct {
	mu         sync.Mutex
	players    map[*websocket.Conn]*Player
	leftY      int
	rightY     int
	ballX      int
	ballY      int
	ballDX     int
	ballDY     int
	leftScore  int
	rightScore int
	gameOver   bool
	winner     string
	started    bool
}

func newRoom() *Room {
	return &Room{
		mu:      sync.Mutex{},
		players: make(map[*websocket.Conn]*Player, 2),
		leftY:   (fieldHeight - paddleHeight) / 2,
		rightY:  (fieldHeight - paddleHeight) / 2,
		ballX:   fieldWidth / 2,
		ballY:   fieldHeight / 2,
		ballDX:  1,
		ballDY:  1,
	}
}

func (r *Room) Add(conn *websocket.Conn) (*Player, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if len(r.players) == 2 {
		return nil, false
	}

	side := protocol.SideLeft
	for _, player := range r.players {
		if player.side == protocol.SideLeft {
			side = protocol.SideRight
		}
	}

	r.players[conn] = &Player{
		conn:    conn,
		side:    side,
		writeMu: sync.Mutex{},
	}

	if len(r.players) == 2 {
		r.resetMatchLocked()
		r.started = true
	}
	return r.players[conn], true
}

func (r *Room) Remove(conn *websocket.Conn) {
	r.mu.Lock()
	defer r.mu.Unlock()

	delete(r.players, conn)
	r.started = false
	r.resetMatchLocked()
}

func (r *Room) Snapshot() protocol.State {
	r.mu.Lock()
	defer r.mu.Unlock()

	return r.snapshotLocked()
}

func (r *Room) Move(conn *websocket.Conn, direction string) (protocol.State, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	player, ok := r.players[conn]
	if !ok {
		return protocol.State{}, false
	}

	if !r.started || r.gameOver {
		return r.snapshotLocked(), true
	}

	switch player.side {
	case protocol.SideLeft:
		r.leftY = nextPaddleY(r.leftY, direction)
	case protocol.SideRight:
		r.rightY = nextPaddleY(r.rightY, direction)
	default:
		return protocol.State{}, false
	}

	return r.snapshotLocked(), true
}

func nextPaddleY(y int, direction string) int {
	nextY := y

	switch direction {
	case protocol.DirectionUp:
		nextY = y - 1
	case protocol.DirectionDown:
		nextY = y + 1
	case protocol.DirectionStop:
		nextY = y
	}

	if nextY < 1 {
		return 1
	}
	if nextY > fieldHeight-paddleHeight-1 {
		return fieldHeight - paddleHeight - 1
	}

	return nextY
}

func (r *Room) Broadcast(msg protocol.Message) {
	r.mu.Lock()
	players := slices.Collect(maps.Values(r.players))
	r.mu.Unlock()

	for _, player := range players {
		if err := player.WriteJSON(msg); err != nil {
			log.Printf("ERROR: player.WriteJSON(msg): %v\n", err)
		}
	}
}

func (r *Room) Step() protocol.State {
	r.mu.Lock()
	defer r.mu.Unlock()

	if !r.started || r.gameOver {
		return r.snapshotLocked()
	}

	nextX := r.ballX + r.ballDX
	nextY := r.ballY + r.ballDY

	if nextY < 1 || nextY > fieldHeight-2 {
		r.ballDY *= -1
		nextY = r.ballY + r.ballDY
	}
	r.ballY = nextY

	if r.ballDX < 0 && nextX == 1 && r.ballY >= r.leftY && r.ballY < r.leftY+paddleHeight {
		r.ballDX *= -1
		nextX = r.ballX + r.ballDX
	}
	if r.ballDX > 0 && nextX == fieldWidth-2 && r.ballY >= r.rightY && r.ballY < r.rightY+paddleHeight {
		r.ballDX *= -1
		nextX = r.ballX + r.ballDX
	}

	if nextX < 1 {
		r.rightScore += 1
		r.resetBall(1)
		if r.rightScore >= winningScore {
			r.gameOver = true
			r.winner = "right"
		}
		return r.snapshotLocked()
	} else if nextX > fieldWidth-2 {
		r.leftScore += 1
		r.resetBall(-1)
		if r.leftScore >= winningScore {
			r.gameOver = true
			r.winner = "left"
		}
		return r.snapshotLocked()
	}

	r.ballX = nextX
	return r.snapshotLocked()
}

func (r *Room) Run() {
	ticker := time.NewTicker(frameInterval)
	defer ticker.Stop()

	for range ticker.C {
		state := r.Step()
		r.Broadcast(protocol.Message{
			Type:  protocol.MessageState,
			State: &state,
		})
	}
}

func (r *Room) resetBall(direction int) {
	r.ballX = fieldWidth / 2
	r.ballY = fieldHeight / 2
	r.ballDX = direction
	r.ballDY = 1
}

func (r *Room) resetMatchLocked() {
	r.leftScore, r.rightScore = 0, 0
	r.gameOver = false
	r.winner = ""
	r.leftY, r.rightY = (fieldHeight-paddleHeight)/2, (fieldHeight-paddleHeight)/2
	r.resetBall(1)
}

func (r *Room) snapshotLocked() protocol.State {
	return protocol.State{
		LeftY:      r.leftY,
		RightY:     r.rightY,
		BallX:      r.ballX,
		BallY:      r.ballY,
		LeftScore:  r.leftScore,
		RightScore: r.rightScore,
		GameOver:   r.gameOver,
		Winner:     r.winner,
		Started:    r.started,
	}
}
