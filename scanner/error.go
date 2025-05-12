package scanner

import (
	"fmt"
)

type Error struct {
	Pos Position
	Msg string
}

func (e Error) Error() string {
	return fmt.Sprintf("%v: %s", e.Pos, e.Msg)
}

type ErrorHandler func(err Error)

type Errors []*Error

func (e *Errors) Add(err Error) {
	*e = append(*e, &err)
}

func (e Errors) Error() string {
	switch len(e) {
	case 0:
		return "no errors"
	case 1:
		return e[0].Error()
	}
	return fmt.Sprintf("%s (and %d more errors)", e[0], len(e)-1)
}

func (e Errors) Err() error {
	if len(e) == 0 {
		return nil
	}
	return e
}
