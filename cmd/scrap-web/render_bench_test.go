package main

import (
	"slices"
	"strconv"
	"testing"

	"github.com/Victorystick/scrapscript/eval"
)

// rows shows a list of 1,000 rows, each with a class, a handler and a
// text that changes with the model.
var rows = `app
; app : web int msg =
    { init = _ -> { model = 0, cmds = [] }
    , update = | #inc -> n -> { model = n + 1, cmds = [] }
    , view = n -> #el { tag = "ul", attrs = [], children = list/map (row n) items }
    , subscriptions = _ -> []
    }
; msg : type = [#inc]
; row = n -> i -> #el { tag = "li", attrs = [#attr { name = "class", value = "row" }, #on-click #inc], children = [#text (int/to-text (i + n))] }
; items = upto 1000 []
; upto = | 0 -> acc -> acc
         | k -> acc -> upto (k - 1) ((k - 1) >+ acc)
` + web

// BenchmarkRender measures diffing two views of 1,000 rows that differ in
// every row's text, and building the first from nothing.
func BenchmarkRender(b *testing.B) {
	s := start(b, eval.NewEnvironment(), rows, NewJsonNull())
	v, err := s.View()
	if err != nil {
		b.Fatal(err)
	}
	inc := v.El().Children().At(0).El().Attrs().At(1).OnClick()
	views := make([]NodeRef, 2)
	for i := range views {
		v, err := s.View()
		if err != nil {
			b.Fatal(err)
		}
		views[i] = v
		if err := s.Update(inc); err != nil {
			b.Fatal(err)
		}
	}
	b.Run("create", func(b *testing.B) {
		for b.Loop() {
			var r renderer
			r.render(views[0])
		}
	})
	b.Run("diff", func(b *testing.B) {
		var r renderer
		r.render(views[0])
		i := 0
		for b.Loop() {
			i ^= 1
			r.render(views[i])
		}
	})
	b.Run("update", func(b *testing.B) {
		if _, err := s.Render(); err != nil {
			b.Fatal(err)
		}
		for b.Loop() {
			if err := s.Update(inc); err != nil {
				b.Fatal(err)
			}
			if _, err := s.Render(); err != nil {
				b.Fatal(err)
			}
		}
	})
}

// BenchmarkRenderKeyed measures diffing a keyed list of 1,000 rows against
// one with a row moved from the front to the back, and the reverse of it;
// each row is the same value in both.
func BenchmarkRenderKeyed(b *testing.B) {
	l := newBindings(eval.NewEnvironment())
	rows := make([]eval.Value, 1000)
	for i := range rows {
		k := strconv.Itoa(i)
		li := NewNodeEl(Element{Tag: "li", Children: []Node{NewNodeText(k)}})
		rows[i] = l.childShape.New(eval.Text(k), li.toValue(l))
	}
	ul := func(rows []eval.Value) NodeRef {
		return NodeRef{ref{l, eval.NewVariant(l.keyed, l.keyedShape.New(eval.NewList(), eval.NewList(rows...), eval.Text("ul")))}}
	}
	moved := append(slices.Clone(rows[1:]), rows[0])
	reversed := slices.Clone(rows)
	slices.Reverse(reversed)
	for _, c := range []struct {
		name string
		to   []eval.Value
	}{{"move", moved}, {"reverse", reversed}} {
		b.Run(c.name, func(b *testing.B) {
			views := []NodeRef{ul(rows), ul(c.to)}
			var r renderer
			r.render(views[0])
			i := 0
			for b.Loop() {
				i ^= 1
				r.render(views[i])
			}
		})
	}
}
