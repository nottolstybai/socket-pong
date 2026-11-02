package main

import (
	"math"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/nottolstybai/socket-pong/internal/protocol"
)

const (
	writeTimeout      = 2 * time.Second
	fastPingCount     = 8
	fastPingInterval  = 60 * time.Millisecond
	slowPingInterval  = 500 * time.Millisecond
	clockSamples      = 12
	clockSnapDistance = 0.1  // seconds; further off than this and the clock jumps
	clockMaxSlew      = 0.02 // seconds of correction per second
)

// clock estimates the server's clock. It keeps the sample with the lowest round
// trip, because that one was disturbed least by queueing, and slews towards it
// slowly so that the ball's speed on screen is not visibly affected.
type clock struct {
	offset  float64
	target  float64
	samples []clockSample
	synced  bool
	rtt     float64
}

type clockSample struct {
	rtt, offset float64
}

func (c *clock) observe(sentAt, serverTime, receivedAt float64) {
	rtt := receivedAt - sentAt
	if rtt < 0 {
		return
	}
	c.samples = append(c.samples, clockSample{rtt: rtt, offset: serverTime + rtt/2 - receivedAt})
	if len(c.samples) > clockSamples {
		c.samples = c.samples[1:]
	}
	best := c.samples[0]
	for _, s := range c.samples {
		if s.rtt < best.rtt {
			best = s
		}
	}
	c.target = best.offset
	c.rtt = rtt
	if !c.synced || math.Abs(c.target-c.offset) > clockSnapDistance {
		c.offset = c.target
		c.synced = true
	}
}

func (c *clock) slew(dt float64) {
	limit := dt * clockMaxSlew
	c.offset += math.Max(-limit, math.Min(limit, c.target-c.offset))
}

// link owns the connection. Reads and writes happen on their own goroutines, so
// messages are stamped the moment they arrive and sent strictly in order, and
// network traffic never competes with drawing on the Bubble Tea event loop.
type link struct {
	conn    *websocket.Conn
	started time.Time

	mu     sync.Mutex
	states []protocol.State
	clock  clock
	err    error

	paddle  chan protocol.Message
	control chan protocol.Message
	done    chan struct{}
	once    sync.Once
}

func newLink(conn *websocket.Conn, initial protocol.State) *link {
	l := &link{
		conn:    conn,
		started: time.Now(),
		paddle:  make(chan protocol.Message, 1),
		control: make(chan protocol.Message, 16),
		done:    make(chan struct{}),
	}
	// A rough start until the first ping comes back.
	l.clock.offset = initial.Time
	l.clock.target = initial.Time
	go l.readLoop()
	go l.writeLoop()
	return l
}

func (l *link) localTime(t time.Time) float64 {
	return t.Sub(l.started).Seconds()
}

func (l *link) readLoop() {
	for {
		var msg protocol.Message
		if err := l.conn.ReadJSON(&msg); err != nil {
			l.mu.Lock()
			l.err = err
			l.mu.Unlock()
			return
		}
		received := l.localTime(time.Now())

		l.mu.Lock()
		switch msg.Type {
		case protocol.MessageState:
			if msg.State != nil {
				l.states = append(l.states, *msg.State)
				if len(l.states) > 32 {
					l.states = l.states[len(l.states)-32:]
				}
			}
		case protocol.MessagePong:
			l.clock.observe(msg.ClientTime, msg.ServerTime, received)
		}
		l.mu.Unlock()
	}
}

func (l *link) writeLoop() {
	pingTimer := time.NewTimer(0)
	defer pingTimer.Stop()
	pings := 0

	for {
		var msg protocol.Message
		select {
		case <-l.done:
			return
		case <-pingTimer.C:
			msg = protocol.Message{Type: protocol.MessagePing, ClientTime: l.localTime(time.Now())}
			pings++
			if pings < fastPingCount {
				pingTimer.Reset(fastPingInterval)
			} else {
				pingTimer.Reset(slowPingInterval)
			}
		case msg = <-l.control:
		case msg = <-l.paddle:
		}

		_ = l.conn.SetWriteDeadline(time.Now().Add(writeTimeout))
		if err := l.conn.WriteJSON(msg); err != nil {
			l.mu.Lock()
			if l.err == nil {
				l.err = err
			}
			l.mu.Unlock()
			return
		}
	}
}

// sendPaddle queues the paddle position, replacing one the writer has not sent
// yet: only the newest position matters.
func (l *link) sendPaddle(y, v float64) {
	msg := protocol.Message{Type: protocol.MessagePaddle, Y: y, V: v}
	select {
	case <-l.paddle:
	default:
	}
	select {
	case l.paddle <- msg:
	default:
	}
}

func (l *link) sendRematch() {
	select {
	case l.control <- protocol.Message{Type: protocol.MessageRematch}:
	default:
	}
}

// poll returns the snapshots received since the last call, the server time for
// t, the latest round trip and any connection error.
func (l *link) poll(t time.Time, dt float64) (states []protocol.State, serverNow, rtt float64, err error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	states = l.states
	l.states = nil
	l.clock.slew(dt)
	return states, l.localTime(t) + l.clock.offset, l.clock.rtt, l.err
}

func (l *link) close() {
	l.once.Do(func() {
		close(l.done)
		// WriteControl is the one write allowed alongside writeLoop.
		_ = l.conn.WriteControl(
			websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""),
			time.Now().Add(250*time.Millisecond),
		)
		_ = l.conn.Close()
	})
}
