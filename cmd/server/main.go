package main

import (
	"log"
	"net/http"

	"github.com/gorilla/websocket"
	"github.com/nottolstybai/socket-pong/internal/protocol"
)

var room = newRoom()

func wsHandler(w http.ResponseWriter, r *http.Request) {
	upgrader := new(websocket.Upgrader)
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	player, ok := room.Add(conn)
	if !ok {
		log.Printf("INFO: room is full")
		conn.WriteMessage(
			websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.ClosePolicyViolation, "room is full"),
		)
		return
	}
	defer room.Remove(conn)

	if err := player.WriteJSON(&protocol.Message{
		Type: protocol.MessageWelcome,
		Side: player.side,
	}); err != nil {
		log.Printf("INFO: player.WriteJSON(welcome): %v\n", err)
		return
	}

	state := room.Snapshot()
	if err := player.WriteJSON(&protocol.Message{
		Type:  protocol.MessageState,
		State: &state,
	}); err != nil {
		log.Printf("INFO: player.WriteJSON(state): %v\n", err)
		return
	}

	for {
		var msg protocol.Message
		if err := conn.ReadJSON(&msg); err != nil {
			if websocket.IsCloseError(
				err,
				websocket.CloseNormalClosure,
				websocket.CloseGoingAway,
			) {
				log.Println("INFO: client disconnected")
				return
			}
			log.Printf("ERROR: conn.ReadJSON: %v\n", err)
			return
		}

		switch msg.Type {
		case protocol.MessageMove:
			switch msg.Direction {
			case protocol.DirectionUp, protocol.DirectionDown, protocol.DirectionStop:
				log.Printf("INFO: got cmd: %s", msg.Direction)
				state, ok := room.Move(conn, msg.Direction)
				if !ok {
					return
				}

				room.Broadcast(protocol.Message{
					Type:  protocol.MessageState,
					State: &state,
				})
			default:
				return
			}
		default:
			log.Printf("ERROR: unknown message type: %s\n", msg.Type)
			return
		}
	}
}

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", wsHandler)

	server := &http.Server{
		Addr:    "127.0.0.1:8080",
		Handler: mux,
	}

	go room.Run()

	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		panic(err)
	}
}
