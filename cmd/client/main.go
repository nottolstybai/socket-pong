package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/gorilla/websocket"
	"github.com/nottolstybai/socket-pong/internal/protocol"
)

func run() error {
	serverURL := flag.String("server", "ws://127.0.0.1:8080/ws", "WebSocket server URL")
	bot := flag.Bool("bot", false, "play as a computer opponent, without a display")
	flag.Parse()

	conn, _, err := websocket.DefaultDialer.Dial(*serverURL, nil)
	if err != nil {
		return fmt.Errorf("connect to server: %w", err)
	}

	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	var welcome protocol.Message
	if err := conn.ReadJSON(&welcome); err != nil {
		_ = conn.Close()
		return fmt.Errorf("receive welcome: %w", err)
	}
	if welcome.Type != protocol.MessageWelcome {
		_ = conn.Close()
		return fmt.Errorf("unexpected server message %q", welcome.Type)
	}

	var initial protocol.Message
	if err := conn.ReadJSON(&initial); err != nil {
		_ = conn.Close()
		return fmt.Errorf("receive initial state: %w", err)
	}
	if initial.Type != protocol.MessageState || initial.State == nil {
		_ = conn.Close()
		return fmt.Errorf("server did not send an initial state")
	}
	_ = conn.SetReadDeadline(time.Time{})

	l := newLink(conn, *initial.State)
	defer l.close()

	if *bot {
		fmt.Printf("bot playing %s, press Ctrl+C to stop\n", welcome.Side)
		return runBot(l, welcome.Side, *initial.State)
	}

	// Windows Terminal reports key releases through its own input mode, also
	// inside WSL. The mode outlives the program, so it is switched off on the way
	// out. Other terminals use the kitty protocol, which Bubble Tea negotiates.
	if os.Getenv("WT_SESSION") != "" {
		fmt.Print(ansi.SetModeWin32Input)
		defer fmt.Print(ansi.ResetModeWin32Input)
	}

	// The renderer flushes on its own clock. Running it at twice the frame rate
	// means a finished frame waits at most about 8 ms to reach the terminal.
	program := tea.NewProgram(newModel(l, welcome.Side, *initial.State), tea.WithFPS(120))
	_, err = program.Run()
	return err
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "socket-pong:", err)
		os.Exit(1)
	}
}
