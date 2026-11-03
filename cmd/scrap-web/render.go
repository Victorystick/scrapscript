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
// matched by their position; #keyed children too, for now.
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
// into those of n.
func (r *renderer) diffChildren(id uint32, i int, old, n *element) {
	if eval.Same(old.children, n.children) {
		r.keep(i+1, i+int(r.sizes[i]))
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
