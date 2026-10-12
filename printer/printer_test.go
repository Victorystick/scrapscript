package printer

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/Victorystick/scrapscript/ast"
	"github.com/Victorystick/scrapscript/parser"
)

func TestPrint(t *testing.T) {
	expect(t, `f 1 ; f = a -> a`, `f 1
; f = a -> a`)

	expect(t, `f "b" ; f = | "a" -> 1 | "b" -> 2 | "c" -> 3 | x -> 0`, `f "b"
; f =
  | "a" -> 1
  | "b" -> 2
  | "c" -> 3
  | x -> 0`)

	expect(t, `| "hey" -> "" | "hello " ++ name -> name | _ -> ""`, `| "hey" -> ""
| "hello " ++ name -> name
| _ -> ""`)

	expect(t, `a + b + c ; a = 1 ; b = 2 ; c = 3`, `a + b + c
; a = 1
; b = 2
; c = 3`)

	// Parentheses are added where the grammar needs them, and only there.
	expect(t, `#fun (x -> x * 2)`, `#fun (x -> x * 2)`)
	expect(t, `list/fold 0 (a -> (b -> (a + b)))`, `list/fold 0 (a -> b -> a + b)`)
	expect(t, `(a - b) - (c - d)`, `a - b - (c - d)`)
	expect(t, `(a >+ b) >+ (c >+ d)`, `(a >+ b) >+ c >+ d`)
	expect(t, `(f 2) * (g (3 * 4))`, `f 2 * g (3 * 4)`)
	expect(t, `(3 |> (a -> a)) |> (b -> b)`, `3 |> (a -> a) |> b -> b`)
	expect(t, `(| 0 -> 1 | n -> n) <| 2`, `| 0 -> 1 | n -> n <| 2`)
	expect(t, `(| 0 -> 1 | n -> n) + 2`, `(| 0 -> 1 | n -> n) + 2`)
	expect(t, `| [a] ++ rest -> (x -> x) | _ -> (y |> f)`, `| [ a ] ++ rest -> x -> x
| _ -> (y |> f)`)
	expect(t, `#some (#some 1)`, `#some (#some 1)`)
	expect(t, `(f x).a`, `(f x).a`)
	expect(t, `{ a = (x ; x = 1) }`, `{ a = x ; x = 1 }`)
	expect(t, `f ; f : a => (a -> a) -> list (a -> a) = g -> [g]`,
		`f
; f : a => (a -> a) -> list (a -> a) = g -> [ g ]`)
}

// Sources whose trees must survive printing and parsing again.
var trees = []string{
	`10 - 3 - 2`,
	`10 - (3 - 2)`,
	`1 >+ 2 >+ xs`,
	`(1 >+ 2) >+ xs`,
	`xs +< 1 +< 2`,
	`xs +< (1 +< 2)`,
	`f 2 * 3`,
	`f (2 * 3)`,
	`f g h`,
	`f (g h)`,
	`f #a 1`,
	`#a (#b 1)`,
	`#a #b`,
	`#a (x -> x)`,
	`#a -1`,
	`f -1 - 2`,
	`r.a.b`,
	`(r.a).b`,
	`(f r).a`,
	`x -> y -> x + y`,
	`(x -> x) >> (y -> y)`,
	`f >> g >> h`,
	`f <| g <| x`,
	`(f <| g) <| x`,
	`3 |> a -> b -> a`,
	`(3 |> a -> a) |> f`,
	`x |> | 0 -> 1 | _ -> 2`,
	`(x |> | 0 -> 1 | _ -> 2) |> f`,
	`| 0 -> 1 | _ -> 2 <| 3`,
	`(| 0 -> 1 | _ -> 2) 3`,
	`| #true -> #true -> #true | _ -> _ -> #false`,
	`| 0 -> (x -> x) | n -> (| 1 -> 2)`,
	`| x >+ xs -> xs | "a" ++ s -> s | { ..r, a = x } -> r`,
	`| a -> (b |> c) | d -> (e >> f)`,
	`(a ; a = 1) + (b ; b = 2)`,
	`a ; a = b ; b = 1`,
	`[ (x ; x = 1), y -> y, | 0 -> 1 ]`,
	`{ ..r, a = 1, b = x -> x }`,
	`{ ..r, }`,
	`$sha256~~abcd $"./a b.scrap"`,
	`"esc\"aped\n"`,
	`f ; f : int -> (int -> int) -> int = a -> g -> g a`,
	`f ; f : a => b => { x : a, ..r } -> [#a b, #c (list a), ..s] = _ -> #c []`,
	`t ; t : type -> type = s => { state : s }`,
	`t ; t = a => [#leaf, #node { left : t a, value : a, right : t a }]`,
	`x ; x : () = ()`,
	`f ; f : list (int -> int) = []`,
	`f ; f : (a => a) -> int = _ -> 1`,
	`a < b`,
	`(a < b) |> f`,
	`a ++ b ++ c`,
	`a ++ (b ++ c)`,
	`a + (b |> f)`,
}

func TestTrees(t *testing.T) {
	for _, source := range trees {
		want, err := parser.ParseExpr(source)
		if err != nil {
			t.Errorf("%s: %v", source, err)
			continue
		}
		var buf bytes.Buffer
		Fprint(&buf, &want, want.Expr)
		printed := buf.String()
		got, err := parser.ParseExpr(printed)
		if err != nil {
			t.Errorf("%s: printed %s, which doesn't parse: %v", source, printed, err)
			continue
		}
		if w, g := dump(&want, want.Expr), dump(&got, got.Expr); w != g {
			t.Errorf("%s: printed %s, which parses as\n%s\nnot\n%s", source, printed, g, w)
		}
	}
}

// dump writes the tree of x as an S-expression.
func dump(se *ast.SourceExpr, x ast.Node) string {
	var b strings.Builder
	var walk func(x ast.Node)
	node := func(name string, children ...ast.Node) {
		b.WriteString("(" + name)
		for _, c := range children {
			b.WriteByte(' ')
			walk(c)
		}
		b.WriteByte(')')
	}
	walk = func(x ast.Node) {
		if x == ast.None {
			b.WriteString("_")
			return
		}
		switch k := se.Kind(x); k {
		case ast.Ident, ast.Literal, ast.Import:
			b.WriteString(se.NodeText(x))
		case ast.Binary:
			op, l, r := se.Binary(x)
			node(op.Op(), l, r)
		case ast.Func, ast.Call:
			l, r := se.Func(x)
			node(k.String(), l, r)
		case ast.MatchFunc:
			node("match", se.Arms(x)...)
		case ast.Variant:
			tag, payload := se.Variant(x)
			node("#"+se.TokenText(tag), payload)
		case ast.Record, ast.RecordType:
			rest, fields := se.Record(x)
			node(k.String(), rest)
			for i := range fields.Len() {
				node(se.TokenText(fields.Key(i)), fields.Value(i))
			}
		case ast.Enum:
			tail, variants := se.Enum(x)
			node("enum", append([]ast.Node{tail}, variants...)...)
		case ast.Access:
			rec, key := se.Access(x)
			node("."+se.TokenText(key), rec)
		case ast.List:
			node("list", se.List(x)...)
		case ast.Where:
			body, id, typ, val := se.Where(x)
			node(fmt.Sprintf("where %s %v", se.TokenText(id), se.Sort(x)), body, typ, val)
		case ast.Forall:
			param, typ := se.Forall(x)
			node("forall "+se.TokenText(param), typ)
		default:
			b.WriteString(k.String())
		}
	}
	walk(x)
	return b.String()
}

func expect(t *testing.T, source, expected string) {
	t.Helper()
	se, err := parser.ParseExpr(source)
	if err != nil {
		t.Error(err)
	} else {
		var buf bytes.Buffer
		err = Fprint(&buf, &se, se.Expr)
		if err != nil {
			t.Error(err)
		} else {
			output := buf.String()
			if output != expected {
				t.Errorf("Expected:\n%s\nGot:\n%s ", expected, output)
			}
		}
	}
}
