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
func Fprint(w io.Writer, se *ast.SourceExpr, expr ast.Expr) error {
	wr := writer{w: w, se: se}
	return wr.print(expr)
}

func (w *writer) print(expr ast.Expr) error {
	if w.parens {
		w.string("(")
		defer w.string(")")
	}

	switch e := expr.(type) {
	case *ast.Ident:
		return w.token(e.Tok)
	case *ast.Literal:
		return w.token(e.Tok)

	case *ast.BinaryExpr:
		err := w.print(e.Left)
		if err != nil {
			return err
		}
		w.space()
		w.string(e.Op.Op())
		w.space()
		return w.print(e.Right)

	case *ast.FuncExpr:
		err := w.print(e.Arg)
		if err != nil {
			return err
		}
		w.string(" -> ")
		return w.print(e.Body)

	case *ast.CallExpr:
		err := w.print(e.Fn)
		if err != nil {
			return err
		}
		w.space()
		return w.print(e.Arg)

	case ast.MatchFuncExpr:
		for _, fn := range e {
			w.newline()
			w.string("| ")
			err := w.print(fn)
			if err != nil {
				return err
			}
		}
		return nil

	case *ast.WhereExpr:
		// w.indent += 1
		err := w.print(e.Expr)
		if err != nil {
			return err
		}
		w.newline()
		w.string(token.WHERE.Op())
		w.string(" ")
		err = w.token(e.Id.Tok)
		if err != nil {
			return err
		}
		w.string(" =")
		if _, ok := e.Val.(ast.MatchFuncExpr); ok {
			w.indent()
			defer w.dedent()
		} else {
			w.string(" ")
		}
		return w.print(e.Val)
	}

	return fmt.Errorf("unhandled AST node: %#v", expr)
}
