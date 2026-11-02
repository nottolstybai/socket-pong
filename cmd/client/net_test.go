package main

import (
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/nottolstybai/socket-pong/internal/protocol"
)

func TestClockTrustsTheLeastDelayedSample(t *testing.T) {
	var c clock
	const offset = 5.0

	// Fast sample, then a slow one whose reply queued asymmetrically.
	c.observe(1.0, 1.0+0.002+offset, 1.004)
	c.observe(2.0, 2.0+0.09+offset, 2.1)

	if math.Abs(c.target-offset) > 0.001 {
		t.Errorf("target = %v, want about %v", c.target, offset)
	}
}

func TestClockSnapsOnceThenSlewsGently(t *testing.T) {
	var c clock
	c.observe(1, 11.001, 1.002) // offset 10
	if math.Abs(c.offset-10) > 1e-9 {
		t.Fatalf("first sample should snap, offset = %v", c.offset)
	}

	// The clock estimate improves slightly: no sudden jump in ball position.
	c.samples = nil
	before := c.offset
	c.observe(2, 12.021, 2.002) // offset 10.02
	if c.offset != before {
		t.Errorf("small correction applied at once: %v -> %v", before, c.offset)
	}
	c.slew(1.0 / 60)
	if step := c.offset - before; step <= 0 || step > clockMaxSlew/60+1e-12 {
		t.Errorf("slewed by %v in one frame", step)
	}
}

// Run under -race: quitting while the writer is busy used to race on the socket.
func TestCloseIsSafeWhileWriting(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := new(websocket.Upgrader).Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}))
	defer server.Close()

	url := "ws" + strings.TrimPrefix(server.URL, "http")
	for range 20 {
		conn, _, err := websocket.DefaultDialer.Dial(url, nil)
		if err != nil {
			t.Fatal(err)
		}
		l := newLink(conn, protocol.State{})
		for i := range 50 {
			l.sendPaddle(float64(i), 0)
		}
		time.Sleep(time.Millisecond)
		l.close()
		l.close() // closing twice is fine
	}
}
