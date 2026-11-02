package protocol

const (
	MessagePaddle  = "paddle"
	MessageRematch = "rematch"
	MessagePing    = "ping"
	MessagePong    = "pong"
	MessageWelcome = "welcome"
	MessageState   = "state"

	SideLeft  = "left"
	SideRight = "right"

	PhaseWaiting = "waiting" // fewer than two players connected
	PhaseServe   = "serve"   // ball in the centre, counting down
	PhasePlay    = "play"    // ball in flight
	PhasePoint   = "point"   // short pause after a goal
	PhaseOver    = "over"    // match finished

	EventPaddle = "paddle"
	EventWall   = "wall"
	EventGoal   = "goal"
)

// Message is the single JSON envelope used in both directions.
type Message struct {
	Type string `json:"type"`
	Side string `json:"side,omitempty"`

	// Y and V report the sender's paddle (MessagePaddle).
	Y float64 `json:"y,omitempty"`
	V float64 `json:"v,omitempty"`

	// ClientTime and ServerTime synchronise clocks (MessagePing, MessagePong).
	ClientTime float64 `json:"clientTime,omitempty"`
	ServerTime float64 `json:"serverTime,omitempty"`

	State *State `json:"state,omitempty"`
}

// State is a full snapshot of the match at server time Time, in seconds.
type State struct {
	Time float64 `json:"time"`

	Phase     string  `json:"phase"`
	PhaseLeft float64 `json:"phaseLeft"`
	ServeDir  float64 `json:"serveDir"`

	// Rally changes whenever the ball teleports, so clients know not to blend
	// the old position into the new one.
	Rally uint64 `json:"rally"`
	Hits  int    `json:"hits"`

	BallX  float64 `json:"ballX"`
	BallY  float64 `json:"ballY"`
	BallVX float64 `json:"ballVX"`
	BallVY float64 `json:"ballVY"`

	LeftY  float64 `json:"leftY"`
	LeftV  float64 `json:"leftV"`
	RightY float64 `json:"rightY"`
	RightV float64 `json:"rightV"`

	LeftScore  int    `json:"leftScore"`
	RightScore int    `json:"rightScore"`
	Winner     string `json:"winner,omitempty"`

	RematchLeft  bool `json:"rematchLeft,omitempty"`
	RematchRight bool `json:"rematchRight,omitempty"`

	EventID   uint64  `json:"eventID"`
	Event     string  `json:"event,omitempty"`
	EventSide string  `json:"eventSide,omitempty"`
	EventX    float64 `json:"eventX"`
	EventY    float64 `json:"eventY"`
}
