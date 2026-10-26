// Package printer prints syntax trees as source text that parses back to the
// same tree. It adds the parentheses that the parser needs, mirroring its
// precedence rules.
package printer

import (
	"io"
	"strings"

	"github.com/Victorystick/scrapscript/ast"
	"github.com/Victorystick/scrapscript/token"
)

// A Context is the position that an expression is printed in, which decides
// whether it needs parentheses. It mirrors what the parser accepts there.
type Context struct {
	kind ctxKind
	min  int  // the loosest binary operator accepted, for ctxBinary
	open bool // whether a function or match function may appear
	arm  bool // whether in a match arm, where neither may appear
	// The precedence of the binary operator that follows, or 0 for none.
	// A function would take it into its body.
	follow int
}

type ctxKind uint8

const (
	ctxExpr    ctxKind = iota // anything, where-clauses included
	ctxBinary                 // binary operators of precedence min or more
	ctxArg                    // a call's argument or a tag's payload
	ctxFn                     // a called function
	ctxPostfix                // a record whose field is accessed
)

var (
	// Expr accepts any expression: the top level, list items, field values
	// and the inside of parentheses.
	Expr = Context{kind: ctxExpr}
	// Body accepts any expression but a where-clause: a binding's value.
	Body = Context{kind: ctxBinary, min: 1, open: true}
	// Arg accepts an atom, a field access or a tag: a call's argument.
	Arg = Context{kind: ctxArg}
)

// armPrec is the loosest binary operator in a match arm.
var armPrec = token.LT.Precedence()

// openPrec is the loosest precedence after which a function may appear, as
// the last operand of a pipe.
var openPrec = token.RCOMP.Precedence()

// A form is what a node looks like to the parser.
type form uint8

const (
	formWhere form = iota
	formFunc
	formMatch
	formBinary
	formApp     // a call, or a tag with a payload
	formTag     // a tag without a payload
	formPostfix // an atom, or a field access
)

func (p *Printer) form(x ast.Node) form {
	switch p.Src.Kind(x) {
	case ast.Where:
		return formWhere
	case ast.Func:
		return formFunc
	case ast.MatchFunc:
		return formMatch
	case ast.Binary:
		return formBinary
	case ast.Call:
		return formApp
	case ast.Variant:
		if _, payload := p.Src.Variant(x); payload != ast.None {
			return formApp
		}
		return formTag
	}
	return formPostfix
}

// parens returns true if x needs parentheses in ctx.
func (p *Printer) parens(x ast.Node, ctx Context) bool {
	f := p.form(x)
	switch ctx.kind {
	case ctxExpr:
		return false
	case ctxBinary:
		switch f {
		case formWhere:
			return true
		case formFunc:
			return !ctx.open || ctx.follow != 0
		case formMatch:
			return !ctx.open || ctx.follow >= armPrec
		case formBinary:
			op, _, _ := p.Src.Binary(x)
			return op.Precedence() < ctx.min
		}
		return false
	case ctxArg:
		return f != formPostfix && f != formTag
	case ctxFn:
		return f != formPostfix && f != formApp ||
			f == formApp && p.Src.Kind(x) == ast.Variant
	case ctxPostfix:
		return f != formPostfix
	}
	return false
}

// A Printer prints the nodes of one SourceExpr.
type Printer struct {
	strings.Builder
	Src *ast.SourceExpr
	// Name returns the name to print for an Ident node, or "" for its own.
	// It may be nil.
	Name func(ast.Node) string

	indent int
}

// Fprint prints expr, which is se.Expr or one of its descendants.
func Fprint(w io.Writer, se *ast.SourceExpr, expr ast.Node) error {
	p := Printer{Src: se}
	p.Top(expr)
	_, err := io.WriteString(w, p.String())
	return err
}

// Top prints x on lines of their own: each binding of a where-clause, and
// each arm of a match function that is the body or a binding's value.
func (p *Printer) Top(x ast.Node) {
	body, binds := p.Src.Group(x)
	p.Stmt(body)
	for i := 0; i < len(binds); i++ {
		b := binds[i]
		_, id, typ, val := p.Src.Where(b)
		p.newline()
		p.WriteString("; ")
		if n := p.destructure(binds[i:]); n > 0 {
			i += n
		} else {
			p.WriteString(p.Src.TokenText(id))
		}
		p.Binding(typ, val, p.Src.Sort(b))
	}
}

// destructure prints the pattern of a destructuring binding, `{ a, b = c }`,
// if binds starts with one, and returns the number of bindings of its
// fields that follow, which it prints as well. The parser puts them right
// after the binding of the record.
func (p *Printer) destructure(binds []ast.Node) int {
	se := p.Src
	brace := se.Main(binds[0])
	if se.Tokens.Kinds[brace] != token.LBRACE {
		return 0
	}
	n := 0
	p.WriteString("{ ")
	for _, b := range binds[1:] {
		if t, ok := se.Destructured(b); !ok || t != brace || se.Main(b) == brace {
			break
		}
		_, id, _, val := se.Where(b)
		_, key := se.Access(val)
		if n > 0 {
			p.WriteString(", ")
		}
		p.token(key)
		if se.TokenText(key) != se.TokenText(id) {
			p.WriteString(" = ")
			p.token(id)
		}
		n++
	}
	p.WriteString(" }")
	return n
}

// Stmt prints a value that is on lines of its own, whose arms each go on
// a line if it is a match function.
func (p *Printer) Stmt(x ast.Node) {
	if p.Src.Kind(x) != ast.MatchFunc {
		p.Expr(x, Body)
		return
	}
	for i, arm := range p.Src.Arms(x) {
		if i > 0 {
			p.newline()
		}
		p.arm(arm)
	}
}

// Binding prints the rest of a binding after its name: its annotation typ,
// which may be None, and its value val, which is a type if sort is set.
func (p *Printer) Binding(typ, val ast.Node, sort bool) {
	if typ != ast.None {
		p.WriteString(" : ")
		p.Type(typ)
	}
	p.WriteString(" =")
	if sort {
		p.WriteByte(' ')
		p.Type(val)
	} else if p.Src.Kind(val) == ast.MatchFunc {
		p.indent++
		p.newline()
		p.Stmt(val)
		p.indent--
	} else {
		p.WriteByte(' ')
		p.Expr(val, Body)
	}
}

func (p *Printer) newline() {
	p.WriteByte('\n')
	for range p.indent {
		p.WriteString("  ")
	}
}

func (p *Printer) token(t ast.Tok) {
	p.WriteString(p.Src.TokenText(t))
}

// punned returns true if the field key, whose value is x, was punned in
// the source, `{ x }`, and x still prints as the key.
func (p *Printer) punned(key ast.Tok, x ast.Node) bool {
	if p.Src.Kind(x) != ast.Ident || p.Src.Main(x) != key {
		return false
	}
	if p.Name == nil {
		return true
	}
	name := p.Name(x)
	return name == "" || name == p.Src.TokenText(key)
}

// Expr prints x on one line, in parentheses if ctx needs them.
func (p *Printer) Expr(x ast.Node, ctx Context) {
	if p.parens(x, ctx) {
		p.WriteByte('(')
		p.expr(x, Expr)
		p.WriteByte(')')
		return
	}
	p.expr(x, ctx)
}

func (p *Printer) expr(x ast.Node, ctx Context) {
	se := p.Src
	switch se.Kind(x) {
	case ast.Ident:
		if p.Name != nil {
			if name := p.Name(x); name != "" {
				p.WriteString(name)
				return
			}
		}
		p.token(se.Main(x))

	case ast.Literal, ast.Import:
		p.WriteString(se.NodeText(x))

	case ast.Binary:
		op, left, right := se.Binary(x)
		prec := op.Precedence()
		lmin, rmin := prec, prec+1
		if op.RightAssoc() {
			lmin, rmin = prec+1, prec
		}
		if ctx.kind != ctxBinary {
			ctx = Body
		}
		// The left operand starts where x does, and the right one is parsed
		// anew, so only it may be a function if it follows a pipe.
		p.Expr(left, Context{kind: ctxBinary, min: lmin, open: ctx.open, arm: ctx.arm, follow: prec})
		p.WriteByte(' ')
		p.WriteString(op.Op())
		p.WriteByte(' ')
		open := !ctx.arm && rmin <= openPrec
		p.Expr(right, Context{kind: ctxBinary, min: rmin, open: open, arm: ctx.arm, follow: ctx.follow})

	case ast.Func:
		arg, body := se.Func(x)
		p.Expr(arg, Arg)
		p.WriteString(" -> ")
		p.Expr(body, Body)

	case ast.MatchFunc:
		for i, arm := range se.Arms(x) {
			if i > 0 {
				p.WriteByte(' ')
			}
			p.arm(arm)
		}

	case ast.Call:
		fn, arg := se.Call(x)
		p.Expr(fn, Context{kind: ctxFn})
		p.WriteByte(' ')
		p.Expr(arg, Arg)

	case ast.Variant:
		tag, payload := se.Variant(x)
		p.WriteByte('#')
		p.token(tag)
		if payload != ast.None {
			p.WriteByte(' ')
			p.Expr(payload, Arg)
		}

	case ast.Record:
		rest, fields := se.Record(x)
		if rest == ast.None && fields.Len() == 0 {
			p.WriteString("{}")
			return
		}
		p.WriteString("{ ")
		if rest != ast.None {
			p.WriteString("..")
			p.Expr(rest, Expr)
			p.WriteString(", ")
		}
		for i := range fields.Len() {
			if i > 0 {
				p.WriteString(", ")
			}
			p.token(fields.Key(i))
			if v := fields.Value(i); !p.punned(fields.Key(i), v) {
				p.WriteString(" = ")
				p.Expr(v, Expr)
			}
		}
		p.WriteString(" }")

	case ast.Access:
		rec, key := se.Access(x)
		p.Expr(rec, Context{kind: ctxPostfix})
		p.WriteByte('.')
		p.token(key)

	case ast.List:
		elems := se.List(x)
		if len(elems) == 0 {
			p.WriteString("[]")
			return
		}
		p.WriteString("[ ")
		for i, elem := range elems {
			if i > 0 {
				p.WriteString(", ")
			}
			p.Expr(elem, Expr)
		}
		p.WriteString(" ]")

	case ast.Where:
		body, binds := se.Group(x)
		p.Expr(body, Body)
		for i := 0; i < len(binds); i++ {
			b := binds[i]
			_, id, typ, val := se.Where(b)
			p.WriteString(" ; ")
			if n := p.destructure(binds[i:]); n > 0 {
				i += n
			} else {
				p.token(id)
			}
			if typ != ast.None {
				p.WriteString(" : ")
				p.Type(typ)
			}
			p.WriteString(" = ")
			if se.Sort(b) {
				p.Type(val)
			} else {
				p.Expr(val, Body)
			}
		}

	default:
		p.WriteString("<" + se.Kind(x).String() + ">")
	}
}

// arm prints a match function's arm, with one pattern per argument.
func (p *Printer) arm(arm ast.Node) {
	patterns := Context{kind: ctxBinary, min: armPrec, arm: true}
	p.WriteString("| ")
	pats, body := p.Src.Arm(arm)
	for _, pat := range pats {
		p.Expr(pat, patterns)
		p.WriteString(" -> ")
	}
	p.Expr(body, patterns)
}

// Type precedences, loosest first.
const (
	typeTop   = iota // a => t
	typeArrow        // a -> b
	typeApp          // f a
	typeAtom
)

func (p *Printer) typePrec(x ast.Node) int {
	switch p.Src.Kind(x) {
	case ast.Forall:
		return typeTop
	case ast.Func:
		return typeArrow
	case ast.Call:
		return typeApp
	}
	return typeAtom
}

// Type prints the type x.
func (p *Printer) Type(x ast.Node) {
	p.typ(x, typeTop)
}

func (p *Printer) typ(x ast.Node, min int) {
	if p.typePrec(x) < min {
		p.WriteByte('(')
		defer p.WriteByte(')')
	}
	se := p.Src
	switch se.Kind(x) {
	case ast.Forall:
		param, t := se.Forall(x)
		p.token(param)
		p.WriteString(" => ")
		p.typ(t, typeTop)

	case ast.Func:
		arg, res := se.Func(x)
		p.typ(arg, typeApp)
		p.WriteString(" -> ")
		p.typ(res, typeArrow)

	case ast.Call:
		fn, arg := se.Call(x)
		p.typ(fn, typeApp)
		p.WriteByte(' ')
		p.typ(arg, typeAtom)

	case ast.RecordType:
		tail, fields := se.Record(x)
		p.row('{', '}', fields.Len(), tail, func(i int) {
			p.token(fields.Key(i))
			p.WriteString(" : ")
			p.Type(fields.Value(i))
		})

	case ast.Enum:
		tail, variants := se.Enum(x)
		p.row('[', ']', len(variants), tail, func(i int) {
			tag, payload := se.Variant(variants[i])
			p.WriteByte('#')
			p.token(tag)
			if payload != ast.None {
				p.WriteByte(' ')
				p.typ(payload, typeAtom)
			}
		})

	default:
		p.expr(x, Expr)
	}
}

// row prints the n labels of a record or enum type, and its tail if it has
// one.
func (p *Printer) row(open, close byte, n int, tail ast.Node, label func(int)) {
	p.WriteByte(open)
	if n == 0 && tail == ast.None {
		p.WriteByte(close)
		return
	}
	p.WriteByte(' ')
	for i := range n {
		if i > 0 {
			p.WriteString(", ")
		}
		label(i)
	}
	if tail != ast.None {
		if n > 0 {
			p.WriteString(", ")
		}
		p.WriteString("..")
		p.expr(tail, Expr)
	}
	p.WriteByte(' ')
	p.WriteByte(close)
}
