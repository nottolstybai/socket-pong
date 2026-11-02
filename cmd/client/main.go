package main

import (
	tea "charm.land/bubbletea/v2"
	"github.com/gorilla/websocket"
	"github.com/nottolstybai/socket-pong/internal/protocol"
)

func main() {
	conn, _, err := websocket.DefaultDialer.Dial(
		"ws://localhost:8080/ws",
		nil,
	)
	if err != nil {
		panic(err)
	}
	defer conn.Close()
	defer func() {
		if err := conn.WriteMessage(
			websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""),
		); err != nil {
			return
		}
	}()

	var welcome protocol.Message
	if err := conn.ReadJSON(&welcome); err != nil {
		panic(err)
	}

	var initial protocol.Message
	if err := conn.ReadJSON(&initial); err != nil {
		panic(err)
	}

	if initial.Type != protocol.MessageState || initial.State == nil {
		panic("not a state or state is nil")
	}

	m := newModel(conn, welcome.Side, *initial.State)

	if _, err := tea.NewProgram(m).Run(); err != nil {
		panic(err)
	}
}
