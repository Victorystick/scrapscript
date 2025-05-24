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

var errorFormat = fmt.Sprintf(
	"%s: %%s\n\n%s: %%s\n%%s%s",
	color(red, "error"),
	color(yellow, "%5d"),
	color(red, "%s"))

func (e Error) Error() string {
	column := e.Pos.Column - 1
	lineLength := min(len(e.Line)-column, e.Range.Len())
	return fmt.Sprintf(
		errorFormat, e.Msg, e.Pos.Line, e.Line, strings.Repeat(" ", 7+column), strings.Repeat("~", lineLength))
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

func color(color Color, text string) string {
	return fmt.Sprintf("\033[%dm%s\033[m", color, text)
}
