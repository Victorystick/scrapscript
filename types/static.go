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
	// With imp set, an imported type, if res is Sort, or else the members
	// of an imported record. Held by value, so that it needn't escape.
	imp     bool
	res     Result
	members map[string]static // the members of a record literal
}

// isType reports whether s denotes a type.
func (s static) isType() bool {
	return s.alias != nil || s.imp && s.res.Sort
}

// isNone reports whether s is no static part at all.
func (s static) isNone() bool {
	return s.alias == nil && !s.imp && s.members == nil
}

// member returns the member name of s, if it has one.
func (s static) member(name string) (static, bool) {
	if s.imp {
		m, ok := s.res.Members[name]
		return static{imp: true, res: m}, ok
	}
	m, ok := s.members[name]
	return m, ok
}

// A group is a where-group's body and bindings, the alias scope of its
// bindings, and the aliases of its sort bindings.
type group struct {
	body    ast.Node
	binds   []ast.Node
	aliases *Scope[*alias]
	sorts   []*alias
	values  []alias // the value bindings that may have a static part
}

// group returns the group of the where node x. A where-group binds each of
// its names in alias scope, a value binding as an entry for its static part,
// so that paths in the group's annotations reach it before it is inferred.
// Paths may reach a group before inference does, so groups are made once
// per node.
func (c *context) group(x ast.Node) group {
	if i, ok := c.groups[x]; ok {
		return c.groupList[i]
	}
	body, binds := c.src.Group(x)
	g := group{body: body, binds: binds, aliases: c.aliases}
	seen := make(map[string]bool, len(binds))
	// The values are allocated at once, since the scope points into them.
	n := 0
	for _, b := range binds {
		if _, _, _, val := c.src.Where(b); !c.src.Sort(b) && c.mayBeStatic(val) {
			n++
		}
	}
	var values []alias
	var statics []static
	if n > 0 {
		values = make([]alias, 0, n)
		statics = make([]static, n)
	}
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
			values = append(values, alias{name: name, node: b, value: val, static: &statics[len(values)]})
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
	g.values = values
	if c.groups == nil {
		c.groups = make(map[ast.Node]int32)
	}
	c.groups[x] = int32(len(c.groupList))
	c.groupList = append(c.groupList, g)
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

// static returns the static part of the expression x, noting x in
// c.statics if it has one.
func (c *context) static(x ast.Node) static {
	s := c.staticNode(x)
	if !s.isNone() {
		if c.statics == nil {
			c.statics = make(NodeSet, (len(c.src.Kinds)+63)/64)
		}
		c.statics.add(x)
	}
	return s
}

func (c *context) staticNode(x ast.Node) static {
	switch c.src.Kind(x) {
	case ast.Ident:
		return c.staticName(c.src.TokenBytes(c.src.Main(x)))
	case ast.Where:
		g := c.group(x)
		aliases := c.aliases
		c.aliases = g.aliases
		s := c.static(g.body)
		c.aliases = aliases
		return s
	case ast.Import:
		if res := c.imported(x); res.Sort || res.Members != nil {
			return static{imp: true, res: res}
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
		if s := c.static(rec); !s.isNone() {
			if m, ok := s.member(c.src.TokenText(key)); ok {
				return m
			}
		}
	}
	return static{}
}

// staticName returns the static part of the name in alias scope. A value
// without one leaves its name to outer types, so that a parser named int
// doesn't hide the type int.
func (c *context) staticName(name []byte) static {
	for b := c.aliases; b != nil; b = b.parent {
		if b.name != string(name) {
			continue
		}
		if b.val.value == ast.None {
			return static{alias: b.val}
		}
		if s := c.valueStatic(b.val); !s.isNone() {
			return s
		}
	}
	return static{}
}

// valueStatic returns the static part of the value binding a, computing it
// on first use in the scope of its group.
func (c *context) valueStatic(a *alias) static {
	if a.resolved {
		return *a.static
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
	a.pending, a.resolved, *a.static = false, true, s
	return s
}

// valueStatics resolves the static parts of all value bindings, so that
// Result.Static has them all for printing, not only those that paths
// reached.
func (c *context) valueStatics() {
	// Resolving may meet more groups, which go to the end of the list.
	for i := 0; i < len(c.groupList); i++ {
		values := c.groupList[i].values
		for j := range values {
			c.valueStatic(&values[j])
		}
	}
}

// A NodeSet is a set of nodes of an expression.
type NodeSet []uint64

// Has reports whether x is in s.
func (s NodeSet) Has(x ast.Node) bool {
	i := int(x / 64)
	return i < len(s) && s[i]&(1<<(x%64)) != 0
}

func (s NodeSet) add(x ast.Node) {
	s[x/64] |= 1 << (x % 64)
}

// typeOnly reports whether the record field value v denotes only a type, so
// that it is a member and not part of the record's value.
func (c *context) typeOnly(v ast.Node) bool {
	switch c.src.Kind(v) {
	case ast.Ident:
		// A value in scope, such as a function's parameter, shadows a type.
		name := c.src.TokenBytes(c.src.Main(v))
		for s := c.scope; s != nil; s = s.parent {
			if s.name == string(name) {
				return false
			}
		}
	case ast.Import, ast.Access:
	default:
		return false
	}
	return c.static(v).isType()
}

// notType fails because the path x doesn't denote a type.
func (c *context) notType(x ast.Node) {
	text := c.src.NodeText(x)
	if c.src.Kind(x) == ast.Access {
		rec, key := c.src.Access(x)
		if _, ok := c.static(rec).member(c.src.TokenText(key)); !ok {
			c.bail(x, fmt.Sprintf("%s has no type member %s", c.src.NodeText(rec), c.src.TokenText(key)))
		}
	}
	c.bail(x, fmt.Sprintf("%s is a value, not a type", text))
}

// exports returns the members of s as the results that importers see.
func (c *context) exports(x ast.Node, s static) map[string]Result {
	if s.imp {
		return s.res.Members
	}
	members := make(map[string]Result, len(s.members))
	for name, m := range s.members {
		switch {
		case m.alias != nil:
			members[name] = c.export(x, m.alias)
		case m.imp:
			members[name] = m.res
		default:
			members[name] = Result{Members: c.exports(x, m)}
		}
	}
	return members
}
