package main

import (
	"github.com/Victorystick/scrapscript/eval"
)

// A renderer turns views into patches to the page. It keeps the last view
// as it is, since values are immutable, with flat tables beside it: for
// each node of the view, in preorder, its DOM id and the size of its
// subtree; and for each DOM id, the attributes of its element, which hold
// its handlers.
//
// It diffs a new view against the last one in preorder too, recursing as
// deep as views nest. A node that is the same value as the one in its
// place before, as when a view reuses a part of the model or a constant,
// is skipped without looking inside: its ids are copied. Children are
// matched by their position, or by their keys where the element is #keyed
// in both views (diffKeyed).
//
// It reads views as values, through the bindings' labels and shapes,
// rather than through Refs, which allocate as they read lists.
type renderer struct {
	l        *Bindings
	view     eval.Value // the last view, once rendered
	rendered bool

	ids   []uint32 // by node of view, in preorder: its DOM id
	sizes []uint32 // by node of view, in preorder: its subtree's size

	// The tables of the view being rendered, which replace ids and sizes.
	nids, nsizes []uint32

	attrs []eval.Value // by DOM id: its element's attrs, or the zero Value
	free  []uint32     // DOM ids not in use, besides those past attrs

	// Scratch for diffing attributes: an element's before and after.
	olds, news []entry

	// Scratch for diffing keyed children: a stack of each keyed element's
	// tables, which nested elements push theirs onto, and a map of keys,
	// used only between them.
	stack []int32
	keys  map[string]int32

	p patch
}

// render returns the patch from the last view to view, which is valid
// until the next render. The first view is appended to the mount element.
func (r *renderer) render(view NodeRef) []byte {
	r.l = view.b
	r.p.reset()
	r.nids, r.nsizes = r.nids[:0], r.nsizes[:0]
	if r.rendered {
		r.diff(r.view, 0, view.v)
	} else {
		if len(r.attrs) == 0 {
			r.attrs = append(r.attrs, eval.Value{}) // the mount element
		}
		r.p.op(opAppend, 0, r.create(view.v))
	}
	r.view, r.rendered = view.v, true
	r.ids, r.nids = r.nids, r.ids
	r.sizes, r.nsizes = r.nsizes, r.sizes
	return r.p.encode()
}

// alloc returns a DOM id that isn't in use.
func (r *renderer) alloc() uint32 {
	if n := len(r.free); n > 0 {
		id := r.free[n-1]
		r.free = r.free[:n-1]
		return id
	}
	r.attrs = append(r.attrs, eval.Value{})
	return uint32(len(r.attrs) - 1)
}

// release frees the ids of the last view's subtree at i.
func (r *renderer) release(i int) {
	for _, id := range r.ids[i : i+int(r.sizes[i])] {
		r.attrs[id] = eval.Value{}
		r.free = append(r.free, id)
	}
}

// keep carries the nodes of the last view from i to end over to the new
// view: a subtree, or the children of an element.
func (r *renderer) keep(i, end int) {
	r.nids = append(r.nids, r.ids[i:end]...)
	r.nsizes = append(r.nsizes, r.sizes[i:end]...)
}

// An element is an #el or a #keyed node, whose children it reads alike.
type element struct {
	tag      string
	attrs    eval.Value
	children eval.Value
	kids     []eval.Value // the children's elements
	keyed    bool
}

// elementOf returns the element of the node n, if it isn't a text node.
func (r *renderer) elementOf(n eval.Value) (element, bool) {
	l := r.l
	t := n.Variant()
	shape := l.elementShape
	switch t.Tag() {
	case l.text:
		return element{}, false
	case l.keyed:
		shape = l.keyedShape
	}
	p := t.Payload()
	e := element{
		tag:      shape.Field(p, 2).Text(),
		attrs:    shape.Field(p, 0),
		children: shape.Field(p, 1),
		keyed:    shape == l.keyedShape,
	}
	e.kids = e.children.List().Elements()
	return e, true
}

// child returns the node of the element's kth child.
func (r *renderer) child(e *element, k int) eval.Value {
	if e.keyed {
		return r.l.childShape.Field(e.kids[k], 1)
	}
	return e.kids[k]
}

// create adds the nodes of n to the new view's tables, and returns the id
// of its root, which isn't in the page yet.
func (r *renderer) create(n eval.Value) uint32 {
	id := r.alloc()
	at := len(r.nids)
	r.nids = append(r.nids, id)
	r.nsizes = append(r.nsizes, 0)
	if e, ok := r.elementOf(n); ok {
		r.p.op(opCreateElement, id, r.p.str(e.tag))
		r.diffAttrs(id, eval.Value{}, e.attrs)
		for k := range e.kids {
			r.p.op(opAppend, id, r.create(r.child(&e, k)))
		}
	} else {
		r.p.op(opCreateText, id, r.p.str(n.Variant().Payload().Text()))
	}
	r.nsizes[at] = uint32(len(r.nids) - at)
	return id
}

// diff patches the last view's node at i, old, into n.
func (r *renderer) diff(old eval.Value, i int, n eval.Value) {
	if eval.Same(old, n) {
		r.keep(i, i+int(r.sizes[i]))
		return
	}
	id := r.ids[i]
	oe, oel := r.elementOf(old)
	ne, nel := r.elementOf(n)
	switch {
	case !oel && !nel:
		if t := n.Variant().Payload().Text(); t != old.Variant().Payload().Text() {
			r.p.op(opSetText, id, r.p.str(t))
		}
		r.nids = append(r.nids, id)
		r.nsizes = append(r.nsizes, 1)
		return
	case oel && nel && oe.tag == ne.tag:
		at := len(r.nids)
		r.nids = append(r.nids, id)
		r.nsizes = append(r.nsizes, 0)
		r.diffAttrs(id, oe.attrs, ne.attrs)
		r.diffChildren(id, i, &oe, &ne)
		r.nsizes[at] = uint32(len(r.nids) - at)
		return
	}
	r.p.op(opReplace, id, r.create(n))
	r.release(i)
}

// diffChildren patches the children of the last view's element at i, old,
// into those of n: by key if both are keyed, and otherwise by position.
func (r *renderer) diffChildren(id uint32, i int, old, n *element) {
	if eval.Same(old.children, n.children) {
		r.keep(i+1, i+int(r.sizes[i]))
		return
	}
	if old.keyed && n.keyed {
		r.diffKeyed(id, i, old, n)
		return
	}
	c := i + 1 // the last view's node of the next old child
	common := min(len(old.kids), len(n.kids))
	for k := range common {
		next := c + int(r.sizes[c])
		r.diff(r.child(old, k), c, r.child(n, k))
		c = next
	}
	for range len(old.kids) - common {
		next := c + int(r.sizes[c])
		r.p.op(opRemove, r.ids[c])
		r.release(c)
		c = next
	}
	for k := common; k < len(n.kids); k++ {
		r.p.op(opAppend, id, r.create(r.child(n, k)))
	}
}

// key returns the key of a keyed element's child.
func (r *renderer) key(kid eval.Value) string {
	return r.l.childShape.Field(kid, 0).Text()
}

// diffKeyed patches the keyed children of the last view's element at i,
// old, into those of n. It diffs children with the same key, which keep
// their DOM nodes, and removes or creates the others. Children with a
// repeated key match in order.
//
// The common prefix and suffix of keys are diffed in place. Of the matched
// children between, those in a longest increasing run of their old places
// stay, and the others move: going backwards, each is inserted before the
// child after it, which is in its place by then, as is any new child.
func (r *renderer) diffKeyed(id uint32, i int, old, n *element) {
	olds, news := old.kids, n.kids
	c := i + 1 // the last view's node of the next old child

	// The prefix, in place.
	pre := 0
	for ; pre < len(olds) && pre < len(news) && r.key(olds[pre]) == r.key(news[pre]); pre++ {
		next := c + int(r.sizes[c])
		r.diff(r.child(old, pre), c, r.child(n, pre))
		c = next
	}
	suf := 0
	for suf < len(olds)-pre && suf < len(news)-pre && r.key(olds[len(olds)-1-suf]) == r.key(news[len(news)-1-suf]) {
		suf++
	}
	no, nn := len(olds)-pre-suf, len(news)-pre-suf

	// The tables of the middle, on the stack: by old child, its node in
	// the last view, the new child it matches or -1, and the next old
	// child with its key or -1; by new child, the old child it matches or
	// -1, its DOM id, whether it stays, and for the run, the index of its
	// predecessor and the run's tails.
	base := len(r.stack)
	r.stack = append(r.stack, make([]int32, 3*no+5*nn)...)
	start, newOf, same := base, base+no, base+2*no
	match, dom, stay, prev, tails := base+3*no, base+3*no+nn, base+3*no+2*nn, base+3*no+3*nn, base+3*no+4*nn
	st := r.stack
	for k := range no {
		st[start+k] = int32(c)
		st[newOf+k] = -1
		c += int(r.sizes[c])
	}
	suffix := c // the last view's node of the first old child of the suffix

	// Match keys, through the map, which no nested call uses meanwhile.
	if r.keys == nil {
		r.keys = map[string]int32{}
	}
	clear(r.keys)
	for k := no - 1; k >= 0; k-- {
		key := r.key(olds[pre+k])
		st[same+k] = -1
		if first, ok := r.keys[key]; ok {
			st[same+k] = first
		}
		r.keys[key] = int32(k)
	}
	for j := range nn {
		st[match+j] = -1
		key := r.key(news[pre+j])
		if k, ok := r.keys[key]; ok {
			st[match+j], st[newOf+int(k)] = k, int32(j)
			if s := st[same+int(k)]; s >= 0 {
				r.keys[key] = s
			} else {
				delete(r.keys, key)
			}
		}
	}

	// The matched children that stay: a longest run of increasing old
	// places, by patience sorting.
	run := 0
	for j := range nn {
		k := st[match+j]
		if k < 0 {
			continue
		}
		lo, hi := 0, run
		for lo < hi {
			mid := (lo + hi) / 2
			if st[match+int(st[tails+mid])] < k {
				lo = mid + 1
			} else {
				hi = mid
			}
		}
		st[prev+j] = -1
		if lo > 0 {
			st[prev+j] = st[tails+lo-1]
		}
		st[tails+lo] = int32(j)
		if lo == run {
			run++
		}
	}
	if run > 0 {
		for j := st[tails+run-1]; j >= 0; j = st[prev+int(j)] {
			st[stay+int(j)] = 1
		}
	}

	// Remove the old children that match none.
	for k := range no {
		if st[newOf+k] < 0 {
			at := int(st[start+k])
			r.p.op(opRemove, r.ids[at])
			r.release(at)
		}
	}

	// Diff the matched children and create the others, in the new order.
	// Nested calls may grow the stack, so read it afresh.
	for j := range nn {
		at := len(r.nids)
		if k := r.stack[match+j]; k >= 0 {
			r.diff(r.child(old, pre+int(k)), int(r.stack[start+int(k)]), r.child(n, pre+j))
			r.stack[dom+j] = int32(r.nids[at])
		} else {
			r.stack[dom+j] = int32(r.create(r.child(n, pre+j)))
		}
	}

	// The suffix, in place.
	anchor := uint32(0) // the DOM id of the first child of the suffix, or 0 for the end
	c = suffix
	for k := range suf {
		at := len(r.nids)
		next := c + int(r.sizes[c])
		r.diff(r.child(old, len(olds)-suf+k), c, r.child(n, len(news)-suf+k))
		if k == 0 {
			anchor = r.nids[at]
		}
		c = next
	}

	// Going backwards, put the children that don't stay in place.
	next := anchor
	for j := nn - 1; j >= 0; j-- {
		d := uint32(r.stack[dom+j])
		if r.stack[stay+j] == 0 {
			r.p.op(opInsert, id, d, next)
		}
		next = d
	}
	r.stack = r.stack[:base]
}

// An entry is an attribute, a property, a style or a handler, as diffAttrs
// compares them: by kind and name, which for a handler is its event.
type entry struct {
	kind  uint8
	name  string
	value eval.Value // a text, a json, or a handler
}

// The kinds of entries, and the ops that remove each.
const (
	kindAttr uint8 = iota
	kindStyle
	kindProp
	kindEvent
)

var removeOps = [...]uint32{opRemoveAttr, opRemoveStyle, opRemoveProp, opUnlisten}

// entries appends the entries of attrs, a list of attributes, to es.
func (r *renderer) entries(es []entry, attrs eval.Value) []entry {
	if !attrs.IsValid() {
		return es
	}
	l := r.l
	for _, a := range attrs.List().Elements() {
		t := a.Variant()
		p := t.Payload()
		switch t.Tag() {
		case l.attr:
			es = append(es, entry{kindAttr, l.attrAttrPayloadShape.Field(p, 0).Text(), l.attrAttrPayloadShape.Field(p, 1)})
		case l.style:
			es = append(es, entry{kindStyle, l.attrStylePayloadShape.Field(p, 0).Text(), l.attrStylePayloadShape.Field(p, 1)})
		case l.prop:
			es = append(es, entry{kindProp, l.attrPropPayloadShape.Field(p, 0).Text(), l.attrPropPayloadShape.Field(p, 1)})
		case l.onClick:
			es = append(es, entry{kindEvent, "click", a})
		case l.onInput:
			es = append(es, entry{kindEvent, "input", a})
		case l.onCheck:
			es = append(es, entry{kindEvent, "change", a})
		case l.onSubmit:
			es = append(es, entry{kindEvent, "submit", a})
		case l.on:
			es = append(es, entry{kindEvent, l.attrOnPayloadShape.Field(p, 0).Text(), a})
		}
	}
	return es
}

// counts returns the entry of es with e's kind and name that counts, the
// last, and whether there is one.
func counts(es []entry, e entry) (entry, bool) {
	for j := len(es) - 1; j >= 0; j-- {
		if es[j].kind == e.kind && es[j].name == e.name {
			return es[j], true
		}
	}
	return entry{}, false
}

// overridden reports whether an entry after k in es has its kind and name,
// and counts instead.
func overridden(es []entry, k int) bool {
	for _, e := range es[k+1:] {
		if e.kind == es[k].kind && e.name == es[k].name {
			return true
		}
	}
	return false
}

// diffAttrs patches the attributes, properties, styles and listeners of
// element id from old to n, and keeps n for its handlers. Of the entries
// with one kind and name, the last counts. Attribute lists are short, so
// it compares them pairwise.
func (r *renderer) diffAttrs(id uint32, old, n eval.Value) {
	r.attrs[id] = n
	if eval.Same(old, n) {
		return
	}
	r.olds = r.entries(r.olds[:0], old)
	r.news = r.entries(r.news[:0], n)
	for k, e := range r.news {
		if overridden(r.news, k) {
			continue
		}
		o, found := counts(r.olds, e)
		if !found || e.kind != kindEvent && !r.equal(e, o) {
			switch e.kind {
			case kindAttr:
				r.p.op(opSetAttr, id, r.p.str(e.name), r.p.str(e.value.Text()))
			case kindStyle:
				r.p.op(opSetStyle, id, r.p.str(e.name), r.p.str(e.value.Text()))
			case kindProp:
				r.p.op(opSetProp, id, r.p.str(e.name), r.p.json(JsonRef{ref{r.l, e.value}}))
			case kindEvent:
				r.p.op(opListen, id, r.p.str(e.name))
			}
		}
	}
	for k, o := range r.olds {
		if overridden(r.olds, k) {
			continue
		}
		if _, found := counts(r.news, o); !found {
			r.p.op(removeOps[o.kind], id, r.p.str(o.name))
		}
	}
}

// equal reports whether the entries' values are equal.
func (r *renderer) equal(a, b entry) bool {
	if eval.Same(a.value, b.value) {
		return true
	}
	if a.kind == kindProp {
		return jsonEqual(JsonRef{ref{r.l, a.value}}, JsonRef{ref{r.l, b.value}})
	}
	return a.value.Text() == b.value.Text()
}

// handler returns the handler of event e on node id, as last rendered.
func (r *renderer) handler(id uint32, e string) (AttrRef, bool) {
	if int(id) >= len(r.attrs) || !r.attrs[id].IsValid() {
		return AttrRef{}, false
	}
	r.news = r.entries(r.news[:0], r.attrs[id])
	h, ok := counts(r.news, entry{kind: kindEvent, name: e})
	return AttrRef{ref{r.l, h.value}}, ok
}

// jsonEqual reports whether a and b are the same JSON.
func jsonEqual(a, b JsonRef) bool {
	if eval.Same(a.v, b.v) {
		return true
	}
	t := a.Tag()
	if t != b.Tag() {
		return false
	}
	switch t {
	case JsonNull:
		return true
	case JsonBool:
		return a.Bool().Tag() == b.Bool().Tag()
	case JsonNumber:
		return a.Number() == b.Number()
	case JsonText:
		return a.Text() == b.Text()
	case JsonList:
		as, bs := a.List(), b.List()
		if as.Len() != bs.Len() {
			return false
		}
		for i := range as.Len() {
			if !jsonEqual(as.At(i), bs.At(i)) {
				return false
			}
		}
		return true
	case JsonObject:
		as, bs := a.Object(), b.Object()
		if as.Len() != bs.Len() {
			return false
		}
		for i := range as.Len() {
			if as.At(i).Key() != bs.At(i).Key() || !jsonEqual(as.At(i).Value(), bs.At(i).Value()) {
				return false
			}
		}
		return true
	}
	panic("jsonEqual: unknown tag")
}
