package eval

import (
	"bytes"
	"slices"

	"github.com/Victorystick/scrapscript/label"
)

// The evaluator: the core compiled to Go closures. Each node becomes a
// function that evaluates it, made once, with what an interpreter would
// decode on every evaluation (the op, its operands, the arms of a match)
// decided when it is made.
//
// A function's body is compiled when it is first entered, and kept per
// program, by node. Code in tail position returns the body it calls
// instead of calling it, for trampoline to run, so that calls in tail
// position don't grow the Go stack.

// code evaluates an expression in the frame f.
type code func(f frame) value

// tcode evaluates an expression in tail position. If it ends in a call of
// a script function, it returns that function's body as next, with its new
// frame g, for the caller to continue with; otherwise it returns its value.
type tcode func(f frame) (v value, next tcode, g frame)

// mcode matches v against a pattern, writing the names it binds into the
// slots f.
type mcode func(v value, f []value) bool

// compiled holds a program's compiled bodies and lambdas, by node.
type compiled struct {
	bodies  []tcode
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
func (l *lambdaCode) enter1(sf *scriptFunc, arg value) (tcode, frame) {
	sf.prog.limit.tick()
	g := frame{caps: sf.caps}
	if l.size > 0 {
		g.slots = make([]value, l.size)
	}
	for _, a := range l.arms {
		if a.pats[0](arg, g.slots) {
			return a.body, g
		}
	}
	panic(sf.prog.errorf(sf.node, "no arm matches %s", printValue(sf.prog.table, arg)))
}

// enterN starts a call of the lambda l of sf with its arguments, the
// first slots of g, returning the body of the arm that matches.
func (l *lambdaCode) enterN(sf *scriptFunc, g frame) tcode {
	sf.prog.limit.tick()
	for _, a := range l.arms {
		if matchEach(a.pats, g.slots, g.slots) {
			return a.body
		}
	}
	panic(sf.prog.noArm(sf.node, g.slots[:l.arity]))
}

// trampoline runs the body c in f, and the bodies that it calls in tail
// position. Each call of it nests in the Go stack, so it counts the depth
// of the non-tail calls of l, failing beyond its maximum. An error that
// unwinds it leaves the depth to catch.
func trampoline(l *limit, c tcode, f frame) value {
	if l.depth >= l.maxDepth {
		panic(runtimeError{tooDeepError{l.maxDepth}})
	}
	l.depth++
	for {
		v, next, g := c(f)
		if next == nil {
			l.depth--
			return v
		}
		c, f = next, g
	}
}

// body returns the compiled body n, compiling it if it is the first time.
func (p *program) body(n cnode) tcode {
	if p.bodies == nil {
		p.bodies = make([]tcode, len(p.ops))
	}
	c := p.bodies[n]
	if c == nil {
		c = p.tail(n)
		p.bodies[n] = c
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

func (o operand) get(f frame) value {
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
func (p *program) builtin(n cnode) (*builtinFunc, bool) {
	if p.ops[n] != opConst {
		return nil, false
	}
	if v := p.consts[p.data[n].a]; v.kind() == kindBuiltin {
		return v.asBuiltin(), true
	}
	return nil, false
}

// expr compiles the expression n, not in tail position.
func (p *program) expr(n cnode) code {
	d := p.data[n]
	switch p.ops[n] {
	case opCall:
		ac := p.operand(cnode(d.b))
		if g, ok := p.builtin(cnode(d.a)); ok && g.arity == 1 {
			return func(f frame) value { return g.one(ac.get(f)) }
		}
		fc := p.operand(cnode(d.a))
		return func(f frame) value {
			fn := fc.get(f)
			arg := ac.get(f)
			if sf, ok := fn.script(); ok && len(sf.args) == 0 && !sf.composition() {
				if l := sf.prog.lam(sf.node); l.arity == 1 {
					body, g := l.enter1(sf, arg)
					return trampoline(sf.prog.limit, body, g)
				}
			}
			body, g, v, _ := enter(fn, arg)
			if v.valid() {
				return v
			}
			return trampoline(p.limit, body, g)
		}

	case opCallN:
		r := p.range_(n)
		acs := p.operands(r[1:])
		k := len(acs)
		if g, ok := p.builtin(cnode(r[0])); ok && g.arity == k {
			switch {
			case g.two != nil:
				a, b := acs[0], acs[1]
				return func(f frame) value {
					x := a.get(f)
					return g.two(x, b.get(f))
				}
			case g.three != nil:
				a, b, c := acs[0], acs[1], acs[2]
				return func(f frame) value {
					x := a.get(f)
					y := b.get(f)
					return g.three(x, y, c.get(f))
				}
			}
		}
		fc := p.operand(cnode(r[0]))
		return func(f frame) value {
			fn := fc.get(f)
			if g, ok := fn.builtin(); ok && g.arity == k {
				switch {
				case g.two != nil:
					x := acs[0].get(f)
					return g.two(x, acs[1].get(f))
				case g.three != nil:
					x := acs[0].get(f)
					y := acs[1].get(f)
					return g.three(x, y, acs[2].get(f))
				}
			}
			if sf, ok := fn.script(); ok && !sf.composition() {
				l := sf.prog.lam(sf.node)
				have := len(sf.args)
				if have+k < l.arity {
					a := sf.applied(k)
					for i, c := range acs {
						a.args[have+i] = c.get(f)
					}
					return scriptVal(a)
				}
				if have == 0 && l.arity == k {
					g := sf.frame(l.size)
					for i, c := range acs {
						g.slots[i] = c.get(f)
					}
					return trampoline(sf.prog.limit, l.enterN(sf, g), g)
				}
			}
			args := make([]value, k)
			for i, c := range acs {
				args[i] = c.get(f)
			}
			body, g, v, _ := enterN(fn, args)
			if v.valid() {
				return v
			}
			return trampoline(p.limit, body, g)
		}

	case opMatch:
		r := p.range_(n)
		vc := p.operand(cnode(r[0]))
		arms := r[1:]
		pats := make([]mcode, len(arms)/2)
		bodies := p.exprsOf(arms, 2)
		for i := range pats {
			pats[i] = p.pattern(cnode(arms[2*i]))
		}
		return func(f frame) value {
			v := vc.get(f)
			for i, m := range pats {
				if m(v, f.slots) {
					return bodies[i](f)
				}
			}
			panic(p.errorf(n, "no arm matches %s", printValue(p.table, v)))
		}

	case opMatchN:
		r := p.range_(n)
		s, k := r[0], int(r[1])
		vcs := p.operands(r[2 : 2+k])
		var pats [][]mcode
		bodies := p.exprsOf(r[2+k:], k+1)
		for a := r[2+k:]; len(a) > 0; a = a[k+1:] {
			pats = append(pats, p.patterns(a[:k]))
		}
		return func(f frame) value {
			args := f.slots[s : int(s)+k]
			for i, vc := range vcs {
				args[i] = vc.get(f)
			}
			for i, ms := range pats {
				if matchEach(ms, args, f.slots) {
					return bodies[i](f)
				}
			}
			panic(p.noArm(n, args))
		}

	case opWhere:
		body, binds := p.where(n)
		bind, bc := p.binds(binds), p.expr(body)
		return func(f frame) value {
			bind(f)
			return bc(f)
		}

	case opConst:
		v := p.consts[d.a]
		return func(frame) value { return v }

	case opLocal:
		s := d.a
		return func(f frame) value { return f.slots[s] }

	case opCapture:
		i := d.a
		return func(f frame) value { return f.caps[i] }

	case opAddInt:
		l, r := p.operand(cnode(d.a)), p.operand(cnode(d.b))
		return func(f frame) value {
			return intVal(l.get(f).asInt() + r.get(f).asInt())
		}

	case opAddFloat:
		l, r := p.operand(cnode(d.a)), p.operand(cnode(d.b))
		return func(f frame) value {
			return floatVal(l.get(f).asFloat() + r.get(f).asFloat())
		}

	case opSubInt:
		l, r := p.operand(cnode(d.a)), p.operand(cnode(d.b))
		return func(f frame) value {
			return intVal(l.get(f).asInt() - r.get(f).asInt())
		}

	case opSubFloat:
		l, r := p.operand(cnode(d.a)), p.operand(cnode(d.b))
		return func(f frame) value {
			return floatVal(l.get(f).asFloat() - r.get(f).asFloat())
		}

	case opMulInt:
		l, r := p.operand(cnode(d.a)), p.operand(cnode(d.b))
		return func(f frame) value {
			return intVal(l.get(f).asInt() * r.get(f).asInt())
		}

	case opMulFloat:
		l, r := p.operand(cnode(d.a)), p.operand(cnode(d.b))
		return func(f frame) value {
			return floatVal(l.get(f).asFloat() * r.get(f).asFloat())
		}

	case opAppendList:
		l, r := p.operand(cnode(d.a)), p.operand(cnode(d.b))
		return func(f frame) value {
			return listVal(append(l.get(f).asList(), r.get(f)))
		}

	case opAppendBytes:
		l, r := p.operand(cnode(d.a)), p.operand(cnode(d.b))
		return func(f frame) value {
			return bytesVal(append(l.get(f).asBytes(), r.get(f).asByte()))
		}

	case opPrependList:
		l, r := p.operand(cnode(d.a)), p.operand(cnode(d.b))
		return func(f frame) value {
			x := l.get(f)
			return listVal(append([]value{x}, r.get(f).asList()...))
		}

	case opPrependBytes:
		l, r := p.operand(cnode(d.a)), p.operand(cnode(d.b))
		return func(f frame) value {
			x := l.get(f).asByte()
			return bytesVal(append([]byte{x}, r.get(f).asBytes()...))
		}

	case opConcatList:
		l, r := p.operand(cnode(d.a)), p.operand(cnode(d.b))
		return func(f frame) value {
			a := l.get(f).asList()
			return listVal(append(a, r.get(f).asList()...))
		}

	case opConcatText:
		l, r := p.operand(cnode(d.a)), p.operand(cnode(d.b))
		return func(f frame) value {
			a := l.get(f).asText()
			return textVal(a + r.get(f).asText())
		}

	case opConcatBytes:
		l, r := p.operand(cnode(d.a)), p.operand(cnode(d.b))
		return func(f frame) value {
			a := l.get(f).asBytes()
			return bytesVal(append(a, r.get(f).asBytes()...))
		}

	case opCompose:
		first, second, _ := p.compose(n)
		fc, sc := p.expr(first), p.expr(second)
		return func(f frame) value {
			return scriptVal(&scriptFunc{prog: p, node: n, caps: []value{fc(f), sc(f)}})
		}

	case opLambda:
		from := p.captures(n)
		if len(from) == 0 {
			sf := scriptVal(&scriptFunc{prog: p, node: n})
			return func(frame) value { return sf }
		}
		return func(f frame) value {
			sf := &scriptFunc{prog: p, node: n, caps: make([]value, len(from))}
			f.capture(sf.caps, from)
			return scriptVal(sf)
		}

	case opFill:
		from := p.captures(cnode(d.a))
		s := d.b
		return func(f frame) value {
			sf := f.slots[s]
			f.capture(sf.asScript().caps, from)
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
			return func(f frame) value {
				r := make([]value, len(s.labels))
				for i, c := range vals {
					r[keys[i]] = c(f)
				}
				return recordVal(s, r)
			}
		}
		bc := p.expr(base)
		if s != nil {
			return func(f frame) value {
				r := make([]value, len(s.labels))
				copy(r, bc(f).fields())
				for i, c := range vals {
					r[keys[i]] = c(f)
				}
				return recordVal(s, r)
			}
		}
		return func(f frame) value {
			b := p.table.record(bc(f))
			r := slices.Clone(b.values)
			for i, c := range vals {
				r[b.shape.index(label.ID(keys[i]))] = c(f)
			}
			return recordVal(b.shape, r)
		}

	case opAccess:
		rc := p.operand(cnode(d.a))
		key := label.ID(d.b)
		return func(f frame) value {
			r := p.table.record(rc.get(f))
			return r.values[r.shape.index(key)]
		}

	case opField:
		rc := p.operand(cnode(d.a))
		return func(f frame) value {
			return rc.get(f).fields()[d.b]
		}

	case opList:
		elems := p.exprs(p.range_(n))
		return func(f frame) value {
			values := make([]value, len(elems))
			for i, c := range elems {
				values[i] = c(f)
			}
			return listVal(values)
		}

	case opVariant:
		tag := label.ID(d.a)
		if cnode(d.b) == none {
			v := tagVal(tag)
			return func(frame) value { return v }
		}
		pc := p.expr(cnode(d.b))
		return func(f frame) value { return variantVal(tag, pc(f)) }
	}
	panic(p.errorf(n, "unhandled op %d", p.ops[n]))
}

// done returns v as the value of a tcode.
func done(v value) (value, tcode, frame) {
	return v, nil, frame{}
}

// tail compiles the expression n in tail position.
func (p *program) tail(n cnode) tcode {
	d := p.data[n]
	switch p.ops[n] {
	case opCall:
		ac := p.operand(cnode(d.b))
		if g, ok := p.builtin(cnode(d.a)); ok && g.arity == 1 {
			return func(f frame) (value, tcode, frame) { return done(g.one(ac.get(f))) }
		}
		fc := p.operand(cnode(d.a))
		return func(f frame) (value, tcode, frame) {
			fn := fc.get(f)
			arg := ac.get(f)
			if sf, ok := fn.script(); ok && len(sf.args) == 0 && !sf.composition() {
				if l := sf.prog.lam(sf.node); l.arity == 1 {
					body, g := l.enter1(sf, arg)
					return value{}, body, g
				}
			}
			body, g, v, _ := enter(fn, arg)
			if v.valid() {
				return done(v)
			}
			return value{}, body, g
		}

	case opCallN:
		r := p.range_(n)
		acs := p.operands(r[1:])
		k := len(acs)
		if g, ok := p.builtin(cnode(r[0])); ok && g.arity == k {
			switch {
			case g.two != nil:
				a, b := acs[0], acs[1]
				return func(f frame) (value, tcode, frame) {
					x := a.get(f)
					return done(g.two(x, b.get(f)))
				}
			case g.three != nil:
				a, b, c := acs[0], acs[1], acs[2]
				return func(f frame) (value, tcode, frame) {
					x := a.get(f)
					y := b.get(f)
					return done(g.three(x, y, c.get(f)))
				}
			}
		}
		fc := p.operand(cnode(r[0]))
		return func(f frame) (value, tcode, frame) {
			fn := fc.get(f)
			if g, ok := fn.builtin(); ok && g.arity == k {
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
			if sf, ok := fn.script(); ok && !sf.composition() {
				l := sf.prog.lam(sf.node)
				have := len(sf.args)
				if have+k < l.arity {
					a := sf.applied(k)
					for i, c := range acs {
						a.args[have+i] = c.get(f)
					}
					return done(scriptVal(a))
				}
				if have == 0 && l.arity == k {
					g := sf.frame(l.size)
					for i, c := range acs {
						g.slots[i] = c.get(f)
					}
					return value{}, l.enterN(sf, g), g
				}
			}
			args := make([]value, k)
			for i, c := range acs {
				args[i] = c.get(f)
			}
			body, g, v, _ := enterN(fn, args)
			if v.valid() {
				return done(v)
			}
			return value{}, body, g
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
		return func(f frame) (value, tcode, frame) {
			v := vc.get(f)
			for i, m := range pats {
				if m(v, f.slots) {
					return bodies[i](f)
				}
			}
			panic(p.errorf(n, "no arm matches %s", printValue(p.table, v)))
		}

	case opMatchN:
		r := p.range_(n)
		s, k := r[0], int(r[1])
		vcs := p.operands(r[2 : 2+k])
		var arms []armCode
		for a := r[2+k:]; len(a) > 0; a = a[k+1:] {
			arms = append(arms, armCode{pats: p.patterns(a[:k]), body: p.tail(cnode(a[k]))})
		}
		return func(f frame) (value, tcode, frame) {
			args := f.slots[s : int(s)+k]
			for i, vc := range vcs {
				args[i] = vc.get(f)
			}
			for _, a := range arms {
				if matchEach(a.pats, args, f.slots) {
					return a.body(f)
				}
			}
			panic(p.noArm(n, args))
		}

	case opWhere:
		body, binds := p.where(n)
		bind, bc := p.binds(binds), p.tail(body)
		return func(f frame) (value, tcode, frame) {
			bind(f)
			return bc(f)
		}
	}
	c := p.expr(n)
	return func(f frame) (value, tcode, frame) { return done(c(f)) }
}

// binds compiles a where's bindings, as pairs of slot and value, into
// code that puts their values in their slots.
func (p *program) binds(binds []int32) func(f frame) {
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
	return func(f frame) {
		for _, b := range bs {
			if b.c == nil {
				f.slots[b.slot] = scriptVal(&scriptFunc{prog: p, node: b.l, caps: make([]value, b.caps)})
				continue
			}
			f.slots[b.slot] = b.c(f)
		}
	}
}

// exprsOf compiles the bodies of arms, the last of each stride nodes of
// ns.
func (p *program) exprsOf(ns []int32, stride int) []code {
	cs := make([]code, 0, len(ns)/stride)
	for i := stride - 1; i < len(ns); i += stride {
		cs = append(cs, p.expr(cnode(ns[i])))
	}
	return cs
}

func matchAny(value, []value) bool { return true }

// patterns compiles each pattern of ns.
func (p *program) patterns(ns []int32) []mcode {
	ms := make([]mcode, len(ns))
	for i, x := range ns {
		ms[i] = p.pattern(cnode(x))
	}
	return ms
}

// matchEach matches each value of vs against the pattern at its index.
func matchEach(ms []mcode, vs []value, f []value) bool {
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
		return func(v value, f []value) bool {
			f[s] = v
			return true
		}

	case patArg, patWild:
		return matchAny

	case patLiteral:
		// A literal is a primitive of the matched value's type. Ints, bytes
		// and () are the same if their n is.
		c := p.consts[d.a]
		switch c.kind() {
		case kindText:
			t := c.asText()
			return func(v value, _ []value) bool { return v.asText() == t }
		case kindBytes:
			b := c.asBytes()
			return func(v value, _ []value) bool { return bytes.Equal(v.asBytes(), b) }
		}
		return func(v value, _ []value) bool { return v.n == c.n }

	case patVariant:
		tag := label.ID(d.a)
		if cnode(d.b) == none {
			return func(v value, _ []value) bool { return v.hasTag(tag) }
		}
		m := p.pattern(cnode(d.b))
		return func(v value, f []value) bool {
			if !v.hasTag(tag) {
				return false
			}
			// A tag without a payload has the payload ().
			payload := v.payload()
			if !payload.valid() {
				payload = hole
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
			return func(v value, f []value) bool {
				r := v.fields()
				for i, m := range ms {
					if !m(r[keys[i]], f) {
						return false
					}
				}
				return true
			}
		}
		rm := p.pattern(rest)
		return func(v value, f []value) bool {
			r := p.table.record(v)
			for i, m := range ms {
				if !m(r.values[r.shape.index(s.labels[keys[i]])], f) {
					return false
				}
			}
			rs := p.table.without(r.shape, s)
			rest := make([]value, 0, len(rs.labels))
			for i, l := range r.shape.labels {
				if s.index(l) < 0 {
					rest = append(rest, r.values[i])
				}
			}
			return rm(recordVal(rs, rest), f)
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
		return func(v value, f []value) bool {
			r := v.fields()
			for i, m := range ms {
				if !m(r[keys[i]], f) {
					return false
				}
			}
			rest := make([]value, len(from))
			for i, j := range from {
				rest[i] = r[j]
			}
			return rm(recordVal(rs, rest), f)
		}

	case patList:
		ms := p.patterns(p.range_(n))
		return func(v value, f []value) bool {
			l := v.asList()
			return len(l) == len(ms) && matchEach(ms, l, f)
		}

	case patCons:
		hm, tm := p.pattern(cnode(d.a)), p.pattern(cnode(d.b))
		return func(v value, f []value) bool {
			l := v.asList()
			return len(l) > 0 && hm(l[0], f) && tm(listVal(l[1:]), f)
		}

	case patSnoc:
		im, lm := p.pattern(cnode(d.a)), p.pattern(cnode(d.b))
		return func(v value, f []value) bool {
			l := v.asList()
			last := len(l) - 1
			return last >= 0 && im(listVal(l[:last]), f) && lm(l[last], f)
		}

	case patPrefix:
		rest, elems := p.elems(n)
		rm, ms := p.pattern(rest), p.patterns(elems)
		return func(v value, f []value) bool {
			l := v.asList()
			return len(ms) <= len(l) &&
				matchEach(ms, l[:len(ms)], f) &&
				rm(listVal(l[len(ms):]), f)
		}

	case patSuffix:
		rest, elems := p.elems(n)
		rm, ms := p.pattern(rest), p.patterns(elems)
		return func(v value, f []value) bool {
			l := v.asList()
			split := len(l) - len(ms)
			return split >= 0 &&
				rm(listVal(l[:split]), f) &&
				matchEach(ms, l[split:], f)
		}
	}
	panic(p.errorf(n, "unhandled pattern %d", p.ops[n]))
}
