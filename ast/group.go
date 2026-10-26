package ast

import "slices"

// Group returns the body of a where-group and its Where nodes in source
// order. A where-group is a maximal chain of Where nodes; its bindings are
// mutually recursive. A node that isn't a Where is a group without bindings.
func (se *SourceExpr) Group(n Node) (body Node, binds []Node) {
	for se.Kinds[n] == Where {
		binds = append(binds, n)
		n = Node(se.Data[n].Lhs)
	}
	slices.Reverse(binds)
	return n, binds
}

// A Component is a strongly connected component of a group's bindings.
type Component struct {
	Binds []Node
	// Whether it refers to itself: it has more than one binding, or its
	// binding refers to its own name.
	Recursive bool
}

// Components orders a group's value bindings by their references to each
// other. It returns the strongly connected components of the reference graph,
// each after the components it refers to. Sort bindings are left out.
//
// A reference is a free identifier in a binding's value that names a binding
// of the group, unless cut returns true for that binding; cut may be nil.
func (se *SourceExpr) Components(binds []Node, cut func(Node) bool) []Component {
	index := make(map[string]int, len(binds))
	for i, b := range binds {
		if !se.Sort(b) && (cut == nil || !cut(b)) {
			index[se.Name(se.Mains[b])] = i
		}
	}

	// Tarjan's algorithm, which emits a component once everything it refers
	// to has been emitted.
	t := tarjan{
		se:     se,
		binds:  binds,
		index:  index,
		order:  make([]int, len(binds)),
		low:    make([]int, len(binds)),
		onPath: make([]bool, len(binds)),
		self:   make([]bool, len(binds)),
		free:   freeNames{se: se, bound: make(map[string]int)},
	}
	t.free.visit = t.ref
	for i, b := range binds {
		if !se.Sort(b) && t.order[i] == 0 {
			t.visit(i)
		}
	}
	return t.components
}

// References counts the references to each of a group's bindings, by its
// index in binds: the free identifiers that name it in the group's body
// and in the bindings' values.
func (se *SourceExpr) References(body Node, binds []Node) []int {
	index := make(map[string]int, len(binds))
	for i, b := range binds {
		if !se.Sort(b) {
			index[se.Name(se.Mains[b])] = i
		}
	}
	counts := make([]int, len(binds))
	f := freeNames{se: se, bound: make(map[string]int), visit: func(name string) {
		if i, ok := index[name]; ok {
			counts[i]++
		}
	}}
	for _, b := range binds {
		if _, _, _, val := se.Where(b); !se.Sort(b) {
			f.expr(val)
		}
	}
	f.expr(body)
	return counts
}

type tarjan struct {
	se         *SourceExpr
	binds      []Node
	index      map[string]int
	next       int   // the next visit order, starting at 1
	order      []int // the visit order of each binding, or 0 if unvisited
	low        []int
	onPath     []bool
	path       []int
	self       []bool // whether each binding refers to itself
	free       freeNames
	refs       []int // the references of the bindings being visited
	components []Component
}

// ref adds a reference to the name, if it is a binding's.
func (t *tarjan) ref(name string) {
	if j, ok := t.index[name]; ok {
		t.refs = append(t.refs, j)
	}
}

func (t *tarjan) visit(i int) {
	t.next++
	t.order[i], t.low[i] = t.next, t.next
	t.path = append(t.path, i)
	t.onPath[i] = true

	_, _, _, val := t.se.Where(t.binds[i])
	start := len(t.refs)
	t.free.expr(val)
	end := len(t.refs)
	for k := start; k < end; k++ {
		j := t.refs[k]
		if j == i {
			t.self[i] = true
		}
		if t.order[j] == 0 {
			t.visit(j)
			t.low[i] = min(t.low[i], t.low[j])
		} else if t.onPath[j] {
			t.low[i] = min(t.low[i], t.order[j])
		}
	}
	t.refs = t.refs[:start]

	if t.low[i] != t.order[i] {
		return
	}
	var comp []Node
	for {
		j := t.path[len(t.path)-1]
		t.path = t.path[:len(t.path)-1]
		t.onPath[j] = false
		comp = append(comp, t.binds[j])
		if j == i {
			break
		}
	}
	slices.Reverse(comp)
	t.components = append(t.components, Component{comp, len(comp) > 1 || t.self[i]})
}

// freeNames walks a value, calling visit with each name that it refers to
// but doesn't bind itself.
type freeNames struct {
	se *SourceExpr
	// The names bound within the node being walked, by how many binders,
	// and the binders' names, innermost last.
	bound map[string]int
	names []string
	visit func(string)
}

// bind binds the names from start onwards, returning start.
func (f *freeNames) bind(start int) int {
	for _, name := range f.names[start:] {
		f.bound[name]++
	}
	return start
}

// unbind unbinds the names from start onwards.
func (f *freeNames) unbind(start int) {
	for _, name := range f.names[start:] {
		f.bound[name]--
	}
	f.names = f.names[:start]
}

func (f *freeNames) expr(n Node) {
	se := f.se
	switch se.Kinds[n] {
	case Ident:
		if name := se.Name(se.Mains[n]); f.bound[name] == 0 {
			f.visit(name)
		}
	case Binary, Call:
		d := se.Data[n]
		f.expr(Node(d.Lhs))
		f.expr(Node(d.Rhs))
	case Func:
		// A function's parameter binds its name.
		arg, body := se.Func(n)
		start := len(f.names)
		f.names = se.patternNames(arg, f.names)
		f.bind(start)
		f.expr(body)
		f.unbind(start)
	case Arm:
		// A match arm's patterns bind their names.
		patterns, body := se.Arm(n)
		start := len(f.names)
		for _, pat := range patterns {
			f.names = se.patternNames(pat, f.names)
		}
		f.bind(start)
		f.expr(body)
		f.unbind(start)
	case MatchFunc:
		for _, arm := range se.Arms(n) {
			f.expr(arm)
		}
	case Variant, Access:
		f.expr(Node(se.Data[n].Lhs))
	case Record:
		rest, fields := se.Record(n)
		f.expr(rest)
		for i := range fields.Len() {
			f.expr(fields.Value(i))
		}
	case List:
		for _, x := range se.List(n) {
			f.expr(x)
		}
	case Where:
		body, binds := se.Group(n)
		start := len(f.names)
		for _, b := range binds {
			if !se.Sort(b) {
				f.names = append(f.names, se.Name(se.Mains[b]))
			}
		}
		f.bind(start)
		for _, b := range binds {
			if _, _, _, val := se.Where(b); !se.Sort(b) {
				f.expr(val)
			}
		}
		f.expr(body)
		f.unbind(start)
	}
}

// patternNames appends the names that the pattern n binds to names.
func (se *SourceExpr) patternNames(n Node, names []string) []string {
	switch se.Kinds[n] {
	case Ident:
		if name := se.TokenText(se.Mains[n]); name != "_" {
			names = append(names, name)
		}
	case Variant:
		if _, payload := se.Variant(n); payload != None {
			names = se.patternNames(payload, names)
		}
	case Record:
		rest, fields := se.Record(n)
		if rest != None {
			names = se.patternNames(rest, names)
		}
		for i := range fields.Len() {
			names = se.patternNames(fields.Value(i), names)
		}
	case List:
		for _, x := range se.List(n) {
			names = se.patternNames(x, names)
		}
	case Binary:
		d := se.Data[n]
		names = se.patternNames(Node(d.Lhs), names)
		names = se.patternNames(Node(d.Rhs), names)
	}
	return names
}
