package types

import (
	"maps"
	"slices"
	"strings"

	"github.com/Victorystick/scrapscript/ast"
	"github.com/Victorystick/scrapscript/token"
)

// Exhaustiveness checking follows Maranget, "Warnings for pattern matching"
// (2007): a match function is exhaustive when a wildcard arm added after its
// arms would match nothing that they don't. Lists split by length, as Rust
// does for slice patterns: lengths below some bound, and all lengths from it.

type patKind uint8

const (
	wildPat    patKind = iota
	litPat             // name: the literal's text
	holePat            // ()
	variantPat         // name: the tag. args: the payload.
	recordPat          // fields: the patterns by label.
	listPat            // args: the elements.
)

// A pat is a pattern, simplified for checking exhaustiveness. The
// constructors that the checker splits a column by, and the values it finds
// that no arm matches, are pats too.
type pat struct {
	kind   patKind
	name   string
	args   []pat
	fields map[string]pat
	// listPat: whether there is a list of any length between the first
	// prefix elements and the rest.
	rest   bool
	prefix int
	// variantPat: whether the tag has no payload, so its payload is ().
	bare bool
	// A record constructor's labels, in order.
	labels []string
}

var wild = pat{}

func wilds(n int) []pat { return make([]pat, n) }

// simplifier turns patterns into pats. A pattern that it can't describe
// clears ok; it matches nothing as far as the checker is concerned.
type simplifier struct {
	src *ast.SourceExpr
	ok  bool
}

func (s *simplifier) pat(x ast.Node) pat {
	src := s.src
	switch src.Kind(x) {
	case ast.Ident:
		return wild

	case ast.Literal:
		if src.LiteralKind(x) == token.HOLE {
			return pat{kind: holePat}
		}
		return pat{kind: litPat, name: src.TokenText(src.Main(x))}

	case ast.Variant:
		tag, payload := src.Variant(x)
		p := wild
		if payload != ast.None {
			p = s.pat(payload)
		}
		return pat{kind: variantPat, name: src.TokenText(tag), args: []pat{p}}

	case ast.Record:
		rest, fields := src.Record(x)
		p := pat{kind: recordPat, fields: make(map[string]pat, fields.Len())}
		for i := range fields.Len() {
			p.fields[src.TokenText(fields.Key(i))] = s.pat(fields.Value(i))
		}
		if rest != ast.None {
			// The rest is a record of the other fields.
			if r := s.pat(rest); r.kind == recordPat {
				maps.Copy(p.fields, r.fields)
			}
		}
		return p

	case ast.List:
		var args []pat
		for _, e := range src.List(x) {
			args = append(args, s.pat(e))
		}
		return pat{kind: listPat, args: args}

	case ast.Binary:
		op, l, r := src.Binary(x)
		switch op {
		case token.PREPEND:
			return s.concat(pat{kind: listPat, args: []pat{s.pat(l)}}, s.pat(r))
		case token.APPEND:
			return s.concat(s.pat(l), pat{kind: listPat, args: []pat{s.pat(r)}})
		case token.CONCAT:
			return s.concat(s.pat(l), s.pat(r))
		}
	}
	s.ok = false
	return wild
}

// concat joins two list patterns, if their result has at most one rest.
func (s *simplifier) concat(l, r pat) pat {
	if l.kind == wildPat {
		l = pat{kind: listPat, rest: true}
	}
	if r.kind == wildPat {
		r = pat{kind: listPat, rest: true}
	}
	if l.rest && r.rest {
		if len(l.args) == l.prefix && r.prefix == 0 {
			return pat{kind: listPat, args: slices.Concat(l.args, r.args), rest: true, prefix: l.prefix}
		}
		s.ok = false
		return wild
	}
	p := pat{kind: listPat, args: slices.Concat(l.args, r.args), rest: l.rest || r.rest, prefix: r.prefix}
	if l.rest {
		p.prefix = l.prefix
	} else if r.rest {
		p.prefix = len(l.args) + r.prefix
	}
	return p
}

// exhaustive bails if some value of the argument type of the match function
// x matches none of its arms.
func (c *context) exhaustive(x ast.Node, argTy TypeRef) {
	arms := c.src.Arms(x)
	for _, arm := range arms {
		if arg, _ := c.src.Func(arm); c.src.Kind(arg) == ast.Ident {
			return // a catch-all arm
		}
	}
	s := simplifier{src: c.src}
	rows := make([][]pat, 0, len(arms))
	for _, arm := range arms {
		arg, _ := c.src.Func(arm)
		s.ok = true
		if p := s.pat(arg); s.ok {
			rows = append(rows, []pat{p})
		}
	}
	if w, ok := c.missing(rows, []TypeRef{argTy}); ok {
		var b strings.Builder
		w[0].write(&b, false)
		c.bail(x, "match is not exhaustive: no arm matches "+b.String())
	}
}

// missing returns values, one per column of types, that no row matches, if
// there are any.
func (c *context) missing(rows [][]pat, types []TypeRef) ([]pat, bool) {
	if len(rows) > 0 && !slices.ContainsFunc(rows[0], pat.refutable) {
		return nil, false // the first row matches everything
	}
	if len(types) == 0 {
		return nil, true
	}

	var shape Shape
	var ctors []pat
	complete, heads := false, slices.ContainsFunc(rows, func(row []pat) bool { return row[0].refutable() })
	if heads {
		shape = c.reg.Shape(types[0])
		ctors, complete = c.signature(shape, rows)
	}
	if complete {
		for _, ctor := range ctors {
			argTypes := ctorTypes(shape, ctor)
			n := len(argTypes)
			w, ok := c.missing(specialize(rows, ctor), append(argTypes, types[1:]...))
			if ok {
				return append([]pat{ctor.with(w[:n])}, w[n:]...), true
			}
		}
		return nil, false
	}

	var rest [][]pat
	for _, row := range rows {
		if row[0].kind == wildPat {
			rest = append(rest, row[1:])
		}
	}
	w, ok := c.missing(rest, types[1:])
	if !ok {
		return nil, false
	}
	head := wild
	if len(ctors) > 0 {
		head = ctors[0]
	}
	return append([]pat{head}, w...), true
}

// signature returns the constructors of the first column of rows, of the
// type shape, and whether they are complete: then each value is in one of
// them. Otherwise, it returns the constructors that no row's head has, if the
// type has finitely many.
func (c *context) signature(shape Shape, rows [][]pat) ([]pat, bool) {
	switch shape.Kind {
	case ShapeEnum:
		var all, missing []pat
		for _, l := range shape.Labels {
			ctor := pat{kind: variantPat, name: l.Name, args: wilds(1), bare: c.reg.Resolve(l.Type) == HoleRef}
			all = append(all, ctor)
			if !slices.ContainsFunc(rows, func(row []pat) bool { return row[0].name == l.Name }) {
				missing = append(missing, ctor)
			}
		}
		if shape.Open {
			return nil, false
		}
		if len(missing) > 0 {
			return missing, false
		}
		return all, true

	case ShapeRecord:
		ctor := pat{kind: recordPat}
		for _, l := range shape.Labels {
			ctor.labels = append(ctor.labels, l.Name)
		}
		ctor.args = wilds(len(ctor.labels))
		return []pat{ctor}, true

	case ShapeList:
		// Lengths up to the longest fixed list, and beyond it all lengths
		// at once, long enough for each prefix and suffix.
		fixed, prefix, suffix := -1, 0, 0
		for _, row := range rows {
			h := row[0]
			if h.kind == wildPat {
				continue
			}
			if h.rest {
				prefix = max(prefix, h.prefix)
				suffix = max(suffix, len(h.args)-h.prefix)
			} else {
				fixed = max(fixed, len(h.args))
			}
		}
		n := max(fixed+1, prefix+suffix)
		var ctors []pat
		for i := range n {
			ctors = append(ctors, pat{kind: listPat, args: wilds(i)})
		}
		return append(ctors, pat{kind: listPat, args: wilds(n), rest: true, prefix: n - suffix}), true

	case ShapePrimitive:
		if shape.Ref == HoleRef {
			return []pat{{kind: holePat}}, true
		}
	}
	return nil, false
}

// ctorTypes returns the types of the arguments of a constructor of a column
// of the type shape.
func ctorTypes(shape Shape, ctor pat) []TypeRef {
	var types []TypeRef
	switch ctor.kind {
	case variantPat:
		i := slices.IndexFunc(shape.Labels, func(l Label) bool { return l.Name == ctor.name })
		types = append(types, shape.Labels[i].Type)
	case recordPat:
		for _, l := range shape.Labels {
			types = append(types, l.Type)
		}
	case listPat:
		for range ctor.args {
			types = append(types, shape.Elem)
		}
	}
	return types
}

// specialize returns the rows whose head matches the constructor ctor, with
// the head replaced by its arguments.
func specialize(rows [][]pat, ctor pat) [][]pat {
	out := make([][]pat, 0, len(rows))
	for _, row := range rows {
		if args, ok := row[0].split(ctor); ok {
			out = append(out, slices.Concat(args, row[1:]))
		}
	}
	return out
}

// split returns the arguments of p as the constructor ctor, if p can match
// a value made with ctor.
func (p pat) split(ctor pat) ([]pat, bool) {
	switch p.kind {
	case wildPat:
		return wilds(len(ctor.args)), true
	case variantPat:
		return p.args, p.name == ctor.name
	case recordPat:
		args := make([]pat, len(ctor.labels))
		for i, l := range ctor.labels {
			args[i] = p.fields[l]
		}
		return args, true
	case listPat:
		n := len(ctor.args)
		if !p.rest {
			return p.args, !ctor.rest && len(p.args) == n
		}
		if n < len(p.args) {
			return nil, false
		}
		// A list with a rest only splits a constructor of all lengths from
		// n with its prefix first and its suffix last.
		return slices.Concat(p.args[:p.prefix], wilds(n-len(p.args)), p.args[p.prefix:]), true
	}
	return nil, p.kind == ctor.kind
}

// refutable returns true if some value doesn't match p.
func (p pat) refutable() bool { return p.kind != wildPat }

// with returns the constructor ctor applied to args.
func (ctor pat) with(args []pat) pat {
	ctor.args = slices.Clone(args)
	return ctor
}

// write writes p as a pattern. In an argument, it parenthesizes patterns
// that take one.
func (p pat) write(b *strings.Builder, arg bool) {
	switch p.kind {
	case wildPat:
		b.WriteString("_")
	case holePat:
		b.WriteString("()")
	case variantPat:
		bare := p.bare && p.args[0].kind == wildPat
		if arg && !bare {
			b.WriteString("(")
		}
		b.WriteString("#" + p.name)
		if !bare {
			b.WriteString(" ")
			p.args[0].write(b, true)
		}
		if arg && !bare {
			b.WriteString(")")
		}
	case recordPat:
		if len(p.labels) == 0 {
			b.WriteString("{}")
			return
		}
		b.WriteString("{ ")
		for i, l := range p.labels {
			if i > 0 {
				b.WriteString(", ")
			}
			b.WriteString(l + " = ")
			p.args[i].write(b, false)
		}
		b.WriteString(" }")
	case listPat:
		if !p.rest {
			writeList(b, p.args)
			return
		}
		if arg {
			b.WriteString("(")
		}
		if p.prefix > 0 {
			writeList(b, p.args[:p.prefix])
			b.WriteString(" ++ ")
		}
		b.WriteString("_")
		if p.prefix < len(p.args) {
			b.WriteString(" ++ ")
			writeList(b, p.args[p.prefix:])
		}
		if arg {
			b.WriteString(")")
		}
	}
}

func writeList(b *strings.Builder, ps []pat) {
	if len(ps) == 0 {
		b.WriteString("[]")
		return
	}
	b.WriteString("[ ")
	for i, p := range ps {
		if i > 0 {
			b.WriteString(", ")
		}
		p.write(b, false)
	}
	b.WriteString(" ]")
}
