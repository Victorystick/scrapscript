package eval

import (
	"fmt"
	"slices"

	"github.com/Victorystick/scrapscript/ast"
	"github.com/Victorystick/scrapscript/label"
	"github.com/Victorystick/scrapscript/token"
)

// The core that eval runs: a SourceExpr after type checking, with names
// resolved to frame slots, literals decoded, pipes turned into calls and
// both kinds of function turned into lambdas with arms.
//
// A frame holds the values that one call of a function binds: its
// parameter, the names its match arms bind and the where-bindings in its
// body, outside the functions nested in it. The top level has a frame too.
// Each expression of a body is evaluated at most once per call, so each
// slot is written once, except that the arms of a match share slots: only
// one arm's names are bound at a time.
//
// Like the AST, the core is stored as parallel arrays indexed by cnode.

// cnode is the index of a node in a core.
type cnode int32

// none is the placeholder node 0, which stands in for a missing optional
// child.
const none cnode = 0

// An op is the kind of a node. Each op's comment says how it uses its data
// A and B; a range A..B is a slice of extra.
type op uint8

const (
	opNone op = iota

	// Expressions.
	opConst   // A: the index of a literal or builtin in consts.
	opLocal   // A: the depth of the frame, how many out from the current one. B: the slot.
	opImport  // The value of the scrap that the AST node refers to.
	opCall    // A: the function. B: the argument.
	opAdd     // A, B: the operands; and so for the operators below.
	opSub     //
	opMul     //
	opAppend  // a +< b
	opPrepend // a >+ b
	opConcat  // a ++ b
	opCompose // A..B: the function applied first, then second, then its uses.
	opLambda  // A..B: its frame's size, its uses, its arity n, then n patterns and a body per arm.
	opWhere   // A..B: the body, then a slot and value per binding, in evaluation order.
	opRecord  // A..B: the record it is based on or none, its shape or -1 if it has a base, then a key and value per field.
	opAccess  // A: the record. B: the key, a label.ID.
	opList    // A..B: the elements.
	opVariant // A: the tag, a label.ID. B: the payload, or none.

	// Patterns.
	patBind    // A: the slot it binds.
	patWild    // _
	patLiteral // A: the index of the value in consts.
	patVariant // A: the tag, a label.ID. B: the payload, or none.
	patRecord  // A..B: the rest or none, the shape of its fields, then a key and pattern per field.
	patList    // A..B: the elements.
	patCons    // head >+ tail. A: head. B: tail.
	patSnoc    // init +< last. A: init. B: last.
	patPrefix  // [a, b] ++ rest. A..B: rest, then the elements.
	patSuffix  // rest ++ [a, b]. A..B: rest, then the elements.
)

// data holds a node's two children or indices; see op for their meaning.
type data struct {
	a, b int32
}

// A core is a compiled scrap. Its nodes are expressions and patterns; the
// other arrays are pools that nodes refer to by index, as their op says.
type core struct {
	// The source it was compiled from, which at refers into.
	src *ast.SourceExpr

	// Per node, indexed by cnode. Node 0 is none.
	ops  []op
	data []data
	at   []ast.Node // its AST node, for errors and printing

	// Not per node; nodes refer to these by index.

	// The children of nodes with more than two, as ranges A..B: cnodes,
	// slots, label IDs and uses indices, by op.
	extra []int32
	// The values of literals and builtins, decoded once.
	consts []Value
	// The table that tags and record keys are interned in.
	labels *label.Table
	// The table that the shapes of records are interned in, and the shapes
	// of record literals and patterns.
	table  *shapeTable
	shapes []*shape

	// For printing closures, not for evaluating them. A lambda's or
	// composition's uses is what its code refers to; a binder is a name that
	// those refer to, by index here.
	binders []binder
	uses    []uses

	// The node evaluated, and the number of slots in its frame.
	root cnode
	top  int
}

// A binder is a name that a frame binds.
type binder struct {
	name  string
	slot  int      // -1 for a sort binding, which has no value
	where ast.Node // its Where node, or None for a parameter or pattern name
	sort  bool
	// The index in uses of what its annotation, or a sort binding's type,
	// refers to.
	types int32
}

// A ref is what an Ident node in printed code refers to: a binder of a
// frame, depth frames out from the one the code is in, or a builtin if b is
// -1.
type ref struct {
	ident ast.Node
	depth int
	b     int32
}

// uses records what code that prints as part of a value refers to.
type uses struct {
	free  []ref    // its free names, from the frame it is in
	names []string // every name in it, bound or free
	home  int32    // the binder whose value it is, which it prints as, or -1
}

func (c *core) range_(n cnode) []int32 {
	d := c.data[n]
	return c.extra[d.a:d.b]
}

// Lambda returns a lambda's frame size, its uses, its arity, the number of
// arguments it takes, and its arms, each its patterns, one per argument,
// and then its body.
func (c *core) lambda(n cnode) (size int, u int32, arity int, arms []int32) {
	r := c.range_(n)
	return int(r[0]), r[1], int(r[2]), r[3:]
}

// Compose returns the functions of a composition, in the order they are
// applied, and its uses.
func (c *core) compose(n cnode) (first, second cnode, u int32) {
	r := c.range_(n)
	return cnode(r[0]), cnode(r[1]), r[2]
}

// Where returns a where's body, and its bindings as pairs of slot and
// value.
func (c *core) where(n cnode) (body cnode, binds []int32) {
	r := c.range_(n)
	return cnode(r[0]), r[1:]
}

// Record returns the record or record pattern that a record is based on,
// its shape, and its fields as pairs of key and value.
//
// A record without a base has the shape of its fields, and a key is the
// index of its value in the record. A record with a base has the base's
// shape, which is not known until it runs, so a key is a label.ID, and the
// shape is nil.
//
// A record pattern's shape is the shape of its fields, and a key is the
// index of its label in that shape. Without a rest, the record it matches
// has that shape too, so the key is the index of its value.
func (c *core) record(n cnode) (rest cnode, s *shape, fields []int32) {
	r := c.range_(n)
	if r[1] >= 0 {
		s = c.shapes[r[1]]
	}
	return cnode(r[0]), s, r[2:]
}

// Elems returns the rest pattern of a prefix or suffix pattern, and its
// elements.
func (c *core) elems(n cnode) (rest cnode, elems []int32) {
	r := c.range_(n)
	return cnode(r[0]), r[1:]
}

// compile lowers se, which must have been type-checked. Free names that it
// doesn't bind refer to globals. Tags and record keys are interned in the
// label table of shapes, and the shapes of records in shapes.
func compile(se *ast.SourceExpr, globals Vars, shapes *shapeTable) (*core, error) {
	c := compiler{
		core:     core{src: se, labels: shapes.labels, table: shapes},
		globals:  globals,
		builtins: make(map[string]int32),
	}
	c.node(opNone, 0, 0, ast.None)
	c.root = c.expr(se.Expr)
	if c.err != nil {
		return nil, c.err
	}
	c.top = c.size
	return &c.core, nil
}

type compiler struct {
	core
	globals Vars
	// The index in consts of each builtin referred to.
	builtins map[string]int32
	err      error

	// The frame being compiled: how deeply it is nested, its next free
	// slot and its size so far.
	level, next, size int

	// The names in scope, innermost last, of values and of types.
	values, types []entry
	// The printed code being compiled, innermost last.
	roots []root
}

type entry struct {
	name  string
	level int
	b     int32 // the binder, or -1 for a type variable
}

// A root is printed code being compiled, which collects into uses[u].
type root struct {
	u int32
	// The frame it is in, and the number of names in scope outside it.
	level         int
	values, types int
}

func (c *compiler) error(n ast.Node, msg string) {
	if c.err == nil {
		c.err = c.src.Error(n, msg)
	}
}

func (c *compiler) name(t ast.Tok) string {
	return c.src.TokenText(t)
}

func (c *compiler) node(o op, a, b int32, x ast.Node) cnode {
	n := cnode(len(c.ops))
	c.ops = append(c.ops, o)
	c.data = append(c.data, data{a, b})
	c.at = append(c.at, x)
	return n
}

// span adds a node whose children are the range xs of extra.
func (c *compiler) span(o op, xs []int32, x ast.Node) cnode {
	start := int32(len(c.extra))
	c.extra = append(c.extra, xs...)
	return c.node(o, start, int32(len(c.extra)), x)
}

func (c *compiler) constant(v Value) int32 {
	c.consts = append(c.consts, v)
	return int32(len(c.consts) - 1)
}

// builtin returns the index in consts of the builtin name, which x refers
// to, adding it the first time.
func (c *compiler) builtin(x ast.Node, name string) int32 {
	i, ok := c.builtins[name]
	if !ok {
		v := c.globals.Get(name)
		if v == nil {
			c.error(x, fmt.Sprintf("unknown variable %s", name))
		}
		i = c.constant(v)
		c.builtins[name] = i
	}
	return i
}

// label returns the ID of the tag or record key t.
func (c *compiler) label(t ast.Tok) int32 {
	return int32(c.labels.Of(c.name(t)))
}

// shape adds the shape of a record's fields to shapes, and returns its
// index there and each field's index in it.
func (c *compiler) shape(fields ast.Fields) (int32, []int32) {
	labels := make([]label.ID, fields.Len())
	for i := range labels {
		labels[i] = label.ID(c.label(fields.Key(i)))
	}
	sorted := slices.Clone(labels)
	c.table.sort(sorted)
	s := c.table.of(sorted)
	keys := make([]int32, len(labels))
	for i, l := range labels {
		keys[i] = int32(s.index(l))
	}
	c.shapes = append(c.shapes, s)
	return int32(len(c.shapes) - 1), keys
}

// open starts collecting what code in the current frame refers to into a
// new uses, returning its index; close stops it.
func (c *compiler) open() int32 {
	u := int32(len(c.uses))
	c.uses = append(c.uses, uses{home: -1})
	c.roots = append(c.roots, root{u, c.level, len(c.values), len(c.types)})
	return u
}

func (c *compiler) close() {
	c.roots = c.roots[:len(c.roots)-1]
}

// use records a name in the code being compiled.
func (c *compiler) use(name string) {
	for _, r := range c.roots {
		c.uses[r.u].names = append(c.uses[r.u].names, name)
	}
}

// refer records that the Ident x refers to the i'th entry of scope, or to a
// builtin if i is -1, in the code that it is free in.
func (c *compiler) refer(x ast.Node, scope []entry, i int, types bool) {
	for _, r := range c.roots {
		mark := r.values
		if types {
			mark = r.types
		}
		u := &c.uses[r.u]
		if i < 0 {
			u.free = append(u.free, ref{x, 0, -1})
		} else if i < mark {
			e := scope[i]
			u.free = append(u.free, ref{x, r.level - e.level, e.b})
		}
	}
}

func lookup(scope []entry, name string) int {
	for i := len(scope) - 1; i >= 0; i-- {
		if scope[i].name == name {
			return i
		}
	}
	return -1
}

// alloc returns a new slot in the current frame.
func (c *compiler) alloc() int {
	s := c.next
	c.next++
	c.size = max(c.size, c.next)
	return s
}

// bind adds a binder, binding its name in the current frame if it is a
// value.
func (c *compiler) bind(b binder) int32 {
	i := int32(len(c.binders))
	c.binders = append(c.binders, b)
	c.use(b.name)
	if b.sort {
		c.types = append(c.types, entry{b.name, c.level, i})
	} else {
		c.values = append(c.values, entry{b.name, c.level, i})
	}
	return i
}

func (c *compiler) expr(x ast.Node) cnode {
	se := c.src
	switch se.Kind(x) {
	case ast.Literal:
		v, err := Literal(se, x)
		if err != nil {
			c.error(x, err.Error())
		}
		return c.node(opConst, c.constant(v), 0, x)

	case ast.Ident:
		name := c.name(se.Main(x))
		c.use(name)
		i := lookup(c.values, name)
		c.refer(x, c.values, i, false)
		if i < 0 {
			return c.node(opConst, c.builtin(x, name), 0, x)
		}
		e := c.values[i]
		return c.node(opLocal, int32(c.level-e.level), int32(c.binders[e.b].slot), x)

	case ast.Binary:
		return c.binary(x)

	case ast.Call:
		fn, arg := se.Call(x)
		return c.node(opCall, int32(c.expr(fn)), int32(c.expr(arg)), x)

	case ast.Where:
		return c.group(x)

	case ast.Variant:
		tag, payload := se.Variant(x)
		p := none
		if payload != ast.None {
			p = c.expr(payload)
		}
		return c.node(opVariant, c.label(tag), int32(p), x)

	case ast.Record:
		rest, fields := se.Record(x)
		if rest != ast.None {
			xs := []int32{int32(c.expr(rest)), -1}
			for i := range fields.Len() {
				xs = append(xs, c.label(fields.Key(i)), int32(c.expr(fields.Value(i))))
			}
			return c.span(opRecord, xs, x)
		}
		s, keys := c.shape(fields)
		xs := []int32{int32(none), s}
		for i := range fields.Len() {
			xs = append(xs, keys[i], int32(c.expr(fields.Value(i))))
		}
		return c.span(opRecord, xs, x)

	case ast.Access:
		rec, key := se.Access(x)
		return c.node(opAccess, int32(c.expr(rec)), c.label(key), x)

	case ast.List:
		elems := se.List(x)
		xs := make([]int32, len(elems))
		for i, elem := range elems {
			xs[i] = int32(c.expr(elem))
		}
		return c.span(opList, xs, x)

	case ast.Func, ast.MatchFunc:
		return c.fn(x)

	case ast.Import:
		return c.node(opImport, 0, 0, x)
	}

	c.error(x, fmt.Sprintf("unhandled node %s", se.Kind(x)))
	return none
}

var binaryOps = map[token.Token]op{
	token.ADD:     opAdd,
	token.SUB:     opSub,
	token.MUL:     opMul,
	token.APPEND:  opAppend,
	token.PREPEND: opPrepend,
	token.CONCAT:  opConcat,
}

func (c *compiler) binary(x ast.Node) cnode {
	op, left, right := c.src.Binary(x)
	switch op {
	case token.RPIPE:
		return c.node(opCall, int32(c.expr(right)), int32(c.expr(left)), x)
	case token.LPIPE:
		return c.node(opCall, int32(c.expr(left)), int32(c.expr(right)), x)
	case token.RCOMP, token.LCOMP:
		if op == token.LCOMP {
			left, right = right, left
		}
		u := c.open()
		first, second := c.expr(left), c.expr(right)
		c.close()
		return c.span(opCompose, []int32{int32(first), int32(second), u}, x)
	}
	o, ok := binaryOps[op]
	if !ok {
		c.error(x, fmt.Sprintf("unhandled %s operator", op))
	}
	return c.node(o, int32(c.expr(left)), int32(c.expr(right)), x)
}

// fn compiles a function, a Func node or a match function, in a new frame.
// A Func node is a match function of one arm with one pattern. The arms'
// slots overlap.
func (c *compiler) fn(x ast.Node) cnode {
	u := c.open()
	level, next, size := c.level, c.next, c.size
	c.level, c.next, c.size = c.level+1, 0, 0

	arms, arity := []ast.Node{x}, 1
	if c.src.Kind(x) == ast.MatchFunc {
		arms, arity = c.src.Arms(x), c.src.Arity(x)
	}
	xs := []int32{0, u, int32(arity)}
	for _, a := range arms {
		var pats []ast.Node
		var body ast.Node
		if c.src.Kind(a) == ast.Func {
			var pat ast.Node
			pat, body = c.src.Func(a)
			pats = []ast.Node{pat}
		} else {
			pats, body = c.src.Arm(a)
		}
		c.next = 0
		scope := len(c.values)
		xs = c.patterns(xs, pats, scope)
		xs = append(xs, int32(c.expr(body)))
		c.values = c.values[:scope]
	}
	xs[0] = int32(c.size)

	c.level, c.next, c.size = level, next, size
	c.close()
	return c.span(opLambda, xs, x)
}

// group compiles a where-group, whose bindings are mutually recursive, in
// the current frame.
func (c *compiler) group(x ast.Node) cnode {
	se := c.src
	body, binds := se.Group(x)
	values, types := len(c.values), len(c.types)

	byNode := make(map[ast.Node]int32, len(binds))
	for _, w := range binds {
		_, id, _, _ := se.Where(w)
		b := binder{name: c.name(id), slot: -1, where: w, sort: se.Sort(w)}
		if !b.sort {
			b.slot = c.alloc()
		}
		byNode[w] = c.bind(b)
	}

	for _, w := range binds {
		_, _, typ, val := se.Where(w)
		b := byNode[w]
		u := c.open()
		if typ != ast.None {
			c.typ(typ)
		}
		if c.binders[b].sort {
			c.typ(val)
		}
		c.close()
		c.binders[b].types = u
	}

	xs := []int32{0}
	for _, comp := range se.Components(binds, nil) {
		for _, w := range comp.Binds {
			_, _, _, val := se.Where(w)
			b := byNode[w]
			v := c.expr(val)
			switch c.ops[v] {
			case opLambda:
				_, u, _, _ := c.lambda(v)
				c.uses[u].home = b
			case opCompose:
				_, _, u := c.compose(v)
				c.uses[u].home = b
			}
			xs = append(xs, int32(c.binders[b].slot), int32(v))
		}
	}
	xs[0] = int32(c.expr(body))

	c.values, c.types = c.values[:values], c.types[:types]
	return c.span(opWhere, xs, x)
}

// pattern compiles a pattern, binding its names in the current frame.
// Names in c.values from scope onwards are bound by the same pattern.
func (c *compiler) pattern(x ast.Node, scope int) cnode {
	se := c.src
	switch se.Kind(x) {
	case ast.Ident:
		name := c.name(se.Main(x))
		if name == "_" {
			c.use(name)
			return c.node(patWild, 0, 0, x)
		}
		if lookup(c.values, name) >= scope {
			c.error(x, fmt.Sprintf("cannot bind %s twice", name))
		}
		slot := c.alloc()
		c.bind(binder{name: name, slot: slot, where: ast.None, types: -1})
		return c.node(patBind, int32(slot), 0, x)

	case ast.Literal:
		v, err := Literal(se, x)
		if err != nil {
			c.error(x, err.Error())
		}
		if _, ok := v.(Float); ok {
			c.error(x, ErrNoFloatMatch.Error())
		}
		return c.node(patLiteral, c.constant(v), 0, x)

	case ast.Variant:
		tag, payload := se.Variant(x)
		p := none
		if payload != ast.None {
			p = c.pattern(payload, scope)
		}
		return c.node(patVariant, c.label(tag), int32(p), x)

	case ast.Record:
		rest, fields := se.Record(x)
		s, keys := c.shape(fields)
		xs := []int32{int32(none), s}
		for i := range fields.Len() {
			xs = append(xs, keys[i], int32(c.pattern(fields.Value(i), scope)))
		}
		if rest != ast.None {
			xs[0] = int32(c.pattern(rest, scope))
		}
		return c.span(patRecord, xs, x)

	case ast.List:
		return c.span(patList, c.patterns(nil, se.List(x), scope), x)

	case ast.Binary:
		op, left, right := se.Binary(x)
		switch op {
		case token.PREPEND:
			return c.node(patCons, int32(c.pattern(left, scope)), int32(c.pattern(right, scope)), x)
		case token.APPEND:
			return c.node(patSnoc, int32(c.pattern(left, scope)), int32(c.pattern(right, scope)), x)
		case token.CONCAT:
			if se.Kind(left) == ast.List {
				elems := c.patterns([]int32{0}, se.List(left), scope)
				elems[0] = int32(c.pattern(right, scope))
				return c.span(patPrefix, elems, x)
			}
			if se.Kind(right) == ast.List {
				rest := c.pattern(left, scope)
				return c.span(patSuffix, c.patterns([]int32{int32(rest)}, se.List(right), scope), x)
			}
		}
	}

	c.error(x, "unsupported pattern")
	return none
}

// patterns appends the patterns xs to ps.
func (c *compiler) patterns(ps []int32, xs []ast.Node, scope int) []int32 {
	for _, x := range xs {
		ps = append(ps, int32(c.pattern(x, scope)))
	}
	return ps
}

// typ resolves the names in the type x, for printing.
func (c *compiler) typ(x ast.Node) {
	se := c.src
	switch se.Kind(x) {
	case ast.Ident:
		name := c.name(se.Main(x))
		c.use(name)
		i := lookup(c.types, name)
		if i >= 0 && c.types[i].b < 0 {
			return // a type variable
		}
		c.refer(x, c.types, i, true)
	case ast.Forall:
		param, t := se.Forall(x)
		name := c.name(param)
		c.use(name)
		c.types = append(c.types, entry{name, c.level, -1})
		c.typ(t)
		c.types = c.types[:len(c.types)-1]
	case ast.Func, ast.Call:
		l, r := se.Func(x)
		c.typ(l)
		c.typ(r)
	case ast.RecordType:
		tail, fields := se.Record(x)
		if tail != ast.None {
			c.typ(tail)
		}
		for i := range fields.Len() {
			c.typ(fields.Value(i))
		}
	case ast.Enum:
		tail, variants := se.Enum(x)
		if tail != ast.None {
			c.typ(tail)
		}
		for _, v := range variants {
			if _, payload := se.Variant(v); payload != ast.None {
				c.typ(payload)
			}
		}
	}
}
