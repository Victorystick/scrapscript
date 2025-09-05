package eval

import (
	"errors"
	"fmt"
	"maps"

	"github.com/Victorystick/scrapscript/ast"
	"github.com/Victorystick/scrapscript/token"
	"github.com/Victorystick/scrapscript/types"
)

type bail struct{}

var (
	ErrNoMatch      = errors.New("no match found")
	ErrNoFloatMatch = errors.New("cannot match on floats")
)

type matcher struct {
	source *token.Source
	reg    *types.Registry
	vars   Variables
	err    error
}

// Abandons matching returning err as the error.
func (m *matcher) error(err error) {
	m.err = err
	panic(bail{})
}

// Abandons matching, creating an error pointing at the culprit span.
func (m *matcher) errorf(span token.Span, format string, args ...any) {
	m.error(m.source.Error(span, fmt.Sprintf(format, args...)))
}

// Matches an expression onto val returning new bindings.
// It is a match if err is nil.
func Match(source *token.Source, reg *types.Registry, x ast.Expr, val Value) (vars Variables, err error) {
	m := matcher{source, reg, make(Variables), err}

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
		// Ignore _.
		if name == "_" {
			return
		}

		if _, ok := m.vars[name]; ok {
			m.errorf(x.Pos, "cannot bind %s twice", name)
		}
		m.vars[name] = val
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
			m.match(x.Typ, val.value)
			return
		}

	case *ast.RecordExpr:
		if record, ok := val.(Record); ok {
			for tag, x := range x.Entries {
				val, ok := record.values[tag]
				if !ok {
					// TODO: should point to the key, not the value (x).
					m.errorf(x.Span(), "cannot bind to missing key %s", tag)
				}
				// Recursively match further.
				m.match(x, val)
			}

			// If there's a rest expression; clone the record, clear used keys and recurse.
			if x.Rest != nil {
				ref := maps.Clone(m.reg.GetRecord(record.typ))
				rest := maps.Clone(record.values)
				for tag := range x.Entries {
					delete(ref, tag)
					delete(rest, tag)
				}
				m.match(x.Rest, Record{m.reg.Record(ref), rest})
			}

			return
		}

	case *ast.ListExpr:
		if list, ok := val.(List); ok {
			if len(x.Elements) != len(list.elements) {
				m.err = ErrNoMatch
				return
			}

			for index, x := range x.Elements {
				// Recursively match further.
				m.match(x, list.elements[index])
			}
			return
		}

	case *ast.BinaryExpr:
		if x.Op == token.PREPEND {
			if list, ok := val.(List); ok && len(list.elements) > 0 {
				// Match head.
				m.match(x.Left, list.elements[0])
				// Match tail.
				m.match(x.Right, List{list.typ, list.elements[1:]})
				return
			}
		}
		if x.Op == token.APPEND {
			if list, ok := val.(List); ok && len(list.elements) > 0 {
				// Match head.
				m.match(x.Left, List{list.typ, list.elements[:len(list.elements)-1]})
				// Match tail.
				m.match(x.Right, list.elements[0])
				return
			}
		}
		if x.Op == token.CONCAT {
			if list, ok := val.(List); ok {
				if sublist, ok := x.Left.(*ast.ListExpr); ok {
					if len(sublist.Elements) > len(list.elements) {
						m.err = ErrNoMatch
						return
					}

					head, tail := split(list.elements, len(sublist.Elements))

					for index, elem := range head {
						m.match(sublist.Elements[index], elem)
					}
					m.match(x.Right, List{list.typ, tail})
					return
				}

				if sublist, ok := x.Right.(*ast.ListExpr); ok {
					if len(sublist.Elements) > len(list.elements) {
						m.err = ErrNoMatch
						return
					}

					head, tail := split(list.elements, len(list.elements)-len(sublist.Elements))

					m.match(x.Left, List{list.typ, head})
					for index, elem := range tail {
						m.match(sublist.Elements[index], elem)
					}
					return
				}
			}
		}
	}

	m.err = ErrNoMatch
}

func split(list []Value, n int) ([]Value, []Value) {
	return list[:n], list[n:]
}
