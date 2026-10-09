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
	src  *ast.SourceExpr
	reg  *types.Registry
	vars Variables
	err  error
}

// Abandons matching returning err as the error.
func (m *matcher) error(err error) {
	m.err = err
	panic(bail{})
}

// Abandons matching, creating an error pointing at the culprit span.
func (m *matcher) errorf(n ast.Node, format string, args ...any) {
	m.error(m.src.Error(n, fmt.Sprintf(format, args...)))
}

// Matches an expression onto val returning new bindings.
// It is a match if err is nil.
func Match(src *ast.SourceExpr, reg *types.Registry, x ast.Node, val Value) (vars Variables, err error) {
	m := matcher{src, reg, make(Variables), err}

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

func (m *matcher) match(x ast.Node, val Value) {
	switch m.src.Kind(x) {
	case ast.Ident:
		name := m.src.TokenText(m.src.Main(x))
		// Ignore _.
		if name == "_" {
			return
		}

		if _, ok := m.vars[name]; ok {
			m.errorf(x, "cannot bind %s twice", name)
		}
		m.vars[name] = val
		return

	case ast.Literal:
		lit, err := Literal(m.src, x)

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

	case ast.Variant:
		tag, typ := m.src.Variant(x)
		if val, ok := val.(Variant); ok && m.src.TokenText(tag) == val.tag {
			// TODO: handle nil
			// Recursively match further.
			m.match(typ, val.value)
			return
		}

	case ast.Record:
		if record, ok := val.(Record); ok {
			rest, fields := m.src.Record(x)
			for i := range fields.Len() {
				tag, x := m.src.TokenText(fields.Key(i)), fields.Value(i)
				val, ok := record.values[tag]
				if !ok {
					// TODO: should point to the key, not the value (x).
					m.errorf(x, "cannot bind to missing key %s", tag)
				}
				// Recursively match further.
				m.match(x, val)
			}

			// If there's a rest expression; clone the record, clear used keys and recurse.
			if rest != ast.None {
				ref := maps.Clone(m.reg.GetRecord(record.typ))
				restVals := maps.Clone(record.values)
				for i := range fields.Len() {
					tag := m.src.TokenText(fields.Key(i))
					delete(ref, tag)
					delete(restVals, tag)
				}
				m.match(rest, Record{m.reg.Record(ref), restVals})
			}

			return
		}

	case ast.List:
		if list, ok := val.(List); ok {
			elems := m.src.List(x)
			if len(elems) != len(list.elements) {
				m.err = ErrNoMatch
				return
			}

			for index, x := range elems {
				// Recursively match further.
				m.match(x, list.elements[index])
			}
			return
		}

	case ast.Binary:
		op, left, right := m.src.Binary(x)
		if op == token.PREPEND {
			if list, ok := val.(List); ok && len(list.elements) > 0 {
				// Match head.
				m.match(left, list.elements[0])
				// Match tail.
				m.match(right, List{list.typ, list.elements[1:]})
				return
			}
		}
		if op == token.APPEND {
			if list, ok := val.(List); ok && len(list.elements) > 0 {
				// Match head.
				m.match(left, List{list.typ, list.elements[:len(list.elements)-1]})
				// Match tail.
				m.match(right, list.elements[0])
				return
			}
		}
		if op == token.CONCAT {
			if list, ok := val.(List); ok {
				if m.src.Kind(left) == ast.List {
					sublist := m.src.List(left)
					if len(sublist) > len(list.elements) {
						m.err = ErrNoMatch
						return
					}

					head, tail := split(list.elements, len(sublist))

					for index, elem := range head {
						m.match(sublist[index], elem)
					}
					m.match(right, List{list.typ, tail})
					return
				}

				if m.src.Kind(right) == ast.List {
					sublist := m.src.List(right)
					if len(sublist) > len(list.elements) {
						m.err = ErrNoMatch
						return
					}

					head, tail := split(list.elements, len(list.elements)-len(sublist))

					m.match(left, List{list.typ, head})
					for index, elem := range tail {
						m.match(sublist[index], elem)
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
