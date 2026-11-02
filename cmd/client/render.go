package main

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
)

func (m model) View() tea.View {
	if m.err != nil {
		return tea.NewView(m.err.Error())
	}

	if !m.state.Started {
		return tea.NewView(
			fmt.Sprintf(
				"You are %s\nWaiting for opponent...\nPress q to quit",
				m.side,
			),
		)
	}

	field := newField(m.width, m.height)
	drawBorder(field)
	drawPaddle(field, leftPaddleX, m.state.LeftY, m.paddleHeight)
	drawPaddle(field, m.width-2, m.state.RightY, m.paddleHeight)
	field[m.state.BallY][m.state.BallX] = 'o'

	score := fmt.Sprintf("Score %d : %d    You are %s\n", m.state.LeftScore, m.state.RightScore, m.side)

	var gameOverStr string
	if m.state.GameOver {
		gameOverStr = fmt.Sprintf("%s side won", m.state.Winner)
	}

	return tea.NewView(score + renderField(field) + gameOverStr)
}

func newField(width, height int) [][]rune {
	field := make([][]rune, height)
	for y := range field {
		field[y] = make([]rune, width)
		for x := range field[y] {
			field[y][x] = ' '
		}
	}
	return field
}

func drawBorder(field [][]rune) {
	height := len(field)
	width := len(field[0])

	field[0][0] = '+'
	field[0][width-1] = '+'
	field[height-1][0] = '+'
	field[height-1][width-1] = '+'

	for y := 1; y < height-1; y++ {
		field[y][0] = '|'
		field[y][width-1] = '|'
	}
	for x := 1; x < width-1; x++ {
		field[0][x] = '-'
		field[height-1][x] = '-'
	}
}

func drawPaddle(field [][]rune, x, y, height int) {
	for i := range height {
		field[y+i][x] = '|'
	}
}

func renderField(field [][]rune) string {
	var builder strings.Builder
	for _, row := range field {
		builder.WriteString(string(row))
		builder.WriteByte('\n')
	}
	return builder.String()
}
