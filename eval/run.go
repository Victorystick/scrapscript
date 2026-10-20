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
	return trampoline(p.body(p.root), frame{slots: make([]Value, p.top)}), nil
}

// apply calls the function fn with arg.
func apply(fn, arg Value) Value {
	body, f, v := enter(fn, arg)
	if v != nil {
		return v
	}
	return trampoline(body, f)
}

// call calls the function fn with args, one or more, whether it takes as
// many or not.
func call(fn Value, args ...Value) Value {
	body, f, v := enterN(fn, args)
	if v != nil {
		return v
	}
	return trampoline(body, f)
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
func enter(fn, arg Value) (body tcode, f frame, v Value) {
	for {
		switch g := fn.(type) {
		case BuiltInFunc:
			if g.arity == 1 {
				return nil, f, g.one(arg)
			}
			return nil, f, Partial{g, []Value{arg}}
		case Partial:
			// Each partial application has its own arguments.
			args := append(g.args[:len(g.args):len(g.args)], arg)
			if len(args) == g.fn.arity {
				return nil, f, g.fn.invoke(args)
			}
			return nil, f, Partial{g.fn, args}
		}
		sf := fn.(ScriptFunc)
		if sf.composition() {
			arg = apply(sf.caps[0], arg)
			fn = sf.caps[1]
			continue
		}
		l := sf.prog.lam(sf.node)
		if l.arity == 1 {
			body, f = l.enter1(sf, arg)
			return body, f, nil
		}
		if len(sf.args)+1 < l.arity {
			sf.args = append(sf.args[:len(sf.args):len(sf.args)], arg)
			return nil, f, sf
		}
		f = sf.frame(l.size)
		copy(f.slots, sf.args)
		f.slots[l.arity-1] = arg
		return l.enterN(sf, f), f, nil
	}
}

// enterN is enter for one or more arguments args at once. A function that
// takes exactly as many is called with all of them, without applying it to
// one at a time.
func enterN(fn Value, args []Value) (body tcode, f frame, v Value) {
	switch g := fn.(type) {
	case BuiltInFunc:
		if g.arity == len(args) && g.arity > 1 {
			return nil, f, g.invoke(args)
		}
	case ScriptFunc:
		if len(g.args) == 0 && len(args) > 1 && !g.composition() {
			if l := g.prog.lam(g.node); l.arity == len(args) {
				f = g.frame(l.size)
				copy(f.slots, args)
				return l.enterN(g, f), f, nil
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

// noArm returns the error that no arm of the function or match n matches
// the arguments args.
func (p *program) noArm(n cnode, args []Value) runtimeError {
	strs := make([]string, len(args))
	for i, a := range args {
		strs[i] = printValue(p.labels, a)
	}
	return p.errorf(n, "no arm matches %s", strings.Join(strs, ", "))
}
