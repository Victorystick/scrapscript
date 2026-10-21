package eval

import (
	"fmt"
	"strings"
)

// A program is a compiled scrap, ready to run.
type program struct {
	*core
	compiled
}

// A frame holds the slots of one call of a function, or of the top level,
// and the captures of the function.
type frame struct {
	slots, caps []value
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
func (f frame) capture(caps []value, from []int32) {
	for i, x := range from {
		if x >= 0 {
			caps[i] = f.slots[x]
		} else {
			caps[i] = f.caps[-1-x]
		}
	}
}

// eval runs the program in a new top frame.
func (p *program) eval() (v value, err error) {
	defer catch(&err)
	return trampoline(p.body(p.root), frame{slots: make([]value, p.top)}), nil
}

// apply calls the function fn with arg.
func apply(fn, arg value) value {
	body, f, v := enter(fn, arg)
	if v.valid() {
		return v
	}
	return trampoline(body, f)
}

// call calls the function fn with args, one or more, whether it takes as
// many or not.
func call(fn value, args ...value) value {
	body, f, v := enterN(fn, args)
	if v.valid() {
		return v
	}
	return trampoline(body, f)
}

// call2 is call for two arguments, which a builtin or a script function
// that takes exactly two gets without a slice.
func call2(fn, a, b value) value {
	switch fn.boxedKind() {
	case kindBuiltin:
		if g := fn.asBuiltin(); g.two != nil {
			return g.two(a, b)
		}
	case kindScript:
		if g := fn.asScript(); len(g.args) == 0 && !g.composition() {
			if l := g.prog.lam(g.node); l.arity == 2 {
				f := g.frame(l.size)
				f.slots[0], f.slots[1] = a, b
				return trampoline(l.enterN(g, f), f)
			}
		}
	}
	return call(fn, a, b)
}

// enter starts calling fn with arg. A builtin returns its result v, as
// does a function that takes more arguments, with arg added to the ones it
// has. Otherwise, enter returns the body of a script function's first arm
// that matches all its arguments, in a new frame f, to run. A composition
// applies its first function and enters its second.
func enter(fn, arg value) (body tcode, f frame, v value) {
	for {
		switch fn.boxedKind() {
		case kindBuiltin:
			g := fn.asBuiltin()
			if g.arity == 1 {
				return nil, f, g.one(arg)
			}
			return nil, f, partialVal(&partial{g, []value{arg}})
		case kindPartial:
			// Each partial application has its own arguments.
			g := fn.asPartial()
			args := append(g.args[:len(g.args):len(g.args)], arg)
			if len(args) == g.fn.arity {
				return nil, f, g.fn.invoke(args)
			}
			return nil, f, partialVal(&partial{g.fn, args})
		}
		sf := fn.asScript()
		if sf.composition() {
			arg = apply(sf.caps[0], arg)
			fn = sf.caps[1]
			continue
		}
		l := sf.prog.lam(sf.node)
		if l.arity == 1 {
			body, f = l.enter1(sf, arg)
			return body, f, value{}
		}
		if len(sf.args)+1 < l.arity {
			a := sf.applied(1)
			a.args[len(sf.args)] = arg
			return nil, f, scriptVal(a)
		}
		f = sf.frame(l.size)
		copy(f.slots, sf.args)
		f.slots[l.arity-1] = arg
		return l.enterN(sf, f), f, value{}
	}
}

// enterN is enter for one or more arguments args at once. A function that
// takes exactly as many is called with all of them, without applying it to
// one at a time.
func enterN(fn value, args []value) (body tcode, f frame, v value) {
	switch fn.boxedKind() {
	case kindBuiltin:
		if g := fn.asBuiltin(); g.arity == len(args) && g.arity > 1 {
			return nil, f, g.invoke(args)
		}
	case kindScript:
		if g := fn.asScript(); len(args) > 1 && !g.composition() {
			l := g.prog.lam(g.node)
			switch have := len(g.args) + len(args); {
			case have < l.arity:
				// Applied to all of args at once, rather than to one at a
				// time.
				a := g.applied(len(args))
				copy(a.args[len(g.args):], args)
				return nil, f, scriptVal(a)
			case have == l.arity:
				f = g.frame(l.size)
				copy(f.slots[copy(f.slots, g.args):], args)
				return l.enterN(g, f), f, value{}
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
func (sf *scriptFunc) frame(size int) frame {
	return frame{slots: make([]value, size), caps: sf.caps}
}

// noArm returns the error that no arm of the function or match n matches
// the arguments args.
func (p *program) noArm(n cnode, args []value) runtimeError {
	strs := make([]string, len(args))
	for i, a := range args {
		strs[i] = printValue(p.table, a)
	}
	return p.errorf(n, "no arm matches %s", strings.Join(strs, ", "))
}
