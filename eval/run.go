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
	compiled
}

// A frame holds the slots of one call of a function, or of the top level,
// and the captures of the function.
type frame struct {
	slots, caps []Value
}

// A runtimeError is a failure during evaluation. It unwinds to the edge of
// the evaluator as a panic, where catch turns it back into an error.
type runtimeError struct {
	err error
}

func (p *program) errorf(n cnode, format string, args ...any) runtimeError {
	return runtimeError{p.source(n).Error(p.at[n], fmt.Sprintf(format, args...))}
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

// capture copies what a lambda captures, from, out of f into caps.
func (f frame) capture(caps []Value, from []int32) {
	for i, x := range from {
		if x >= 0 {
			caps[i] = f.slots[x]
		} else {
			caps[i] = f.caps[-1-x]
		}
	}
}

// eval runs the program in a new top frame.
func (p *program) eval() (v Value, err error) {
	defer catch(&err)
	return p.runBody(p.root, frame{slots: make([]Value, p.top)}), nil
}

// runBody evaluates n, a function's body or the root, in f.
func (p *program) runBody(n cnode, f frame) Value {
	if closures {
		return trampoline(p.body(n), f)
	}
	return p.run(n, f)
}

// matches matches v against the pattern n of a lambda's arm.
func (p *program) matches(n cnode, v Value, f []Value) bool {
	if closures {
		return p.pat(n)(v, f)
	}
	return p.match(n, v, f)
}

// value evaluates the operand n in f. Most operands read a slot, which
// it does without a call of run, whose Go frame is large. It is just
// within Go's inlining budget: reading captures or constants too, or
// passing f by value, puts it over.
func (p *program) value(n cnode, f *frame) Value {
	if p.ops[n] == opLocal {
		return f.slots[p.data[n].a]
	}
	return p.runAt(n, f)
}

// runAt is run, for value to call without copying f where it is inlined.
//
//go:noinline
func (p *program) runAt(n cnode, f *frame) Value {
	return p.run(n, *f)
}

// run evaluates the expression n in the frame f.
//
// Calls in tail position don't grow the Go stack: a call, a where's body
// and a matching arm's body continue the loop in place of the expression,
// so a function that calls itself last runs in constant stack.
func (p *program) run(n cnode, f frame) Value {
	for {
		d := p.data[n]
		switch p.ops[n] {
		case opCall:
			fn := p.value(cnode(d.a), &f)
			arg := p.value(cnode(d.b), &f)
			var v Value
			p, n, f, v = enter(fn, arg)
			if v != nil {
				return v
			}

		case opCallN:
			r := p.range_(n)
			fn := p.value(cnode(r[0]), &f)
			argNodes := r[1:]
			// A builtin that takes exactly these arguments gets them as Go
			// arguments.
			if g, ok := fn.(BuiltInFunc); ok && g.arity == len(argNodes) {
				switch {
				case g.two != nil:
					a := p.value(cnode(argNodes[0]), &f)
					return g.two(a, p.value(cnode(argNodes[1]), &f))
				case g.three != nil:
					a := p.value(cnode(argNodes[0]), &f)
					b := p.value(cnode(argNodes[1]), &f)
					return g.three(a, b, p.value(cnode(argNodes[2]), &f))
				}
			}
			// A script function that takes exactly these arguments gets
			// them in its frame directly.
			if sf, ok := fn.(ScriptFunc); ok && len(sf.args) == 0 && !sf.composition() {
				if size, _, arity, _ := sf.prog.lambda(sf.node); arity == len(argNodes) {
					g := sf.frame(size)
					for i, x := range argNodes {
						g.slots[i] = p.value(cnode(x), &f)
					}
					p, f = sf.prog, g
					n = p.arm(sf, f)
					continue
				}
			}
			args := make([]Value, len(argNodes))
			for i, x := range argNodes {
				args[i] = p.value(cnode(x), &f)
			}
			var v Value
			p, n, f, v = enterN(fn, args)
			if v != nil {
				return v
			}

		case opMatch:
			r := p.range_(n)
			v := p.value(cnode(r[0]), &f)
			arms := r[1:]
			i := 0
			for i < len(arms) && !p.match(cnode(arms[i]), v, f.slots) {
				i += 2
			}
			if i == len(arms) {
				panic(p.errorf(n, "no arm matches %s", printValue(p.labels, v)))
			}
			n = cnode(arms[i+1])

		case opWhere:
			body, binds := p.where(n)
			for i := 0; i < len(binds); i += 2 {
				if s := binds[i]; s < 0 {
					// A closure of a recursive binding, before its captures.
					l := cnode(binds[i+1])
					f.slots[-1-s] = ScriptFunc{prog: p, node: l, caps: make([]Value, len(p.captures(l)))}
					continue
				}
				f.slots[binds[i]] = p.value(cnode(binds[i+1]), &f)
			}
			n = body

		case opConst:
			return p.consts[d.a]

		case opLocal:
			return f.slots[d.a]

		case opCapture:
			return f.caps[d.a]

		// The checker settled the operands' types, which the op says.
		case opAddInt:
			return p.value(cnode(d.a), &f).(Int) + p.value(cnode(d.b), &f).(Int)
		case opSubInt:
			return p.value(cnode(d.a), &f).(Int) - p.value(cnode(d.b), &f).(Int)
		case opMulInt:
			return p.value(cnode(d.a), &f).(Int) * p.value(cnode(d.b), &f).(Int)
		case opAddFloat:
			return p.value(cnode(d.a), &f).(Float) + p.value(cnode(d.b), &f).(Float)
		case opSubFloat:
			return p.value(cnode(d.a), &f).(Float) - p.value(cnode(d.b), &f).(Float)
		case opMulFloat:
			return p.value(cnode(d.a), &f).(Float) * p.value(cnode(d.b), &f).(Float)

		// Values are immutable, so appending clips the left operand first:
		// append must copy rather than write into spare capacity that other
		// values share.
		case opAppendList:
			l := p.value(cnode(d.a), &f).(List)
			return List{append(slices.Clip(l.elements), p.value(cnode(d.b), &f))}

		case opAppendBytes:
			l := p.value(cnode(d.a), &f).(Bytes)
			return append(slices.Clip(l), byte(p.value(cnode(d.b), &f).(Byte)))

		case opPrependList:
			r := p.value(cnode(d.b), &f).(List)
			return List{append([]Value{p.value(cnode(d.a), &f)}, r.elements...)}

		case opPrependBytes:
			r := p.value(cnode(d.b), &f).(Bytes)
			return append(Bytes{byte(p.value(cnode(d.a), &f).(Byte))}, r...)

		case opConcatList:
			l := p.value(cnode(d.a), &f).(List)
			return List{append(slices.Clip(l.elements), p.value(cnode(d.b), &f).(List).elements...)}

		case opConcatText:
			l := p.value(cnode(d.a), &f).(Text)
			return l + p.value(cnode(d.b), &f).(Text)

		case opConcatBytes:
			l := p.value(cnode(d.a), &f).(Bytes)
			return append(slices.Clip(l), p.value(cnode(d.b), &f).(Bytes)...)

		case opCompose:
			first, second, _ := p.compose(n)
			fns := []Value{p.value(first, &f), p.value(second, &f)}
			return ScriptFunc{prog: p, node: n, caps: fns}

		case opLambda:
			from := p.captures(n)
			if len(from) == 0 {
				return ScriptFunc{prog: p, node: n}
			}
			sf := ScriptFunc{prog: p, node: n, caps: make([]Value, len(from))}
			f.capture(sf.caps, from)
			return sf

		case opFill:
			// Copies of the closure share its captures, so this fills them
			// in for the ones that the binding's value has captured.
			sf := f.slots[d.b].(ScriptFunc)
			f.capture(sf.caps, p.captures(cnode(d.a)))
			return sf

		case opRecord:
			base, s, fields := p.record(n)
			if base == none {
				values := make([]Value, len(fields)/2)
				for i := 0; i < len(fields); i += 2 {
					values[fields[i]] = p.value(cnode(fields[i+1]), &f)
				}
				return Record{s, values}
			}
			// A record based on another, with some of its fields updated.
			r := p.value(base, &f).(Record)
			values := slices.Clone(r.values)
			for i := 0; i < len(fields); i += 2 {
				key := int(fields[i])
				if s == nil {
					key = r.shape.index(label.ID(key))
				}
				values[key] = p.value(cnode(fields[i+1]), &f)
			}
			return Record{r.shape, values}

		case opAccess:
			r := p.value(cnode(d.a), &f).(Record)
			return r.values[r.shape.index(label.ID(d.b))]

		case opField:
			return p.value(cnode(d.a), &f).(Record).values[d.b]

		case opList:
			elems := p.range_(n)
			values := make([]Value, len(elems))
			for i, x := range elems {
				values[i] = p.value(cnode(x), &f)
			}
			return List{values}

		case opVariant:
			v := Variant{tag: label.ID(d.a)}
			if cnode(d.b) != none {
				v.value = p.value(cnode(d.b), &f)
			}
			return v

		default:
			panic(p.errorf(n, "unhandled op %d", p.ops[n]))
		}
	}
}

// apply calls the function fn with arg.
func apply(fn, arg Value) Value {
	p, n, f, v := enter(fn, arg)
	if v != nil {
		return v
	}
	return p.runBody(n, f)
}

// call calls the function fn with args, one or more, whether it takes as
// many or not.
func call(fn Value, args ...Value) Value {
	p, n, f, v := enterN(fn, args)
	if v != nil {
		return v
	}
	return p.runBody(n, f)
}

// call2 is call for two arguments, which a builtin or a script function
// that takes exactly two gets without a slice.
func call2(fn, a, b Value) Value {
	switch g := fn.(type) {
	case BuiltInFunc:
		if g.two != nil {
			return g.two(a, b)
		}
	case ScriptFunc:
		if len(g.args) == 0 && !g.composition() {
			if closures {
				if l := g.prog.lam(g.node); l.arity == 2 {
					f := g.frame(l.size)
					f.slots[0], f.slots[1] = a, b
					return trampoline(l.enterN(g, f), f)
				}
				break
			}
			if size, _, arity, _ := g.prog.lambda(g.node); arity == 2 {
				f := g.frame(size)
				f.slots[0], f.slots[1] = a, b
				return g.prog.runBody(g.prog.arm(g, f), f)
			}
		}
	}
	return call(fn, a, b)
}

// enter starts calling fn with arg. A builtin returns its result v, as
// does a function that takes more arguments, with arg added to the ones it
// has. Otherwise, enter returns the body n of a script function's first
// arm that matches all its arguments, in a new frame f, for its program p
// to evaluate. A composition applies its first function and enters its
// second.
func enter(fn, arg Value) (p *program, n cnode, f frame, v Value) {
	for {
		switch g := fn.(type) {
		case BuiltInFunc:
			if g.arity == 1 {
				return nil, none, f, g.one(arg)
			}
			return nil, none, f, Partial{g, []Value{arg}}
		case Partial:
			// Each partial application has its own arguments.
			args := append(g.args[:len(g.args):len(g.args)], arg)
			if len(args) == g.fn.arity {
				return nil, none, f, g.fn.invoke(args)
			}
			return nil, none, f, Partial{g.fn, args}
		}
		sf := fn.(ScriptFunc)
		if sf.composition() {
			arg = apply(sf.caps[0], arg)
			fn = sf.caps[1]
			continue
		}
		size, _, arity, arms := sf.prog.lambda(sf.node)
		if arity == 1 {
			// The common case: match arg as it is, without storing it.
			p = sf.prog
			f = frame{caps: sf.caps}
			if size > 0 {
				f.slots = make([]Value, size)
			}
			for i := 0; i < len(arms); i += 2 {
				if p.matches(cnode(arms[i]), arg, f.slots) {
					return p, cnode(arms[i+1]), f, nil
				}
			}
			panic(p.errorf(sf.node, "no arm matches %s", printValue(p.labels, arg)))
		}
		if len(sf.args)+1 < arity {
			sf.args = append(sf.args[:len(sf.args):len(sf.args)], arg)
			return nil, none, f, sf
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
func enterN(fn Value, args []Value) (p *program, n cnode, f frame, v Value) {
	switch g := fn.(type) {
	case BuiltInFunc:
		if g.arity == len(args) && g.arity > 1 {
			return nil, none, f, g.invoke(args)
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
func (sf ScriptFunc) frame(size int) frame {
	return frame{slots: make([]Value, size), caps: sf.caps}
}

// arm returns the body of the first arm of the lambda sf that matches its
// arguments, the first slots of f, binding the arm's names in f. sf takes
// more than one argument.
func (p *program) arm(sf ScriptFunc, f frame) cnode {
	_, _, arity, arms := p.lambda(sf.node)
	args := f.slots[:arity]
	for i := 0; i < len(arms); i += arity + 1 {
		if p.matchArm(arms[i:i+arity], args, f.slots) {
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
// the slots f. An arm that fails may have written some of its slots; the
// next arm overwrites the ones it uses.
func (p *program) match(n cnode, v Value, f []Value) bool {
	d := p.data[n]
	switch p.ops[n] {
	case patBind:
		f[d.a] = v
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

	case patSplit:
		r, ok := v.(Record)
		if !ok {
			return false
		}
		rest, _, rs, from, fields := p.split(n)
		for i := 0; i < len(fields); i += 2 {
			if !p.match(cnode(fields[i+1]), r.values[fields[i]], f) {
				return false
			}
		}
		values := make([]Value, len(from))
		for i, j := range from {
			values[i] = r.values[j]
		}
		return p.match(rest, Record{rs, values}, f)

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

// matchArm matches each argument against the pattern of an arm at the same index.
func (p *program) matchArm(pats []int32, vs []Value, f []Value) bool {
	for i, x := range pats {
		if !p.matches(cnode(x), vs[i], f) {
			return false
		}
	}
	return true
}

// matchAll matches each value against the pattern at the same index.
func (p *program) matchAll(pats []int32, vs []Value, f []Value) bool {
	for i, x := range pats {
		if !p.match(cnode(x), vs[i], f) {
			return false
		}
	}
	return true
}
