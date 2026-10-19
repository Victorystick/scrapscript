package types

import (
	"fmt"

	"github.com/Victorystick/scrapscript/ast"
)

// A static is the compile-time part of an expression: the type it denotes,
// or the type members of the record it denotes. A record's members are its
// fields whose values have a static part, so `{ tree = tree, … }` has the
// member tree, where tree is a sort binding. Paths in type position, such as
// `avl.tree`, reach members through names, where-clauses and imports, the
// same locally as across imports, without inferring any value. An
// expression of any other form, such as a function or a call, has none.
type static struct {
	alias *alias // a local type
	// An imported type, if it is Sort, or else the members of an imported
	// record.
	imported *Result
	members  map[string]static // the members of a record literal
}

// isType reports whether s denotes a type.
func (s static) isType() bool {
	return s.alias != nil || s.imported != nil && s.imported.Sort
}

// isNone reports whether s is no static part at all.
func (s static) isNone() bool {
	return s.alias == nil && s.imported == nil && s.members == nil
}

// member returns the member name of s, if it has one.
func (s static) member(name string) (static, bool) {
	if s.imported != nil {
		m, ok := s.imported.Members[name]
		return static{imported: &m}, ok
	}
	m, ok := s.members[name]
	return m, ok
}

// A group is the alias scope of a where-group's bindings, and the aliases
// of its sort bindings.
type group struct {
	aliases *Scope[*alias]
	sorts   []*alias
}

// group returns the group of the where node x. A where-group binds each of
// its names in alias scope, a value binding as an entry for its static part,
// so that paths in the group's annotations reach it before it is inferred.
// Paths may reach a group before inference does, so groups are made once
// per node.
func (c *context) group(x ast.Node) group {
	if g, ok := c.groups[x]; ok {
		return g
	}
	_, binds := c.src.Group(x)
	g := group{aliases: c.aliases}
	seen := make(map[string]bool, len(binds))
	var values []alias
	for _, b := range binds {
		_, id, typ, val := c.src.Where(b)
		name := c.src.TokenText(id)
		if seen[name] {
			c.bail(b, fmt.Sprintf("cannot bind %s twice", name))
		}
		seen[name] = true

		var a *alias
		switch {
		case c.src.Sort(b):
			a = c.sortBinding(b, name, typ, val)
			g.sorts = append(g.sorts, a)
		case c.mayBeStatic(val):
			if values == nil {
				values = make([]alias, 0, len(binds))
			}
			values = append(values, alias{name: name, node: b, value: val})
			a = &values[len(values)-1]
		default:
			// Most values, such as functions, can't have a static part.
			continue
		}
		g.aliases = g.aliases.Bind(name, a)
	}
	// The group's bindings see each other.
	for _, a := range g.sorts {
		a.scope = g.aliases
	}
	for i := range values {
		values[i].scope = g.aliases
	}
	if c.groups == nil {
		c.groups = make(map[ast.Node]group)
	}
	c.groups[x] = g
	return g
}

// mayBeStatic reports whether the expression x is of a form that can have
// a static part.
func (c *context) mayBeStatic(x ast.Node) bool {
	switch c.src.Kind(x) {
	case ast.Ident, ast.Where, ast.Import, ast.Record, ast.Access:
		return true
	}
	return false
}

// static returns the static part of the expression x.
func (c *context) static(x ast.Node) static {
	switch c.src.Kind(x) {
	case ast.Ident:
		// A value without a static part leaves its name to outer types,
		// so that a parser named int doesn't hide the type int.
		name := c.src.TokenText(c.src.Main(x))
		for b := c.aliases.Get(name); b != nil; b = b.parent.Get(name) {
			if b.val.value == ast.None {
				return static{alias: b.val}
			}
			if s := c.valueStatic(b.val); !s.isNone() {
				return s
			}
		}
	case ast.Where:
		body, _ := c.src.Group(x)
		aliases := c.aliases
		c.aliases = c.group(x).aliases
		s := c.static(body)
		c.aliases = aliases
		return s
	case ast.Import:
		if res := c.imported(x); res.Sort || res.Members != nil {
			return static{imported: &res}
		}
	case ast.Record:
		rest, fields := c.src.Record(x)
		if rest != ast.None {
			// A spread only updates values.
			break
		}
		var members map[string]static
		for i := range fields.Len() {
			if s := c.static(fields.Value(i)); !s.isNone() {
				if members == nil {
					members = make(map[string]static)
				}
				members[c.src.TokenText(fields.Key(i))] = s
			}
		}
		if members != nil {
			return static{members: members}
		}
	case ast.Access:
		rec, key := c.src.Access(x)
		if m, ok := c.static(rec).member(c.src.TokenText(key)); ok {
			return m
		}
	}
	return static{}
}

// valueStatic returns the static part of the value binding a, computing it
// on first use in the scope of its group.
func (c *context) valueStatic(a *alias) static {
	if a.resolved {
		return a.static
	}
	if a.pending {
		// A value that refers to itself has no static part; inference
		// reports it.
		return static{}
	}
	a.pending = true
	aliases, tvars := c.aliases, c.tvars
	c.aliases, c.tvars = a.scope, nil
	s := c.static(a.value)
	c.aliases, c.tvars = aliases, tvars
	a.pending, a.resolved, a.static = false, true, s
	return s
}

// typeOnly reports whether the record field value v denotes only a type, so
// that it is a member and not part of the record's value.
func (c *context) typeOnly(v ast.Node) bool {
	switch c.src.Kind(v) {
	case ast.Ident:
		// A value in scope, such as a function's parameter, shadows a type.
		if c.scope.Get(c.src.TokenText(c.src.Main(v))) != nil {
			return false
		}
	case ast.Import, ast.Access:
	default:
		return false
	}
	return c.static(v).isType()
}

// constructor returns the type that the path x denotes in type position,
// failing if it is not a type.
func (c *context) constructor(x ast.Node) static {
	s := c.static(x)
	if s.isType() {
		return s
	}
	text := c.src.NodeText(x)
	if c.src.Kind(x) == ast.Access {
		rec, key := c.src.Access(x)
		if _, ok := c.static(rec).member(c.src.TokenText(key)); !ok {
			c.bail(x, fmt.Sprintf("%s has no type member %s", c.src.NodeText(rec), c.src.TokenText(key)))
		}
	}
	c.bail(x, fmt.Sprintf("%s is a value, not a type", text))
	return static{}
}

// exports returns the members of s as the results that importers see.
func (c *context) exports(x ast.Node, s static) map[string]Result {
	if s.imported != nil {
		return s.imported.Members
	}
	members := make(map[string]Result, len(s.members))
	for name, m := range s.members {
		switch {
		case m.alias != nil:
			members[name] = c.export(x, m.alias)
		case m.imported != nil:
			members[name] = *m.imported
		default:
			members[name] = Result{Members: c.exports(x, m)}
		}
	}
	return members
}
