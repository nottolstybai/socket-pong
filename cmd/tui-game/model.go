package main

import (
	"time"

	tea "charm.land/bubbletea/v2"
)

const (
	fieldWidth    = 40
	fieldHeight   = 12
	paddleHeight  = 4
	frameInterval = 80 * time.Millisecond
	leftPaddleX   = 1
	winningScore  = 2
)

type model struct {
	width  int
	height int

	leftY  int
	rightY int

	ballX  int
	ballY  int
	ballDX int
	ballDY int

	paddleHeight int

	leftScore  int
	rightScore int

	gameOver bool
	winner   string
}

func newModel(width, height, paddleHeight int) model {
	return model{
		width:        width,
		height:       height,
		leftY:        height/2 - paddleHeight/2,
		rightY:       height/2 - paddleHeight/2,
		ballX:        width / 2,
		ballY:        height / 2,
		ballDX:       1,
		ballDY:       1,
		paddleHeight: paddleHeight,
	}
}

func (m model) Init() tea.Cmd {
	return tick()
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "w":
			m.leftY = movePaddle(m.leftY, -1, m.height, m.paddleHeight)
		case "s":
			m.leftY = movePaddle(m.leftY, 1, m.height, m.paddleHeight)
		case "up":
			m.rightY = movePaddle(m.rightY, -1, m.height, m.paddleHeight)
		case "down":
			m.rightY = movePaddle(m.rightY, 1, m.height, m.paddleHeight)
		}
	case tickMsg:
		m.moveBall()
		if m.gameOver {
			return m, nil
		}
		return m, tick()
	}

	return m, nil
}

func movePaddle(y, delta, fieldHeight, paddleHeight int) int {
	nextY := y + delta
	if nextY < 1 || nextY+paddleHeight > fieldHeight-1 {
		return y
	}
	return nextY
}

func (m *model) moveBall() {
	nextX := m.ballX + m.ballDX
	nextY := m.ballY + m.ballDY

	if m.ballDX < 0 && nextX == leftPaddleX && m.ballY >= m.leftY && m.ballY < m.leftY+m.paddleHeight {
		m.ballDX *= -1
		m.ballX += m.ballDX
		m.ballY += m.ballDY
		return
	}

	if m.ballDX > 0 && nextX == m.width-2 && m.ballY >= m.rightY && m.ballY < m.rightY+m.paddleHeight {
		m.ballDX *= -1
		m.ballX += m.ballDX
		m.ballY += m.ballDY
		return
	}

	if nextY >= 1 && nextY <= m.height-2 {
		m.ballY = nextY
	} else {
		m.ballDY *= -1
		m.ballY += m.ballDY
	}

	if nextX >= 1 && nextX <= m.width-2 {
		m.ballX = nextX
	} else if nextX < 1 {
		m.rightScore += 1
		m.resetBall(1)
		if m.rightScore >= winningScore {
			m.gameOver = true
			m.winner = "right"
		}
		return
	} else if nextX > m.width-2 {
		m.leftScore += 1
		m.resetBall(-1)
		if m.leftScore >= winningScore {
			m.gameOver = true
			m.winner = "left"
		}
		return
	} else {
		m.ballX = nextX
	}
}

type tickMsg time.Time

func tick() tea.Cmd {
	return tea.Tick(frameInterval, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func (m *model) resetBall(direction int) {
	m.ballX = m.width / 2
	m.ballY = m.height / 2
	m.ballDX = direction
	m.ballDY = 1
}
