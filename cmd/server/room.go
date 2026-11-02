package main

import (
	"log"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/nottolstybai/socket-pong/internal/protocol"
	"github.com/nottolstybai/socket-pong/internal/sim"
)

const (
	writeTimeout = 2 * time.Second
	maxCatchUp   = 12 // physics ticks replayed after a stall, at most
)

// Player owns the outgoing side of a connection. A single goroutine writes to
// the socket, so a slow client can never hold up the physics loop: if it falls
// behind it simply skips to the newest snapshot.
type Player struct {
	conn *websocket.Conn
	side string

	states  chan protocol.State
	control chan protocol.Message
	done    chan struct{}
	once    sync.Once
}

func newPlayer(conn *websocket.Conn, side string) *Player {
	return &Player{
		conn:    conn,
		side:    side,
		states:  make(chan protocol.State, 1),
		control: make(chan protocol.Message, 16),
		done:    make(chan struct{}),
	}
}

func (p *Player) close() {
	p.once.Do(func() { close(p.done) })
}

// writeNow writes directly to the socket. It may only be used before
// writeLoop starts.
func (p *Player) writeNow(msg protocol.Message) error {
	_ = p.conn.SetWriteDeadline(time.Now().Add(writeTimeout))
	return p.conn.WriteJSON(msg)
}

func (p *Player) writeLoop() {
	for {
		var msg protocol.Message
		select {
		case <-p.done:
			return
		case msg = <-p.control:
		case state := <-p.states:
			msg = protocol.Message{Type: protocol.MessageState, State: &state}
		}
		if err := p.writeNow(msg); err != nil {
			log.Printf("player %s: write: %v", p.side, err)
			_ = p.conn.Close() // ends the read loop, which cleans up
			return
		}
	}
}

// pushState replaces any snapshot the writer has not picked up yet.
func (p *Player) pushState(state protocol.State) {
	select {
	case <-p.states:
	default:
	}
	select {
	case p.states <- state:
	default:
	}
}

func (p *Player) pushControl(msg protocol.Message) {
	select {
	case p.control <- msg:
	default: // a dropped pong only delays clock synchronisation
	}
}

// Room hosts the single match on this server.
type Room struct {
	mu      sync.Mutex
	players map[*websocket.Conn]*Player
	game    *game
	started time.Time
}

func newRoom() *Room {
	return &Room{
		players: make(map[*websocket.Conn]*Player, 2),
		game:    newGame(),
		started: time.Now(),
	}
}

func (r *Room) Add(conn *websocket.Conn) (*Player, protocol.State, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if len(r.players) == 2 {
		return nil, protocol.State{}, false
	}

	side := protocol.SideLeft
	for _, player := range r.players {
		if player.side == protocol.SideLeft {
			side = protocol.SideRight
		}
	}

	player := newPlayer(conn, side)
	r.players[conn] = player
	r.game.join(side)
	if len(r.players) == 2 {
		r.game.start()
	}
	return player, r.game.snapshot(), true
}

func (r *Room) Remove(conn *websocket.Conn) {
	r.mu.Lock()
	defer r.mu.Unlock()

	player, ok := r.players[conn]
	if !ok {
		return
	}
	player.close()
	delete(r.players, conn)
	r.game.stop()
}

func (r *Room) SetPaddle(player *Player, y, v float64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.game.setPaddle(player.side, y, v)
}

func (r *Room) Rematch(player *Player) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.game.requestRematch(player.side)
}

// Pong answers a clock-sync ping. The reply carries the server clock that game
// snapshots are stamped with.
func (r *Room) Pong(player *Player, clientTime float64) {
	player.pushControl(protocol.Message{
		Type:       protocol.MessagePong,
		ClientTime: clientTime,
		ServerTime: time.Since(r.started).Seconds(),
	})
}

// Run drives the fixed-step simulation. Ticks are counted against the real
// clock, so game speed stays exact however the OS timer happens to fire.
func (r *Room) Run() {
	ticker := time.NewTicker(time.Second / sim.TickRate)
	defer ticker.Stop()

	ticksPerSnapshot := uint64(sim.TickRate / sim.SnapshotRate)
	var done uint64
	for range ticker.C {
		due := uint64(time.Since(r.started).Seconds() * sim.TickRate)
		if due <= done {
			continue
		}
		if due-done > maxCatchUp {
			done = due - maxCatchUp
		}

		r.mu.Lock()
		// The clock keeps following real time even if ticks were skipped.
		r.game.time = float64(done) * tickSeconds
		send := false
		for done < due {
			r.game.step()
			done++
			send = send || done%ticksPerSnapshot == 0
		}
		if send {
			state := r.game.snapshot()
			for _, player := range r.players {
				player.pushState(state)
			}
		}
		r.mu.Unlock()
	}
}
