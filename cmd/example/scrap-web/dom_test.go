package main

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"slices"
	"strings"
	"testing"
)

// A frame applies patches as a page does, to elements of its own.
type frame struct {
	nodes map[int]*element
}

type element struct {
	id        int
	tag, text string
	attrs     []attr
	events    []string
	parent    *element
	children  []*element
}

func newFrame() *frame {
	return &frame{nodes: map[int]*element{0: {tag: "body"}}}
}

// apply applies the patch, and fails the test if it is invalid.
func (f *frame) apply(t testing.TB, patch json.RawMessage) {
	t.Helper()
	var ops []any
	if err := json.Unmarshal(patch, &ops); err != nil {
		t.Fatalf("bad patch %s: %v", patch, err)
	}
	num := func() int {
		n := int(ops[0].(float64))
		ops = ops[1:]
		return n
	}
	str := func() string {
		s := ops[0].(string)
		ops = ops[1:]
		return s
	}
	get := func(id int) *element {
		e, ok := f.nodes[id]
		if !ok {
			t.Fatalf("no node %d in %s", id, patch)
		}
		return e
	}
	var build func() *element
	build = func() *element {
		e := &element{id: num()}
		if _, ok := f.nodes[e.id]; ok {
			t.Fatalf("node %d is already shown", e.id)
		}
		f.nodes[e.id] = e
		if ops[0] == nil {
			ops = ops[1:]
			e.text = str()
			return e
		}
		e.tag = str()
		for range num() {
			e.attrs = set(e.attrs, str(), str())
		}
		for range num() {
			e.events = append(e.events, str())
		}
		for range num() {
			c := build()
			c.parent = e
			e.children = append(e.children, c)
		}
		return e
	}
	// insert inserts c into p after the child after, or first.
	insert := func(p *element, after int, c *element) {
		if c.parent != nil {
			c.parent.children = slices.DeleteFunc(c.parent.children, func(x *element) bool { return x == c })
		}
		c.parent = p
		i := 0
		if after != 0 {
			i = slices.Index(p.children, get(after)) + 1
			if i == 0 {
				t.Fatalf("node %d is not a child of %d", after, p.id)
			}
		}
		p.children = slices.Insert(p.children, i, c)
	}
	for len(ops) > 0 {
		switch op := num(); op {
		case opClear:
			f.nodes = newFrame().nodes
		case opInsert:
			p, after := get(num()), num()
			insert(p, after, build())
		case opMove:
			p, after, c := get(num()), num(), num()
			insert(p, after, get(c))
		case opRemove:
			e := get(num())
			e.parent.children = slices.DeleteFunc(e.parent.children, func(x *element) bool { return x == e })
			f.forget(e)
		case opText:
			get(num()).text = str()
		case opSet:
			e := get(num())
			e.attrs = set(e.attrs, str(), str())
		case opUnset:
			e, name := get(num()), str()
			e.attrs = slices.DeleteFunc(e.attrs, func(a attr) bool { return a.name == name })
		case opOn:
			e := get(num())
			e.events = append(e.events, str())
		case opOff:
			e, event := get(num()), str()
			e.events = slices.DeleteFunc(e.events, func(x string) bool { return x == event })
		default:
			t.Fatalf("bad op %d", op)
		}
	}
}

// resetView forgets the view, as a new module has none.
func resetView() {
	shown, spare, nextID = view{}, view{}, 1
}

// set sets the attribute name of attrs, and returns them.
func set(attrs []attr, name, value string) []attr {
	if i := slices.IndexFunc(attrs, named(name)); i >= 0 {
		attrs[i].value = value
		return attrs
	}
	return append(attrs, attr{name, value})
}

func (f *frame) forget(e *element) {
	delete(f.nodes, e.id)
	for _, c := range e.children {
		f.forget(c)
	}
}

// html returns the body's children as HTML, with the events that elements
// listen for as attributes "on:event", and their attributes in order.
func (f *frame) html() string {
	var b strings.Builder
	var write func(e *element)
	write = func(e *element) {
		if e.tag == "" {
			b.WriteString(e.text)
			return
		}
		b.WriteString("<" + e.tag)
		attrs := slices.Clone(e.attrs)
		slices.SortFunc(attrs, func(a, b attr) int { return strings.Compare(a.name, b.name) })
		for _, a := range attrs {
			fmt.Fprintf(&b, " %s=%q", a.name, a.value)
		}
		events := slices.Clone(e.events)
		slices.Sort(events)
		for _, e := range events {
			b.WriteString(" on:" + e)
		}
		b.WriteString(">")
		for _, c := range e.children {
			write(c)
		}
		b.WriteString("</" + e.tag + ">")
	}
	for _, c := range f.nodes[0].children {
		write(c)
	}
	return b.String()
}

// find returns the id of the n-th element with the tag, in order.
func (f *frame) find(t testing.TB, tag string, n int) int {
	t.Helper()
	var found []int
	var walk func(e *element)
	walk = func(e *element) {
		if e.tag == tag {
			found = append(found, e.id)
		}
		for _, c := range e.children {
			walk(c)
		}
	}
	walk(f.nodes[0])
	if n >= len(found) {
		t.Fatalf("no %s %d in %s", tag, n, f.html())
	}
	return found[n]
}

// htmlOf returns h as html returns it.
func htmlOf(h Html) string {
	f := newFrame()
	f.nodes[0].children = []*element{elementOf(h)}
	return f.html()
}

func elementOf(h Html) *element {
	if h.Tag() == HtmlText {
		return &element{text: h.Text()}
	}
	el := h.El()
	e := &element{tag: el.Tag}
	for _, a := range el.Attrs {
		event := ""
		switch a.Tag() {
		case AttrAttr:
			if a.Attr().Name != "key" {
				e.attrs = set(e.attrs, a.Attr().Name, a.Attr().Value)
			}
			continue
		case AttrOn:
			event = a.On().Event
		case AttrOnValue:
			event = a.OnValue().Event
		}
		if !slices.Contains(e.events, event) {
			e.events = append(e.events, event)
		}
	}
	for _, c := range el.Children {
		e.children = append(e.children, elementOf(c))
	}
	return e
}

// randomView returns a random view of elements, keyed or not, and text.
func randomView(r *rand.Rand, depth int) Html {
	if depth == 0 || r.Intn(4) == 0 {
		return NewHtmlText(fmt.Sprint("t", r.Intn(3)))
	}
	var attrs []Attr
	if r.Intn(2) == 0 {
		attrs = append(attrs, NewAttrAttr(AttrAttrPayload{"class", fmt.Sprint("c", r.Intn(3))}))
	}
	if r.Intn(2) == 0 {
		attrs = append(attrs, NewAttrOn(AttrOnPayload{Event: []string{"click", "input"}[r.Intn(2)]}))
	}
	if r.Intn(3) > 0 {
		attrs = append(attrs, NewAttrAttr(AttrAttrPayload{"key", fmt.Sprint(r.Intn(1000))}))
	}
	var children []Html
	for range r.Intn(6) {
		children = append(children, randomView(r, depth-1))
	}
	return NewHtmlEl(HtmlElPayload{Tag: []string{"div", "p", "li"}[r.Intn(3)], Attrs: attrs, Children: children})
}

// mutate returns h with a few random changes: of its text and attributes,
// and of its children, which it swaps, removes, adds, and mutates in turn.
func mutate(r *rand.Rand, h Html, depth int) Html {
	if r.Intn(10) == 0 {
		return randomView(r, depth)
	}
	if h.Tag() == HtmlText {
		return NewHtmlText(fmt.Sprint("t", r.Intn(3)))
	}
	el := h.El()
	attrs := slices.Clone(el.Attrs)
	if r.Intn(3) == 0 {
		attrs = append(attrs, NewAttrAttr(AttrAttrPayload{"class", fmt.Sprint("c", r.Intn(3))}))
	}
	if r.Intn(4) == 0 && len(attrs) > 0 {
		attrs = slices.Delete(attrs, 0, 1)
	}
	children := slices.Clone(el.Children)
	for range r.Intn(3) {
		switch n := len(children); {
		case n > 1 && r.Intn(3) == 0:
			i, j := r.Intn(n), r.Intn(n)
			children[i], children[j] = children[j], children[i]
		case n > 0 && r.Intn(2) == 0:
			i := r.Intn(n)
			children = slices.Delete(children, i, i+1)
		default:
			children = slices.Insert(children, r.Intn(n+1), randomView(r, max(depth-1, 0)))
		}
	}
	for i, c := range children {
		if r.Intn(2) == 0 {
			children[i] = mutate(r, c, max(depth-1, 0))
		}
	}
	return NewHtmlEl(HtmlElPayload{Tag: el.Tag, Attrs: attrs, Children: children})
}

// Each patch makes a frame show the view, as a new frame shows it.
func TestPatch(t *testing.T) {
	r := rand.New(rand.NewSource(1))
	resetView()
	f := newFrame()
	h := randomView(r, 4)
	for i := range 5000 {
		h = mutate(r, h, 4)
		patch, _ := patch(h)
		f.apply(t, patch)
		if got, want := f.html(), htmlOf(h); got != want {
			t.Fatalf("view %d: expected\n%s\ngot\n%s\nafter %s", i, want, got, patch)
		}
		fresh := newFrame()
		fresh.apply(t, reshow())
		if got, want := fresh.html(), f.html(); got != want {
			t.Fatalf("view %d: shown anew as\n%s\nrather than\n%s", i, got, want)
		}
	}
}

// A keyed element moves, keeping its id, and the elements around it stay.
func TestPatchKeyed(t *testing.T) {
	li := func(key string) Html {
		return NewHtmlEl(HtmlElPayload{Tag: "li", Attrs: []Attr{NewAttrAttr(AttrAttrPayload{"key", key})}, Children: []Html{NewHtmlText(key)}})
	}
	ul := func(keys ...string) Html {
		var lis []Html
		for _, k := range keys {
			lis = append(lis, li(k))
		}
		return NewHtmlEl(HtmlElPayload{Tag: "ul", Children: lis})
	}
	resetView()
	f := newFrame()
	examples := []struct {
		keys  []string
		patch string
	}{
		{[]string{"a", "b", "c", "d"}, `[0,1,0,0,1,"ul",0,0,4,2,"li",0,0,1,3,null,"a",4,"li",0,0,1,5,null,"b",6,"li",0,0,1,7,null,"c",8,"li",0,0,1,9,null,"d"]`},
		// The last moves first, and the others stay.
		{[]string{"d", "a", "b", "c"}, `[2,1,0,8]`},
		// One is removed, and one added in its place.
		{[]string{"d", "a", "e", "c"}, `[3,4,1,1,2,10,"li",0,0,1,11,null,"e"]`},
		// Two swap: the last moves first, and the first after the third.
		{[]string{"c", "a", "e", "d"}, `[2,1,0,6,2,1,10,8]`},
		{nil, `[3,6,3,2,3,10,3,8]`},
	}
	for _, ex := range examples {
		patch, _ := patch(ul(ex.keys...))
		if string(patch) != ex.patch {
			t.Errorf("%v: expected %s, got %s", ex.keys, ex.patch, patch)
		}
		f.apply(t, patch)
		if got, want := f.html(), htmlOf(ul(ex.keys...)); got != want {
			t.Errorf("%v: expected %s, got %s", ex.keys, want, got)
		}
	}
}

// appendString writes what encoding/json reads back as the same string.
func TestAppendString(t *testing.T) {
	for _, s := range []string{"", "plain", `"quoted" \ back`, "\n\r\t\x00\x1f", "ünïcødé 😀", "bad \xff utf-8", "\u2028"} {
		var got, want string
		std, _ := json.Marshal(s)
		if err := json.Unmarshal(std, &want); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(appendString(nil, s), &got); err != nil || got != want {
			t.Errorf("%q: read back as %q, %v", s, got, err)
		}
	}
}
