package eval

// Allocation assigns each binder that has a value a slot of a frame, each
// lambda its frame's size and captures, and each variable where it reads
// its binder from, into side tables: binders' slots, loc and frames.
//
// It walks the core twice, in evaluation order, counting the same
// positions both times: each read of a slot, and each write. The first
// walk computes each lambda's captures, and each binder's last read: a
// variable of its own frame, or a lambda made in its frame that captures
// it, which copies it out of its slot. The second gives each binder the
// lowest slot whose binder is dead when it is written, one whose last read
// is behind. A frame is code without loops, whose only branches are the
// arms of matches, so positions in evaluation order order the reads and
// writes of any one run; a binder that only a later arm reads is kept
// alive through the arms before it.
//
// The arguments of a function of more than one, and of an inlined call of
// one, are in consecutive slots, which every arm's patterns read, and an
// error when none matches. So they are kept while the patterns run, and in
// an arm's body only if it names them.

// An allocator walks a core, keeping the frame it is in.
type allocator struct {
	*core
	// Which walk this is, 1 or 2, and the position of the last read or
	// write.
	walk int
	at   int32
	// How deeply the frame being allocated is nested.
	level int
	// The level of each binder's frame, or -1 before it is written; and the
	// position of its last read, or -1.
	levels, last []int32
	// The binders that the function of each frame being allocated
	// captures, by level, in the first walk.
	fns [][]int32
	// The level of the innermost lambda that refers to the names that code
	// refers to, or -1 in inlined bodies outside a lambda, which refer to
	// them only from their own function's code.
	refers int
	// The binders that the patterns of the arm being walked bind, in the
	// first walk.
	bound []int32

	// In the second walk, the frame's slots and its size so far: the binder
	// each slot holds, or free or reserved.
	slots []int32
	size  int
	// The slots of the frames around the current one, by level, kept for
	// reuse, and the index in frames of each lambda being allocated.
	spare   [][]int32
	lambdas []int32
}

// What a slot holds besides a binder.
const (
	free     = -1
	reserved = -2 // an argument, while patterns read it
)

// allocate assigns c's slots and captures.
func (c *compiler) allocate() {
	a := allocator{core: &c.core, walk: 1, fns: [][]int32{nil}, refers: -1}
	a.levels = make([]int32, 2*len(c.binders))
	a.levels, a.last = a.levels[:len(c.binders)], a.levels[len(c.binders):]
	for i := range a.levels {
		a.levels[i], a.last[i] = -1, -1
	}
	c.loc = make([]int32, len(c.ops))
	a.expr(c.root)
	a.walk, a.at = 2, 0
	a.expr(c.root)
	c.top = a.size
}

// tick advances the position to the next read or write.
func (a *allocator) tick() int32 {
	a.at++
	return a.at
}

// read notes a read of the binder b's slot in the first walk.
func (a *allocator) read(b int32) {
	if a.walk == 1 {
		a.last[b] = a.at
	}
}

// dead reports whether the slot s is free at the current position.
func (a *allocator) dead(s int) bool {
	if s >= len(a.slots) {
		return true
	}
	o := a.slots[s]
	return o == free || o >= 0 && a.last[o] < a.at
}

// claim marks the k slots from s as held by o, growing the frame.
func (a *allocator) claim(s, k int, o int32) {
	for len(a.slots) < s+k {
		a.slots = append(a.slots, free)
	}
	for i := s; i < s+k; i++ {
		a.slots[i] = o
	}
	a.size = max(a.size, len(a.slots))
}

// bind writes the binder b in the current frame: in the second walk, into
// the lowest dead slot.
func (a *allocator) bind(b int32) {
	a.tick()
	a.levels[b] = int32(a.level)
	if a.walk == 1 {
		return
	}
	s := 0
	for !a.dead(s) {
		s++
	}
	a.claim(s, 1, b)
	a.binders[b].slot = s
}

// reserve reserves k consecutive dead slots for arguments in the second
// walk, returning the first.
func (a *allocator) reserve(k int) int {
	a.tick()
	if a.walk == 1 {
		return 0
	}
	s := 0
	for {
		i := 0
		for i < k && a.dead(s+i) {
			i++
		}
		if i == k {
			break
		}
		s += i + 1
	}
	a.claim(s, k, reserved)
	return s
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
	if a.walk == 1 && b >= 0 && a.refers >= 0 && int(a.levels[b]) < a.refers {
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
		switch {
		case int(a.levels[d.a]) != a.level:
			if a.walk == 1 {
				a.loc[n] = -1 - a.capture(a.level, d.a)
			}
		case a.walk == 1:
			a.read(d.a)
		default:
			a.loc[n] = int32(a.binders[d.a].slot)
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
		a.read(d.b)
	case opMatch:
		v, callee, arms := a.match(n)
		a.refer(callee)
		a.expr(v)
		refers := a.refers
		if callee != notInlined {
			a.refers = -1
		}
		for i := 0; i < len(arms); i += 2 {
			a.arm(arms[i:i+2], 1, -1)
		}
		a.refers = refers
	case opMatchN:
		callee, args, arms := a.matchN(n)
		a.refer(callee)
		k := len(args)
		s := a.reserve(k)
		if a.walk == 2 {
			a.loc[n] = int32(s)
		}
		a.exprs(args)
		refers := a.refers
		a.refers = -1
		for ; len(arms) > 0; arms = arms[k+1:] {
			a.arm(arms[:k+1], k, s)
		}
		a.refers = refers
		a.release(s, k)
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
			if a.walk == 1 && a.levels[b] < 0 || a.walk == 2 && a.binders[b].slot < 0 {
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

// arm allocates an arm, its k patterns and then its body. If base is at
// least 0, the patterns match the arguments in the k slots from there,
// which are kept while they run.
func (a *allocator) arm(arm []int32, k, base int) {
	pats, body := arm[:k], cnode(arm[k])
	if a.walk == 2 && base >= 0 {
		a.claim(base, k, reserved)
	}
	mark := len(a.bound)
	for _, p := range pats {
		a.pattern(cnode(p), int32(base))
	}
	// A pattern's names are kept until all its patterns have matched, in
	// whichever order they write them.
	end := a.tick()
	if a.walk == 1 {
		for _, b := range a.bound[mark:] {
			a.last[b] = max(a.last[b], end)
		}
		a.bound = a.bound[:mark]
	} else if base >= 0 {
		// The body keeps only the arguments it names.
		for i, p := range pats {
			o := int32(free)
			if a.ops[p] == patArg {
				o = a.data[p].a
			}
			a.slots[base+i] = o
		}
	}
	a.expr(body)
}

// release frees what is left reserved of the k slots from s.
func (a *allocator) release(s, k int) {
	if a.walk == 2 {
		for i := s; i < s+k; i++ {
			if a.slots[i] == reserved {
				a.slots[i] = free
			}
		}
	}
}

// lambda allocates the lambda n's frame, and its captures, and sets the
// captures that its uses refer to.
func (a *allocator) lambda(n cnode) {
	r := a.range_(n)
	u, arity, arms := r[0], int(r[1]), r[2:]
	level, refers, slots, size := a.level, a.refers, a.slots, a.size
	a.level++
	a.refers = a.level
	if a.walk == 1 {
		// The list of the lambda before it at this level, whose captures
		// are copied to frames, is reused.
		if a.level < len(a.fns) {
			a.fns = a.fns[:a.level+1]
			a.fns[a.level] = a.fns[a.level][:0]
		} else {
			a.fns = append(a.fns, nil)
		}
	} else {
		a.lambdas = append(a.lambdas, a.loc[n])
		for len(a.spare) <= a.level {
			a.spare = append(a.spare, nil)
		}
		a.slots, a.size = a.spare[a.level][:0], 0
	}
	// A function of one argument matches it as it is; one of more keeps
	// them in its first slots, for its arms to match.
	base := -1
	if arity > 1 {
		base = 0
		if a.walk == 2 {
			a.claim(0, arity, reserved)
		}
	}
	for i := 0; i < len(arms); i += arity + 1 {
		a.arm(arms[i:i+arity+1], arity, base)
	}

	if a.walk == 1 {
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
	} else {
		i := a.loc[n]
		a.frames[i] = int32(a.size)
		a.spare[a.level] = a.slots
	}
	a.level, a.refers = level, refers
	if a.walk == 2 {
		a.slots, a.size = slots, size
		a.lambdas = a.lambdas[:len(a.lambdas)-1]
	}

	// It is made here, copying what it captures out of the slots of this
	// frame, or out of the captures of this frame's function.
	a.tick()
	i := a.loc[n]
	caps := a.frames[i+2 : i+2+a.frames[i+1]]
	for j, b := range caps {
		if int(a.levels[b]) == a.level {
			if a.walk == 1 {
				a.read(b)
			} else {
				caps[j] = int32(a.binders[b].slot)
			}
		} else if a.walk == 2 {
			caps[j] = -1 - a.outer(b)
		}
	}
}

// outer returns the index of b among the captures of the function of the
// current frame, in the second walk, whose captures are still binders.
func (a *allocator) outer(b int32) int32 {
	i := a.lambdas[len(a.lambdas)-1]
	for j, x := range a.frames[i+2 : i+2+a.frames[i+1]] {
		if x == b {
			return int32(j)
		}
	}
	panic("not captured")
}

// pattern allocates the binders of the pattern n, whose arguments, if it
// names one, are in the slots from base.
func (a *allocator) pattern(n cnode, base int32) {
	d := a.data[n]
	switch a.ops[n] {
	case patBind:
		a.bind(d.a)
		if a.walk == 1 {
			a.bound = append(a.bound, d.a)
		}
	case patArg:
		a.levels[d.a] = int32(a.level)
		if a.walk == 1 {
			a.bound = append(a.bound, d.a)
		} else {
			a.binders[d.a].slot = int(base + d.b)
		}
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
