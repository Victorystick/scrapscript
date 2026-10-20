package eval

import (
	"fmt"
	"slices"

	"github.com/Victorystick/scrapscript/ast"
	"github.com/Victorystick/scrapscript/label"
	"github.com/Victorystick/scrapscript/token"
	"github.com/Victorystick/scrapscript/types"
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
	opImport  // The value of the scrap that the AST node refers to.
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
	opWhere   // A..B: the body, then a slot and value per binding, in evaluation order, with recursive ones' closures made first.
	opFill    // A: a lambda. B: the slot of the closure made for it before, whose captures it fills in.
	opRecord  // A..B: the record it is based on or none, its shape or -1 if it has a base, then a key and value per field.
	opAccess  // A: the record. B: the key, a label.ID.
	opList    // A..B: the elements.
	opVariant // A: the tag, a label.ID. B: the payload, or none.

	// Patterns.
	patBind    // A: the slot it binds.
	patArg     // A: the slot of the argument it names, which already holds it.
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
	// The nodes with a static part, and the uses of the static part of each
	// where-group's body that has one, by Where node.
	static types.NodeSet
	wheres []whereStatic

	// The node evaluated, and the number of slots in its frame.
	root cnode
	top  int
	// The index in uses of what the static part of the node evaluated
	// refers to, or -1 if it has none. A where-group's is in wheres.
	rootStatic int32
}

// A whereStatic is the uses of the static part of a where-group's body.
type whereStatic struct {
	x ast.Node
	u int32
}

// whereUses returns the uses of the static part of the body of the
// where-group x, or -1 if it has none.
func (c *core) whereUses(x ast.Node) int32 {
	for _, w := range c.wheres {
		if w.x == x {
			return w.u
		}
	}
	return -1
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
//
// typed is the type of each node, from type checking, whose refs are in
// reg. Only code without operators may be compiled without it.
func compile(se *ast.SourceExpr, reg *types.Registry, typed []types.TypeRef, static types.NodeSet, globals Vars, shapes *shapeTable) (*core, error) {
	c := compiler{
		core:     core{src: se, labels: shapes.labels, table: shapes, static: static},
		reg:      reg,
		typed:    typed,
		globals:  globals,
		builtins: make(map[string]int32),
	}
	c.node(opNone, 0, 0, ast.None)
	c.fns = [][]capture{nil}
	c.root = c.expr(se.Expr)
	c.rootStatic = -1
	if static.Has(se.Expr) {
		c.rootStatic = c.open(false)
		c.staticPart(se.Expr)
		c.close()
	}
	if c.err != nil {
		return nil, c.err
	}
	c.top = c.size
	return &c.core, nil
}

type compiler struct {
	core
	reg     *types.Registry
	typed   []types.TypeRef
	globals Vars
	// The index in consts of each builtin referred to.
	builtins map[string]int32
	err      error

	// The frame being compiled: how deeply it is nested, its next free
	// slot and its size so far.
	level, next, size int
	// The captures of the function of each frame being compiled, by level.
	fns [][]capture

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
	c.shapes = append(c.shapes, s)
	return int32(len(c.shapes) - 1), keys
}

// open starts collecting what code in the current frame refers to into a
// new uses, returning its index; close stops it. lambda says whether the
// code is a lambda.
func (c *compiler) open(lambda bool) int32 {
	u := int32(len(c.uses))
	c.uses = append(c.uses, uses{home: -1})
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
		c.uses = append(c.uses, uses{home: -1})
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
	if len(args) == 1 {
		if arms := c.arms(fn); arms != nil {
			return c.match(fn, arg, arms)
		}
		return c.node(opCall, int32(c.expr(fn)), int32(c.expr(arg)), x)
	}
	xs := []int32{int32(c.expr(fn))}
	for i := len(args) - 1; i >= 0; i-- {
		xs = append(xs, int32(c.expr(args[i])))
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
// arms are fn's patterns and bodies. The arms' slots overlap, but not with
// the slots of what follows the match, which closures made in an arm may
// still read.
func (c *compiler) match(fn, arg ast.Node, arms []ast.Node) cnode {
	xs := []int32{int32(c.expr(arg))}
	start, end := c.next, c.next
	for i := 0; i < len(arms); i += 2 {
		c.next = start
		scope := len(c.values)
		xs = append(xs, int32(c.pattern(arms[i], scope)), int32(c.expr(arms[i+1])))
		c.values = c.values[:scope]
		end = max(end, c.next)
	}
	c.next = end
	return c.span(opMatch, xs, fn)
}

// fn compiles a function, a Func node or a match function, in a new frame,
// whose first slots are its arguments. A chain of Func nodes, a -> b -> …,
// is one function that takes an argument per node, and if the chain ends
// in a match function, its arguments too. A Func node is a match function
// of one arm with one pattern. The arms' slots overlap.
func (c *compiler) fn(x ast.Node) cnode {
	u := c.open(true)
	level, next, size := c.level, c.next, c.size
	c.level, c.next, c.size = c.level+1, 0, 0
	c.fns = append(c.fns[:c.level], nil)

	var params []ast.Node
	body := x
	for c.src.Kind(body) == ast.Func {
		var param ast.Node
		param, body = c.src.Func(body)
		params = append(params, param)
	}
	arms := []ast.Node{body}
	arity := len(params)
	match := c.src.Kind(body) == ast.MatchFunc
	if match {
		arms = c.src.Arms(body)
		arity += c.src.Arity(body)
	}
	// A function of one argument matches it as it is; one of more keeps
	// them in its first slots, for its arms to match.
	args := 0
	if arity > 1 {
		args = arity
	}
	c.size = args

	var xs []int32
	for _, a := range arms {
		c.next = args
		scope := len(c.values)
		// Each parameter is a scope of its own, which may shadow the ones
		// before it.
		for i, param := range params {
			xs = append(xs, int32(c.param(param, i, arity, len(c.values))))
		}
		body := a
		if match {
			var pats []ast.Node
			pats, body = c.src.Arm(a)
			arm := len(c.values)
			for i, pat := range pats {
				xs = append(xs, int32(c.param(pat, len(params)+i, arity, arm)))
			}
		}
		xs = append(xs, int32(c.expr(body)))
		c.values = c.values[:scope]
	}
	caps := c.fns[c.level]
	head := []int32{int32(c.size), u, int32(arity), int32(len(caps))}
	for _, x := range caps {
		head = append(head, x.from)
	}

	c.level, c.next, c.size = level, next, size
	c.close()
	return c.span(opLambda, append(head, xs...), x)
}

// group compiles a where-group, whose bindings are mutually recursive, in
// the current frame.
func (c *compiler) group(x ast.Node) cnode {
	se := c.src
	body, binds := se.Group(x)
	values, types := len(c.values), len(c.types)

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
		c.wheres = append(c.wheres, whereStatic{x, u})
	}

	xs := []int32{0}
	for _, comp := range se.Components(binds, nil) {
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
	return c.span(opWhere, xs, x)
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

// param compiles the pattern x of argument i of a function of arity
// arguments. If it takes more than one, they are in its first slots, and a
// name binds the argument's own slot.
func (c *compiler) param(x ast.Node, i, arity, scope int) cnode {
	if arity == 1 || c.src.Kind(x) != ast.Ident || c.src.TokenText(c.src.Main(x)) == "_" {
		return c.pattern(x, scope)
	}
	name := c.name(c.src.Main(x))
	if lookup(c.values, name) >= scope {
		c.error(x, fmt.Sprintf("cannot bind %s twice", name))
	}
	c.bind(binder{name: name, slot: i, where: ast.None, types: -1, static: -1})
	return c.node(patArg, int32(i), 0, x)
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
