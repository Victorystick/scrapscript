package token

import (
	"fmt"
	"strings"
)

type Error struct {
	Pos   Position
	Range Span
	Line  string
	Msg   string
}

// Error returns the message and the line it is about, without color.
func (e Error) Error() string {
	return e.Render(false)
}

// Render returns the message and the line it is about, with the span under
// it marked, in terminal colors if color is set.
func (e Error) Render(color bool) string {
	paint := func(c Color, text string) string { return text }
	if color {
		paint = colored
	}
	column := e.Pos.Column - 1
	lineLength := min(len(e.Line)-column, e.Range.Len())
	return fmt.Sprintf("%s: %s\n\n%s: %s\n%s%s",
		paint(red, "error"), e.Msg,
		paint(yellow, fmt.Sprintf("%5d", e.Pos.Line)), e.Line,
		strings.Repeat(" ", 7+column), paint(red, strings.Repeat("~", lineLength)))
}

// Render renders err with its own Render method, as an Error has, or
// returns err.Error() if it has none.
func Render(err error, color bool) string {
	if r, ok := err.(interface{ Render(bool) string }); ok {
		return r.Render(color)
	}
	return err.Error()
}

type Color rune

const (
	red = 31 + iota
	green
	yellow
	blue
	purple
	teal
	gray
)

func colored(color Color, text string) string {
	return fmt.Sprintf("\033[%dm%s\033[m", color, text)
}
