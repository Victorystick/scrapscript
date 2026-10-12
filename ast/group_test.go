package ast_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/Victorystick/scrapscript/ast"
	"github.com/Victorystick/scrapscript/parser"
)

func TestComponents(t *testing.T) {
	examples := []struct{ source, order string }{
		// Bindings come after the ones they refer to.
		{`x ; a = b ; b = 1`, `b a`},
		{`x ; a = 1 ; b = a`, `a b`},
		// Mutually recursive bindings share a component.
		{`x ; a = b ; b = c ; c = a ; d = a`, `a,b,c d`},
		{`x ; f = n -> f n`, `f`},
		// Names bound within a value don't refer to the group.
		{`x ; a = b -> b ; b = (a ; a = 1) ; c = | [c] -> c`, `a b c`},
		// Sort bindings are left out.
		{`x ; t : type = int ; a = 1`, `a`},
	}

	for _, ex := range examples {
		se, err := parser.ParseExpr(ex.source)
		if err != nil {
			t.Fatal(err)
		}
		_, binds := se.Group(se.Expr)
		var comps []string
		for _, comp := range se.Components(binds, nil) {
			var names []string
			for _, b := range comp.Binds {
				names = append(names, se.TokenText(se.Main(b)))
			}
			slices.Sort(names)
			comps = append(comps, strings.Join(names, ","))
		}
		if got := strings.Join(comps, " "); got != ex.order {
			t.Errorf("%s: expected %s, got %s", ex.source, ex.order, got)
		}
	}
}

func TestRecursive(t *testing.T) {
	examples := []struct {
		source    string
		recursive bool
	}{
		{`x ; a = 1`, false},
		{`x ; a = a`, true},
		{`x ; f = n -> f n`, true},
		{`x ; f = f -> f`, false},
		{`x ; a = (a ; a = 1)`, false},
		{`x ; f = | { f = g } -> g`, false},
	}
	for _, ex := range examples {
		se, err := parser.ParseExpr(ex.source)
		if err != nil {
			t.Fatal(err)
		}
		_, binds := se.Group(se.Expr)
		if got := se.Components(binds, nil)[0].Recursive; got != ex.recursive {
			t.Errorf("%s: expected recursive to be %v", ex.source, ex.recursive)
		}
	}
}

func TestGroup(t *testing.T) {
	se, err := parser.ParseExpr(`x ; a = 1 ; b = 2`)
	if err != nil {
		t.Fatal(err)
	}
	body, binds := se.Group(se.Expr)
	if se.Kind(body) != ast.Ident || len(binds) != 2 ||
		se.TokenText(se.Main(binds[0])) != "a" || se.TokenText(se.Main(binds[1])) != "b" {
		t.Errorf("unexpected group %v %v", body, binds)
	}
}
