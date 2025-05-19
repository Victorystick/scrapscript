package scanner

import (
	"fmt"

	"github.com/Victorystick/scrapscript/token"
)

type ErrorHandler func(err token.Error)

type Errors []*token.Error

func (e *Errors) Add(err token.Error) {
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
