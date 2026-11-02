package protocol

const (
	MessageMove    = "move"
	MessageWelcome = "welcome"
	MessageState   = "state"

	SideLeft  = "left"
	SideRight = "right"

	DirectionUp   = "up"
	DirectionDown = "down"
	DirectionStop = "stop"
)

type Message struct {
	Type      string `json:"type"`
	Direction string `json:"direction,omitempty"`
	Side      string `json:"side,omitempty"`
	State     *State `json:"state,omitempty"`
}

type State struct {
	LeftY      int    `json:"leftY"`
	RightY     int    `json:"rightY"`
	BallX      int    `json:"ballX"`
	BallY      int    `json:"ballY"`
	LeftScore  int    `json:"leftScore"`
	RightScore int    `json:"rightScore"`
	GameOver   bool   `json:"gameOver"`
	Winner     string `json:"winner,omitempty"`
	Started    bool   `json:"started"`
}
