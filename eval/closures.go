package eval

import (
	"slices"

	"github.com/Victorystick/scrapscript/label"
)

// The core compiled to Go closures: each node becomes a function that
// evaluates it, made once, with what run decodes on every evaluation (the
// op, its operands, the arms of a match) decided when it is made.
//
// A function's body is compiled when it is first entered, and kept per
// program, by node.

// code evaluates an expression in the frame f.
type code func(f frame) Value

// tcode evaluates an expression in tail position. If it ends in a call of
// a script function, it returns that function's body as next, with its new
// frame g, for the caller to continue with; otherwise it returns its value.
type tcode func(f frame) (v Value, next tcode, g frame)

// mcode matches v against a pattern, writing the names it binds into the
// slots f.
type mcode func(v Value, f []Value) bool

// compiled holds a program's compiled bodies and patterns, by node.
type compiled struct {
	bodies  []tcode
	pats    []mcode
	lambdas []*lambdaCode
}

// lambdaCode is a compiled lambda: its frame's size, its arity, and its
// arms.
type lambdaCode struct {
	size, arity int
	arms        []armCode
}

type armCode struct {
	pats []mcode // one per argument
	body tcode
}

// lam returns the compiled lambda n, compiling it if it is the first time.
func (p *program) lam(n cnode) *lambdaCode {
	if p.lambdas == nil {
		p.lambdas = make([]*lambdaCode, len(p.ops))
	}
	l := p.lambdas[n]
	if l == nil {
		size, _, arity, arms := p.lambda(n)
		l = &lambdaCode{size: size, arity: arity}
		for i := 0; i < len(arms); i += arity + 1 {
			l.arms = append(l.arms, armCode{
				pats: p.patterns(arms[i : i+arity]),
				body: p.body(cnode(arms[i+arity])),
			})
		}
		p.lambdas[n] = l
	}
	return l
}

// enter1 starts a call of the lambda l of sf, which takes one argument,
// with arg, returning the body of the arm that matches and its frame.
func (l *lambdaCode) enter1(sf ScriptFunc, arg Value) (tcode, frame) {
	g := frame{caps: sf.caps}
	if l.size > 0 {
		g.slots = make([]Value, l.size)
	}
	for _, a := range l.arms {
		if a.pats[0](arg, g.slots) {
			return a.body, g
		}
	}
	panic(sf.prog.errorf(sf.node, "no arm matches %s", printValue(sf.prog.labels, arg)))
}

// enterN starts a call of the lambda l of sf with its arguments, the
// first slots of g, returning the body of the arm that matches.
func (l *lambdaCode) enterN(sf ScriptFunc, g frame) tcode {
	for _, a := range l.arms {
		if matchEach(a.pats, g.slots, g.slots) {
			return a.body
		}
	}
	// Report it as the interpreter does.
	sf.prog.arm(sf, g)
	return nil
}

// trampoline runs the body c in f, and the bodies that it calls in tail
// position.
func trampoline(c tcode, f frame) Value {
	for {
		v, next, g := c(f)
		if next == nil {
			return v
		}
		c, f = next, g
	}
}

// body returns the compiled body n, compiling it if it is the first time.
func (p *program) body(n cnode) tcode {
	if p.bodies == nil {
		p.bodies = make([]tcode, len(p.ops))
		p.pats = make([]mcode, len(p.ops))
	}
	c := p.bodies[n]
	if c == nil {
		c = p.tail(n)
		p.bodies[n] = c
	}
	return c
}

// pat returns the compiled pattern n of a lambda's arm.
func (p *program) pat(n cnode) mcode {
	c := p.pats[n]
	if c == nil {
		c = p.pattern(n)
		p.pats[n] = c
	}
	return c
}

// exprs compiles each node of ns.
func (p *program) exprs(ns []int32) []code {
	cs := make([]code, len(ns))
	for i, x := range ns {
		cs[i] = p.expr(cnode(x))
	}
	return cs
}

// An operand is a compiled expression that reads a slot without a call,
// if it is one.
type operand struct {
	slot int32 // the slot it reads, or -1
	c    code
}

func (o operand) get(f frame) Value {
	if o.slot >= 0 {
		return f.slots[o.slot]
	}
	return o.c(f)
}

// operand compiles the expression n as an operand.
func (p *program) operand(n cnode) operand {
	if p.ops[n] == opLocal {
		return operand{slot: p.data[n].a}
	}
	return operand{slot: -1, c: p.expr(n)}
}

// operands compiles each node of ns as an operand.
func (p *program) operands(ns []int32) []operand {
	os := make([]operand, len(ns))
	for i, x := range ns {
		os[i] = p.operand(cnode(x))
	}
	return os
}

// builtin returns the builtin that n is a constant of, if it is.
func (p *program) builtin(n cnode) (BuiltInFunc, bool) {
	if p.ops[n] != opConst {
		return BuiltInFunc{}, false
	}
	g, ok := p.consts[p.data[n].a].(BuiltInFunc)
	return g, ok
}

// expr compiles the expression n, not in tail position.
func (p *program) expr(n cnode) code {
	d := p.data[n]
	switch p.ops[n] {
	case opCall, opCallN, opMatch, opWhere:
		t := p.tail(n)
		return func(f frame) Value {
			v, next, g := t(f)
			if next != nil {
				return trampoline(next, g)
			}
			return v
		}

	case opConst:
		v := p.consts[d.a]
		return func(frame) Value { return v }

	case opLocal:
		s := d.a
		return func(f frame) Value { return f.slots[s] }

	case opCapture:
		i := d.a
		return func(f frame) Value { return f.caps[i] }

	case opImport:
		return func(frame) Value {
			v, err := p.evalImport(p.at[n])
			if err != nil {
				panic(runtimeError{err})
			}
			return v
		}

	case opAddInt:
		l, r := p.operand(cnode(d.a)), p.operand(cnode(d.b))
		return func(f frame) Value {
			return l.get(f).(Int) + r.get(f).(Int)
		}

	case opAddFloat:
		l, r := p.operand(cnode(d.a)), p.operand(cnode(d.b))
		return func(f frame) Value {
			return l.get(f).(Float) + r.get(f).(Float)
		}

	case opSubInt:
		l, r := p.operand(cnode(d.a)), p.operand(cnode(d.b))
		return func(f frame) Value {
			return l.get(f).(Int) - r.get(f).(Int)
		}

	case opSubFloat:
		l, r := p.operand(cnode(d.a)), p.operand(cnode(d.b))
		return func(f frame) Value {
			return l.get(f).(Float) - r.get(f).(Float)
		}

	case opMulInt:
		l, r := p.operand(cnode(d.a)), p.operand(cnode(d.b))
		return func(f frame) Value {
			return l.get(f).(Int) * r.get(f).(Int)
		}

	case opMulFloat:
		l, r := p.operand(cnode(d.a)), p.operand(cnode(d.b))
		return func(f frame) Value {
			return l.get(f).(Float) * r.get(f).(Float)
		}

	case opAppendList:
		l, r := p.operand(cnode(d.a)), p.operand(cnode(d.b))
		return func(f frame) Value {
			a := l.get(f).(List)
			return List{append(slices.Clip(a.elements), r.get(f))}
		}

	case opAppendBytes:
		l, r := p.operand(cnode(d.a)), p.operand(cnode(d.b))
		return func(f frame) Value {
			a := l.get(f).(Bytes)
			return append(slices.Clip(a), byte(r.get(f).(Byte)))
		}

	case opPrependList:
		l, r := p.operand(cnode(d.a)), p.operand(cnode(d.b))
		return func(f frame) Value {
			b := r.get(f).(List)
			return List{append([]Value{l.get(f)}, b.elements...)}
		}

	case opPrependBytes:
		l, r := p.operand(cnode(d.a)), p.operand(cnode(d.b))
		return func(f frame) Value {
			b := r.get(f).(Bytes)
			return append(Bytes{byte(l.get(f).(Byte))}, b...)
		}

	case opConcatList:
		l, r := p.operand(cnode(d.a)), p.operand(cnode(d.b))
		return func(f frame) Value {
			a := l.get(f).(List)
			return List{append(slices.Clip(a.elements), r.get(f).(List).elements...)}
		}

	case opConcatText:
		l, r := p.operand(cnode(d.a)), p.operand(cnode(d.b))
		return func(f frame) Value {
			return l.get(f).(Text) + r.get(f).(Text)
		}

	case opConcatBytes:
		l, r := p.operand(cnode(d.a)), p.operand(cnode(d.b))
		return func(f frame) Value {
			a := l.get(f).(Bytes)
			return append(slices.Clip(a), r.get(f).(Bytes)...)
		}

	case opCompose:
		first, second, _ := p.compose(n)
		fc, sc := p.expr(first), p.expr(second)
		return func(f frame) Value {
			return ScriptFunc{prog: p, node: n, caps: []Value{fc(f), sc(f)}}
		}

	case opLambda:
		from := p.captures(n)
		if len(from) == 0 {
			sf := ScriptFunc{prog: p, node: n}
			return func(frame) Value { return sf }
		}
		return func(f frame) Value {
			sf := ScriptFunc{prog: p, node: n, caps: make([]Value, len(from))}
			f.capture(sf.caps, from)
			return sf
		}

	case opFill:
		from := p.captures(cnode(d.a))
		s := d.b
		return func(f frame) Value {
			sf := f.slots[s].(ScriptFunc)
			f.capture(sf.caps, from)
			return sf
		}

	case opRecord:
		base, s, fields := p.record(n)
		keys := make([]int32, len(fields)/2)
		vals := make([]code, len(fields)/2)
		for i := range keys {
			keys[i] = fields[2*i]
			vals[i] = p.expr(cnode(fields[2*i+1]))
		}
		if base == none {
			return func(f frame) Value {
				values := make([]Value, len(vals))
				for i, c := range vals {
					values[keys[i]] = c(f)
				}
				return Record{s, values}
			}
		}
		bc := p.expr(base)
		if s != nil {
			return func(f frame) Value {
				values := slices.Clone(bc(f).(Record).values)
				for i, c := range vals {
					values[keys[i]] = c(f)
				}
				return Record{s, values}
			}
		}
		return func(f frame) Value {
			r := bc(f).(Record)
			values := slices.Clone(r.values)
			for i, c := range vals {
				values[r.shape.index(label.ID(keys[i]))] = c(f)
			}
			return Record{r.shape, values}
		}

	case opAccess:
		rc := p.operand(cnode(d.a))
		key := label.ID(d.b)
		return func(f frame) Value {
			r := rc.get(f).(Record)
			return r.values[r.shape.index(key)]
		}

	case opField:
		rc := p.operand(cnode(d.a))
		return func(f frame) Value {
			return rc.get(f).(Record).values[d.b]
		}

	case opList:
		elems := p.exprs(p.range_(n))
		return func(f frame) Value {
			values := make([]Value, len(elems))
			for i, c := range elems {
				values[i] = c(f)
			}
			return List{values}
		}

	case opVariant:
		tag := label.ID(d.a)
		if cnode(d.b) == none {
			v := Variant{tag: tag}
			return func(frame) Value { return v }
		}
		pc := p.expr(cnode(d.b))
		return func(f frame) Value { return Variant{tag: tag, value: pc(f)} }
	}
	panic(p.errorf(n, "unhandled op %d", p.ops[n]))
}

// done returns v as the value of a tcode.
func done(v Value) (Value, tcode, frame) {
	return v, nil, frame{}
}

// tail compiles the expression n in tail position.
func (p *program) tail(n cnode) tcode {
	d := p.data[n]
	switch p.ops[n] {
	case opCall:
		ac := p.operand(cnode(d.b))
		if g, ok := p.builtin(cnode(d.a)); ok && g.arity == 1 {
			return func(f frame) (Value, tcode, frame) { return done(g.one(ac.get(f))) }
		}
		fc := p.operand(cnode(d.a))
		return func(f frame) (Value, tcode, frame) {
			fn := fc.get(f)
			arg := ac.get(f)
			if sf, ok := fn.(ScriptFunc); ok && len(sf.args) == 0 && !sf.composition() {
				if l := sf.prog.lam(sf.node); l.arity == 1 {
					body, g := l.enter1(sf, arg)
					return nil, body, g
				}
			}
			q, body, g, v := enter(fn, arg)
			if v != nil {
				return done(v)
			}
			return nil, q.body(body), g
		}

	case opCallN:
		r := p.range_(n)
		acs := p.operands(r[1:])
		k := len(acs)
		if g, ok := p.builtin(cnode(r[0])); ok && g.arity == k {
			switch {
			case g.two != nil:
				a, b := acs[0], acs[1]
				return func(f frame) (Value, tcode, frame) {
					x := a.get(f)
					return done(g.two(x, b.get(f)))
				}
			case g.three != nil:
				a, b, c := acs[0], acs[1], acs[2]
				return func(f frame) (Value, tcode, frame) {
					x := a.get(f)
					y := b.get(f)
					return done(g.three(x, y, c.get(f)))
				}
			}
		}
		fc := p.operand(cnode(r[0]))
		return func(f frame) (Value, tcode, frame) {
			fn := fc.get(f)
			if g, ok := fn.(BuiltInFunc); ok && g.arity == k {
				switch {
				case g.two != nil:
					x := acs[0].get(f)
					return done(g.two(x, acs[1].get(f)))
				case g.three != nil:
					x := acs[0].get(f)
					y := acs[1].get(f)
					return done(g.three(x, y, acs[2].get(f)))
				}
			}
			if sf, ok := fn.(ScriptFunc); ok && len(sf.args) == 0 && !sf.composition() {
				if l := sf.prog.lam(sf.node); l.arity == k {
					g := sf.frame(l.size)
					for i, c := range acs {
						g.slots[i] = c.get(f)
					}
					return nil, l.enterN(sf, g), g
				}
			}
			args := make([]Value, k)
			for i, c := range acs {
				args[i] = c.get(f)
			}
			q, body, g, v := enterN(fn, args)
			if v != nil {
				return done(v)
			}
			return nil, q.body(body), g
		}

	case opMatch:
		r := p.range_(n)
		vc := p.operand(cnode(r[0]))
		arms := r[1:]
		pats := make([]mcode, len(arms)/2)
		bodies := make([]tcode, len(arms)/2)
		for i := range pats {
			pats[i] = p.pattern(cnode(arms[2*i]))
			bodies[i] = p.tail(cnode(arms[2*i+1]))
		}
		return func(f frame) (Value, tcode, frame) {
			v := vc.get(f)
			for i, m := range pats {
				if m(v, f.slots) {
					return bodies[i](f)
				}
			}
			panic(p.errorf(n, "no arm matches %s", printValue(p.labels, v)))
		}

	case opWhere:
		body, binds := p.where(n)
		type bind struct {
			slot int32
			c    code  // its value, or nil for a closure made before its captures
			l    cnode // the lambda of that closure
			caps int
		}
		bs := make([]bind, len(binds)/2)
		for i := range bs {
			s, x := binds[2*i], cnode(binds[2*i+1])
			if s < 0 {
				bs[i] = bind{slot: -1 - s, l: x, caps: len(p.captures(x))}
			} else {
				bs[i] = bind{slot: s, c: p.expr(x)}
			}
		}
		bc := p.tail(body)
		return func(f frame) (Value, tcode, frame) {
			for _, b := range bs {
				if b.c == nil {
					f.slots[b.slot] = ScriptFunc{prog: p, node: b.l, caps: make([]Value, b.caps)}
					continue
				}
				f.slots[b.slot] = b.c(f)
			}
			return bc(f)
		}
	}
	c := p.expr(n)
	return func(f frame) (Value, tcode, frame) { return done(c(f)) }
}

func matchAny(Value, []Value) bool { return true }

// patterns compiles each pattern of ns.
func (p *program) patterns(ns []int32) []mcode {
	ms := make([]mcode, len(ns))
	for i, x := range ns {
		ms[i] = p.pattern(cnode(x))
	}
	return ms
}

// matchEach matches each value of vs against the pattern at its index.
func matchEach(ms []mcode, vs []Value, f []Value) bool {
	for i, m := range ms {
		if !m(vs[i], f) {
			return false
		}
	}
	return true
}

// pattern compiles the pattern n.
func (p *program) pattern(n cnode) mcode {
	d := p.data[n]
	switch p.ops[n] {
	case patBind:
		s := d.a
		return func(v Value, f []Value) bool {
			f[s] = v
			return true
		}

	case patArg, patWild:
		return matchAny

	case patLiteral:
		c := p.consts[d.a]
		return func(v Value, _ []Value) bool { return c.eq(v) }

	case patVariant:
		tag := label.ID(d.a)
		if cnode(d.b) == none {
			return func(v Value, _ []Value) bool {
				t, ok := v.(Variant)
				return ok && t.tag == tag
			}
		}
		m := p.pattern(cnode(d.b))
		return func(v Value, f []Value) bool {
			t, ok := v.(Variant)
			if !ok || t.tag != tag {
				return false
			}
			// A tag without a payload has the payload ().
			payload := t.value
			if payload == nil {
				payload = Hole{}
			}
			return m(payload, f)
		}

	case patRecord:
		rest, s, fields := p.record(n)
		keys := make([]int32, len(fields)/2)
		ms := make([]mcode, len(fields)/2)
		for i := range keys {
			keys[i] = fields[2*i]
			ms[i] = p.pattern(cnode(fields[2*i+1]))
		}
		if rest == none {
			// The checker closed the record's type to the pattern's fields.
			return func(v Value, f []Value) bool {
				r, ok := v.(Record)
				if !ok {
					return false
				}
				for i, m := range ms {
					if !m(r.values[keys[i]], f) {
						return false
					}
				}
				return true
			}
		}
		rm := p.pattern(rest)
		return func(v Value, f []Value) bool {
			r, ok := v.(Record)
			if !ok {
				return false
			}
			for i, m := range ms {
				if !m(r.values[r.shape.index(s.labels[keys[i]])], f) {
					return false
				}
			}
			values := make([]Value, 0, len(r.values)-len(s.labels))
			for i, l := range r.shape.labels {
				if s.index(l) < 0 {
					values = append(values, r.values[i])
				}
			}
			return rm(Record{p.table.without(r.shape, s), values}, f)
		}

	case patSplit:
		rest, _, rs, from, fields := p.split(n)
		keys := make([]int32, len(fields)/2)
		ms := make([]mcode, len(fields)/2)
		for i := range keys {
			keys[i] = fields[2*i]
			ms[i] = p.pattern(cnode(fields[2*i+1]))
		}
		rm := p.pattern(rest)
		return func(v Value, f []Value) bool {
			r, ok := v.(Record)
			if !ok {
				return false
			}
			for i, m := range ms {
				if !m(r.values[keys[i]], f) {
					return false
				}
			}
			values := make([]Value, len(from))
			for i, j := range from {
				values[i] = r.values[j]
			}
			return rm(Record{rs, values}, f)
		}

	case patList:
		ms := p.patterns(p.range_(n))
		return func(v Value, f []Value) bool {
			l, ok := v.(List)
			return ok && len(l.elements) == len(ms) && matchEach(ms, l.elements, f)
		}

	case patCons:
		hm, tm := p.pattern(cnode(d.a)), p.pattern(cnode(d.b))
		return func(v Value, f []Value) bool {
			l, ok := v.(List)
			return ok && len(l.elements) > 0 &&
				hm(l.elements[0], f) &&
				tm(List{l.elements[1:]}, f)
		}

	case patSnoc:
		im, lm := p.pattern(cnode(d.a)), p.pattern(cnode(d.b))
		return func(v Value, f []Value) bool {
			l, ok := v.(List)
			last := len(l.elements) - 1
			return ok && last >= 0 &&
				im(List{l.elements[:last]}, f) &&
				lm(l.elements[last], f)
		}

	case patPrefix:
		rest, elems := p.elems(n)
		rm, ms := p.pattern(rest), p.patterns(elems)
		return func(v Value, f []Value) bool {
			l, ok := v.(List)
			return ok && len(ms) <= len(l.elements) &&
				matchEach(ms, l.elements[:len(ms)], f) &&
				rm(List{l.elements[len(ms):]}, f)
		}

	case patSuffix:
		rest, elems := p.elems(n)
		rm, ms := p.pattern(rest), p.patterns(elems)
		return func(v Value, f []Value) bool {
			l, ok := v.(List)
			split := len(l.elements) - len(ms)
			return ok && split >= 0 &&
				rm(List{l.elements[:split]}, f) &&
				matchEach(ms, l.elements[split:], f)
		}
	}
	panic(p.errorf(n, "unhandled pattern %d", p.ops[n]))
}
