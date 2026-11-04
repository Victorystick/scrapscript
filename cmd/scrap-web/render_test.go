package main

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/Victorystick/scrapscript/eval"
)

var opNames = [...]string{
	opCreateElement: "CREATE_ELEMENT", opCreateText: "CREATE_TEXT", opSetText: "SET_TEXT",
	opSetAttr: "SET_ATTR", opRemoveAttr: "REMOVE_ATTR", opSetProp: "SET_PROP",
	opRemoveProp: "REMOVE_PROP", opSetStyle: "SET_STYLE", opRemoveStyle: "REMOVE_STYLE",
	opListen: "LISTEN", opUnlisten: "UNLISTEN", opAppend: "APPEND", opReplace: "REPLACE",
	opRemove: "REMOVE",
}

// The operands of each op: i for an id, s for a string.
var opArgs = [...]string{
	opCreateElement: "is", opCreateText: "is", opSetText: "is",
	opSetAttr: "iss", opRemoveAttr: "is", opSetProp: "iss",
	opRemoveProp: "is", opSetStyle: "iss", opRemoveStyle: "is",
	opListen: "is", opUnlisten: "is", opAppend: "ii", opReplace: "ii",
	opRemove: "i",
}

// An op is a decoded op, with its strings looked up.
type op struct {
	code uint32
	ids  []uint32
	strs []string
}

func (o op) String() string {
	var b strings.Builder
	b.WriteString(opNames[o.code])
	ids, strs := o.ids, o.strs
	for _, k := range opArgs[o.code] {
		if k == 'i' {
			fmt.Fprintf(&b, " %d", ids[0])
			ids = ids[1:]
		} else {
			fmt.Fprintf(&b, " %s", strs[0])
			strs = strs[1:]
		}
	}
	return b.String()
}

// decode reads a patch as the page would.
func decode(t *testing.T, b []byte) []op {
	t.Helper()
	word := func() uint32 {
		if len(b) < 4 {
			t.Fatal("patch ends in a word")
		}
		w := binary.LittleEndian.Uint32(b)
		b = b[4:]
		return w
	}
	words := make([]uint32, word())
	strs := make([]string, word())
	for i := range words {
		words[i] = word()
	}
	lens := make([]uint32, len(strs))
	for i := range lens {
		lens[i] = word()
	}
	for i, n := range lens {
		strs[i], b = string(b[:n]), b[n:]
	}
	if len(b) != 0 {
		t.Fatalf("%d bytes after the strings", len(b))
	}
	var ops []op
	for len(words) > 0 {
		o := op{code: words[0]}
		if o.code == 0 || int(o.code) >= len(opArgs) {
			t.Fatalf("unknown op %d", o.code)
		}
		words = words[1:]
		for _, k := range opArgs[o.code] {
			if k == 'i' {
				o.ids = append(o.ids, words[0])
			} else {
				o.strs = append(o.strs, strs[words[0]])
			}
			words = words[1:]
		}
		ops = append(ops, o)
	}
	return ops
}

// A dom is a mock of the page, which applies patches as the page would,
// and fails on any op that the page couldn't apply.
type dom struct {
	t     *testing.T
	nodes map[uint32]*mnode
}

type mnode struct {
	tag, text            string // a text node has no tag
	attrs, props, styles map[string]string
	listen               map[string]bool
	children             []uint32
	parent               int64 // -1 when not in a parent
}

func newDOM(t *testing.T) *dom {
	return &dom{t, map[uint32]*mnode{0: {tag: "mount", parent: -1}}}
}

func (d *dom) node(id uint32) *mnode {
	n := d.nodes[id]
	if n == nil {
		d.t.Fatalf("no node %d", id)
	}
	return n
}

func (d *dom) el(id uint32) *mnode {
	n := d.node(id)
	if n.tag == "" {
		d.t.Fatalf("node %d isn't an element", id)
	}
	return n
}

// inPage reports whether a node is in the mount element's tree.
func (d *dom) inPage(id uint32) bool {
	for id != 0 {
		n := d.nodes[id]
		if n == nil || n.parent < 0 {
			return false
		}
		id = uint32(n.parent)
	}
	return true
}

func (d *dom) apply(ops []op) {
	for _, o := range ops {
		ids, s := o.ids, o.strs
		switch o.code {
		case opCreateElement, opCreateText:
			if ids[0] == 0 || d.inPage(ids[0]) {
				d.t.Fatalf("%v: the id is in use", o)
			}
			n := &mnode{parent: -1}
			if o.code == opCreateText {
				n.text = s[0]
			} else {
				n.tag = s[0]
				n.attrs, n.props, n.styles, n.listen = map[string]string{}, map[string]string{}, map[string]string{}, map[string]bool{}
			}
			d.nodes[ids[0]] = n
		case opSetText:
			n := d.node(ids[0])
			if n.tag != "" {
				d.t.Fatalf("%v: not a text node", o)
			}
			n.text = s[0]
		case opSetAttr:
			d.el(ids[0]).attrs[s[0]] = s[1]
		case opSetProp:
			d.el(ids[0]).props[s[0]] = s[1]
		case opSetStyle:
			d.el(ids[0]).styles[s[0]] = s[1]
		case opRemoveAttr, opRemoveProp, opRemoveStyle:
			m := map[uint32]map[string]string{opRemoveAttr: d.el(ids[0]).attrs, opRemoveProp: d.el(ids[0]).props, opRemoveStyle: d.el(ids[0]).styles}[o.code]
			if _, ok := m[s[0]]; !ok {
				d.t.Fatalf("%v: not set", o)
			}
			delete(m, s[0])
		case opListen:
			if n := d.el(ids[0]); n.listen[s[0]] {
				d.t.Fatalf("%v: already listening", o)
			} else {
				n.listen[s[0]] = true
			}
		case opUnlisten:
			if n := d.el(ids[0]); !n.listen[s[0]] {
				d.t.Fatalf("%v: not listening", o)
			} else {
				delete(n.listen, s[0])
			}
		case opAppend:
			p, c := d.el(ids[0]), d.node(ids[1])
			if c.parent >= 0 {
				d.t.Fatalf("%v: the child has a parent", o)
			}
			p.children = append(p.children, ids[1])
			c.parent = int64(ids[0])
		case opReplace:
			old, n := d.node(ids[0]), d.node(ids[1])
			if !d.inPage(ids[0]) || n.parent >= 0 {
				d.t.Fatalf("%v: old isn't in the page, or new has a parent", o)
			}
			p := d.node(uint32(old.parent))
			p.children[slices.Index(p.children, ids[0])] = ids[1]
			n.parent, old.parent = old.parent, -1
		case opRemove:
			n := d.node(ids[0])
			if !d.inPage(ids[0]) {
				d.t.Fatalf("%v: not in the page", o)
			}
			p := d.node(uint32(n.parent))
			p.children = slices.DeleteFunc(p.children, func(c uint32) bool { return c == ids[0] })
			n.parent = -1
		}
	}
}

// print prints the page's content, and lists its ids in preorder.
func (d *dom) print(ids *[]uint32) string {
	var b strings.Builder
	var print func(id uint32)
	print = func(id uint32) {
		*ids = append(*ids, id)
		n := d.nodes[id]
		if n.tag == "" {
			fmt.Fprintf(&b, "%q", n.text)
			return
		}
		writeOpen(&b, n.tag, n.attrs, n.props, n.styles, n.listen)
		for _, c := range n.children {
			print(c)
		}
		b.WriteString("</" + n.tag + ">")
	}
	for _, c := range d.nodes[0].children {
		print(c)
	}
	return b.String()
}

func writeOpen(b *strings.Builder, tag string, attrs, props, styles map[string]string, listen map[string]bool) {
	var parts []string
	for k, v := range attrs {
		parts = append(parts, "a:"+k+"="+v)
	}
	for k, v := range props {
		parts = append(parts, "p:"+k+"="+v)
	}
	for k, v := range styles {
		parts = append(parts, "s:"+k+"="+v)
	}
	for k := range listen {
		parts = append(parts, "on:"+k)
	}
	sort.Strings(parts)
	b.WriteString("<" + strings.Join(append([]string{tag}, parts...), " ") + ">")
}

// expected prints a view as the page should show it, read through the
// bindings' Refs.
func expected(n NodeRef) string {
	var b strings.Builder
	var print func(n NodeRef)
	print = func(n NodeRef) {
		var tag string
		var attrs ListRef[AttrRef]
		var children []NodeRef
		switch n.Tag() {
		case NodeText:
			fmt.Fprintf(&b, "%q", n.Text())
			return
		case NodeEl:
			e := n.El()
			tag, attrs, children = e.Tag(), e.Attrs(), e.Children().Load()
		case NodeKeyed:
			e := n.Keyed()
			tag, attrs = e.Tag(), e.Attrs()
			for _, c := range e.Children().All() {
				children = append(children, c.Node())
			}
		}
		as, props, styles, listen := map[string]string{}, map[string]string{}, map[string]string{}, map[string]bool{}
		for _, a := range attrs.All() {
			switch a.Tag() {
			case AttrAttr:
				as[a.Attr().Name()] = a.Attr().Value()
			case AttrProp:
				props[a.Prop().Name()] = string(appendJSON(nil, a.Prop().Value()))
			case AttrStyle:
				styles[a.Style().Name()] = a.Style().Value()
			case AttrOn:
				listen[a.On().Event()] = true
			default:
				listen[map[AttrTag]string{AttrOnClick: "click", AttrOnInput: "input", AttrOnCheck: "change", AttrOnSubmit: "submit"}[a.Tag()]] = true
			}
		}
		writeOpen(&b, tag, as, props, styles, listen)
		for _, c := range children {
			print(c)
		}
		b.WriteString("</" + tag + ">")
	}
	print(n)
	return b.String()
}

// render renders view, applies the patch to d and checks the page, and
// that the renderer's tables agree with it. It returns the patch's ops.
func (d *dom) render(r *renderer, view NodeRef) []op {
	d.t.Helper()
	ops := decode(d.t, r.render(view))
	d.apply(ops)
	var ids []uint32
	if got, want := d.print(&ids), expected(view); got != want {
		d.t.Fatalf("got page\n%s\nwant\n%s\nafter %v", got, want, ops)
	}
	if !eval.Same(r.view, view.v) {
		d.t.Fatalf("the renderer doesn't keep the view")
	}
	if !slices.Equal(ids, r.ids) {
		d.t.Fatalf("the page's ids %v aren't the renderer's %v", ids, r.ids)
	}
	if len(r.ids)+len(r.free)+1 != len(r.attrs) {
		d.t.Fatalf("%d ids in use and %d free of %d", len(r.ids), len(r.free), len(r.attrs)-1)
	}
	return ops
}

// A gen generates views, from few tags, texts and attributes, so that
// views share parts with the last ones, and from parts of earlier views,
// which are the same values.
type gen struct {
	rnd                 *rand.Rand
	l                   *Bindings
	attrs               []eval.Value
	nodes, attrLists    []eval.Value
	children, keyedKids []eval.Value
}

func newGen(seed uint64, l *Bindings) *gen {
	g := &gen{rnd: rand.New(rand.NewPCG(seed, 0)), l: l}
	msg := Msg{eval.Int(1)}
	input := func(s string) (Msg, error) { return Msg{eval.Text(s)}, nil }
	check := func(Bool) (Msg, error) { return msg, nil }
	obj := NewJsonObject([]Member{{Key: "k", Value: NewJsonList([]Json{NewJsonNull(), NewJsonNumber(2)})}})
	for _, a := range []Attr{
		NewAttrAttr(AttrAttrPayload{Name: "class", Value: "a"}),
		NewAttrAttr(AttrAttrPayload{Name: "class", Value: "b"}),
		NewAttrAttr(AttrAttrPayload{Name: "id", Value: "x"}),
		NewAttrStyle(AttrStylePayload{Name: "color", Value: "red"}),
		NewAttrStyle(AttrStylePayload{Name: "color", Value: "blue"}),
		NewAttrProp(AttrPropPayload{Name: "value", Value: NewJsonText("v\"")}),
		NewAttrProp(AttrPropPayload{Name: "value", Value: NewJsonNumber(1.5)}),
		NewAttrProp(AttrPropPayload{Name: "data", Value: obj}),
		NewAttrProp(AttrPropPayload{Name: "checked", Value: NewJsonBool(NewBoolTrue())}),
		NewAttrOnClick(msg),
		NewAttrOnInput(input),
		NewAttrOnCheck(check),
		NewAttrOnSubmit(msg),
		NewAttrOn(AttrOnPayload{Event: "click", Msg: msg}),
		NewAttrOn(AttrOnPayload{Event: "mouseover", Msg: msg}),
	} {
		g.attrs = append(g.attrs, a.toValue(l))
	}
	return g
}

func pick[T any](g *gen, xs []T) T { return xs[g.rnd.IntN(len(xs))] }

func (g *gen) attrList() eval.Value {
	if len(g.attrLists) > 0 && g.rnd.IntN(3) == 0 {
		return pick(g, g.attrLists)
	}
	as := make([]eval.Value, g.rnd.IntN(5))
	for i := range as {
		as[i] = pick(g, g.attrs)
	}
	v := eval.NewList(as...)
	g.attrLists = append(g.attrLists, v)
	return v
}

func (g *gen) node(depth int) eval.Value {
	l := g.l
	if len(g.nodes) > 0 && g.rnd.IntN(6) == 0 {
		return pick(g, g.nodes)
	}
	var v eval.Value
	if depth == 0 || g.rnd.IntN(3) == 0 {
		v = eval.NewVariant(l.text, eval.Text(pick(g, []string{"a", "b", ""})))
	} else if tag := eval.Text(pick(g, []string{"div", "p"})); g.rnd.IntN(4) == 0 {
		kids := g.list(&g.keyedKids, func() eval.Value {
			return l.childShape.New(eval.Text(pick(g, []string{"k", "j"})), g.node(depth-1))
		})
		v = eval.NewVariant(l.keyed, l.keyedShape.New(g.attrList(), kids, tag))
	} else {
		kids := g.list(&g.children, func() eval.Value { return g.node(depth - 1) })
		v = eval.NewVariant(l.el, l.elementShape.New(g.attrList(), kids, tag))
	}
	g.nodes = append(g.nodes, v)
	return v
}

// list returns a list of up to 4 elements, or sometimes one from pool.
func (g *gen) list(pool *[]eval.Value, elem func() eval.Value) eval.Value {
	if len(*pool) > 0 && g.rnd.IntN(5) == 0 {
		return pick(g, *pool)
	}
	xs := make([]eval.Value, g.rnd.IntN(5))
	for i := range xs {
		xs[i] = elem()
	}
	v := eval.NewList(xs...)
	*pool = append(*pool, v)
	return v
}

// Applying the patch from each view to the next to the page of the first
// gives the page of the next.
func TestRenderRandom(t *testing.T) {
	l := newBindings(eval.NewEnvironment())
	for seed := range uint64(300) {
		g := newGen(seed, l)
		d := newDOM(t)
		var r renderer
		for range 20 {
			d.render(&r, NodeRef{ref{l, g.node(4)}})
		}
	}
}

func TestRender(t *testing.T) {
	l := newBindings(eval.NewEnvironment())
	text := func(s string) Node { return NewNodeText(s) }
	el := func(tag string, attrs []Attr, children ...Node) Node {
		return NewNodeEl(Element{Tag: tag, Attrs: attrs, Children: children})
	}
	class := func(v string) Attr { return NewAttrAttr(AttrAttrPayload{Name: "class", Value: v}) }
	click := NewAttrOnClick(Msg{eval.Int(1)})
	clickToo := NewAttrOn(AttrOnPayload{Event: "click", Msg: Msg{eval.Int(2)}})

	steps := []struct {
		view Node
		ops  string
	}{
		{el("div", []Attr{class("a")}, text("x")),
			"CREATE_ELEMENT 1 div; SET_ATTR 1 class a; CREATE_TEXT 2 x; APPEND 1 2; APPEND 0 1"},
		{el("div", []Attr{class("a")}, text("y")), "SET_TEXT 2 y"},
		{el("div", []Attr{class("b"), click}, text("y"), text("z")),
			"SET_ATTR 1 class b; LISTEN 1 click; CREATE_TEXT 3 z; APPEND 1 3"},
		// Another handler of the same event changes no listener.
		{el("div", []Attr{clickToo}, text("y")), "REMOVE_ATTR 1 class; REMOVE 3"},
		// The freed id is reused.
		{el("div", nil, el("p", nil), text("y")),
			"UNLISTEN 1 click; CREATE_ELEMENT 3 p; REPLACE 2 3; CREATE_TEXT 2 y; APPEND 1 2"},
		{el("p", nil), "CREATE_ELEMENT 4 p; REPLACE 1 4"},
		{text("t"), "CREATE_TEXT 2 t; REPLACE 4 2"},
	}
	d := newDOM(t)
	var r renderer
	for _, s := range steps {
		ops := d.render(&r, s.view.Ref(l))
		var got []string
		for _, o := range ops {
			got = append(got, o.String())
		}
		if g := strings.Join(got, "; "); g != s.ops {
			t.Errorf("rendering %s:\ngot  %s\nwant %s", expected(s.view.Ref(l)), g, s.ops)
		}
	}

	// The same view again needs no ops.
	if ops := d.render(&r, NodeRef{ref{l, r.view}}); len(ops) != 0 {
		t.Errorf("rendering the same view again gave %v", ops)
	}
}

func TestJSON(t *testing.T) {
	l := newBindings(eval.NewEnvironment())
	for _, s := range []string{"", "a\"b\\c", "\x00\n\x1f", "é ✓ 😀 \u2028", "bad \xff"} {
		got := string(appendJSON(nil, NewJsonText(s).Ref(l)))
		want, _ := json.Marshal(s)
		var back string
		if err := json.Unmarshal([]byte(got), &back); err != nil || back != strings.ToValidUTF8(s, "\uFFFD") {
			t.Errorf("%q encodes as %s, which reads back as %q, %v; json.Marshal gives %s", s, got, back, err, want)
		}
	}
	j := NewJsonObject([]Member{
		{Key: "a", Value: NewJsonList([]Json{NewJsonNull(), NewJsonBool(NewBoolFalse()), NewJsonNumber(-0.5), NewJsonNumber(1e21)})},
		{Key: "b", Value: NewJsonObject(nil)},
	})
	if got := string(appendJSON(nil, j.Ref(l))); got != `{"a":[null,false,-0.5,1e+21],"b":{}}` {
		t.Errorf("got %s", got)
	}
}
