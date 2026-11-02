package main

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
)

func (m model) View() tea.View {
	field := newField(m.width, m.height)
	drawBorder(field)
	drawPaddle(field, leftPaddleX, m.leftY, m.paddleHeight)
	drawPaddle(field, m.width-2, m.rightY, m.paddleHeight)
	field[m.ballY][m.ballX] = 'o'

	score := fmt.Sprintf("%d : %d\n", m.leftScore, m.rightScore)

	return tea.NewView(score + renderField(field))
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
