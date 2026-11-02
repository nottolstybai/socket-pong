package main

import (
	"flag"
	"log"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
	"github.com/nottolstybai/socket-pong/internal/protocol"
)

const maxMessageBytes = 1024

var room = newRoom()

func wsHandler(w http.ResponseWriter, r *http.Request) {
	upgrader := new(websocket.Upgrader)
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()
	conn.SetReadLimit(maxMessageBytes)

	player, state, ok := room.Add(conn)
	if !ok {
		_ = conn.WriteMessage(
			websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.ClosePolicyViolation, "room is full"),
		)
		return
	}
	defer room.Remove(conn)

	if err := player.writeNow(protocol.Message{Type: protocol.MessageWelcome, Side: player.side}); err != nil {
		return
	}
	if err := player.writeNow(protocol.Message{Type: protocol.MessageState, State: &state}); err != nil {
		return
	}
	go player.writeLoop()

	log.Printf("player joined: %s", player.side)
	defer log.Printf("player left: %s", player.side)

	for {
		var msg protocol.Message
		if err := conn.ReadJSON(&msg); err != nil {
			return
		}

		switch msg.Type {
		case protocol.MessagePaddle:
			room.SetPaddle(player, msg.Y, msg.V)
		case protocol.MessagePing:
			room.Pong(player, msg.ClientTime)
		case protocol.MessageRematch:
			room.Rematch(player)
		}
	}
}

func main() {
	addr := flag.String("addr", "127.0.0.1:8080", "HTTP listen address")
	flag.Parse()

	mux := http.NewServeMux()
	mux.HandleFunc("/ws", wsHandler)

	server := &http.Server{
		Addr:              *addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	go room.Run()
	log.Printf("socket-pong listening on ws://%s/ws", *addr)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}
