package printer

import (
	"fmt"
	"io"
	"strings"

	"github.com/Victorystick/scrapscript/ast"
	"github.com/Victorystick/scrapscript/token"
)

type writer struct {
	w  io.Writer
	se *ast.SourceExpr

	spaces int
	parens bool // for debugging
}

func (w *writer) string(s string) error {
	_, err := io.WriteString(w.w, s)
	return err
}

func (w *writer) indent() {
	w.spaces += 1
}

func (w *writer) dedent() {
	w.spaces -= 1
}

func (w *writer) newline() error {
	w.string("\n")
	return w.string(strings.Repeat("  ", w.spaces))
}

func (w *writer) token(t ast.Tok) error {
	return w.string(w.se.TokenText(t))
}

func (w *writer) space() error {
	_, err := w.w.Write([]byte{' '})
	return err
}

// Fprint prints expr, which is se.Expr or one of its descendants.
func Fprint(w io.Writer, se *ast.SourceExpr, expr ast.Node) error {
	wr := writer{w: w, se: se}
	return wr.print(expr)
}

func (w *writer) print(x ast.Node) error {
	if w.parens {
		w.string("(")
		defer w.string(")")
	}

	se := w.se
	switch se.Kind(x) {
	case ast.Ident, ast.Literal:
		return w.token(se.Main(x))

	case ast.Binary:
		op, left, right := se.Binary(x)
		err := w.print(left)
		if err != nil {
			return err
		}
		w.space()
		w.string(op.Op())
		w.space()
		return w.print(right)

	case ast.Func:
		arg, body := se.Func(x)
		err := w.print(arg)
		if err != nil {
			return err
		}
		w.string(" -> ")
		return w.print(body)

	case ast.Call:
		fn, arg := se.Call(x)
		err := w.print(fn)
		if err != nil {
			return err
		}
		w.space()
		return w.print(arg)

	case ast.MatchFunc:
		for _, fn := range se.Arms(x) {
			w.newline()
			w.string("| ")
			err := w.print(fn)
			if err != nil {
				return err
			}
		}
		return nil

	case ast.Where:
		body, id, _, val := se.Where(x)
		// w.indent += 1
		err := w.print(body)
		if err != nil {
			return err
		}
		w.newline()
		w.string(token.WHERE.Op())
		w.string(" ")
		err = w.token(id)
		if err != nil {
			return err
		}
		w.string(" =")
		if se.Kind(val) == ast.MatchFunc {
			w.indent()
			defer w.dedent()
		} else {
			w.string(" ")
		}
		return w.print(val)
	}

	return fmt.Errorf("unhandled AST node: %s", se.Kind(x))
}
