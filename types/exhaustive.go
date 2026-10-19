package types

import (
	"slices"
	"strings"

	"github.com/Victorystick/scrapscript/ast"
	"github.com/Victorystick/scrapscript/token"
)

// Exhaustiveness checking follows Maranget, "Warnings for pattern matching"
// (2007): a match function is exhaustive when a wildcard arm added after its
// arms would match nothing that they don't. Lists split by length, as Rust
// does for slice patterns: lengths below some bound, and all lengths from it.
//
// A matrix of patterns is a window of rows in cells, which holds pattern
// nodes, with None for a wildcard. Its columns' types are the last ones in
// types. Specializing a matrix pushes a new one after it, which is popped
// once checked, so the buffers are reused across a scrap.

type exhauster struct {
	cells []ast.Node
	types []TypeRef
	// The elements of the list patterns being split, with rest for a rest.
	items []ast.Node
	// A value that no arm matches, as its constructors in reverse preorder.
	missing []ctor
}

// rest marks the rest of a list pattern in items.
const rest = ^ast.Node(0)

// A ctor is a constructor that a column splits by, or a part of a value that
// no arm matches.
type ctor struct {
	kind  ctorKind
	label string  // tagCtor: the tag.
	typ   TypeRef // tagCtor: the payload type. recordCtor: the record type. listCtor: the element type.
	// listCtor: the number of elements and, for all lengths from n,
	// how many of them are first.
	n, prefix int
	rest      bool
}

type ctorKind uint8

const (
	wildCtor ctorKind = iota
	holeCtor
	tagCtor
	recordCtor
	listCtor
)

// exhaustive bails if some arguments of the types args, one per pattern of
// an arm, match none of the arms of the match function x.
func (c *context) exhaustive(x ast.Node, args []TypeRef) {
	arms := c.src.Arms(x)
	for _, arm := range arms {
		pats, _ := c.src.Arm(arm)
		if !slices.ContainsFunc(pats, func(p ast.Node) bool { return refutable(c.cell(p)) }) {
			return // a catch-all arm
		}
	}
	e := &c.ex
	for _, arm := range arms {
		pats, _ := c.src.Arm(arm)
		for _, p := range pats {
			e.cells = append(e.cells, c.cell(p))
		}
	}
	e.types = append(e.types, args...)
	e.missing = e.missing[:0]
	missing := c.missingIn(0, len(arms), len(args))
	e.cells, e.types = e.cells[:0], e.types[:0]
	if missing {
		slices.Reverse(e.missing)
		var b strings.Builder
		ks := e.missing
		for i := range args {
			if i > 0 {
				b.WriteString(", ")
			}
			ks = c.write(&b, ks, false)
		}
		c.bail(x, "match is not exhaustive: no arm matches "+b.String())
	}
}

// missingIn returns true if some value matches no row of the matrix of rows
// and width columns at cells[at:], whose types are the last width types. If
// so, it pushes such a value to missing.
func (c *context) missingIn(at, rows, width int) bool {
	e := &c.ex
	if rows > 0 && !slices.ContainsFunc(e.cells[at:at+width], refutable) {
		return false // the first row matches everything
	}
	if width == 0 {
		return true
	}
	heads := false
	for i := range rows {
		heads = heads || e.cells[at+i*width] != ast.None
	}
	typ := c.reg.Resolve(e.types[len(e.types)-width])
	n := c.reg.at(typ)
	// A constructor that no head has.
	head := ctor{}

	switch {
	case !heads:
	case n.kind == enumKind:
		missing := false
		k, row, ok := c.tag(n.a)
		for ; ok; k, row, ok = c.tag(row) {
			if !missing && !c.hasTag(at, rows, width, k.label) {
				head, missing = k, true
			}
		}
		if c.reg.at(row).kind != emptyKind {
			head = ctor{} // other tags than these
			break
		}
		if missing {
			break
		}
		for k, row, ok := c.tag(n.a); ok; k, row, ok = c.tag(row) {
			if c.split(at, rows, width, k) {
				return true
			}
		}
		return false

	case n.kind == recordKind:
		return c.split(at, rows, width, ctor{kind: recordCtor, typ: typ})

	case n.kind == listKind:
		// Lengths up to the longest fixed list, and beyond it all lengths
		// at once, long enough for each prefix and suffix.
		fixed, prefix, suffix := -1, 0, 0
		for i := range rows {
			h := e.cells[at+i*width]
			if h == ast.None {
				continue
			}
			start := len(e.items)
			items, p, ok := c.listItems(h)
			e.items = e.items[:start]
			switch {
			case !ok:
			case p < 0:
				fixed = max(fixed, items)
			default:
				prefix, suffix = max(prefix, p), max(suffix, items-p)
			}
		}
		length := max(fixed+1, prefix+suffix)
		for i := range length {
			if c.split(at, rows, width, ctor{kind: listCtor, typ: n.a, n: i}) {
				return true
			}
		}
		return c.split(at, rows, width, ctor{kind: listCtor, typ: n.a, n: length, rest: true, prefix: length - suffix})

	case typ == HoleRef:
		return c.split(at, rows, width, ctor{kind: holeCtor})
	}

	// The heads' constructors are incomplete, so the rows with a wildcard
	// head must cover the other columns.
	end, types := len(e.cells), len(e.types)
	kept := 0
	for i := range rows {
		if e.cells[at+i*width] == ast.None {
			e.cells = append(e.cells, e.cells[at+i*width+1:at+(i+1)*width]...)
			kept++
		}
	}
	e.types = append(e.types, e.types[types-width+1:]...)
	missing := c.missingIn(end, kept, width-1)
	e.cells, e.types = e.cells[:end], e.types[:types]
	if missing {
		if head.kind == tagCtor {
			e.missing = append(e.missing, ctor{}) // its payload
		}
		e.missing = append(e.missing, head)
	}
	return missing
}

// tag returns the first tag that the enum row may have, and the rest of the
// row.
func (c *context) tag(row TypeRef) (ctor, TypeRef, bool) {
	l, payload, _, rest, ok := c.reg.entry(row)
	if !ok {
		return ctor{}, rest, false
	}
	return ctor{kind: tagCtor, label: c.reg.labels.Name(l), typ: payload}, rest, true
}

// split returns true if some value made with the constructor k matches no
// row of the matrix at at. If so, it pushes such a value to missing.
func (c *context) split(at, rows, width int, k ctor) bool {
	e := &c.ex
	end, types := len(e.cells), len(e.types)
	arity := c.argTypes(k)
	e.types = append(e.types, e.types[types-width+1:types]...)
	kept := 0
	for i := range rows {
		mark := len(e.cells)
		if c.args(e.cells[at+i*width], k) {
			e.cells = append(e.cells, e.cells[at+i*width+1:at+(i+1)*width]...)
			kept++
		} else {
			e.cells = e.cells[:mark]
		}
	}
	missing := c.missingIn(end, kept, arity+width-1)
	e.cells, e.types = e.cells[:end], e.types[:types]
	if missing {
		e.missing = append(e.missing, k)
	}
	return missing
}

// argTypes pushes the types of k's arguments, and returns how many there are.
func (c *context) argTypes(k ctor) int {
	e := &c.ex
	start := len(e.types)
	switch k.kind {
	case tagCtor:
		e.types = append(e.types, k.typ)
	case recordCtor:
		for _, payload := range c.reg.fields(c.reg.at(k.typ).a) {
			e.types = append(e.types, payload)
		}
	case listCtor:
		for range k.n {
			e.types = append(e.types, k.typ)
		}
	}
	return len(e.types) - start
}

// args pushes the arguments of the pattern p as the constructor k, and
// returns true if p can match a value made with k.
func (c *context) args(p ast.Node, k ctor) bool {
	e := &c.ex
	src := c.src
	if p == ast.None {
		for range c.arity(k) {
			e.cells = append(e.cells, ast.None)
		}
		return true
	}
	switch k.kind {
	case holeCtor:
		return true

	case tagCtor:
		tag, payload := src.Variant(p)
		if !c.tokenIs(tag, k.label) {
			return false
		}
		e.cells = append(e.cells, c.cell(payload))
		return true

	case recordCtor:
		for l := range c.reg.fields(c.reg.at(k.typ).a) {
			e.cells = append(e.cells, c.field(p, c.reg.labels.Name(l)))
		}
		return true

	case listCtor:
		start := len(e.items)
		items, prefix, ok := c.listItems(p)
		elems := e.items[start:]
		switch {
		case !ok:
		case prefix < 0:
			ok = !k.rest && items == k.n
			if ok {
				e.cells = append(e.cells, elems...)
			}
		case items > k.n:
			ok = false
		default:
			// A list with a rest splits the constructor of all lengths
			// from n with its prefix first and its suffix last.
			e.cells = append(e.cells, elems[:prefix]...)
			for range k.n - items {
				e.cells = append(e.cells, ast.None)
			}
			e.cells = append(e.cells, elems[prefix+1:]...)
		}
		e.items = e.items[:start]
		return ok
	}
	return false
}

// arity returns the number of k's arguments.
func (c *context) arity(k ctor) int {
	switch k.kind {
	case tagCtor:
		return 1
	case recordCtor:
		n := 0
		for range c.reg.fields(c.reg.at(k.typ).a) {
			n++
		}
		return n
	case listCtor:
		return k.n
	}
	return 0
}

// listItems pushes the elements of the list pattern p to items, with rest
// for its rest, if it has one. It returns the number of elements and the
// index of the rest, or -1, and whether p has at most one rest.
func (c *context) listItems(p ast.Node) (items, prefix int, ok bool) {
	e := &c.ex
	start := len(e.items)
	if !c.flatten(p) {
		return 0, 0, false
	}
	prefix, j := -1, start
	for i := start; i < len(e.items); i++ {
		x := e.items[i]
		if x == rest {
			if prefix >= 0 && e.items[j-1] != rest {
				return 0, 0, false
			}
			if prefix >= 0 {
				continue // adjacent rests are one rest
			}
			prefix = j - start
		}
		e.items[j] = x
		j++
	}
	e.items = e.items[:j]
	items = j - start
	if prefix >= 0 {
		items--
	}
	return items, prefix, true
}

// flatten pushes the elements of the list pattern p to items.
func (c *context) flatten(p ast.Node) bool {
	e := &c.ex
	src := c.src
	switch src.Kind(p) {
	case ast.Ident:
		e.items = append(e.items, rest)
		return true
	case ast.List:
		for _, x := range src.List(p) {
			e.items = append(e.items, c.cell(x))
		}
		return true
	case ast.Binary:
		op, l, r := src.Binary(p)
		switch op {
		case token.PREPEND:
			e.items = append(e.items, c.cell(l))
			return c.flatten(r)
		case token.APPEND:
			ok := c.flatten(l)
			e.items = append(e.items, c.cell(r))
			return ok
		case token.CONCAT:
			return c.flatten(l) && c.flatten(r)
		}
	}
	return false
}

// hasTag returns true if some head of the matrix at at has the tag.
func (c *context) hasTag(at, rows, width int, tag string) bool {
	for i := range rows {
		h := c.ex.cells[at+i*width]
		if h == ast.None {
			continue
		}
		if t, _ := c.src.Variant(h); c.tokenIs(t, tag) {
			return true
		}
	}
	return false
}

// field returns the pattern of the record pattern p for the label.
func (c *context) field(p ast.Node, label string) ast.Node {
	for c.src.Kind(p) == ast.Record {
		rest, fields := c.src.Record(p)
		for i := range fields.Len() {
			if c.tokenIs(fields.Key(i), label) {
				return c.cell(fields.Value(i))
			}
		}
		p = rest
	}
	return ast.None
}

// cell returns the pattern p as a cell, with None for a name.
func (c *context) cell(p ast.Node) ast.Node {
	if p == ast.None || c.src.Kind(p) == ast.Ident {
		return ast.None
	}
	return p
}

func refutable(p ast.Node) bool { return p != ast.None }

// tokenIs returns true if the token t's text is s.
func (c *context) tokenIs(t ast.Tok, s string) bool {
	span := c.src.TokenSpan(t)
	return string(c.src.Source.Bytes()[span.Start:span.End]) == s
}

// write writes the value ks, in preorder, as a pattern, and returns the
// rest of ks. In an argument, it parenthesizes patterns that take one.
func (c *context) write(b *strings.Builder, ks []ctor, arg bool) []ctor {
	k, ks := ks[0], ks[1:]
	switch k.kind {
	case wildCtor:
		b.WriteString("_")
	case holeCtor:
		b.WriteString("()")
	case tagCtor:
		if c.reg.Resolve(k.typ) == HoleRef && ks[0].kind == wildCtor {
			b.WriteString("#" + k.label)
			return ks[1:]
		}
		if arg {
			b.WriteString("(")
		}
		b.WriteString("#" + k.label + " ")
		ks = c.write(b, ks, true)
		if arg {
			b.WriteString(")")
		}
	case recordCtor:
		// The fields are in row order; write them by label.
		var fields []string
		for l := range c.reg.fields(c.reg.at(k.typ).a) {
			var f strings.Builder
			f.WriteString(c.reg.labels.Name(l) + " = ")
			ks = c.write(&f, ks, false)
			fields = append(fields, f.String())
		}
		if len(fields) == 0 {
			b.WriteString("{}")
			return ks
		}
		slices.Sort(fields)
		b.WriteString("{ " + strings.Join(fields, ", ") + " }")
	case listCtor:
		if !k.rest {
			return c.writeList(b, ks, k.n)
		}
		if arg {
			b.WriteString("(")
		}
		if k.prefix > 0 {
			ks = c.writeList(b, ks, k.prefix)
			b.WriteString(" ++ ")
		}
		b.WriteString("_")
		if k.prefix < k.n {
			b.WriteString(" ++ ")
			ks = c.writeList(b, ks, k.n-k.prefix)
		}
		if arg {
			b.WriteString(")")
		}
	}
	return ks
}

func (c *context) writeList(b *strings.Builder, ks []ctor, n int) []ctor {
	if n == 0 {
		b.WriteString("[]")
		return ks
	}
	b.WriteString("[ ")
	for i := range n {
		if i > 0 {
			b.WriteString(", ")
		}
		ks = c.write(b, ks, false)
	}
	b.WriteString(" ]")
	return ks
}
