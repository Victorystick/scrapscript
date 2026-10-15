package eval

import (
	"fmt"
	"slices"
	"strings"

	"github.com/Victorystick/scrapscript/label"
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
//
// Calls in tail position don't grow the Go stack: a call, a where's body
// and a matching arm's body continue the loop in place of the expression,
// so a function that calls itself last runs in constant stack.
func (p *program) run(n cnode, f *frame) Value {
	for {
		d := p.data[n]
		switch p.ops[n] {
		case opCall:
			fn := p.run(cnode(d.a), f)
			arg := p.run(cnode(d.b), f)
			var v Value
			p, n, f, v = enter(fn, arg)
			if v != nil {
				return v
			}

		case opCallN:
			r := p.range_(n)
			fn := p.run(cnode(r[0]), f)
			argNodes := r[1:]
			// A script function that takes exactly these arguments gets
			// them in its frame directly.
			if sf, ok := fn.(ScriptFunc); ok && len(sf.args) == 0 && !sf.composition() {
				if size, _, arity, _ := sf.prog.lambda(sf.node); arity == len(argNodes) {
					g := sf.frame(size)
					for i, x := range argNodes {
						g.slots[i] = p.run(cnode(x), f)
					}
					p, f = sf.prog, g
					n = p.arm(sf, f)
					continue
				}
			}
			args := make([]Value, len(argNodes))
			for i, x := range argNodes {
				args[i] = p.run(cnode(x), f)
			}
			var v Value
			p, n, f, v = enterN(fn, args)
			if v != nil {
				return v
			}

		case opWhere:
			body, binds := p.where(n)
			for i := 0; i < len(binds); i += 2 {
				f.slots[binds[i]] = p.run(cnode(binds[i+1]), f)
			}
			n = body

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

		// The checker guarantees the operands' types. Overloaded operators still
		// choose by the left operand at runtime, until types reach the core.
		case opAdd, opSub, opMul:
			l := p.run(cnode(d.a), f)
			r := p.run(cnode(d.b), f)
			if l, ok := l.(Int); ok {
				return arith(p.ops[n], l, r.(Int))
			}
			return arith(p.ops[n], l.(Float), r.(Float))

		// Values are immutable, so appending clips the left operand first:
		// append must copy rather than write into spare capacity that other
		// values share.
		case opAppend:
			l := p.run(cnode(d.a), f)
			r := p.run(cnode(d.b), f)
			if l, ok := l.(Bytes); ok {
				return append(slices.Clip(l), byte(r.(Byte)))
			}
			ls := l.(List).elements
			return List{append(slices.Clip(ls), r)}

		case opPrepend:
			r := p.run(cnode(d.b), f)
			l := p.run(cnode(d.a), f)
			if r, ok := r.(Bytes); ok {
				return append(Bytes{byte(l.(Byte))}, r...)
			}
			return List{append([]Value{l}, r.(List).elements...)}

		case opConcat:
			l := p.run(cnode(d.a), f)
			r := p.run(cnode(d.b), f)
			switch l := l.(type) {
			case Bytes:
				return append(slices.Clip(l), r.(Bytes)...)
			case Text:
				return l + r.(Text)
			}
			return List{append(slices.Clip(l.(List).elements), r.(List).elements...)}

		case opCompose:
			first, second, _ := p.compose(n)
			fns := []Value{p.run(first, f), p.run(second, f)}
			return ScriptFunc{prog: p, node: n, env: f, args: fns}

		case opLambda:
			return ScriptFunc{prog: p, node: n, env: f}

		case opRecord:
			base, s, fields := p.record(n)
			if base == none {
				values := make([]Value, len(fields)/2)
				for i := 0; i < len(fields); i += 2 {
					values[fields[i]] = p.run(cnode(fields[i+1]), f)
				}
				return Record{s, values}
			}
			// A record based on another, with some of its fields updated.
			r := p.run(base, f).(Record)
			values := slices.Clone(r.values)
			for i := 0; i < len(fields); i += 2 {
				values[r.shape.index(label.ID(fields[i]))] = p.run(cnode(fields[i+1]), f)
			}
			return Record{r.shape, values}

		case opAccess:
			r := p.run(cnode(d.a), f).(Record)
			return r.values[r.shape.index(label.ID(d.b))]

		case opList:
			elems := p.range_(n)
			values := make([]Value, len(elems))
			for i, x := range elems {
				values[i] = p.run(cnode(x), f)
			}
			return List{values}

		case opVariant:
			v := Variant{tag: label.ID(d.a)}
			if cnode(d.b) != none {
				v.value = p.run(cnode(d.b), f)
			}
			return v

		default:
			panic(p.errorf(n, "unhandled op %d", p.ops[n]))
		}
	}
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

// apply calls the function fn with arg.
func apply(fn, arg Value) Value {
	p, n, f, v := enter(fn, arg)
	if v != nil {
		return v
	}
	return p.run(n, f)
}

// call calls the function fn with args, one or more, whether it takes as
// many or not.
func call(fn Value, args ...Value) Value {
	p, n, f, v := enterN(fn, args)
	if v != nil {
		return v
	}
	return p.run(n, f)
}

// enter starts calling fn with arg. A builtin returns its result v, as
// does a function that takes more arguments, with arg added to the ones it
// has. Otherwise, enter returns the body n of a script function's first
// arm that matches all its arguments, in a new frame f, for its program p
// to evaluate. A composition applies its first function and enters its
// second.
func enter(fn, arg Value) (p *program, n cnode, f *frame, v Value) {
	for {
		switch g := fn.(type) {
		case BuiltInFunc:
			if g.arity == 1 {
				return nil, none, nil, g.one(arg)
			}
			return nil, none, nil, Partial{g, []Value{arg}}
		case Partial:
			// Each partial application has its own arguments.
			args := append(g.args[:len(g.args):len(g.args)], arg)
			if len(args) == g.fn.arity {
				return nil, none, nil, g.fn.call(args)
			}
			return nil, none, nil, Partial{g.fn, args}
		}
		sf := fn.(ScriptFunc)
		if sf.composition() {
			arg = apply(sf.args[0], arg)
			fn = sf.args[1]
			continue
		}
		size, _, arity, arms := sf.prog.lambda(sf.node)
		if arity == 1 {
			// The common case: match arg as it is, without storing it.
			p = sf.prog
			f = &frame{parent: sf.env}
			if size > 0 {
				f.slots = make([]Value, size)
			}
			for i := 0; i < len(arms); i += 2 {
				if p.match(cnode(arms[i]), arg, f) {
					return p, cnode(arms[i+1]), f, nil
				}
			}
			panic(p.errorf(sf.node, "no arm matches %s", printValue(p.labels, arg)))
		}
		if len(sf.args)+1 < arity {
			sf.args = append(sf.args[:len(sf.args):len(sf.args)], arg)
			return nil, none, nil, sf
		}
		f = sf.frame(size)
		copy(f.slots, sf.args)
		f.slots[arity-1] = arg
		return sf.prog, sf.prog.arm(sf, f), f, nil
	}
}

// enterN is enter for one or more arguments args at once. A function that
// takes exactly as many is called with all of them, without applying it to
// one at a time.
func enterN(fn Value, args []Value) (p *program, n cnode, f *frame, v Value) {
	switch g := fn.(type) {
	case BuiltInFunc:
		if g.arity == len(args) && g.arity > 1 {
			return nil, none, nil, g.call(args)
		}
	case ScriptFunc:
		if len(g.args) == 0 && len(args) > 1 && !g.composition() {
			if size, _, arity, _ := g.prog.lambda(g.node); arity == len(args) {
				f = g.frame(size)
				copy(f.slots, args)
				return g.prog, g.prog.arm(g, f), f, nil
			}
		}
	}
	last := len(args) - 1
	for _, arg := range args[:last] {
		fn = apply(fn, arg)
	}
	return enter(fn, args[last])
}

// frame returns a new frame of size slots for a call of the lambda sf,
// which takes more than one argument.
func (sf ScriptFunc) frame(size int) *frame {
	return &frame{parent: sf.env, slots: make([]Value, size)}
}

// arm returns the body of the first arm of the lambda sf that matches its
// arguments, the first slots of f, binding the arm's names in f. sf takes
// more than one argument.
func (p *program) arm(sf ScriptFunc, f *frame) cnode {
	_, _, arity, arms := p.lambda(sf.node)
	args := f.slots[:arity]
	for i := 0; i < len(arms); i += arity + 1 {
		if p.matchAll(arms[i:i+arity], args, f) {
			return cnode(arms[i+arity])
		}
	}
	strs := make([]string, arity)
	for i, a := range args {
		strs[i] = printValue(p.labels, a)
	}
	panic(p.errorf(sf.node, "no arm matches %s", strings.Join(strs, ", ")))
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

	case patArg:
		return true

	case patWild:
		return true

	case patLiteral:
		return p.consts[d.a].eq(v)

	case patVariant:
		t, ok := v.(Variant)
		if !ok || t.tag != label.ID(d.a) {
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
		rest, s, fields := p.record(n)
		if rest == none {
			// The checker closed the record's type to the pattern's fields.
			for i := 0; i < len(fields); i += 2 {
				if !p.match(cnode(fields[i+1]), r.values[fields[i]], f) {
					return false
				}
			}
			return true
		}
		for i := 0; i < len(fields); i += 2 {
			val := r.values[r.shape.index(s.labels[fields[i]])]
			if !p.match(cnode(fields[i+1]), val, f) {
				return false
			}
		}
		values := make([]Value, 0, len(r.values)-len(s.labels))
		for i, l := range r.shape.labels {
			if s.index(l) < 0 {
				values = append(values, r.values[i])
			}
		}
		return p.match(rest, Record{p.table.without(r.shape, s), values}, f)

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
