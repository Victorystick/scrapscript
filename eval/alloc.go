package eval

// Allocation assigns each binder that has a value a slot of a frame, each
// lambda its frame's size and captures, and each variable where it reads
// its binder from, into side tables: binders' slots, loc and frames.
//
// A walk of the core in evaluation order counts positions, one per read of
// a slot and per write, and computes each lambda's captures and each
// binder's last read: a variable of its own frame, or a lambda made in its
// frame that captures it, which copies it out of its slot. It logs the
// writes and frames in order, and a replay of the log then gives each
// binder the lowest slot whose binder is dead when it is written, one
// whose last read is behind. A frame is code without loops, whose only
// branches are the arms of matches, so positions in evaluation order order
// the reads and writes of any one run; a binder that only a later arm
// reads is kept alive through the arms before it.
//
// The arguments of a function of more than one, and of an inlined call of
// one, are in consecutive slots, which every arm's patterns read, and an
// error when none matches. So they are kept while the patterns run, and in
// an arm's body only if it names them.

// An allocator walks a core, keeping the frame it is in.
type allocator struct {
	*core
	// The position of the last read or write.
	at int32
	// How deeply the frame being walked is nested.
	level int
	// The level of each binder's frame, or -1 before it is written; and the
	// position of its last read, or -1.
	levels, last []int32
	// The binders that the function of each frame being walked captures,
	// by level.
	fns [][]int32
	// The level of the innermost lambda that refers to the names that code
	// refers to, or -1 in inlined bodies outside a lambda, which refer to
	// them only from their own function's code.
	refers int
	// The binders that the patterns of the arm being walked bind.
	bound []int32
	// The writes and frames, in evaluation order, as events of four
	// int32s: a kind, two operands and a position.
	log []int32
}

// The kinds of events in the log.
const (
	evBind     = iota // binder; position
	evReserve         // opMatchN, its number of arguments k; position
	evPatterns        // the opMatchN of the arguments, or none for a lambda's, k
	evBody            // the same, and the index in extra of the arm's patterns
	evRelease         // the opMatchN, k
	evEnter           // lambda
	evExit            // lambda
)

// allocate assigns c's slots and captures.
func (c *compiler) allocate() {
	n := len(c.binders)
	a := allocator{core: &c.core, fns: [][]int32{nil}, refers: -1, levels: make([]int32, 2*n)}
	a.levels, a.last = a.levels[:n], a.levels[n:]
	for i := range a.levels {
		a.levels[i], a.last[i] = -1, -1
	}
	c.loc = make([]int32, len(c.ops))
	a.expr(c.root)
	c.top = a.replay()
	// Variables of their own frame read their binder's slot; the others
	// have their capture.
	for n, o := range c.ops {
		if o == opVar && c.loc[n] >= 0 {
			c.loc[n] = int32(c.binders[c.data[n].a].slot)
		}
	}
}

// tick advances the position to the next read or write.
func (a *allocator) tick() int32 {
	a.at++
	return a.at
}

func (a *allocator) event(kind, x, y, at int32) {
	a.log = append(a.log, kind, x, y, at)
}

// bind writes the binder b in the current frame.
func (a *allocator) bind(b int32) {
	a.levels[b] = int32(a.level)
	a.event(evBind, b, 0, a.tick())
}

// capture returns the index of the capture of b by the function of the
// frame at level, which b is bound outside of, adding it to the functions
// between them the first time.
func (a *allocator) capture(level int, b int32) int32 {
	for i, x := range a.fns[level] {
		if x == b {
			return int32(i)
		}
	}
	if int(a.levels[b]) < level-1 {
		a.capture(level-1, b)
	}
	a.fns[level] = append(a.fns[level], b)
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
		a.tick()
		if int(a.levels[d.a]) == a.level {
			a.last[d.a] = a.at
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
		// It reads the closure in its binder's slot, and fills it in.
		a.lambda(cnode(d.a))
		a.last[d.b] = a.at
	case opMatch:
		v, callee, arms := a.match(n)
		a.refer(callee)
		a.expr(v)
		refers := a.refers
		if callee != notInlined {
			a.refers = -1
		}
		for i := 0; i < len(arms); i += 2 {
			a.arm(n, arms, i, 1, false)
		}
		a.refers = refers
	case opMatchN:
		callee, args, arms := a.matchN(n)
		a.refer(callee)
		k := int32(len(args))
		a.event(evReserve, int32(n), k, a.tick())
		a.exprs(args)
		refers := a.refers
		a.refers = -1
		for i := 0; i < len(arms); i += int(k) + 1 {
			a.arm(n, arms, i, int(k), true)
		}
		a.refers = refers
		a.event(evRelease, int32(n), k, 0)
	case opWhere:
		r := a.range_(n)
		binds := r[1:]
		for i := 0; i < len(binds); i += 2 {
			b := binds[i]
			if b < 0 {
				// A closure, made of the lambda that its binding's value
				// fills in.
				a.bind(-1 - b)
				continue
			}
			a.expr(cnode(binds[i+1]))
			if a.levels[b] < 0 {
				a.bind(b)
			}
		}
		a.expr(cnode(r[0]))
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

// arm walks the arm at arms[i:] of the match or lambda n, its k patterns
// and then its body. If args, the patterns match arguments in slots of
// their own, which are kept while they run.
func (a *allocator) arm(n cnode, arms []int32, i, k int, args bool) {
	pats, body := arms[i:i+k], cnode(arms[i+k])
	of := int32(n)
	if a.ops[n] == opLambda {
		of = int32(none)
	}
	if args {
		a.event(evPatterns, of, int32(k), 0)
	}
	mark := len(a.bound)
	for _, p := range pats {
		a.pattern(cnode(p))
	}
	// A pattern's names are kept until all its patterns have matched, in
	// whichever order they write them.
	end := a.tick()
	for _, b := range a.bound[mark:] {
		a.last[b] = max(a.last[b], end)
	}
	a.bound = a.bound[:mark]
	if args {
		// The index in extra of the arm's patterns.
		a.event(evBody, of, int32(k), a.data[n].a+int32(len(a.range_(n))-len(arms)+i))
	}
	a.expr(body)
}

// lambda walks the lambda n's frame, and sets its captures and the
// captures that its uses refer to.
func (a *allocator) lambda(n cnode) {
	r := a.range_(n)
	u, arity, arms := r[0], int(r[1]), r[2:]
	level, refers := a.level, a.refers
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
	a.event(evEnter, int32(n), 0, 0)
	// A function of one argument matches it as it is; one of more keeps
	// them in its first slots, for its arms to match.
	for i := 0; i < len(arms); i += arity + 1 {
		a.arm(n, arms, i, arity, arity > 1)
	}

	caps := a.fns[a.level]
	a.loc[n] = int32(len(a.frames))
	a.frames = append(a.frames, 0, int32(len(caps)))
	a.frames = append(a.frames, caps...)
	refs := a.uses[u].free
	for i, f := range refs {
		if f.cap < 0 {
			continue
		}
		for j, x := range caps {
			if x == f.b {
				refs[i].cap = int32(j)
				break
			}
		}
	}
	a.event(evExit, int32(n), 0, 0)
	a.level, a.refers = level, refers

	// It is made here, copying what it captures out of the slots of this
	// frame, or out of the captures of this frame's function.
	a.tick()
	for _, b := range caps {
		if int(a.levels[b]) == a.level {
			a.last[b] = a.at
		}
	}
}

// pattern walks the binders of the pattern n.
func (a *allocator) pattern(n cnode) {
	d := a.data[n]
	switch a.ops[n] {
	case patBind:
		a.bind(d.a)
		a.bound = append(a.bound, d.a)
	case patArg:
		a.levels[d.a] = int32(a.level)
		a.bound = append(a.bound, d.a)
	case patVariant:
		if cnode(d.b) != none {
			a.pattern(cnode(d.b))
		}
	case patRecord:
		rest, _, fields := a.record(n)
		for i := 1; i < len(fields); i += 2 {
			a.pattern(cnode(fields[i]))
		}
		if rest != none {
			a.pattern(rest)
		}
	case patSplit:
		rest, _, _, _, fields := a.split(n)
		for i := 1; i < len(fields); i += 2 {
			a.pattern(cnode(fields[i]))
		}
		a.pattern(rest)
	case patList:
		for _, p := range a.range_(n) {
			a.pattern(cnode(p))
		}
	case patCons, patSnoc, patConsBytes, patSnocBytes:
		a.pattern(cnode(d.a))
		a.pattern(cnode(d.b))
	case patPrefix:
		rest, elems := a.elems(n)
		for _, p := range elems {
			a.pattern(cnode(p))
		}
		a.pattern(rest)
	case patSuffix:
		rest, elems := a.elems(n)
		a.pattern(rest)
		for _, p := range elems {
			a.pattern(cnode(p))
		}
	}
}

// What a slot holds besides a binder.
const (
	free     = -1
	reserved = -2 // an argument, while patterns read it
)

// A replay assigns slots by the log, keeping the slots of the frame it is
// in: the binder each holds, or free or reserved.
type replay struct {
	*allocator
	slots []int32
	size  int
	at    int32
}

// dead reports whether the slot s is free at the current position.
func (r *replay) dead(s int) bool {
	if s >= len(r.slots) {
		return true
	}
	o := r.slots[s]
	return o == free || o >= 0 && r.last[o] < r.at
}

// claim marks the k slots from s as held by o, growing the frame.
func (r *replay) claim(s, k int, o int32) {
	for len(r.slots) < s+k {
		r.slots = append(r.slots, free)
	}
	for i := s; i < s+k; i++ {
		r.slots[i] = o
	}
	r.size = max(r.size, len(r.slots))
}

// base returns the first slot of the arguments of the opMatchN n, or of a
// lambda's if n is none.
func (r *replay) base(n int32) int {
	if cnode(n) == none {
		return 0
	}
	return int(r.loc[n])
}

// replay assigns the slots of the log's writes, and the frames' sizes and
// captures, returning the size of the top frame.
func (a *allocator) replay() int {
	r := replay{allocator: a}
	type saved struct {
		slots []int32
		size  int
	}
	var frames []saved
	var spare [][]int32 // the slots of frames left, for reuse
	var lambdas []int32 // the index in frames of each lambda entered
	for e := a.log; len(e) > 0; e = e[4:] {
		x, y := e[1], e[2]
		r.at = e[3]
		switch e[0] {
		case evBind:
			s := 0
			for !r.dead(s) {
				s++
			}
			r.claim(s, 1, x)
			a.binders[x].slot = s
		case evReserve:
			k := int(y)
			s := 0
			for {
				i := 0
				for i < k && r.dead(s+i) {
					i++
				}
				if i == k {
					break
				}
				s += i + 1
			}
			r.claim(s, k, reserved)
			a.loc[x] = int32(s)
		case evPatterns:
			r.claim(r.base(x), int(y), reserved)
		case evBody:
			// The body keeps only the arguments it names.
			s := r.base(x)
			for i, p := range a.extra[e[3] : e[3]+y] {
				o := int32(free)
				if a.ops[p] == patArg {
					o = a.data[p].a
					a.binders[o].slot = s + i
				}
				r.slots[s+i] = o
			}
		case evRelease:
			for i := r.base(x); i < r.base(x)+int(y); i++ {
				if r.slots[i] == reserved {
					r.slots[i] = free
				}
			}
		case evEnter:
			frames = append(frames, saved{r.slots, r.size})
			lambdas = append(lambdas, a.loc[x])
			r.slots, r.size = nil, 0
			if len(spare) > 0 {
				r.slots, spare = spare[len(spare)-1][:0], spare[:len(spare)-1]
			}
			if arity := int(a.extra[a.data[x].a+1]); arity > 1 {
				r.claim(0, arity, reserved)
			}
		case evExit:
			i := a.loc[x]
			a.frames[i] = int32(r.size)
			spare = append(spare, r.slots)
			f := frames[len(frames)-1]
			frames, lambdas = frames[:len(frames)-1], lambdas[:len(lambdas)-1]
			r.slots, r.size = f.slots, f.size
			// What it captures, now that the frame it is made in has its
			// slots, and the captures of that frame's function are still
			// binders.
			level := int32(len(frames))
			caps := a.frames[i+2 : i+2+a.frames[i+1]]
			for j, b := range caps {
				if a.levels[b] == level {
					caps[j] = int32(a.binders[b].slot)
					continue
				}
				outer := lambdas[len(lambdas)-1]
				for k, o := range a.frames[outer+2 : outer+2+a.frames[outer+1]] {
					if o == b {
						caps[j] = -1 - int32(k)
						break
					}
				}
			}
		}
	}
	return r.size
}
