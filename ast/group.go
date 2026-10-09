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

// Components orders a group's value bindings by their references to each
// other. It returns the strongly connected components of the reference graph,
// each after the components it refers to. Type-only bindings are left out.
//
// A reference is any identifier in a binding's value that names a binding of
// the group. Shadowing isn't taken into account, so a component may be larger
// than strictly needed.
func (se *SourceExpr) Components(binds []Node) [][]Node {
	index := make(map[string]int, len(binds))
	for i, b := range binds {
		if _, _, _, val := se.Where(b); val != None {
			index[se.TokenText(se.Mains[b])] = i
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
	}
	for i, b := range binds {
		if _, _, _, val := se.Where(b); val != None && t.order[i] == 0 {
			t.visit(i)
		}
	}
	return t.components
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
	components [][]Node
}

func (t *tarjan) visit(i int) {
	t.next++
	t.order[i], t.low[i] = t.next, t.next
	t.path = append(t.path, i)
	t.onPath[i] = true

	_, _, _, val := t.se.Where(t.binds[i])
	t.se.names(val, func(name string) {
		j, ok := t.index[name]
		if !ok {
			return
		}
		if t.order[j] == 0 {
			t.visit(j)
			t.low[i] = min(t.low[i], t.low[j])
		} else if t.onPath[j] {
			t.low[i] = min(t.low[i], t.order[j])
		}
	})

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
	t.components = append(t.components, comp)
}

// names calls visit with the name of every identifier in n's values.
func (se *SourceExpr) names(n Node, visit func(string)) {
	switch se.Kinds[n] {
	case Ident:
		visit(se.TokenText(se.Mains[n]))
	case Binary, Call, Func:
		d := se.Data[n]
		se.names(Node(d.Lhs), visit)
		se.names(Node(d.Rhs), visit)
	case MatchFunc:
		for _, arm := range se.Arms(n) {
			se.names(arm, visit)
		}
	case Variant, Access:
		se.names(Node(se.Data[n].Lhs), visit)
	case Record:
		rest, fields := se.Record(n)
		se.names(rest, visit)
		for i := range fields.Len() {
			se.names(fields.Value(i), visit)
		}
	case List:
		for _, x := range se.List(n) {
			se.names(x, visit)
		}
	case Where:
		body, _, _, val := se.Where(n)
		se.names(body, visit)
		se.names(val, visit)
	}
}
