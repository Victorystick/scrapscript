package eval

import (
	"fmt"
	"maps"
	"reflect"
	"slices"
)

// A program is a compiled scrap, ready to run.
type program struct {
	*core
	evalImport EvalImport
}

// A frame holds the slots of one call of a function, or of the top level,
// and the frame that the function was made in.
type frame struct {
	slots  []Value
	parent *frame
}

// A runtimeError is a failure during evaluation. It unwinds to the edge of
// the evaluator as a panic, where catch turns it back into an error.
type runtimeError struct {
	err error
}

func (p *program) errorf(n cnode, format string, args ...any) runtimeError {
	return runtimeError{p.src.Error(p.at[n], fmt.Sprintf(format, args...))}
}

// catch recovers a runtimeError into *err. Other panics carry on.
func catch(err *error) {
	if r := recover(); r != nil {
		re, ok := r.(runtimeError)
		if !ok {
			panic(r)
		}
		*err = re.err
	}
}

// eval runs the program in a new top frame.
func (p *program) eval() (v Value, err error) {
	defer catch(&err)
	return p.run(p.root, &frame{slots: make([]Value, p.top)}), nil
}

// run evaluates the expression n in the frame f.
func (p *program) run(n cnode, f *frame) Value {
	d := p.data[n]
	switch p.ops[n] {
	case opConst:
		return p.consts[d.a]

	case opLocal:
		for range d.a {
			f = f.parent
		}
		return f.slots[d.b]

	case opImport:
		v, err := p.evalImport(p.at[n])
		if err != nil {
			panic(runtimeError{err})
		}
		return v

	case opCall:
		fn := p.run(cnode(d.a), f)
		return p.apply(cnode(d.a), fn, p.run(cnode(d.b), f))

	case opAdd, opSub, opMul:
		switch l := p.run(cnode(d.a), f).(type) {
		case Float:
			r := runAs[Float](p, cnode(d.b), f, "non-float")
			return arith(p.ops[n], l, r)
		case Int:
			r := runAs[Int](p, cnode(d.b), f, "non-int")
			return arith(p.ops[n], l, r)
		default:
			panic(p.errorf(n, "cannot perform arithmetic on %s", l))
		}

	// Values are immutable, so appending clips the left operand first:
	// append must copy rather than write into spare capacity that other
	// values share.
	case opAppend:
		switch l := p.run(cnode(d.a), f).(type) {
		case Bytes:
			r := runAs[Byte](p, cnode(d.b), f, "non-byte")
			return append(slices.Clip(l), byte(r))
		case List:
			return List{append(slices.Clip(l.elements), p.run(cnode(d.b), f))}
		default:
			panic(p.errorf(n, "cannot append to non-list %s", reflect.TypeOf(l)))
		}

	case opPrepend:
		switch r := p.run(cnode(d.b), f).(type) {
		case Bytes:
			l := runAs[Byte](p, cnode(d.a), f, "non-byte")
			return append(Bytes{byte(l)}, r...)
		case List:
			return List{append([]Value{p.run(cnode(d.a), f)}, r.elements...)}
		default:
			panic(p.errorf(n, "cannot prepend to non-list %s", reflect.TypeOf(r)))
		}

	case opConcat:
		switch l := p.run(cnode(d.a), f).(type) {
		case Bytes:
			r := runAs[Bytes](p, cnode(d.b), f, "non-bytes")
			return append(slices.Clip(l), r...)
		case List:
			r := runAs[List](p, cnode(d.b), f, "non-list")
			return List{append(slices.Clip(l.elements), r.elements...)}
		case Text:
			r := runAs[Text](p, cnode(d.b), f, "non-text")
			return l + r
		default:
			panic(p.errorf(n, "cannot append to non-list %s", reflect.TypeOf(l)))
		}

	case opCompose:
		first, second, _ := p.compose(n)
		fns := []Value{p.fn(first, f), p.fn(second, f)}
		return ScriptFunc{prog: p, node: n, env: f, composed: fns}

	case opLambda:
		return ScriptFunc{prog: p, node: n, env: f}

	case opWhere:
		body, binds := p.where(n)
		for i := 0; i < len(binds); i += 2 {
			f.slots[binds[i]] = p.run(cnode(binds[i+1]), f)
		}
		return p.run(body, f)

	case opRecord:
		rest, fields := p.record(n)
		var values map[string]Value
		if rest == none {
			values = make(map[string]Value, len(fields)/2)
		} else {
			// A record based on another.
			values = maps.Clone(runAs[Record](p, rest, f, "non-record").values)
		}
		for i := 0; i < len(fields); i += 2 {
			values[p.strings[fields[i]]] = p.run(cnode(fields[i+1]), f)
		}
		return Record{values}

	case opAccess:
		r := runAs[Record](p, cnode(d.a), f, "non-record")
		key := p.strings[d.b]
		v, ok := r.values[key]
		if !ok {
			panic(p.errorf(n, "record %s has no key %s", r, key))
		}
		return v

	case opList:
		elems := p.range_(n)
		values := make([]Value, len(elems))
		for i, x := range elems {
			values[i] = p.run(cnode(x), f)
		}
		return List{values}

	case opVariant:
		v := Variant{tag: p.strings[d.a]}
		if cnode(d.b) != none {
			v.value = p.run(cnode(d.b), f)
		}
		return v
	}

	panic(p.errorf(n, "unhandled op %d", p.ops[n]))
}

func arith[T ~int | ~float64](o op, a, b T) T {
	switch o {
	case opAdd:
		return a + b
	case opSub:
		return a - b
	}
	return a * b
}

// runAs evaluates n, which must be a T; what names T in errors.
func runAs[T Value](p *program, n cnode, f *frame, what string) T {
	v := p.run(n, f)
	if t, ok := v.(T); ok {
		return t
	}
	panic(p.errorf(n, "%s value %s", what, v))
}

// fn evaluates n, which must be a function.
func (p *program) fn(n cnode, f *frame) Value {
	v := p.run(n, f)
	switch v.(type) {
	case ScriptFunc, BuiltInFunc, Partial:
		return v
	}
	panic(p.errorf(n, "non-func value %s", v))
}

// apply calls the function fn, which the expression n evaluated to, with
// arg.
func (p *program) apply(n cnode, fn Value, arg Value) Value {
	switch fn := fn.(type) {
	case ScriptFunc:
		return fn.call(arg)
	case BuiltInFunc:
		return builtin(fn.fn, arg)
	case Partial:
		return builtin(fn.fn, arg)
	}
	panic(p.errorf(n, "non-func value %s", fn))
}

// builtin calls a builtin, whose errors unwind like the evaluator's.
func builtin(fn Func, arg Value) Value {
	v, err := fn(arg)
	if err != nil {
		panic(runtimeError{err})
	}
	return v
}

// call calls a script function: it matches arg against each arm in turn,
// in a new frame, and evaluates the body of the first that matches.
func (sf ScriptFunc) call(arg Value) Value {
	p := sf.prog
	if sf.composed != nil {
		return p.apply(sf.node, sf.composed[1], p.apply(sf.node, sf.composed[0], arg))
	}
	size, _, arms := p.lambda(sf.node)
	f := &frame{parent: sf.env}
	if size > 0 {
		f.slots = make([]Value, size)
	}
	for i := 0; i < len(arms); i += 2 {
		if p.match(cnode(arms[i]), arg, f) {
			return p.run(cnode(arms[i+1]), f)
		}
	}
	panic(p.errorf(sf.node, "no arm matches %s", arg))
}

// match matches v against the pattern n, writing the names it binds into
// f. An arm that fails may have written some of its slots; the next arm
// overwrites the ones it uses.
func (p *program) match(n cnode, v Value, f *frame) bool {
	d := p.data[n]
	switch p.ops[n] {
	case patBind:
		f.slots[d.a] = v
		return true

	case patWild:
		return true

	case patLiteral:
		return p.consts[d.a].eq(v)

	case patVariant:
		t, ok := v.(Variant)
		if !ok || t.tag != p.strings[d.a] {
			return false
		}
		if cnode(d.b) == none {
			return true
		}
		// A tag without a payload has the payload ().
		payload := t.value
		if payload == nil {
			payload = Hole{}
		}
		return p.match(cnode(d.b), payload, f)

	case patRecord:
		r, ok := v.(Record)
		if !ok {
			return false
		}
		rest, fields := p.record(n)
		for i := 0; i < len(fields); i += 2 {
			val, ok := r.values[p.strings[fields[i]]]
			if !ok || !p.match(cnode(fields[i+1]), val, f) {
				return false
			}
		}
		if rest == none {
			return true
		}
		values := maps.Clone(r.values)
		for i := 0; i < len(fields); i += 2 {
			delete(values, p.strings[fields[i]])
		}
		return p.match(rest, Record{values}, f)

	case patList:
		l, ok := v.(List)
		elems := p.range_(n)
		return ok && len(l.elements) == len(elems) && p.matchAll(elems, l.elements, f)

	case patCons:
		l, ok := v.(List)
		return ok && len(l.elements) > 0 &&
			p.match(cnode(d.a), l.elements[0], f) &&
			p.match(cnode(d.b), List{l.elements[1:]}, f)

	case patSnoc:
		l, ok := v.(List)
		last := len(l.elements) - 1
		return ok && last >= 0 &&
			p.match(cnode(d.a), List{l.elements[:last]}, f) &&
			p.match(cnode(d.b), l.elements[last], f)

	case patPrefix:
		l, ok := v.(List)
		rest, elems := p.elems(n)
		return ok && len(elems) <= len(l.elements) &&
			p.matchAll(elems, l.elements[:len(elems)], f) &&
			p.match(rest, List{l.elements[len(elems):]}, f)

	case patSuffix:
		l, ok := v.(List)
		rest, elems := p.elems(n)
		split := len(l.elements) - len(elems)
		return ok && split >= 0 &&
			p.match(rest, List{l.elements[:split]}, f) &&
			p.matchAll(elems, l.elements[split:], f)
	}
	return false
}

// matchAll matches each value against the pattern at the same index.
func (p *program) matchAll(pats []int32, vs []Value, f *frame) bool {
	for i, x := range pats {
		if !p.match(cnode(x), vs[i], f) {
			return false
		}
	}
	return true
}
