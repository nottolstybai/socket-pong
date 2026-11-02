package main

import (
	"sync"

	tea "charm.land/bubbletea/v2"
	"github.com/gorilla/websocket"
	"github.com/nottolstybai/socket-pong/internal/protocol"
)

const (
	fieldWidth   = 40
	fieldHeight  = 12
	paddleHeight = 4
	leftPaddleX  = 1
)

type serverMsg struct {
	message protocol.Message
}

type networkErrorMsg struct {
	err error
}

type model struct {
	conn *websocket.Conn
	side string

	state protocol.State

	width        int
	height       int
	paddleHeight int

	err error

	writeMu *sync.Mutex
}

func newModel(conn *websocket.Conn, side string, state protocol.State) model {
	return model{
		conn:         conn,
		side:         side,
		state:        state,
		width:        fieldWidth,
		height:       fieldHeight,
		paddleHeight: paddleHeight,
		writeMu:      &sync.Mutex{},
	}
}

func (m model) Init() tea.Cmd {
	return listenToServer(m.conn)
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "up":
			return m, sendMove(m.conn, m.writeMu, protocol.DirectionUp)
		case "down":
			return m, sendMove(m.conn, m.writeMu, protocol.DirectionDown)
		}
	case serverMsg:
		if msg.message.Type == protocol.MessageState && msg.message.State != nil {
			m.state = *msg.message.State
			return m, listenToServer(m.conn)
		}
	case networkErrorMsg:
		m.err = msg.err
	}

	return m, nil
}

func listenToServer(conn *websocket.Conn) tea.Cmd {
	return func() tea.Msg {
		var msg protocol.Message
		if err := conn.ReadJSON(&msg); err != nil {
			return networkErrorMsg{err: err}
		}
		return serverMsg{message: msg}
	}
}

func sendMove(conn *websocket.Conn, writeMu *sync.Mutex, direction string) tea.Cmd {
	writeMu.Lock()
	defer writeMu.Unlock()

	if err := conn.WriteJSON(protocol.Message{
		Type:      protocol.MessageMove,
		Direction: direction,
	}); err != nil {
		return func() tea.Msg {
			return networkErrorMsg{err: err}
		}
	}

	return nil
}
