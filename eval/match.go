package eval

import (
	"errors"
	"fmt"
	"maps"

	"github.com/Victorystick/scrapscript/ast"
	"github.com/Victorystick/scrapscript/token"
)

type bail struct{}

var (
	ErrNoMatch      = errors.New("no match found")
	ErrNoFloatMatch = errors.New("cannot match on floats")
)

type matcher struct {
	source *token.Source
	vars   Variables
	err    error
}

// Abandons matching returning err as the error.
func (m *matcher) error(err error) {
	m.err = err
	panic(bail{})
}

// Matches an expression onto val returning new bindings.
// It is a match if err is nil.
func Match(source *token.Source, x ast.Expr, val Value) (vars Variables, err error) {
	m := matcher{source, make(Variables), err}

	defer func() {
		if pnc := recover(); pnc != nil {
			// resume same panic if it's not a bascanner.Errorilout
			if _, ok := pnc.(bail); !ok {
				panic(pnc)
			}
			err = m.err
		}
	}()

	m.match(x, val)
	return m.vars, m.err
}

func (m *matcher) match(x ast.Expr, val Value) {
	switch x := x.(type) {
	case *ast.Ident:
		name := m.source.GetString(x.Pos)
		m.set(name, val)
		return

	case *ast.Literal:
		lit, err := Literal(m.source, x)

		if err != nil {
			m.error(err)
		}

		if _, ok := lit.(Float); ok {
			m.error(ErrNoFloatMatch)
		}

		if !lit.eq(val) {
			m.err = ErrNoMatch
		}
		return

	case *ast.VariantExpr:
		if val, ok := val.(Variant); ok && m.source.GetString(x.Tag.Pos) == val.tag {
			// TODO: handle nil
			// Recursively match further.
			m.match(x.Val, val.value)
			return
		}

	case *ast.RecordExpr:
		if record, ok := val.(Record); ok {
			for tag, x := range x.Entries {
				val, ok := record[tag]
				if !ok {
					m.error(fmt.Errorf("cannot bind to missing key %s", tag))
				}
				// Recursively match further.
				m.match(x, val)
			}

			// If there's a rest expression; clone the record, clear used keys and recurse.
			if x.Rest != nil {
				rest := maps.Clone(record)
				for tag := range x.Entries {
					delete(rest, tag)
				}
				m.match(x.Rest, rest)
			}

			return
		}

	case *ast.ListExpr:
		if list, ok := val.(List); ok {
			if len(x.Elements) != len(list) {
				m.err = ErrNoMatch
				return
			}

			for index, x := range x.Elements {
				// Recursively match further.
				m.match(x, list[index])
			}
			return
		}

	case *ast.BinaryExpr:
		if x.Op == token.CONCAT {

		}
	}

	m.err = ErrNoMatch
}

func (m *matcher) set(name string, val Value) {
	// Ignore _.
	if name == "_" {
		return
	}

	if _, ok := m.vars[name]; ok {
		m.error(fmt.Errorf("cannot bind %s twice", name))
	}
	m.vars[name] = val
}
