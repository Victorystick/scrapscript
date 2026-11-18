package eval

// Allocation assigns each binder that has a value a slot of a frame, each
// lambda its frame's size and captures, and each variable where it reads
// its binder from, into side tables: binders' slots, loc and frames.
//
// Slots come from a counter per frame, in evaluation order. The arms of a
// match share slots, and so does what follows the match: only code in an
// arm reads them, and closures copy what they capture. Likewise after a
// where-group in a function, but not at the top level, where other scraps'
// code that is inlined reads them. The arguments of a function of more
// than one, and of an inlined call of one, go in slots of their own, before
// the slots of code that computes them, since each is put there before the
// next is computed.

// An allocator walks a core, keeping the frame it is in.
type allocator struct {
	*core
	// The frame being allocated: how deeply it is nested, its next free
	// slot and its size so far.
	level, next, size int
	// The level of each binder's frame, or -1 before it has a slot.
	levels []int32
	// The captures of the function of each frame being allocated, by level.
	fns [][]capture
	// The level of the innermost lambda that refers to the names that code
	// refers to, or -1 in inlined bodies outside a lambda, which refer to
	// them only from their own function's code.
	refers int
}

// A capture is a binder that a function captures, and what it captures it
// from, as captures says.
type capture struct {
	b, from int32
}

// allocate assigns c's slots and captures.
func (c *compiler) allocate() {
	a := allocator{core: &c.core, levels: make([]int32, len(c.binders)), fns: [][]capture{nil}, refers: -1}
	for i := range a.levels {
		a.levels[i] = -1
	}
	c.loc = make([]int32, len(c.ops))
	a.expr(c.root)
	c.top = a.size
}

// bind gives the binder b the next slot of the current frame.
func (a *allocator) bind(b int32) {
	a.binders[b].slot = a.next
	a.levels[b] = int32(a.level)
	a.next++
	a.size = max(a.size, a.next)
}

// capture returns the index of the capture of b by the function of the
// frame at level, which b is bound outside of, adding it to the functions
// between them the first time.
func (a *allocator) capture(level int, b int32) int32 {
	for i, x := range a.fns[level] {
		if x.b == b {
			return int32(i)
		}
	}
	from := int32(a.binders[b].slot)
	if int(a.levels[b]) < level-1 {
		from = -1 - a.capture(level-1, b)
	}
	a.fns[level] = append(a.fns[level], capture{b, from})
	return int32(len(a.fns[level]) - 1)
}

// refer captures what code refers to without reading it: the name an
// inlined call calls by, which it prints with.
func (a *allocator) refer(b int32) {
	if b >= 0 && a.refers >= 0 && int(a.levels[b]) < a.refers {
		a.capture(a.refers, b)
	}
}

func (a *allocator) exprs(ns []int32) {
	for _, n := range ns {
		a.expr(cnode(n))
	}
}

func (a *allocator) expr(n cnode) {
	d := a.data[n]
	switch a.ops[n] {
	case opVar:
		if int(a.levels[d.a]) == a.level {
			a.loc[n] = int32(a.binders[d.a].slot)
		} else {
			a.loc[n] = -1 - a.capture(a.level, d.a)
		}
	case opCall, opAddInt, opSubInt, opMulInt, opAddFloat, opSubFloat, opMulFloat,
		opAppendList, opAppendBytes, opPrependList, opPrependBytes,
		opConcatList, opConcatText, opConcatBytes:
		a.expr(cnode(d.a))
		a.expr(cnode(d.b))
	case opCallN, opList:
		a.exprs(a.range_(n))
	case opCallKnown:
		a.exprs(a.range_(n)[1:])
	case opCompose:
		first, second, _ := a.compose(n)
		a.expr(first)
		a.expr(second)
	case opLambda:
		a.lambda(n)
	case opFill:
		a.lambda(cnode(d.a))
	case opMatch:
		v, callee, arms := a.match(n)
		a.refer(callee)
		a.expr(v)
		refers := a.refers
		if callee != notInlined {
			a.refers = -1
		}
		start := a.next
		for i := 0; i < len(arms); i += 2 {
			a.next = start
			a.pattern(cnode(arms[i]), 0)
			a.expr(cnode(arms[i+1]))
		}
		a.next, a.refers = start, refers
	case opMatchN:
		callee, args, arms := a.matchN(n)
		a.refer(callee)
		k := len(args)
		start := a.next
		a.loc[n] = int32(start)
		a.next += k
		a.size = max(a.size, a.next)
		a.exprs(args)
		refers := a.refers
		a.refers = -1
		for ; len(arms) > 0; arms = arms[k+1:] {
			a.next = start + k
			for _, p := range arms[:k] {
				a.pattern(cnode(p), int32(start))
			}
			a.expr(cnode(arms[k]))
		}
		a.next, a.refers = start, refers
	case opWhere:
		r := a.range_(n)
		start := a.next
		for b := r[1]; b < r[2]; b++ {
			if !a.binders[b].sort {
				a.bind(b)
			}
		}
		binds := r[3:]
		for i := 0; i < len(binds); i += 2 {
			b := binds[i]
			if b < 0 {
				continue // a closure, made of the lambda that its binding's value fills in
			}
			a.expr(cnode(binds[i+1]))
			if a.levels[b] < 0 {
				a.bind(b) // a scrap's, after its value
			}
		}
		a.expr(cnode(r[0]))
		if a.level > 0 {
			a.next = start
		}
	case opRecord:
		rest, _, fields := a.record(n)
		if rest != none {
			a.expr(rest)
		}
		for i := 1; i < len(fields); i += 2 {
			a.expr(cnode(fields[i]))
		}
	case opAccess, opField:
		a.expr(cnode(d.a))
	case opVariant:
		if cnode(d.b) != none {
			a.expr(cnode(d.b))
		}
	}
}

// lambda allocates the lambda n's frame, and its captures, and sets the
// captures that its uses refer to.
func (a *allocator) lambda(n cnode) {
	r := a.range_(n)
	u, arity, arms := r[0], int(r[1]), r[2:]
	level, next, size, refers := a.level, a.next, a.size, a.refers
	a.level++
	a.refers = a.level
	// The list of the lambda before it at this level, whose captures are
	// copied to frames, is reused.
	if a.level < len(a.fns) {
		a.fns = a.fns[:a.level+1]
		a.fns[a.level] = a.fns[a.level][:0]
	} else {
		a.fns = append(a.fns, nil)
	}
	// A function of one argument matches it as it is; one of more keeps
	// them in its first slots, for its arms to match.
	args := 0
	if arity > 1 {
		args = arity
	}
	a.size = args
	for i := 0; i < len(arms); i += arity + 1 {
		a.next = args
		for _, p := range arms[i : i+arity] {
			a.pattern(cnode(p), 0)
		}
		a.expr(cnode(arms[i+arity]))
	}

	caps := a.fns[a.level]
	a.loc[n] = int32(len(a.frames))
	a.frames = append(a.frames, int32(a.size), int32(len(caps)))
	for _, x := range caps {
		a.frames = append(a.frames, x.from)
	}
	free := a.uses[u].free
	for i, f := range free {
		if f.cap < 0 {
			continue
		}
		for j, x := range caps {
			if x.b == f.b {
				free[i].cap = int32(j)
				break
			}
		}
	}
	a.level, a.next, a.size, a.refers = level, next, size, refers
}

// pattern allocates the binders of the pattern n, whose arguments, if it
// names one, are in the slots from base.
func (a *allocator) pattern(n cnode, base int32) {
	d := a.data[n]
	switch a.ops[n] {
	case patBind:
		a.bind(d.a)
	case patArg:
		a.binders[d.a].slot = int(base + d.b)
		a.levels[d.a] = int32(a.level)
	case patVariant:
		if cnode(d.b) != none {
			a.pattern(cnode(d.b), base)
		}
	case patRecord:
		rest, _, fields := a.record(n)
		for i := 1; i < len(fields); i += 2 {
			a.pattern(cnode(fields[i]), base)
		}
		if rest != none {
			a.pattern(rest, base)
		}
	case patSplit:
		rest, _, _, _, fields := a.split(n)
		for i := 1; i < len(fields); i += 2 {
			a.pattern(cnode(fields[i]), base)
		}
		a.pattern(rest, base)
	case patList:
		for _, p := range a.range_(n) {
			a.pattern(cnode(p), base)
		}
	case patCons, patSnoc, patConsBytes, patSnocBytes:
		a.pattern(cnode(d.a), base)
		a.pattern(cnode(d.b), base)
	case patPrefix:
		rest, elems := a.elems(n)
		for _, p := range elems {
			a.pattern(cnode(p), base)
		}
		a.pattern(rest, base)
	case patSuffix:
		rest, elems := a.elems(n)
		a.pattern(rest, base)
		for _, p := range elems {
			a.pattern(cnode(p), base)
		}
	}
}
