# socket-pong

A two-player Pong game in the terminal.

Built to learn Bubble Tea and WebSocket through a small multiplayer project. The server handles the game logic, while clients send commands and render the game state.

```text
Score 0 : 0      You are left
+----------------------------------------+
| |                                    | |
| |                                    | |
| |                 o                  | |
| |                                    | |
+----------------------------------------+
```

```bash
# Start the server
go run ./cmd/server

# Connect two players, each in a separate terminal
go run ./cmd/client
```

The match starts when both players are connected. Use `↑`/`↓` to move and `q` to quit.
