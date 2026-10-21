package eval

import (
	"fmt"
	"slices"

	"github.com/Victorystick/scrapscript/ast"
	"github.com/Victorystick/scrapscript/label"
	"github.com/Victorystick/scrapscript/token"
	"github.com/Victorystick/scrapscript/types"
)

// The core that eval runs: a scrap and the scraps it refers to, after type
// checking, as one program, with names resolved to frame slots, literals
// decoded, pipes turned into calls and both kinds of function turned into
// lambdas with arms.
//
// Scraps are partial programs: the scraps that a scrap refers to are
// bindings of an outer where-group, in the order that they depend on each
// other, and each of its Import nodes reads one, as a name would. A scrap
// that is referred to more than once is bound once.
//
// A frame holds the values that one call of a function binds: its
// parameter, the names its match arms bind and the where-bindings in its
// body, outside the functions nested in it. The top level has a frame too.
// Each expression of a body is evaluated at most once per call, so each
// slot is written once, except that the arms of a match share slots: only
// one arm's names are bound at a time.
//
// Closures are flat: a lambda copies the values of the names it uses from
// outside it into its captures when it is made, since values never
// change. Code reads a name either from a slot of its own frame or from a
// capture of the function that it runs in.
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
	opLocal   // A: the slot in the current frame.
	opCapture // A: the index of the capture of the running function.
	opCall    // A: the function. B: the argument.
	opCallN   // A..B: the function, then two or more arguments.

	// Operators. A, B: the operands, of the types that the op names, which
	// the checker settled on.
	opAddInt
	opSubInt
	opMulInt
	opAddFloat
	opSubFloat
	opMulFloat
	opAppendList   // a +< b
	opAppendBytes  //
	opPrependList  // a >+ b
	opPrependBytes //
	opConcatList   // a ++ b
	opConcatText   //
	opConcatBytes  //

	opCompose // A..B: the function applied first, then second, then its uses.
	opLambda  // A..B: its frame's size, its uses, its arity n, its number of captures k, then k captures, then n patterns and a body per arm. If n > 1, its frame's first n slots are its arguments.
	opMatch   // A..B: the value matched, then a pattern and a body per arm. Its patterns bind slots of the current frame.
	opMatchN  // A..B: the first of n slots of the current frame, n, then n arguments, which go in those slots, then n patterns and a body per arm.
	opWhere   // A..B: the body, then a slot and value per binding, in evaluation order, with recursive ones' closures made first.
	opFill    // A: a lambda. B: the slot of the closure made for it before, whose captures it fills in.
	opRecord  // A..B: the record it is based on or none, its shape or -1, then a key and value per field.
	opAccess  // A: the record. B: the key, a label.ID.
	opField   // A: the record, of a closed type. B: the index of the field's value.
	opList    // A..B: the elements.
	opVariant // A: the tag, a label.ID. B: the payload, or none.

	// Patterns.
	patBind    // A: the slot it binds.
	patArg     // A: the slot of the argument it names, which already holds it.
	patWild    // _
	patLiteral // A: the index of the value in consts.
	patVariant // A: the tag, a label.ID. B: the payload, or none.
	patRecord  // A..B: the rest or none, the shape of its fields, then a key and pattern per field.
	patSplit   // A..B: the rest, the shape of the record, of a closed type, and of the rest, the index in the record of each of the rest's values, then a key and pattern per field.
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

// A core is a compiled program. Its nodes are expressions and patterns; the
// other arrays are pools that nodes refer to by index, as their op says.
type core struct {
	// The scraps it was compiled from, and the runs of nodes compiled from
	// each, which at refers into.
	srcs  []source
	spans []span

	// Per node, indexed by cnode. Node 0 is none.
	ops  []op
	data []data
	at   []ast.Node // its AST node, for errors and printing

	// Not per node; nodes refer to these by index.

	// The children of nodes with more than two, as ranges A..B: cnodes,
	// slots, label IDs and uses indices, by op.
	extra []int32
	// The values of literals and builtins, decoded once.
	consts []value
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
	// The uses of the static part of each where-group's body that has one,
	// by source and Where node.
	wheres []whereStatic

	// The node evaluated, and the number of slots in its frame.
	root cnode
	top  int
	// The source whose static part the value of root has: the root
	// scrap's, or if that is an import, the source it refers to.
	main int32
}

// A source is a scrap that a core was compiled from.
type source struct {
	se *ast.SourceExpr
	// The nodes with a static part.
	static types.NodeSet
	// The index in uses of what the static part of its expression refers
	// to, or -1 if it has none. A where-group's is in wheres.
	rootStatic int32
	// The reference to a published scrap, $source:…, which its value
	// prints as if it is a function; empty for a draft.
	ref string
}

// A span is a run of nodes compiled from the source src, from start up to
// the next span's start.
type span struct {
	start cnode
	src   int32
}

// source returns the source that the node n was compiled from.
func (c *core) source(n cnode) *ast.SourceExpr {
	i, _ := slices.BinarySearchFunc(c.spans, n+1, func(s span, n cnode) int {
		return int(s.start - n)
	})
	return c.srcs[c.spans[i-1].src].se
}

// A whereStatic is the uses of the static part of a where-group's body.
type whereStatic struct {
	src int32
	x   ast.Node
	u   int32
}

// whereUses returns the uses of the static part of the body of the
// where-group x in the source src, or -1 if it has none.
func (c *core) whereUses(src int32, x ast.Node) int32 {
	for _, w := range c.wheres {
		if w.src == src && w.x == x {
			return w.u
		}
	}
	return -1
}

// A binder is a name that a frame binds.
type binder struct {
	name  string
	slot  int      // -1 for a sort binding, which has no value
	src   int32    // the source it is in
	where ast.Node // its Where node, or None for a parameter, pattern name or scrap
	sort  bool
	// The index in uses of what its annotation, or a sort binding's type,
	// refers to.
	types int32
	// The index in uses of what its value's static part refers to, or -1
	// if it has none. Its members print from it, as does the binding
	// itself when only types refer to it.
	static int32
}

// A ref is what an Ident node in printed code refers to: a binder, or a
// builtin if b is -1. In a lambda's code, a value binder's value is the
// lambda's capture cap; otherwise cap is -1.
type ref struct {
	ident ast.Node
	b     int32
	cap   int32
}

// uses records what code that prints as part of a value refers to.
type uses struct {
	free  []ref    // its free names, from outside the code
	names []string // every name in it, bound or free
	home  int32    // the binder whose value it is, which it prints as, or -1
	src   int32    // the source of the code
	// The source of the published scrap whose value it is, which it
	// prints as the reference to, or -1.
	published int32
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
	return int(r[0]), r[1], int(r[2]), r[4+r[3]:]
}

// Captures returns what a lambda captures when it is made, in the order of
// its captures: a slot s of the frame it is made in as s, or capture i of
// the function it is made in as -1-i.
func (c *core) captures(n cnode) []int32 {
	r := c.range_(n)
	return r[4 : 4+r[3]]
}

// Compose returns the functions of a composition, in the order they are
// applied, and its uses.
func (c *core) compose(n cnode) (first, second cnode, u int32) {
	r := c.range_(n)
	return cnode(r[0]), cnode(r[1]), r[2]
}

// Where returns a where's body, and its bindings as pairs of slot and
// value. The bindings of a recursive component are functions that may
// capture each other, so before them come pairs of -1 minus a slot and a
// lambda: a closure of that lambda, whose captures its binding's value
// fills in later, with opFill.
func (c *core) where(n cnode) (body cnode, binds []int32) {
	r := c.range_(n)
	return cnode(r[0]), r[1:]
}

// Record returns the record or record pattern that a record is based on,
// its shape, and its fields as pairs of key and value.
//
// A record without a base has the shape of its fields, and a key is the
// index of its value in the record. A record with a base has the base's
// shape. If the base's type is closed, the shape is known, and a key is an
// index too; otherwise the shape is nil, and a key is a label.ID.
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

// split returns the rest of a patSplit, the shapes of the record it matches
// and of the rest, the index in the record of each of the rest's values,
// and its fields as pairs of the index of a value in the record and its
// pattern.
func (c *core) split(n cnode) (rest cnode, s, rs *shape, from, fields []int32) {
	r := c.range_(n)
	s, rs = c.shapes[r[1]], c.shapes[r[2]]
	k := 3 + len(rs.labels)
	return cnode(r[0]), s, rs, r[3:k], r[k:]
}

// Elems returns the rest pattern of a prefix or suffix pattern, and its
// elements.
func (c *core) elems(n cnode) (rest cnode, elems []int32) {
	r := c.range_(n)
	return cnode(r[0]), r[1:]
}

// A unit is a scrap to compile, which must have been type-checked.
type unit struct {
	se *ast.SourceExpr
	// The type of each node, from type checking, whose refs are in the
	// registry. Only code without operators may be compiled without it.
	typed []types.TypeRef
	// The nodes with a static part.
	static types.NodeSet
	// Whether it denotes a type, which has no value.
	sort bool
	// The reference to it if it is published, $source:…, or empty.
	ref string
	// The index among the units of the scrap that each Import node refers
	// to, which comes before it.
	deps map[ast.Node]int32
}

// compile lowers the last of units and the scraps it refers to, the units
// before it, in the order that they depend on each other, into one core.
// Free names that they don't bind refer to globals. Tags and record keys
// are interned in the label table of shapes, and the shapes of records in
// shapes.
func compile(units []unit, reg *types.Registry, globals variables, shapes *shapeTable) (*core, error) {
	root := int32(len(units) - 1)
	c := compiler{
		core:     core{labels: shapes.labels, table: shapes, spans: []span{{0, root}}},
		units:    units,
		reg:      reg,
		globals:  globals,
		builtins: make(map[string]int32),
		scraps:   make([]int32, len(units)),
		inlines:  make(map[int32]inlinable),
		imported: make(map[int32]int32),
		exports:  make([]map[string]int32, len(units)),
	}
	c.srcs = make([]source, len(units))
	for i, u := range units {
		c.srcs[i] = source{se: u.se, static: u.static, rootStatic: -1, ref: u.ref}
	}
	c.node(opNone, 0, 0, ast.None)
	c.fns = [][]capture{nil}

	// Each scrap that the root refers to is a binding of a where-group
	// around it, unless it is a type.
	var binds []int32
	for i := range root {
		c.scraps[i] = -1
		if units[i].sort {
			continue
		}
		restore := c.enter(i)
		v := c.scrap()
		if units[i].ref != "" {
			if u := c.result(v); u >= 0 {
				c.uses[u].published = i
			}
		}
		slot := c.alloc()
		c.scraps[i] = int32(len(c.binders))
		c.binders = append(c.binders, binder{slot: slot, src: i, where: ast.None, types: -1, static: -1})
		binds = append(binds, int32(slot), int32(v))
		restore()
	}
	c.enter(root)
	c.root = c.scrap()
	if binds != nil {
		c.root = c.span(opWhere, append([]int32{int32(c.root)}, binds...), units[root].se.Expr)
	}
	c.main = root
	for x := units[root].se.Expr; units[c.main].se.Kind(x) == ast.Import; x = units[c.main].se.Expr {
		c.main = units[c.main].deps[x]
	}
	if c.err != nil {
		return nil, c.err
	}
	c.top = c.size
	return &c.core, nil
}

// scrap compiles the expression of the current source, and resolves its
// static part for printing.
func (c *compiler) scrap() cnode {
	x := c.src.Expr
	c.budget = inlineBudget
	c.export = x
	for c.src.Kind(c.export) == ast.Where {
		c.export, _ = c.src.Group(c.export)
	}
	v := c.expr(x)
	if c.static.Has(x) {
		c.srcs[c.cur].rootStatic = c.open(false)
		c.staticPart(x)
		c.close()
	}
	return v
}

// enter starts compiling the source i, and returns a function that goes
// back to the source before it.
func (c *compiler) enter(i int32) (restore func()) {
	prev := c.cur
	c.switchTo(i)
	return func() { c.switchTo(prev) }
}

func (c *compiler) switchTo(i int32) {
	c.cur = i
	u := c.units[i]
	c.src, c.typed, c.static = u.se, u.typed, u.static
	n := cnode(len(c.ops))
	if last := &c.spans[len(c.spans)-1]; last.start == n {
		last.src = i
	} else {
		c.spans = append(c.spans, span{n, i})
	}
}

// result returns the index in uses of the function that the top-level
// expression v evaluates to, if it is made once, as the value of a scrap or
// of a binding of a where-group around it; otherwise -1.
func (c *compiler) result(v cnode) int32 {
	var binds []int32
	for {
		switch c.ops[v] {
		case opWhere:
			var bs []int32
			v, bs = c.where(v)
			binds = append(binds, bs...)
		case opFill:
			v = cnode(c.data[v].a)
		case opLocal:
			slot, found := c.data[v].a, false
			for i := 0; i < len(binds); i += 2 {
				if binds[i] == slot {
					v, found = cnode(binds[i+1]), true
				}
			}
			if !found {
				return -1
			}
		case opLambda:
			_, u, _, _ := c.lambda(v)
			return u
		case opCompose:
			_, _, u := c.compose(v)
			return u
		default:
			return -1
		}
	}
}

type compiler struct {
	core
	units   []unit
	reg     *types.Registry
	globals variables
	// The index in consts of each builtin referred to.
	builtins map[string]int32
	err      error

	// The source being compiled: its index, its expression, the types of
	// its nodes and its nodes with a static part.
	cur    int32
	src    *ast.SourceExpr
	typed  []types.TypeRef
	static types.NodeSet
	// The binder of each unit's value, or -1 for a type.
	scraps []int32

	// The frame being compiled: how deeply it is nested, its next free
	// slot and its size so far.
	level, next, size int
	// The captures of the function of each frame being compiled, by level.
	fns [][]capture

	// The names in scope, innermost last, of values and of types.
	values, types []entry
	// The printed code being compiled, innermost last.
	roots []root

	// The functions that calls may be inlined into, by binder; how many
	// inlined bodies are being compiled; and how many more tokens of code
	// may be inlined into the function being compiled by size.
	inlines map[int32]inlinable
	depth   int
	budget  int
	// The unit that each binder bound to an import refers to, and the
	// binder of each field of each unit whose value is a record literal,
	// which other scraps may inline calls to.
	imported map[int32]int32
	exports  []map[string]int32
	// The record literal that the current unit's value is, if it is one.
	export ast.Node
}

// The most tokens that a function may have to be inlined by size, and the
// most tokens that may be inlined so into one function. An inlined call
// keeps its slots in the caller's frame, so inlining more can make frames
// so large that calls get slower; BENCHMARK.md has the sweep.
const (
	inlineMax    = 40
	inlineBudget = 4 * inlineMax
)

// An inlinable is a where-bound function literal that isn't recursive, so
// that a call to it with all its arguments may compile as a match of its
// arms in the caller's frame.
type inlinable struct {
	val   ast.Node
	arity int
	src   int32
	// The number of names in scope where it is bound, of values and of
	// types, which its body sees.
	values, types int
	// The names in scope where it is bound, if that is at the top level of
	// its scrap, where other scraps may call it from.
	scope *scope
	// Whether one call is the only reference to it.
	once bool
}

type scope struct {
	values, types []entry
}

type entry struct {
	name  string
	level int
	b     int32 // the binder, or -1 for a type variable
}

// A capture is a binder that a function captures, and what it captures it
// from, as captures says.
type capture struct {
	b, from int32
}

// A root is printed code being compiled, which collects into uses[u].
type root struct {
	u int32
	// The frame it is in, and the number of names in scope outside it.
	level         int
	values, types int
	// Whether it is a lambda, whose frame is one level further in.
	lambda bool
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

func (c *compiler) constant(v value) int32 {
	c.consts = append(c.consts, v)
	return int32(len(c.consts) - 1)
}

// builtin returns the index in consts of the builtin name, which x refers
// to, adding it the first time.
func (c *compiler) builtin(x ast.Node, name string) int32 {
	i, ok := c.builtins[name]
	if !ok {
		v := c.globals[name]
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

// valueFields returns the fields of a record that are values. The others are
// type members, which the checker gave no type, and which are erased.
func (c *compiler) valueFields(fields ast.Fields) ast.Fields {
	if c.typed == nil {
		return fields
	}
	var kept ast.Fields
	for i := range fields.Len() {
		if c.typed[fields.Value(i)] == types.NoType {
			if kept == nil {
				kept = slices.Clip(fields[:2*i])
			}
			continue
		}
		if kept != nil {
			kept = append(kept, ast.Node(fields.Key(i)), fields.Value(i))
		}
	}
	if kept == nil {
		return fields
	}
	return kept
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
	return c.addShape(s), keys
}

// addShape adds s to the shapes, returning its index.
func (c *compiler) addShape(s *shape) int32 {
	c.shapes = append(c.shapes, s)
	return int32(len(c.shapes) - 1)
}

// closed returns the shape of the records that x evaluates to or matches,
// if the checker closed their type, so that they have just its labels;
// otherwise nil.
func (c *compiler) closed(x ast.Node) *shape {
	if c.typed == nil {
		return nil
	}
	t := c.reg.Shape(c.typed[x])
	if t.Kind != types.ShapeRecord || t.Open {
		return nil
	}
	labels := make([]label.ID, len(t.Labels))
	for i, l := range t.Labels {
		labels[i] = c.labels.Of(l.Name)
	}
	c.table.sort(labels)
	return c.table.of(labels)
}

// open starts collecting what code in the current frame refers to into a
// new uses, returning its index; close stops it. lambda says whether the
// code is a lambda.
func (c *compiler) open(lambda bool) int32 {
	u := int32(len(c.uses))
	c.uses = append(c.uses, uses{home: -1, src: c.cur, published: -1})
	c.roots = append(c.roots, root{u, c.level, len(c.values), len(c.types), lambda})
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

// refer records that the Ident x refers to the i'th entry of the values in
// scope, or of the types, or to a builtin if i is -1, in the code that it
// is free in. A lambda captures a value that it refers to, unless only its
// static part is used, as in a type.
func (c *compiler) refer(x ast.Node, i int, values, capture bool) {
	for _, r := range c.roots {
		scope, mark := c.types, r.types
		if values {
			scope, mark = c.values, r.values
		}
		u := &c.uses[r.u]
		if i < 0 {
			u.free = append(u.free, ref{x, -1, -1})
		} else if i < mark {
			e := scope[i]
			cap := int32(-1)
			if r.lambda && capture {
				cap = c.capture(r.level+1, e)
			}
			u.free = append(u.free, ref{x, e.b, cap})
		}
	}
}

// capture returns the index of the capture of e by the function of the
// frame at level, which e is bound outside of, adding it to the functions
// between them the first time.
func (c *compiler) capture(level int, e entry) int32 {
	for i, x := range c.fns[level] {
		if x.b == e.b {
			return int32(i)
		}
	}
	from := int32(c.binders[e.b].slot)
	if e.level < level-1 {
		from = -1 - c.capture(level-1, e)
	}
	c.fns[level] = append(c.fns[level], capture{e.b, from})
	return int32(len(c.fns[level]) - 1)
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
	b.src = c.cur
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
		v, err := literal(se, x)
		if err != nil {
			c.error(x, err.Error())
		}
		return c.node(opConst, c.constant(v), 0, x)

	case ast.Ident:
		name := c.name(se.Main(x))
		c.use(name)
		i := lookup(c.values, name)
		c.refer(x, i, true, true)
		if i < 0 {
			return c.node(opConst, c.builtin(x, name), 0, x)
		}
		e := c.values[i]
		if e.level < c.level {
			return c.node(opCapture, c.capture(c.level, e), 0, x)
		}
		return c.node(opLocal, int32(c.binders[e.b].slot), 0, x)

	case ast.Binary:
		return c.binary(x)

	case ast.Call:
		fn, arg := se.Call(x)
		return c.call(x, fn, arg)

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
		fields = c.valueFields(fields)
		if rest != ast.None {
			xs := []int32{int32(c.expr(rest)), -1}
			s := c.closed(rest)
			if s != nil {
				xs[1] = c.addShape(s)
			}
			for i := range fields.Len() {
				key := c.label(fields.Key(i))
				if s != nil {
					key = int32(s.index(label.ID(key)))
				}
				xs = append(xs, key, int32(c.expr(fields.Value(i))))
			}
			return c.span(opRecord, xs, x)
		}
		if x == c.export && c.level == 0 && c.depth == 0 {
			c.exportFields(fields)
		}
		s, keys := c.shape(fields)
		xs := []int32{int32(none), s}
		for i := range fields.Len() {
			xs = append(xs, keys[i], int32(c.expr(fields.Value(i))))
		}
		return c.span(opRecord, xs, x)

	case ast.Access:
		rec, key := se.Access(x)
		if s := c.closed(rec); s != nil {
			return c.node(opField, int32(c.expr(rec)), int32(s.index(label.ID(c.label(key)))), x)
		}
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
		// It reads the binding of the scrap that it refers to.
		b := c.scraps[c.units[c.cur].deps[x]]
		if b < 0 {
			c.error(x, "cannot evaluate a type")
			return none
		}
		if c.level > 0 {
			return c.node(opCapture, c.capture(c.level, entry{level: 0, b: b}), 0, x)
		}
		return c.node(opLocal, int32(c.binders[b].slot), 0, x)
	}

	c.error(x, fmt.Sprintf("unhandled node %s", se.Kind(x)))
	return none
}

// binaryOps gives the op of an operator by the type of its result, which
// the checker settles: a primitive, or NoType for any list.
var binaryOps = map[binaryOp]op{
	{token.ADD, types.IntRef}:       opAddInt,
	{token.SUB, types.IntRef}:       opSubInt,
	{token.MUL, types.IntRef}:       opMulInt,
	{token.ADD, types.FloatRef}:     opAddFloat,
	{token.SUB, types.FloatRef}:     opSubFloat,
	{token.MUL, types.FloatRef}:     opMulFloat,
	{token.APPEND, types.NoType}:    opAppendList,
	{token.APPEND, types.BytesRef}:  opAppendBytes,
	{token.PREPEND, types.NoType}:   opPrependList,
	{token.PREPEND, types.BytesRef}: opPrependBytes,
	{token.CONCAT, types.NoType}:    opConcatList,
	{token.CONCAT, types.TextRef}:   opConcatText,
	{token.CONCAT, types.BytesRef}:  opConcatBytes,
}

type binaryOp struct {
	tok    token.Token
	result types.TypeRef
}

func (c *compiler) binary(x ast.Node) cnode {
	op, left, right := c.src.Binary(x)
	switch op {
	case token.RPIPE:
		return c.call(x, right, left)
	case token.LPIPE:
		return c.call(x, left, right)
	case token.RCOMP, token.LCOMP:
		if op == token.LCOMP {
			left, right = right, left
		}
		// A composition prints as its functions, so its uses has only its
		// home.
		u := int32(len(c.uses))
		c.uses = append(c.uses, uses{home: -1, src: c.cur, published: -1})
		first, second := c.expr(left), c.expr(right)
		return c.span(opCompose, []int32{int32(first), int32(second), u}, x)
	}
	result := c.reg.Resolve(c.typed[x])
	if c.reg.Shape(result).Kind == types.ShapeList {
		result = types.NoType
	}
	o, ok := binaryOps[binaryOp{op, result}]
	if !ok {
		c.error(x, fmt.Sprintf("unhandled %s operator", op))
	}
	return c.node(o, int32(c.expr(left)), int32(c.expr(right)), x)
}

// call compiles the call x of fn to arg. Calls whose function is itself a
// call, f a b, become one call with all the arguments.
func (c *compiler) call(x, fn, arg ast.Node) cnode {
	args := []ast.Node{arg}
	for c.src.Kind(fn) == ast.Call {
		var a ast.Node
		fn, a = c.src.Call(fn)
		args = append(args, a)
	}
	slices.Reverse(args)
	if len(args) == 1 {
		if arms := c.arms(fn); arms != nil {
			return c.match(fn, c.expr(arg), arms)
		}
	}
	if n, ok := c.inline(fn, args); ok {
		return n
	}
	if len(args) == 1 {
		return c.node(opCall, int32(c.expr(fn)), int32(c.expr(arg)), x)
	}
	xs := []int32{int32(c.expr(fn))}
	for _, a := range args {
		xs = append(xs, int32(c.expr(a)))
	}
	return c.span(opCallN, xs, x)
}

// arms returns the arms of fn, as pairs of pattern and body, if it is a
// function literal of one argument: a match function of one argument, or a
// Func node whose body is neither.
func (c *compiler) arms(fn ast.Node) []ast.Node {
	switch c.src.Kind(fn) {
	case ast.Func:
		param, body := c.src.Func(fn)
		if k := c.src.Kind(body); k == ast.Func || k == ast.MatchFunc {
			return nil
		}
		return []ast.Node{param, body}
	case ast.MatchFunc:
		if c.src.Arity(fn) != 1 {
			return nil
		}
		var arms []ast.Node
		for _, a := range c.src.Arms(fn) {
			pats, body := c.src.Arm(a)
			arms = append(arms, pats[0], body)
		}
		return arms
	}
	return nil
}

// match compiles a function literal fn of one argument, applied to arg, as
// a match in the current frame, which makes neither a closure nor a frame.
// arms are fn's patterns and bodies. The arms' slots overlap each other and
// what follows the match: only code in an arm reads them, and closures
// copy what they capture.
func (c *compiler) match(fn ast.Node, arg cnode, arms []ast.Node) cnode {
	xs := []int32{int32(arg)}
	start := c.next
	for i := 0; i < len(arms); i += 2 {
		c.next = start
		scope := len(c.values)
		xs = append(xs, int32(c.pattern(arms[i], scope)), int32(c.expr(arms[i+1])))
		c.values = c.values[:scope]
	}
	c.next = start
	return c.span(opMatch, xs, fn)
}

// inline compiles the call of fn to args as a match, as if fn's value were
// written in its place, if fn names an inlinable function of that many
// arguments that is small enough, or that nothing else refers to. Calls in
// an inlined body are inlined by size alone, so that its copies stay
// bounded.
func (c *compiler) inline(fn ast.Node, args []ast.Node) (cnode, bool) {
	in, name, i, ok := c.callee(fn)
	if !ok || in.arity != len(args) {
		return none, false
	}
	se := c.units[in.src].se
	size := int(se.Last(in.val)-se.First(in.val)) + 1
	switch {
	case in.once && c.depth == 0 && in.src == c.cur:
	case size <= inlineMax && size <= c.budget:
		c.budget -= size
	default:
		return none, false
	}
	// The call prints as itself, so it still refers to the name it calls
	// by.
	if name != ast.None {
		c.use(c.name(c.src.Main(name)))
		c.refer(name, i, true, true)
	}
	// The arguments of a function of more than one go in slots of their
	// own, as in its frame, before the slots of code that computes them,
	// since each is put there before the next is computed.
	start := c.next
	if in.arity > 1 {
		for range args {
			c.alloc()
		}
	}
	vs := make([]int32, len(args))
	for i, a := range args {
		vs[i] = int32(c.expr(a))
	}

	// The body sees the names in scope where fn is bound, in its own
	// scrap. It refers to them from this frame, but prints only as part of
	// fn's value.
	values, types, roots := c.values, c.types, c.roots
	outer := scope{values, types}
	if in.src != c.cur {
		outer = *in.scope
		defer c.enter(in.src)()
	}
	c.values, c.types, c.roots = slices.Clip(outer.values[:in.values]), slices.Clip(outer.types[:in.types]), nil
	c.depth++
	var n cnode
	if in.arity == 1 {
		n = c.match(in.val, cnode(vs[0]), c.arms(in.val))
	} else {
		xs := append([]int32{int32(start), int32(in.arity)}, vs...)
		n = c.span(opMatchN, c.fnArms(xs, c.literal(in.val), start, start+in.arity), in.val)
		c.next = start
	}
	c.depth--
	c.values, c.types, c.roots = values, types, roots
	return n, true
}

// callee returns the inlinable function that fn names, if any: a binding
// in scope, or a field of another scrap whose value is a record literal,
// through an import or a name bound to one. name is the Ident that fn
// refers to it by, the i'th value in scope, or None.
func (c *compiler) callee(fn ast.Node) (in inlinable, name ast.Node, i int, ok bool) {
	se := c.src
	var key string
	if se.Kind(fn) == ast.Access {
		rec, k := se.Access(fn)
		fn, key = rec, c.name(k)
	}
	switch se.Kind(fn) {
	case ast.Ident:
		name, i = fn, lookup(c.values, c.name(se.Main(fn)))
		if i < 0 {
			return in, ast.None, 0, false
		}
		b := c.values[i].b
		if key == "" {
			in, ok = c.inlines[b]
			return in, name, i, ok
		}
		u, ok := c.imported[b]
		if !ok {
			return in, ast.None, 0, false
		}
		in, ok = c.exported(u, key)
		return in, name, i, ok
	case ast.Import:
		if key != "" {
			in, ok = c.exported(c.units[c.cur].deps[fn], key)
		}
	}
	return in, ast.None, 0, ok
}

// exported returns the inlinable function that the field key of the unit u
// is, if it is a top-level binding there.
func (c *compiler) exported(u int32, key string) (inlinable, bool) {
	b, ok := c.exports[u][key]
	if !ok {
		return inlinable{}, false
	}
	in, ok := c.inlines[b]
	return in, ok && in.scope != nil
}

// fn compiles a function, a Func node or a match function, in a new frame,
// whose first slots are its arguments. A chain of Func nodes, a -> b -> …,
// is one function that takes an argument per node, and if the chain ends
// in a match function, its arguments too. A Func node is a match function
// of one arm with one pattern. The arms' slots overlap.
func (c *compiler) fn(x ast.Node) cnode {
	u := c.open(true)
	level, next, size, budget := c.level, c.next, c.size, c.budget
	c.level, c.next, c.size, c.budget = c.level+1, 0, 0, inlineBudget
	c.fns = append(c.fns[:c.level], nil)

	f := c.literal(x)
	// A function of one argument matches it as it is; one of more keeps
	// them in its first slots, for its arms to match.
	args := 0
	if f.arity > 1 {
		args = f.arity
	}
	c.size = args
	xs := c.fnArms(nil, f, 0, args)

	caps := c.fns[c.level]
	head := []int32{int32(c.size), u, int32(f.arity), int32(len(caps))}
	for _, x := range caps {
		head = append(head, x.from)
	}

	c.level, c.next, c.size, c.budget = level, next, size, budget
	c.close()
	return c.span(opLambda, append(head, xs...), x)
}

// A fnLiteral is a function literal: a chain of Func nodes, a -> b -> …,
// each of which takes an argument, perhaps ending in a match function,
// which takes its arguments too.
type fnLiteral struct {
	params []ast.Node // the Func nodes' parameters
	// The match function's arms if match, or else the chain's body.
	arms  []ast.Node
	match bool
	arity int
}

func (c *compiler) literal(x ast.Node) fnLiteral {
	var f fnLiteral
	body := x
	for c.src.Kind(body) == ast.Func {
		var param ast.Node
		param, body = c.src.Func(body)
		f.params = append(f.params, param)
	}
	f.arms = []ast.Node{body}
	f.arity = len(f.params)
	f.match = c.src.Kind(body) == ast.MatchFunc
	if f.match {
		f.arms = c.src.Arms(body)
		f.arity += c.src.Arity(body)
	}
	return f
}

// fnArms appends to xs the patterns and body of each arm of f, as a
// function of one argument matches it, or else for arguments in the slots
// from base. Each arm's own slots start at next and overlap the other
// arms'.
func (c *compiler) fnArms(xs []int32, f fnLiteral, base, next int) []int32 {
	for _, a := range f.arms {
		c.next = next
		scope := len(c.values)
		// Each parameter is a scope of its own, which may shadow the ones
		// before it.
		for i, param := range f.params {
			xs = append(xs, int32(c.param(param, base+i, f.arity, len(c.values))))
		}
		body := a
		if f.match {
			var pats []ast.Node
			pats, body = c.src.Arm(a)
			arm := len(c.values)
			for i, pat := range pats {
				xs = append(xs, int32(c.param(pat, base+len(f.params)+i, f.arity, arm)))
			}
		}
		xs = append(xs, int32(c.expr(body)))
		c.values = c.values[:scope]
	}
	return xs
}

// group compiles a where-group, whose bindings are mutually recursive, in
// the current frame.
func (c *compiler) group(x ast.Node) cnode {
	se := c.src
	body, binds := se.Group(x)
	values, types, start := len(c.values), len(c.types), c.next

	byNode := make(map[ast.Node]int32, len(binds))
	for _, w := range binds {
		_, id, _, val := se.Where(w)
		b := binder{name: c.name(id), slot: -1, where: w, sort: se.Sort(w), static: -1}
		if !b.sort {
			b.slot = c.alloc()
			if c.static.Has(val) {
				b.static = 0 // its uses come below
			}
		}
		byNode[w] = c.bind(b)
		if se.Kind(val) == ast.Import {
			c.imported[byNode[w]] = c.units[c.cur].deps[val]
		}
	}

	for _, w := range binds {
		_, _, typ, val := se.Where(w)
		b := byNode[w]
		u := c.open(false)
		if typ != ast.None {
			c.typ(typ)
		}
		if c.binders[b].sort {
			c.typ(val)
		}
		c.close()
		c.binders[b].types = u
		if c.binders[b].static >= 0 {
			u := c.open(false)
			c.staticPart(val)
			c.close()
			c.binders[b].static = u
		}
	}
	if c.static.Has(body) {
		u := c.open(false)
		c.staticPart(body)
		c.close()
		c.wheres = append(c.wheres, whereStatic{c.cur, x, u})
	}

	comps := se.Components(binds, nil)
	c.inlinable(body, binds, comps, byNode)

	xs := []int32{0}
	for _, comp := range comps {
		var made []int32
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
			slot := int32(c.binders[b].slot)
			if comp.Recursive {
				var l cnode
				v, l = c.fill(v, slot)
				made = append(made, -1-slot, int32(l))
			}
			xs = append(xs, slot, int32(v))
		}
		if made != nil {
			// The closures go before the bindings, which capture them.
			start := len(xs) - 2*len(comp.Binds)
			xs = slices.Insert(xs, start, made...)
		}
	}
	xs[0] = int32(c.expr(body))

	c.values, c.types = c.values[:values], c.types[:types]
	// Code after the group may reuse its slots, as after a match, but not
	// at the top level, where other scraps' code that is inlined reads
	// them.
	if c.level > 0 {
		c.next = start
	}
	return c.span(opWhere, xs, x)
}

// inlinable notes the group's bindings that calls may be inlined into: the
// function literals that aren't recursive.
func (c *compiler) inlinable(body ast.Node, binds []ast.Node, comps []ast.Component, byNode map[ast.Node]int32) {
	var counts []int
	var sc *scope
	for _, comp := range comps {
		w := comp.Binds[0]
		_, _, _, val := c.src.Where(w)
		if k := c.src.Kind(val); comp.Recursive || k != ast.Func && k != ast.MatchFunc {
			continue
		}
		if counts == nil {
			counts = c.src.References(body, binds)
			if c.level == 0 {
				sc = &scope{slices.Clone(c.values), slices.Clone(c.types)}
			}
		}
		// The group's binders are in the order of binds.
		b := byNode[w]
		c.inlines[b] = inlinable{val, c.literal(val).arity, c.cur, len(c.values), len(c.types), sc, counts[b-byNode[binds[0]]] == 1}
	}
}

// exportFields notes the binder of each field of the record literal that
// the current unit's value is, whose value is a name.
func (c *compiler) exportFields(fields ast.Fields) {
	m := make(map[string]int32)
	for i := range fields.Len() {
		v := fields.Value(i)
		if c.src.Kind(v) != ast.Ident {
			continue
		}
		if j := lookup(c.values, c.name(c.src.Main(v))); j >= 0 {
			m[c.name(fields.Key(i))] = c.values[j].b
		}
	}
	c.exports[c.cur] = m
}

// typeName resolves the Ident x in type position: a type variable, a sort
// binding, or a value binding with a static part. As in the checker, a
// value without one doesn't hide a type of the same name.
func (c *compiler) typeName(x ast.Node) {
	name := c.name(c.src.Main(x))
	c.use(name)
	t := lookup(c.types, name)
	if t >= 0 && c.types[t].b < 0 {
		return // a type variable
	}
	for v := len(c.values) - 1; v >= 0; v-- {
		e := c.values[v]
		if e.name != name || c.binders[e.b].static < 0 {
			continue
		}
		// The innermost binder was bound last.
		if t < 0 || e.b > c.types[t].b {
			c.refer(x, v, true, false)
			return
		}
		break
	}
	c.refer(x, t, false, false)
}

// staticPart resolves the names that the static part of the expression x
// refers to, for printing: the parts of x that its members come from. The
// static part of a where-group's body has its own uses, in wheres.
func (c *compiler) staticPart(x ast.Node) {
	se := c.src
	switch se.Kind(x) {
	case ast.Ident, ast.Access:
		c.typ(x)
	case ast.Record:
		_, fields := se.Record(x)
		for i := range fields.Len() {
			if v := fields.Value(i); c.static.Has(v) {
				c.staticPart(v)
			}
		}
	}
}

// fill makes the value v of a recursive binding in slot fill in the
// closure made for it before, instead of making one, and returns it and
// its lambda. The checker only lets functions be recursive, so the value
// is a lambda, perhaps as the body of a where.
func (c *compiler) fill(v cnode, slot int32) (cnode, cnode) {
	if c.ops[v] == opWhere {
		body, l := c.fill(cnode(c.extra[c.data[v].a]), slot)
		c.extra[c.data[v].a] = int32(body)
		return v, l
	}
	return c.node(opFill, int32(v), slot, c.at[v]), v
}

// param compiles the pattern x of an argument of a function of arity
// arguments. If it takes more than one, the argument is in slot, and a name
// binds that slot.
func (c *compiler) param(x ast.Node, slot, arity, scope int) cnode {
	if arity == 1 || c.src.Kind(x) != ast.Ident || c.src.TokenText(c.src.Main(x)) == "_" {
		return c.pattern(x, scope)
	}
	name := c.name(c.src.Main(x))
	if lookup(c.values, name) >= scope {
		c.error(x, fmt.Sprintf("cannot bind %s twice", name))
	}
	c.bind(binder{name: name, slot: slot, where: ast.None, types: -1, static: -1})
	return c.node(patArg, int32(slot), 0, x)
}

// split compiles the record pattern x, with a rest, that matches records
// of the closed shape s: its fields and the rest's values are at static
// indices.
func (c *compiler) split(x ast.Node, s *shape, rest ast.Node, fields ast.Fields, scope int) cnode {
	labels := make([]label.ID, fields.Len())
	pairs := make([]int32, 0, 2*fields.Len())
	for i := range labels {
		labels[i] = label.ID(c.label(fields.Key(i)))
		pairs = append(pairs, int32(s.index(labels[i])), int32(c.pattern(fields.Value(i), scope)))
	}
	c.table.sort(labels)
	rs := c.table.without(s, c.table.of(labels))
	xs := []int32{int32(c.pattern(rest, scope)), c.addShape(s), c.addShape(rs)}
	for _, l := range rs.labels {
		xs = append(xs, int32(s.index(l)))
	}
	return c.span(patSplit, append(xs, pairs...), x)
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
		c.bind(binder{name: name, slot: slot, where: ast.None, types: -1, static: -1})
		return c.node(patBind, int32(slot), 0, x)

	case ast.Literal:
		v, err := literal(se, x)
		if err != nil {
			c.error(x, err.Error())
		}
		if _, ok := v.(floatValue); ok {
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
		if s := c.closed(x); rest != ast.None && s != nil {
			return c.split(x, s, rest, fields, scope)
		}
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
		c.typeName(x)
	case ast.Access:
		// A path to a type member, which refers to the static part of
		// the value at its head.
		rec, _ := se.Access(x)
		c.typ(rec)
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
