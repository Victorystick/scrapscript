package eval

import (
	"fmt"
	"strings"
	"testing"

	"github.com/Victorystick/scrapscript/label"
	"github.com/Victorystick/scrapscript/parser"
	"github.com/Victorystick/scrapscript/types"
)

// The tests below compare a core with an S-expression that dump writes.
//
// @ reads a slot and > writes one. Writes always go to the current frame.
//
// Expressions:
//
//	1, "a", list/length  a constant: a literal or a builtin, as it prints
//	@s                   reads slot s of the current frame
//	@d.s                 reads slot s of the frame d out from the current
//	                     one: @1.0 is slot 0 of the frame around it
//	(f x)                a call; pipes are calls too
//	(+ a b)              an operator: + - * +< >+ ++
//	{ k = v ..r }        a record, based on r if ..r is there
//	r.k                  a field access
//	[a, b]               a list
//	#t, (#t p)           a tag, without and with a payload
//	(body ; >s v ; …)    a where: each binding writes the value v into slot
//	                     s of the current frame, in evaluation order, then
//	                     body is evaluated
//	(>> uses f g)        a composition, f applied first; << is flipped
//	(fn/n uses | p -> b | …)
//	                     a lambda whose frame has n slots, and its arms: a
//	                     pattern and a body each; a Func node has one arm
//
// Patterns:
//
//	>s                   matches anything, writing it into slot s of the
//	                     lambda's frame
//	_                    matches anything
//	1, "a"               matches a literal
//	(#t p)               matches a tag, and its payload with p
//	{ k = p ..r }        matches fields, and the remaining record with r
//	[p, q]               matches a list of exactly that length
//	(>+ h t), (+< i l)   matches a non-empty list by head and tail, or by
//	                     init and last element
//	(++ [p] r), (++ r [p])
//	                     matches a list by a prefix or suffix of elements,
//	                     and the rest
//
// A lambda's or composition's uses is what its code refers to, for printing
// it as a value. It lists its free names, once per occurrence in source
// order, and then its home:
//
//	^name@d.s            a value binder named name, in slot s of the frame
//	                     d out from the one the closure is made in. A
//	                     lambda's body runs in its own frame, one further
//	                     in, so a read of the same name there is one
//	                     deeper: in (fn/1 ^n@0.1 | >0 -> @1.1), ^n@0.1 and
//	                     @1.1 are the same n. A composition has no frame,
//	                     so its reads have the same depth
//	^name@d              a sort binding, which has no slot; these are only
//	                     in annotations and types
//	^name                a builtin
//	home=f               the where-binding f whose value it is, if it is one,
//	                     which it prints as
//
// Names bound within the code, by its own patterns or where-groups, are not
// free and aren't listed. For example:
//
//	f = x -> x + n ; n = 1   (fn/1 ^n@0.1 home=f | >0 -> (+ @0 @1.1))
//	f = x -> x               (fn/1 home=f | >0 -> @0)
//	(x -> x + n) ; n = 1     (fn/1 ^n@0.0 | >0 -> (+ @0 @1.0))

var opStrings = map[op]string{
	opAdd: "+", opSub: "-", opMul: "*",
	opAppend: "+<", opPrepend: ">+", opConcat: "++",
	patCons: ">+", patSnoc: "+<",
}

// dump writes node n of c as an S-expression, described above.
func (c *core) dump(s *strings.Builder, n cnode) {
	d := c.data[n]
	switch o := c.ops[n]; o {
	case opConst, patLiteral:
		s.WriteString(printValue(c.labels, c.consts[d.a]))
	case opLocal:
		if d.a == 0 {
			fmt.Fprintf(s, "@%d", d.b)
		} else {
			fmt.Fprintf(s, "@%d.%d", d.a, d.b)
		}
	case opImport:
		s.WriteString(c.src.NodeText(c.at[n]))
	case opCall:
		c.list(s, "(", ")", "", cnode(d.a), cnode(d.b))
	case opCallN:
		var ns []cnode
		for _, x := range c.range_(n) {
			ns = append(ns, cnode(x))
		}
		c.list(s, "(", ")", "", ns...)
	case opAdd, opSub, opMul, opAppend, opPrepend, opConcat, patCons, patSnoc:
		c.list(s, "("+opStrings[o]+" ", ")", "", cnode(d.a), cnode(d.b))
	case opCompose:
		first, second, u := c.compose(n)
		s.WriteString("(>>")
		c.uses_(s, u)
		c.list(s, " ", ")", "", first, second)
	case opLambda:
		size, u, arity, arms := c.lambda(n)
		fmt.Fprintf(s, "(fn/%d", size)
		c.uses_(s, u)
		for i := 0; i < len(arms); i += arity + 1 {
			s.WriteString(" |")
			for _, pat := range arms[i : i+arity] {
				s.WriteString(" ")
				c.dump(s, cnode(pat))
				s.WriteString(" ->")
			}
			s.WriteString(" ")
			c.dump(s, cnode(arms[i+arity]))
		}
		s.WriteString(")")
	case opWhere:
		body, binds := c.where(n)
		s.WriteString("(")
		c.dump(s, body)
		for i := 0; i < len(binds); i += 2 {
			fmt.Fprintf(s, " ; >%d ", binds[i])
			c.dump(s, cnode(binds[i+1]))
		}
		s.WriteString(")")
	case opRecord, patRecord:
		rest, sh, fields := c.record(n)
		s.WriteString("{")
		for i := 0; i < len(fields); i += 2 {
			if i > 0 {
				s.WriteString(",")
			}
			key := label.ID(fields[i])
			if sh != nil {
				key = sh.labels[fields[i]]
			}
			fmt.Fprintf(s, " %s = ", c.labels.Name(key))
			c.dump(s, cnode(fields[i+1]))
		}
		if rest != none {
			s.WriteString(" ..")
			c.dump(s, rest)
		}
		s.WriteString(" }")
	case opAccess:
		c.dump(s, cnode(d.a))
		s.WriteString("." + c.labels.Name(label.ID(d.b)))
	case opList, patList:
		c.list(s, "[", "]", ", ", nodes(c.range_(n))...)
	case opVariant, patVariant:
		if cnode(d.b) == none {
			s.WriteString("#" + c.labels.Name(label.ID(d.a)))
		} else {
			c.list(s, "(#"+c.labels.Name(label.ID(d.a))+" ", ")", "", cnode(d.b))
		}
	case patBind:
		fmt.Fprintf(s, ">%d", d.a)
	case patWild:
		s.WriteString("_")
	case patPrefix:
		rest, elems := c.elems(n)
		s.WriteString("(++ ")
		c.list(s, "[", "]", ", ", nodes(elems)...)
		s.WriteString(" ")
		c.dump(s, rest)
		s.WriteString(")")
	case patSuffix:
		rest, elems := c.elems(n)
		s.WriteString("(++ ")
		c.dump(s, rest)
		s.WriteString(" ")
		c.list(s, "[", "]", ", ", nodes(elems)...)
		s.WriteString(")")
	default:
		fmt.Fprintf(s, "?%d", o)
	}
}

func nodes(xs []int32) []cnode {
	ns := make([]cnode, len(xs))
	for i, x := range xs {
		ns[i] = cnode(x)
	}
	return ns
}

// list writes ns between open and close, separated by sep, or by spaces.
func (c *core) list(s *strings.Builder, open, close, sep string, ns ...cnode) {
	if sep == "" {
		sep = " "
	}
	s.WriteString(open)
	for i, n := range ns {
		if i > 0 {
			s.WriteString(sep)
		}
		c.dump(s, n)
	}
	s.WriteString(close)
}

// uses_ writes the free names of uses u, and its home.
func (c *core) uses_(s *strings.Builder, u int32) {
	us := c.uses[u]
	for _, r := range us.free {
		if r.b < 0 {
			s.WriteString(" ^" + c.src.NodeText(r.ident))
		} else if b := c.binders[r.b]; b.slot < 0 {
			fmt.Fprintf(s, " ^%s@%d", b.name, r.depth)
		} else {
			fmt.Fprintf(s, " ^%s@%d.%d", b.name, r.depth, b.slot)
		}
	}
	if us.home >= 0 {
		s.WriteString(" home=" + c.binders[us.home].name)
	}
}

func compileString(t *testing.T, source string) *core {
	t.Helper()
	se, err := parser.ParseExpr(source)
	if err != nil {
		t.Fatalf("parse %q: %v", source, err)
	}
	var reg types.Registry
	_, globals := bindBuiltIns(&reg)
	c, err := compile(&se, globals, newShapeTable(reg.Labels()))
	if err != nil {
		t.Fatalf("compile %q: %v", source, err)
	}
	return c
}

func TestCompile(t *testing.T) {
	tests := []struct {
		source, core string
		top          int
	}{
		{`1 + 2 * 3`, `(+ 1 (* 2 3))`, 0},
		// Pipes are calls, builtins are constants.
		{`[1] |> list/length`, `(list/length [1])`, 0},
		{`list/length <| [1]`, `(list/length [1])`, 0},
		// Where-bindings take slots in the frame, in evaluation order.
		{`a + b ; a = 1 ; b = a`, `((+ @0 @1) ; >0 1 ; >1 @0)`, 2},
		{`b ; b = a ; a = 1`, `(@0 ; >1 1 ; >0 @1)`, 2},
		// A function has its own frame; the outer one is one out.
		{`f 2 ; f = x -> x + n ; n = 1`,
			`((@0 2) ; >1 1 ; >0 (fn/1 ^n@0.1 home=f | >0 -> (+ @0 @1.1)))`, 2},
		// Recursion refers to the frame the function is made in.
		{`f ; f = | 0 -> 1 | n -> n * f (n - 1)`,
			`(@0 ; >0 (fn/1 ^f@0.0 home=f | 0 -> 1 | >0 -> (* @0 (@1.0 (- @0 1)))))`, 1},
		// A where in a function body takes slots in the function's frame.
		{`x -> (y ; y = x)`, `(fn/2 | >0 -> (@1 ; >1 @0))`, 0},
		// The arms of a match share slots; the frame is the largest arm.
		{`| #a x -> x | #b { p = y, q = z } -> y + z`,
			`(fn/3 | (#a >1) -> @1 | (#b { p = >1, q = >2 }) -> (+ @1 @2))`, 0},
		// A where in an arm takes slots after the arm's pattern; the next arm
		// reuses them.
		{`| #a x -> (y ; y = x) | #b z -> z`,
			`(fn/3 | (#a >1) -> (@2 ; >2 @1) | (#b >1) -> @1)`, 0},
		// An inner function's free names are free in the outer one too,
		// one frame further out; names bound inside it are not.
		{`(a -> (g ; g = b -> a + b + n)) ; n = 1`,
			`((fn/2 ^n@0.0 | >0 -> (@1 ; >1 (fn/1 ^a@0.0 ^n@1.0 home=g | >0 -> (+ (+ @1.0 @0) @2.0)))) ; >0 1)`, 1},
		// A chain of lambdas is one function of all their arguments.
		{`(a -> b -> a + b + n) ; n = 1`,
			`((fn/2 ^n@0.0 | >0 -> >1 -> (+ (+ @0 @1) @1.0)) ; >0 1)`, 1},
		// A parameter shadows a where-binding.
		{`(x -> x) ; x = 1`, `((fn/1 | >0 -> @0) ; >0 1)`, 1},
		// Composition.
		{`f >> g ; f = x -> x ; g = x -> x`,
			`((>> ^f@0.0 ^g@0.1 @0 @1) ; >0 (fn/1 home=f | >0 -> @0) ; >1 (fn/1 home=g | >0 -> @0))`, 2},
		{`g << f ; f = x -> x ; g = x -> x`,
			`((>> ^f@0.0 ^g@0.1 @0 @1) ; >0 (fn/1 home=f | >0 -> @0) ; >1 (fn/1 home=g | >0 -> @0))`, 2},
		// Builtins are free names too.
		{`xs -> list/map (x -> x) xs`,
			`(fn/1 ^list/map | >0 -> (list/map (fn/1 | >0 -> @0) @0))`, 0},
		// Patterns.
		{`| [] -> 0 | [a, _] -> a | h >+ t -> h | i +< l -> l`,
			`(fn/3 | [] -> 0 | [>1, _] -> @1 | (>+ >1 >2) -> @1 | (+< >1 >2) -> @2)`, 0},
		{`| [a] ++ r -> a | r ++ [a] -> a | { ..r, a = x } -> x`,
			`(fn/3 | (++ [>1] >2) -> @1 | (++ >1 [>2]) -> @2 | { a = >1 ..>2 } -> @1)`, 0},
		// Values.
		{`{ ..r, a = 1 }.a ; r = { a = 2 }`, `({ a = 1 ..@0 }.a ; >0 { a = 2 })`, 1},
		{`$"lib.scrap"`, `$"lib.scrap"`, 0},
		{`#a (#b ())`, `(#a (#b ()))`, 0},
		{`"a" ++ "b"`, `(++ "a" "b")`, 0},
	}
	for _, tt := range tests {
		c := compileString(t, tt.source)
		var s strings.Builder
		c.dump(&s, c.root)
		if s.String() != tt.core {
			t.Errorf("%s\n got: %s\nwant: %s", tt.source, s.String(), tt.core)
		}
		if c.top != tt.top {
			t.Errorf("%s: top frame has %d slots, want %d", tt.source, c.top, tt.top)
		}
	}
}

// A builtin is one constant, however often it is referred to.
func TestCompileBuiltinsOnce(t *testing.T) {
	c := compileString(t, `list/map f (list/map f []) ; f = x -> x`)
	if len(c.consts) != 1 {
		t.Errorf("got consts %v, want only list/map", c.consts)
	}
}

// The free type names of annotations and sort bindings, for printing.
func TestCompileTypes(t *testing.T) {
	c := compileString(t, `f
; f : a => t a -> int = _ -> 1
; t : type -> type = a => [#x a, #y (t a), #z u]
; u : type = { n : int }`)
	want := map[string]string{
		"f": " ^t@0 ^int",
		"t": " ^type ^type ^t@0 ^u@0",
		"u": " ^type ^int",
	}
	for _, b := range c.binders {
		var s strings.Builder
		c.uses_(&s, b.types)
		if s.String() != want[b.name] {
			t.Errorf("%s: got%q, want%q", b.name, s.String(), want[b.name])
		}
	}
}

func TestCompileErrors(t *testing.T) {
	tests := []struct{ source, err string }{
		{`| [a, a] -> a`, "cannot bind a twice"},
		{`| 1.0 -> 1`, "cannot match on floats"},
		{`nope`, "unknown variable nope"},
		{`| a + b -> a`, "unsupported pattern"},
	}
	for _, tt := range tests {
		se, err := parser.ParseExpr(tt.source)
		if err != nil {
			t.Fatal(err)
		}
		var reg types.Registry
		_, globals := bindBuiltIns(&reg)
		_, err = compile(&se, globals, newShapeTable(reg.Labels()))
		if err == nil || !strings.Contains(err.Error(), tt.err) {
			t.Errorf("%s: got %v, want %q", tt.source, err, tt.err)
		}
	}
}
