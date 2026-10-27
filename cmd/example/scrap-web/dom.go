package main

import (
	"slices"
	"strconv"
	"unicode/utf8"
)

// The page shows a view as a document that the host builds and changes
// with patches: it keeps the view that the page shows, and answers each
// request that changes the view with what changed since. A patch is a
// JSON array of operations, each an op code and its operands, in turn:
//
//	0                       clear: empty the body
//	1 parent after node     insert the node into parent, after the child after, or first if 0
//	2 parent after id       move the node id within parent, after the child after, or first if 0
//	3 id                    remove the node id
//	4 id text               set the text of the text node id
//	5 id name value         set the attribute name of the element id
//	6 id name               remove the attribute name of the element id
//	7 id event              listen for the event on the element id
//	8 id event              stop listening for the event on the element id
//
// Each node has an id, which it keeps for as long as the page shows it;
// the body is 0. An inserted node is a text node, as its id, null and its
// text, or an element, as its id, tag, the number of its attributes and
// each attribute's name and value, the number of events it listens for and
// each event, and the number of its children and each child.
//
// An element whose attributes have a key is matched by its key with its
// siblings in the last view, as React matches elements by theirs, so that
// it moves, keeping its state, such as focus, rather than another element
// changing to look like it. The key is no attribute of the element itself.
const (
	opClear = iota
	opInsert
	opMove
	opRemove
	opText
	opSet
	opUnset
	opOn
	opOff
)

// A view is the view that the page shows: its nodes in preorder, each
// followed by its subtree, the first the body, and the attributes and
// events of its elements.
type view struct {
	nodes  []vnode
	attrs  []attr
	events []string
}

// A vnode is a node of a view.
type vnode struct {
	id int
	// The number of nodes in its subtree, itself among them.
	size int
	// The tag of an element, or "" for a text node, its key, and a text
	// node's text.
	tag, key, text string
	// Its attributes and events, as ranges of the view's.
	attrs, events span
}

type span struct{ start, end int }

type attr struct{ name, value string }

// The view that the page shows, without nodes before the first, the last
// view, whose memory the next reuses, and the id of the next new node.
// They outlive sessions, so that a new version of the app changes the view,
// as the page shows it, rather than replacing it.
var (
	shown, spare view
	nextID       = 1
)

// A handler is one of the handlers of an element's events in a view.
type handler struct {
	node int
	attr Attr
}

// A patcher diffs a view with the last into a patch.
type patcher struct {
	out      []byte
	old, new *view
	// The handlers of the new view.
	handlers []handler
	// A stack of the indices of old children.
	kids []int
}

// The number of handlers of the last view, which the next likely has.
var lastHandlers int

// patch makes the page show h, and returns the patch, as JSON, and the
// handlers of the view.
func patch(h Html) ([]byte, []handler) {
	p := patcher{out: []byte{'['}, old: &shown, new: &spare, handlers: make([]handler, 0, lastHandlers)}
	if len(shown.nodes) == 0 {
		shown.nodes = append(shown.nodes, vnode{size: 1})
		p.op(opClear)
	}
	p.new.nodes, p.new.attrs, p.new.events = append(p.new.nodes[:0], vnode{}), p.new.attrs[:0], p.new.events[:0]
	p.children(0, 0, []Html{h})
	p.new.nodes[0].size = len(p.new.nodes)
	shown, spare = spare, shown
	lastHandlers = len(p.handlers)
	return append(p.out, ']'), p.handlers
}

// reshow returns a patch that shows the view anew, as for a page that lost
// it.
func reshow() []byte {
	p := patcher{out: []byte{'['}, new: &shown}
	p.op(opClear)
	after := 0
	for i := 1; i < len(shown.nodes); i += shown.nodes[i].size {
		p.op(opInsert, 0, after)
		p.node(i)
		after = shown.nodes[i].id
	}
	return append(p.out, ']')
}

// children makes the children of the new node ni show hs, from those of
// the old node oi. It matches each with an old child, by key, or by order
// among those without a key, and changes that, and then removes the old
// children that none matched, inserts new ones, and moves as few of the
// others as it can into place.
func (p *patcher) children(oi, ni int, hs []Html) {
	base := len(p.kids)
	for j, end := oi+1, oi+p.old.nodes[oi].size; j < end; j += p.old.nodes[j].size {
		p.kids = append(p.kids, j)
	}
	if p.lined(base, hs) {
		for i, h := range hs {
			p.update(p.kids[base+i], h)
		}
		p.kids = p.kids[:base]
		return
	}
	kids := slices.Clone(p.kids[base:])
	p.kids = p.kids[:base]
	var byKey map[string]int
	var unkeyed []int
	for k, j := range kids {
		if key := p.old.nodes[j].key; key == "" {
			unkeyed = append(unkeyed, k)
		} else if byKey == nil {
			byKey = map[string]int{key: k}
		} else if _, ok := byKey[key]; !ok {
			byKey[key] = k
		}
	}
	// Of the old children, the one that each new one matches, or -1.
	from := make([]int, len(hs))
	used := make([]bool, len(kids))
	keys := make([]string, len(hs))
	for i, h := range hs {
		k, ok := -1, false
		if keys[i] = keyOf(h); keys[i] != "" {
			k, ok = byKey[keys[i]]
		} else if len(unkeyed) > 0 {
			k, ok, unkeyed = unkeyed[0], true, unkeyed[1:]
		}
		if !ok || used[k] || !same(p.old.nodes[kids[k]], h) {
			from[i] = -1
			continue
		}
		from[i] = k
		used[k] = true
	}
	for k, j := range kids {
		if !used[k] {
			p.op(opRemove, p.old.nodes[j].id)
		}
	}
	stay := stays(from)
	parent, after := p.new.nodes[ni].id, 0
	for i, h := range hs {
		if k := from[i]; k < 0 {
			at := len(p.new.nodes)
			p.build(h, keys[i])
			p.op(opInsert, parent, after)
			p.node(at)
			after = p.new.nodes[at].id
		} else {
			id := p.update(kids[k], h)
			if !stay[i] {
				p.op(opMove, parent, after, id)
			}
			after = id
		}
	}
}

// lined returns whether each of hs matches the old child of the same
// index, of those on the stack from base, as children mostly do.
func (p *patcher) lined(base int, hs []Html) bool {
	if len(p.kids)-base != len(hs) {
		return false
	}
	for i, h := range hs {
		if n := p.old.nodes[p.kids[base+i]]; !same(n, h) || n.key != keyOf(h) {
			return false
		}
	}
	return true
}

// same returns whether n can show h: both are text, or elements with the
// same tag.
func same(n vnode, h Html) bool {
	if h.Tag() == HtmlText {
		return n.tag == ""
	}
	return n.tag == h.El().Tag
}

func keyOf(h Html) string {
	if h.Tag() != HtmlEl {
		return ""
	}
	key := ""
	for _, a := range h.El().Attrs {
		if a.Tag() == AttrAttr && a.Attr().Name == "key" {
			key = a.Attr().Value
		}
	}
	return key
}

// stays returns, of the new children that match old ones, the most that
// can stay where they are, in order, while the others move around them:
// those whose old indices form a longest increasing subsequence.
func stays(from []int) []bool {
	stay := make([]bool, len(from))
	last, sorted := -1, true
	for i, j := range from {
		if j >= 0 {
			sorted = sorted && j > last
			last = j
			stay[i] = true
		}
	}
	if sorted {
		return stay
	}
	clear(stay)
	// tails[k] is the index in from of the least end of an increasing
	// subsequence of length k+1, and prev the index before each in its.
	var tails []int
	prev := make([]int, len(from))
	for i, j := range from {
		if j < 0 {
			continue
		}
		k, hi := 0, len(tails)
		for k < hi {
			if m := (k + hi) / 2; from[tails[m]] < j {
				k = m + 1
			} else {
				hi = m
			}
		}
		prev[i] = -1
		if k > 0 {
			prev[i] = tails[k-1]
		}
		if k == len(tails) {
			tails = append(tails, i)
		} else {
			tails[k] = i
		}
	}
	for i := tails[len(tails)-1]; i >= 0; i = prev[i] {
		stay[i] = true
	}
	return stay
}

// build adds new nodes, with new ids, that show h.
func (p *patcher) build(h Html, key string) {
	ni := len(p.new.nodes)
	p.new.nodes = append(p.new.nodes, vnode{id: nextID, size: 1, key: key})
	nextID++
	if h.Tag() == HtmlText {
		p.new.nodes[ni].text = h.Text()
		return
	}
	el := h.El()
	p.new.nodes[ni].tag = el.Tag
	p.new.nodes[ni].attrs, p.new.nodes[ni].events = p.attrs(p.new.nodes[ni].id, el.Attrs)
	for _, c := range el.Children {
		p.build(c, keyOf(c))
	}
	p.new.nodes[ni].size = len(p.new.nodes) - ni
}

// update adds a node that shows h, which the old node oi can show, as it
// changes, and returns its id.
func (p *patcher) update(oi int, h Html) int {
	o := p.old.nodes[oi]
	ni := len(p.new.nodes)
	p.new.nodes = append(p.new.nodes, vnode{id: o.id, size: 1, tag: o.tag, key: o.key})
	if h.Tag() == HtmlText {
		t := h.Text()
		p.new.nodes[ni].text = t
		if o.text != t {
			p.op(opText, o.id)
			p.str(t)
		}
		return o.id
	}
	el := h.El()
	as, es := p.attrs(o.id, el.Attrs)
	p.new.nodes[ni].attrs, p.new.nodes[ni].events = as, es
	olds, news := p.old.attrs[o.attrs.start:o.attrs.end], p.new.attrs[as.start:as.end]
	if !slices.Equal(olds, news) {
		for _, a := range olds {
			if !slices.ContainsFunc(news, named(a.name)) {
				p.op(opUnset, o.id)
				p.str(a.name)
			}
		}
		for _, a := range news {
			if !slices.Contains(olds, a) {
				p.op(opSet, o.id)
				p.str(a.name)
				p.str(a.value)
			}
		}
	}
	olde, newe := p.old.events[o.events.start:o.events.end], p.new.events[es.start:es.end]
	if !slices.Equal(olde, newe) {
		for _, e := range olde {
			if !slices.Contains(newe, e) {
				p.op(opOff, o.id)
				p.str(e)
			}
		}
		for _, e := range newe {
			if !slices.Contains(olde, e) {
				p.op(opOn, o.id)
				p.str(e)
			}
		}
	}
	p.children(oi, ni, el.Children)
	p.new.nodes[ni].size = len(p.new.nodes) - ni
	return o.id
}

func named(name string) func(attr) bool {
	return func(a attr) bool { return a.name == name }
}

// attrs adds the attributes of the element id, the last of each name, and
// the events that it handles, to the new view, and returns their ranges,
// and keeps its handlers.
func (p *patcher) attrs(id int, as []Attr) (span, span) {
	a0, e0 := len(p.new.attrs), len(p.new.events)
	for _, a := range as {
		var event string
		switch a.Tag() {
		case AttrAttr:
			a := a.Attr()
			if a.Name == "key" {
				continue
			}
			if i := slices.IndexFunc(p.new.attrs[a0:], named(a.Name)); i >= 0 {
				p.new.attrs[a0+i].value = a.Value
			} else {
				p.new.attrs = append(p.new.attrs, attr{a.Name, a.Value})
			}
			continue
		case AttrOn:
			event = a.On().Event
		case AttrOnValue:
			event = a.OnValue().Event
		}
		if !slices.Contains(p.new.events[e0:], event) {
			p.new.events = append(p.new.events, event)
		}
		p.handlers = append(p.handlers, handler{id, a})
	}
	return span{a0, len(p.new.attrs)}, span{e0, len(p.new.events)}
}

// node writes the new node i, as an insert does.
func (p *patcher) node(i int) {
	n := p.new.nodes[i]
	p.num(n.id)
	if n.tag == "" {
		p.out = append(p.out, ",null"...)
		p.str(n.text)
		return
	}
	p.str(n.tag)
	p.num(n.attrs.end - n.attrs.start)
	for _, a := range p.new.attrs[n.attrs.start:n.attrs.end] {
		p.str(a.name)
		p.str(a.value)
	}
	p.num(n.events.end - n.events.start)
	for _, e := range p.new.events[n.events.start:n.events.end] {
		p.str(e)
	}
	end, count := i+n.size, 0
	for j := i + 1; j < end; j += p.new.nodes[j].size {
		count++
	}
	p.num(count)
	for j := i + 1; j < end; j += p.new.nodes[j].size {
		p.node(j)
	}
}

// op writes an op code and its numeric operands.
func (p *patcher) op(code int, operands ...int) {
	p.num(code)
	for _, o := range operands {
		p.num(o)
	}
}

func (p *patcher) num(n int) {
	p.sep()
	p.out = strconv.AppendInt(p.out, int64(n), 10)
}

func (p *patcher) str(s string) {
	p.sep()
	p.out = appendString(p.out, s)
}

func (p *patcher) sep() {
	if len(p.out) > 1 {
		p.out = append(p.out, ',')
	}
}

// appendString appends s as a JSON string, with invalid UTF-8 as U+FFFD,
// as encoding/json does.
func appendString(b []byte, s string) []byte {
	const hex = "0123456789abcdef"
	b = append(b, '"')
	start := 0
	for i := 0; i < len(s); {
		c := s[i]
		if c >= 0x20 && c != '"' && c != '\\' && c < utf8.RuneSelf {
			i++
			continue
		}
		if c < utf8.RuneSelf {
			b = append(b, s[start:i]...)
			switch c {
			case '"', '\\':
				b = append(b, '\\', c)
			case '\n':
				b = append(b, '\\', 'n')
			case '\r':
				b = append(b, '\\', 'r')
			case '\t':
				b = append(b, '\\', 't')
			default:
				b = append(b, '\\', 'u', '0', '0', hex[c>>4], hex[c&0xf])
			}
			i++
			start = i
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		if r == utf8.RuneError && size == 1 {
			b = append(b, s[start:i]...)
			b = append(b, `�`...)
			i += size
			start = i
			continue
		}
		i += size
	}
	b = append(b, s[start:]...)
	return append(b, '"')
}
