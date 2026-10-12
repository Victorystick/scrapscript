package scanner

import (
	"fmt"

	"github.com/Victorystick/scrapscript/token"
)

// ErrCode identifies a scanning error. Its message is only rendered when the
// error is reported.
type ErrCode uint8

const (
	ErrIllegalChar ErrCode = iota + 1
	ErrNonASCII
	ErrInvalidUTF8
	ErrUnterminatedText
	ErrExpectedHex
	ErrBase64Padding
	ErrTooLarge
	ErrBadComment
	ErrBadEscape
	ErrBadHash
	ErrBadReference
)

var errMessages = [...]string{
	ErrIllegalChar:      "illegal character",
	ErrNonASCII:         "non-ASCII character outside a text literal",
	ErrInvalidUTF8:      "illegal UTF-8 encoding",
	ErrUnterminatedText: "string literal not terminated",
	ErrExpectedHex:      "expected hex",
	ErrBase64Padding:    "missing base64 padding",
	ErrTooLarge:         "source is larger than 4 GiB",
	ErrBadComment:       "-- must be followed by a space to start a comment",
	ErrBadEscape:        `invalid escape: use \", \\, \n, \t or \u{HEX}`,
	ErrBadHash:          `expected a hash, $scheme:hex, with lowercase hex digits`,
	ErrBadReference:     `expected a path, $"path", or a hash, $scheme:hex`,
}

func (c ErrCode) String() string {
	if int(c) < len(errMessages) && errMessages[c] != "" {
		return errMessages[c]
	}
	return "unknown error"
}

// Error is a scanning error and the byte span it covers.
type Error struct {
	Code       ErrCode
	Start, End uint32
}

func (e Error) Span() token.Span {
	return token.Span{Start: int(e.Start), End: int(e.End)}
}

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
