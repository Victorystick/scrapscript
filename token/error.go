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
	Src   []byte // the source that Line is from
}

// Error returns the message and the line it is about, without color.
func (e Error) Error() string {
	return e.Render(nil)
}

// A Highlighter returns the code of src from start to end in terminal
// colors.
type Highlighter func(src []byte, start, end int) string

// Render returns the message and the line it is about, with the span under
// it marked. If hl is set, it is in terminal colors, with hl coloring the
// line.
func (e Error) Render(hl Highlighter) string {
	paint := func(c Color, text string) string { return text }
	line := e.Line
	column := e.Pos.Column - 1
	if hl != nil {
		paint = colored
		if e.Src != nil {
			start := e.Range.Start - column
			line = hl(e.Src, start, start+len(e.Line))
		}
	}
	lineLength := min(len(e.Line)-column, e.Range.Len())
	return fmt.Sprintf("%s: %s\n\n%s: %s\n%s%s",
		paint(red, "error"), e.Msg,
		paint(yellow, fmt.Sprintf("%5d", e.Pos.Line)), line,
		strings.Repeat(" ", 7+column), paint(red, strings.Repeat("~", lineLength)))
}

// Render renders err with its own Render method, as an Error has, or
// returns err.Error() if it has none.
func Render(err error, hl Highlighter) string {
	if r, ok := err.(interface{ Render(Highlighter) string }); ok {
		return r.Render(hl)
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
