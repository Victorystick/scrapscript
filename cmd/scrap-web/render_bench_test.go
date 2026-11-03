package main

import (
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
