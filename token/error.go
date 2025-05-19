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

func (e Error) Error() string {
	column := e.Pos.Column - 1
	lineLength := min(len(e.Line)-column, e.Range.Len())
	return fmt.Sprintf(`scan error: %s
%6d: %s
%s%s`, e.Msg, e.Pos.Line, e.Line, strings.Repeat(" ", 8+column), strings.Repeat("^", lineLength))
}
