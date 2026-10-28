package eval

import (
	"errors"
	"fmt"
	"io/fs"
	"slices"
	"sync"
	"unsafe"

	"github.com/Victorystick/scrapscript/label"
)

// Native code: a program's core compiled to Go ahead of time, by
// GenerateGo, instead of to closures while it runs. The generated code is
// a Go package that calls the API in this file, whose names start with N;
// hosts use host.go's API instead. Like the evaluator, native code trusts
// the checker, and reads values without checking their kinds.
//
// Each lambda of the core is a Go function of its arguments, its frame's
// slots Go variables and its captures a slice. A call in tail position to
// the function itself loops. A call in tail position that may come back
// to its caller in tail position, through functions that it doesn't know
// or that call each other, returns a pending call instead: a value of
// kind kindTail that points to a Tail, which the nearest call not in tail
// position runs, with Settle. The calls of an evaluation share one Tail,
// so that a pending call allocates nothing.
//
// The program is not needed to run native code, only to print functions,
// which print as their source. A NativeProgram rebuilds it from the
// sources it embeds the first time it prints one.

// A NativeCode is a lambda or a composition of a program compiled to Go: a
// function of its arity's arguments and its captures c. Generated code
// sets the field for its arity, or FN for more than three arguments. FN
// must read its arguments before it uses t, since they may be in t.
type NativeCode struct {
	Node  int32 // its node in the core, for printing
	Arity int

	F1 func(t *Tail, c []Value, a Value) Value
	F2 func(t *Tail, c []Value, a, b Value) Value
	F3 func(t *Tail, c []Value, a, b, d Value) Value
	FN func(t *Tail, c []Value, args []Value) Value
}

// A nativeFunc is a closure of native code, applied to fewer arguments
// than its arity.
type nativeFunc struct {
	code *NativeCode
	caps []value
	args []value
}

func nativeVal(nf *nativeFunc) value  { return boxed(kindNative, 0, unsafe.Pointer(nf)) }
func (v value) asNative() *nativeFunc { return (*nativeFunc)(v.p) }

// A nativeWith is a closure allocated together with its captures.
type nativeWith[A any] struct {
	nativeFunc
	buf A
}

func withCaps[A any](code *NativeCode, caps func(*A) []value) *nativeFunc {
	nf := &nativeWith[A]{nativeFunc: nativeFunc{code: code}}
	nf.caps = caps(&nf.buf)
	return &nf.nativeFunc
}

// newNative returns a closure of code with k captures, which the caller
// sets. A closure of up to eight is one allocation.
func newNative(code *NativeCode, k int) *nativeFunc {
	switch k {
	case 0:
		return &nativeFunc{code: code}
	case 1:
		return withCaps(code, func(b *[1]value) []value { return b[:] })
	case 2:
		return withCaps(code, func(b *[2]value) []value { return b[:] })
	case 3:
		return withCaps(code, func(b *[3]value) []value { return b[:] })
	case 4:
		return withCaps(code, func(b *[4]value) []value { return b[:] })
	case 5:
		return withCaps(code, func(b *[5]value) []value { return b[:] })
	case 6:
		return withCaps(code, func(b *[6]value) []value { return b[:] })
	case 7:
		return withCaps(code, func(b *[7]value) []value { return b[:] })
	case 8:
		return withCaps(code, func(b *[8]value) []value { return b[:] })
	}
	return &nativeFunc{code: code, caps: make([]value, k)}
}

// applied returns nf applied to more arguments too, fewer than it lacks.
// An application to up to four arguments is one allocation.
func (nf *nativeFunc) applied(more ...value) value {
	n := len(nf.args) + len(more)
	var a *nativeFunc
	switch {
	case n <= 2:
		a = nativeArgs(nf, func(b *[2]value) []value { return b[:n] })
	case n <= 4:
		a = nativeArgs(nf, func(b *[4]value) []value { return b[:n] })
	default:
		a = &nativeFunc{code: nf.code, caps: nf.caps, args: make([]value, n)}
	}
	copy(a.args[copy(a.args, nf.args):], more)
	return nativeVal(a)
}

func nativeArgs[A any](nf *nativeFunc, args func(*A) []value) *nativeFunc {
	a := &nativeWith[A]{nativeFunc: nativeFunc{code: nf.code, caps: nf.caps}}
	a.args = args(&a.buf)
	return &a.nativeFunc
}

// A Tail holds the pending call of an evaluation of native code: a
// function, its captures and all its arguments.
type Tail struct {
	code *NativeCode
	caps []value
	args []value
	buf  [8]value
}

// pend returns a pending call of code with the captures caps and the
// arguments args, then more. It allocates a Tail if t is nil.
func (t *Tail) pend(code *NativeCode, caps []value, args []value, more ...value) Value {
	if t == nil {
		t = new(Tail)
	}
	t.code, t.caps = code, caps
	t.args = append(append(t.buf[:0], args...), more...)
	return Value{value{uint64(kindTail) << kindShift, unsafe.Pointer(t)}}
}

// Pend returns a pending call of code with the captures c and args, all
// its arguments, to return from a function in tail position.
func (t *Tail) Pend(code *NativeCode, c []Value, args ...Value) Value {
	return t.pend(code, fromHost(c), nil, fromHost(args)...)
}

// Settle runs v and the calls that it leads to, if it is a pending call,
// and returns the value they end in.
func (t *Tail) Settle(v Value) Value {
	if v.v.kind() != kindTail {
		return v
	}
	return Value{t.settle(v.v)}
}

func (t *Tail) settle(v value) value {
	for v.kind() == kindTail {
		p := (*Tail)(v.p)
		if t == nil {
			t = p
		}
		// The call may reuse p, so read it first.
		code, c := p.code, toHost(p.caps)
		switch code.Arity {
		case 1:
			a := p.args[0]
			v = code.F1(t, c, Value{a}).v
		case 2:
			a, b := p.args[0], p.args[1]
			v = code.F2(t, c, Value{a}, Value{b}).v
		case 3:
			a, b, d := p.args[0], p.args[1], p.args[2]
			v = code.F3(t, c, Value{a}, Value{b}, Value{d}).v
		default:
			v = code.FN(t, c, toHost(p.args)).v
		}
	}
	return v
}

// call calls code with the captures caps and the arguments args and then
// more, which are all it takes, and returns its result, which may be a
// pending call.
func (code *NativeCode) call(t *Tail, caps []value, args, more []value) value {
	c := toHost(caps)
	switch code.Arity {
	case 1:
		return code.F1(t, c, argAt(args, more, 0)).v
	case 2:
		return code.F2(t, c, argAt(args, more, 0), argAt(args, more, 1)).v
	case 3:
		return code.F3(t, c, argAt(args, more, 0), argAt(args, more, 1), argAt(args, more, 2)).v
	}
	// No call is pending, so t holds nothing.
	var all []value
	if t != nil {
		all = t.buf[:0]
	}
	return code.FN(t, c, toHost(append(append(all, args...), more...))).v
}

// argAt returns the argument i of args and then more.
func argAt(args, more []value, i int) Value {
	if i < len(args) {
		return Value{args[i]}
	}
	return Value{more[i-len(args)]}
}

// callNative calls nf with more arguments, in a position that isn't a
// tail. If nf takes fewer, it calls the function that it returns with the
// rest.
func (t *Tail) callNative(nf *nativeFunc, more []value) value {
	for {
		lacks := nf.code.Arity - len(nf.args)
		if len(more) < lacks {
			return nf.applied(more...)
		}
		v := t.settle(nf.code.call(t, nf.caps, nf.args, more[:lacks]))
		if more = more[lacks:]; len(more) == 0 {
			return v
		}
		if v.boxedKind() != kindNative {
			return call(v, slices.Clone(more)...)
		}
		nf = v.asNative()
	}
}

// tailNative calls nf with more arguments in tail position: a pending call
// if they are all it lacks.
func (t *Tail) tailNative(nf *nativeFunc, more []value) value {
	for {
		lacks := nf.code.Arity - len(nf.args)
		switch {
		case len(more) == lacks:
			return t.pend(nf.code, nf.caps, nf.args, more...).v
		case len(more) < lacks:
			return nf.applied(more...)
		}
		v := t.settle(nf.code.call(t, nf.caps, nf.args, more[:lacks]))
		more = more[lacks:]
		if v.boxedKind() != kindNative {
			return call(v, slices.Clone(more)...)
		}
		nf = v.asNative()
	}
}

// Functions.

// NClosure returns a closure of code with the captures caps.
func NClosure(code *NativeCode, caps ...Value) Value {
	nf := newNative(code, len(caps))
	copy(nf.caps, fromHost(caps))
	return Value{nativeVal(nf)}
}

// NRec returns a closure of code with k captures, which NFill sets once
// the values that it captures, itself among them, are made.
func NRec(code *NativeCode, k int) Value {
	return Value{nativeVal(newNative(code, k))}
}

// NFill sets the captures of the closure v, made by NRec, and returns it.
func NFill(v Value, caps ...Value) Value {
	copy(v.v.asNative().caps, fromHost(caps))
	return v
}

// NCaps returns the captures of the closure v.
func NCaps(v Value) []Value {
	return toHost(v.v.asNative().caps)
}

// NComposeCode returns the code of the composition node, which applies
// its first function and then its second: the captures of NClosure.
func NComposeCode(node int32) *NativeCode {
	return &NativeCode{Node: node, Arity: 1, F1: func(t *Tail, c []Value, a Value) Value {
		return NTail1(t, c[1], NCall1(t, c[0], a))
	}}
}

// NCall1 calls fn with a, in a position that isn't a tail.
func NCall1(t *Tail, fn, a Value) Value {
	switch fn.v.boxedKind() {
	case kindNative:
		nf := fn.v.asNative()
		if len(nf.args) == 0 && nf.code.Arity == 1 {
			return t.Settle(nf.code.F1(t, toHost(nf.caps), a))
		}
		return Value{t.callNative(nf, []value{a.v})}
	case kindBuiltin:
		if g := fn.v.asBuiltin(); g.arity == 1 {
			return Value{g.one(a.v)}
		}
	}
	return Value{apply(fn.v, a.v)}
}

// NCall2 calls fn with a and b, in a position that isn't a tail.
func NCall2(t *Tail, fn, a, b Value) Value {
	if fn.v.boxedKind() == kindNative {
		nf := fn.v.asNative()
		if len(nf.args) == 0 && nf.code.Arity == 2 {
			return t.Settle(nf.code.F2(t, toHost(nf.caps), a, b))
		}
		return Value{t.callNative(nf, []value{a.v, b.v})}
	}
	return Value{call2(fn.v, a.v, b.v)}
}

// NCallN calls fn with args, in a position that isn't a tail.
func NCallN(t *Tail, fn Value, args ...Value) Value {
	if fn.v.boxedKind() == kindNative {
		return Value{t.callNative(fn.v.asNative(), fromHost(args))}
	}
	return Value{call(fn.v, slices.Clone(fromHost(args))...)}
}

// NTail1 calls fn with a in tail position: a pending call if fn is native
// code that lacks only a. If fn lacks fewer, it calls fn with those, and
// the function that it returns with the rest, in tail position.
func NTail1(t *Tail, fn, a Value) Value {
	if fn.v.boxedKind() == kindNative {
		return Value{t.tailNative(fn.v.asNative(), []value{a.v})}
	}
	return NCall1(t, fn, a)
}

// NTail2 calls fn with a and b in tail position, as NTail1 does.
func NTail2(t *Tail, fn, a, b Value) Value {
	if fn.v.boxedKind() == kindNative {
		return Value{t.tailNative(fn.v.asNative(), []value{a.v, b.v})}
	}
	return NCall2(t, fn, a, b)
}

// NTailN calls fn with args in tail position, as NTail1 does.
func NTailN(t *Tail, fn Value, args ...Value) Value {
	if fn.v.boxedKind() == kindNative {
		return Value{t.tailNative(fn.v.asNative(), fromHost(args))}
	}
	return NCallN(t, fn, args...)
}

// NBuiltin1 returns the Go function of the builtin v, which takes one
// argument.
func NBuiltin1(v Value) func(Value) Value {
	// A Value is laid out and passed as the value it holds is.
	return *(*func(Value) Value)(unsafe.Pointer(&v.v.asBuiltin().one))
}

// NBuiltin2 returns the Go function of the builtin v, which takes two
// arguments.
func NBuiltin2(v Value) func(Value, Value) Value {
	return *(*func(Value, Value) Value)(unsafe.Pointer(&v.v.asBuiltin().two))
}

// NBuiltin3 returns the Go function of the builtin v, which takes three
// arguments.
func NBuiltin3(v Value) func(Value, Value, Value) Value {
	return *(*func(Value, Value, Value) Value)(unsafe.Pointer(&v.v.asBuiltin().three))
}

// NNoArm returns the failure of a match that no arm matches, which the
// checker rules out, for native code to panic with.
func NNoArm() any {
	return runtimeError{errors.New("no arm matches")}
}

// Values.

func NHole() Value                       { return Value{hole} }
func NInt(i int) Value                   { return Value{intVal(i)} }
func NAsInt(v Value) int                 { return v.v.asInt() }
func NFloat(f float64) Value             { return Value{floatVal(f)} }
func NAsFloat(v Value) float64           { return v.v.asFloat() }
func NByte(b byte) Value                 { return Value{byteVal(b)} }
func NAsByte(v Value) byte               { return v.v.asByte() }
func NText(s string) Value               { return Value{textVal(s)} }
func NAsText(v Value) string             { return v.v.asText() }
func NBytes(b []byte) Value              { return Value{bytesVal(b)} }
func NAsBytes(v Value) []byte            { return v.v.asBytes() }
func NWord(v Value) uint64               { return v.v.n }
func NTag(tag label.ID) Value            { return Value{tagVal(tag)} }
func NHasTag(v Value, tag label.ID) bool { return v.v.hasTag(tag) }

// NBind stores v in dst, and returns true, for patterns to bind names in
// conditions.
func NBind(dst *Value, v Value) bool {
	*dst = v
	return true
}

// NVariant returns the tag with the payload.
func NVariant(tag label.ID, payload Value) Value {
	return Value{variantVal(tag, payload.v)}
}

// NPayload returns the payload of the variant v: () if it has none.
func NPayload(v Value) Value {
	if p := v.v.payload(); p.valid() {
		return Value{p}
	}
	return Value{hole}
}

// NList returns the list of elems, which it takes ownership of.
func NList(elems ...Value) Value { return Value{listVal(fromHost(elems))} }

// NLen returns the length of the list v.
func NLen(v Value) int { return int(v.v.n & lenMask) }

// NElem returns the element i of the list v.
func NElem(v Value, i int) Value { return Value{v.v.asList()[i]} }

// NSlice returns the elements of the list v from lo up to hi.
func NSlice(v Value, lo, hi int) Value { return Value{listVal(v.v.asList()[lo:hi])} }

// NRecord returns the record of the shape with the ID shape and values,
// in the order of its labels, which it takes ownership of.
func NRecord(shape uint32, values ...Value) Value {
	return Value{recordOf(shape, fromHost(values))}
}

func recordOf(shape uint32, values []value) value {
	n := uint64(shape)<<shapeShift | uint64(len(values))
	if len(values) == 0 {
		return boxed(kindRecord, n, nil)
	}
	return boxed(kindRecord, n, unsafe.Pointer(unsafe.SliceData(values)))
}

// NField returns the value at index i of the record v.
func NField(v Value, i int) Value { return Value{v.v.fields()[i]} }

// NCopyFields returns a copy of the values of the record v.
func NCopyFields(v Value) []Value { return toHost(slices.Clone(v.v.fields())) }

// NRecordLike returns the record of the same shape as the record base
// with values, which it takes ownership of.
func NRecordLike(base Value, values []Value) Value {
	return Value{recordOf(base.v.shapeID(), fromHost(values))}
}

// NPick returns the record of the shape with the ID shape whose values are
// those of the record v at the indices from.
func NPick(v Value, shape uint32, from ...int) Value {
	r := v.v.fields()
	values := make([]value, len(from))
	for i, j := range from {
		values[i] = r[j]
	}
	return Value{recordOf(shape, values)}
}

func NAppendList(l, x Value) Value { return Value{listVal(append(l.v.asList(), x.v))} }
func NAppendBytes(b, x Value) Value {
	return Value{bytesVal(append(b.v.asBytes(), x.v.asByte()))}
}
func NPrependList(x, l Value) Value {
	return Value{listVal(append([]value{x.v}, l.v.asList()...))}
}
func NPrependBytes(x, b Value) Value {
	return Value{bytesVal(append([]byte{x.v.asByte()}, b.v.asBytes()...))}
}
func NConcatList(a, b Value) Value { return Value{listVal(append(a.v.asList(), b.v.asList()...))} }
func NConcatText(a, b Value) Value { return Value{textVal(a.v.asText() + b.v.asText())} }
func NConcatBytes(a, b Value) Value {
	return Value{bytesVal(append(a.v.asBytes(), b.v.asBytes()...))}
}

// NBytesEqual returns true if the bytes v are b.
func NBytesEqual(v Value, b string) bool { return string(v.v.asBytes()) == b }

// Programs.

// A NativeConfig describes a program that native code was compiled from.
type NativeConfig struct {
	// The names of the labels of its environment, from ID 1, and the labels
	// of its shapes, by ID, which native code refers to by ID.
	Labels []string
	Shapes [][]label.ID
	// The number of nodes of its core, to check the one that it rebuilds
	// to print functions against.
	Nodes int
	// Whether its main scrap has a static part, so that printing its value
	// needs the program even without functions.
	Static bool
	// The source of its main scrap, and of the scraps that it refers to:
	// drafts by path, published ones by sha256 digest.
	Root      string
	Drafts    map[string]string
	Published map[string]string
}

// A NativeProgram is the environment and the program that native code
// runs in.
type NativeProgram struct {
	env *Environment
	cfg NativeConfig

	once sync.Once
	prog *program
	err  error
}

// NewNative returns the program that cfg describes, with the same IDs of
// labels and shapes as the environment that native code was compiled in.
func NewNative(cfg NativeConfig) *NativeProgram {
	env := NewEnvironment()
	labels := env.reg.Labels()
	for i, name := range cfg.Labels {
		if labels.Of(name) != label.ID(i+1) {
			panic(fmt.Sprintf("eval.NewNative: label %s is not %d", name, i+1))
		}
	}
	for i, ls := range cfg.Shapes {
		if s := env.shapes.of(ls); s.id != uint32(i) {
			panic(fmt.Sprintf("eval.NewNative: shape %d is %d", i, s.id))
		}
	}
	return &NativeProgram{env: env, cfg: cfg}
}

// Builtin returns the builtin of the name.
func (p *NativeProgram) Builtin(name string) Value {
	v, ok := p.env.vars[name]
	if !ok {
		panic("eval.NativeProgram.Builtin: no builtin " + name)
	}
	return Value{v}
}

// Run calls root, the native code of the program's main scrap, and settles
// its value.
func (p *NativeProgram) Run(root func(t *Tail) Value) (v Value, err error) {
	defer catch(&err)
	t := new(Tail)
	return t.Settle(root(t)), nil
}

// NGet returns the value of the label l of the record v.
func (p *NativeProgram) NGet(v Value, l label.ID) Value {
	r := p.env.shapes.record(v.v)
	return Value{r.values[r.shape.index(l)]}
}

// NIndex returns the index of the label l in the record v.
func (p *NativeProgram) NIndex(v Value, l label.ID) int {
	return p.env.shapes.at(v.v.shapeID()).index(l)
}

// NWithout returns the record v without the labels of the shape with the
// ID drop, for a rest pattern.
func (p *NativeProgram) NWithout(v Value, drop uint32) Value {
	t := p.env.shapes
	r, s := t.record(v.v), t.at(drop)
	rs := t.without(r.shape, s)
	rest := make([]value, 0, len(rs.labels))
	for i, l := range r.shape.labels {
		if s.index(l) < 0 {
			rest = append(rest, r.values[i])
		}
	}
	return Value{recordVal(rs, rest)}
}

// Print returns a scrap that evaluates to v, the value of the program's
// main scrap, and prints the same, as Environment.PrintScrap does.
func (p *NativeProgram) Print(v Value) (string, error) {
	if !p.cfg.Static && !reachesNative(v.v) {
		return printValue(p.env.shapes, v.v), nil
	}
	prog, err := p.program()
	if err != nil {
		return "", err
	}
	u := unnative{prog: prog, memo: make(map[closureKey]*scriptFunc)}
	return printScrap(p.env.shapes, u.value(v.v), prog), nil
}

// program rebuilds the program from its sources, the first time.
func (p *NativeProgram) program() (*program, error) {
	p.once.Do(func() {
		e := p.env
		e.UseDrafts(sourceFS(p.cfg.Drafts))
		e.UseFetcher(sourceFS(p.cfg.Published))
		s, err := e.Read([]byte(p.cfg.Root))
		if err != nil {
			p.err = err
			return
		}
		if _, err := e.infer(s); err != nil {
			p.err = err
			return
		}
		c, err := compile(e.units(s), &e.reg, e.vars, e.shapes)
		if err != nil {
			p.err = err
			return
		}
		if len(c.ops) != p.cfg.Nodes {
			p.err = fmt.Errorf("the program has %d nodes, not the %d it was compiled with", len(c.ops), p.cfg.Nodes)
			return
		}
		p.prog = &program{core: c}
	})
	return p.prog, p.err
}

// sourceFS holds the sources that a NativeProgram embeds, by path or
// digest, as drafts and as a Fetcher.
type sourceFS map[string]string

func (s sourceFS) Open(name string) (fs.File, error) {
	return nil, &fs.PathError{Op: "open", Path: name, Err: errors.ErrUnsupported}
}

func (s sourceFS) ReadFile(name string) ([]byte, error) {
	src, ok := s[name]
	if !ok {
		return nil, &fs.PathError{Op: "read", Path: name, Err: fs.ErrNotExist}
	}
	return []byte(src), nil
}

func (s sourceFS) FetchSha256(key string) ([]byte, error) {
	src, ok := s[key]
	if !ok {
		return nil, fmt.Errorf("%s is not embedded", key)
	}
	return []byte(src), nil
}

// reachesNative returns true if v is or holds a closure of native code.
func reachesNative(v value) bool {
	switch v.kind() {
	case kindNative:
		return true
	case kindRecord:
		return slices.ContainsFunc(v.fields(), reachesNative)
	case kindList:
		return slices.ContainsFunc(v.asList(), reachesNative)
	case kindVariant:
		return reachesNative(v.payload())
	case kindPartial:
		return slices.ContainsFunc(v.asPartial().args, reachesNative)
	}
	return false
}

// unnative turns the closures of native code in values into the script
// functions of the program that they were compiled from, for printing.
type unnative struct {
	prog *program
	// The script function of each closure, by its code and captures, which
	// its partial applications share.
	memo map[closureKey]*scriptFunc
}

type closureKey struct {
	code *NativeCode
	caps *value
}

func (u *unnative) value(v value) value {
	switch v.kind() {
	case kindRecord:
		if vs := v.fields(); len(vs) > 0 {
			return value{v.n, unsafe.Pointer(unsafe.SliceData(u.values(vs)))}
		}
	case kindList:
		if vs := v.asList(); len(vs) > 0 {
			return value{v.n, unsafe.Pointer(unsafe.SliceData(u.values(vs)))}
		}
	case kindVariant:
		if p := v.payload(); p.valid() {
			return variantVal(v.tag(), u.value(p))
		}
	case kindPartial:
		g := v.asPartial()
		return partialVal(&partial{g.fn, u.values(g.args)})
	case kindNative:
		nf := v.asNative()
		sf := u.closure(nf)
		if len(nf.args) > 0 {
			sf = &scriptFunc{prog: sf.prog, node: sf.node, caps: sf.caps, args: u.values(nf.args)}
		}
		return scriptVal(sf)
	}
	return v
}

// closure returns the script function of the closure of nf, without its
// arguments.
func (u *unnative) closure(nf *nativeFunc) *scriptFunc {
	key := closureKey{code: nf.code}
	if len(nf.caps) > 0 {
		key.caps = &nf.caps[0]
	}
	sf, ok := u.memo[key]
	if !ok {
		sf = &scriptFunc{prog: u.prog, node: cnode(nf.code.Node), caps: make([]value, len(nf.caps))}
		u.memo[key] = sf
		for i, c := range nf.caps {
			sf.caps[i] = u.value(c)
		}
	}
	return sf
}

func (u *unnative) values(vs []value) []value {
	out := make([]value, len(vs))
	for i, x := range vs {
		out[i] = u.value(x)
	}
	return out
}
