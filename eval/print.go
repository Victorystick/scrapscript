package eval

import (
	"encoding/base64"
	"strconv"
	"strings"

	"github.com/Victorystick/scrapscript/ast"
	"github.com/Victorystick/scrapscript/label"
	"github.com/Victorystick/scrapscript/printer"
	"github.com/Victorystick/scrapscript/scanner"
)

// printValue returns a scrap that evaluates to v and prints the same, as SPEC.md
// section 10, rule 6, requires, with the names of labels in labels.
//
// A function prints as its code, with the bindings that the code refers to
// in a where-clause after the value. Those are printed the same way, so
// they are all the bindings that the value reaches, recursive ones
// included. A function that a binding defines prints as that binding's
// name. Bindings keep their annotations, and the sort bindings that those
// refer to are printed too, so that recursive functions still check.
//
// The bindings come from many scopes, but print as one where-group, so a
// name that is already taken gets primes: a, a', a”.
func printValue(labels *label.Table, v Value) string {
	p := valuePrinter{
		labels: labels,
		byKey:  make(map[bindingKey]*binding),
		refs:   make(map[srcNode]*binding),
		names:  make(map[string]bool),
		idents: make(map[string]bool),
	}
	p.collect(v)
	p.name()
	return p.print(v)
}

// A binding is a variable, or a sort binding, that a printed value refers
// to and so binds.
type binding struct {
	bindingKey
	prog    *program // whose binders key.b is in
	value   Value    // nil for a sort binding
	printed string   // its name in the printed scrap
}

// A bindingKey is a binder of the frame that binds it.
type bindingKey struct {
	frame *frame
	b     int32 // the index in binders of the program that frame belongs to
}

type srcNode struct {
	src  *ast.SourceExpr
	node ast.Node
}

type valuePrinter struct {
	labels   *label.Table
	bindings []*binding // in the order that they are found
	byKey    map[bindingKey]*binding
	// The bindings that the free Ident nodes of printed code refer to.
	refs map[srcNode]*binding
	// The names that print as themselves without being bound, builtins.
	names map[string]bool
	// The names of every Ident in printed code.
	idents map[string]bool
}

func (b *binding) binder() binder {
	return b.prog.binders[b.b]
}

// uses returns the index in sf.prog.uses of what sf's code refers to.
func (sf ScriptFunc) uses() int32 {
	if sf.composed != nil {
		_, _, u := sf.prog.compose(sf.node)
		return u
	}
	_, u, _ := sf.prog.lambda(sf.node)
	return u
}

// home returns the key of the binding that defines sf, if its value is sf
// itself.
func home(sf ScriptFunc) (bindingKey, bool) {
	h := sf.prog.uses[sf.uses()].home
	return bindingKey{sf.env, h}, h >= 0
}

// collect finds the bindings that v refers to.
func (p *valuePrinter) collect(v Value) {
	switch v := v.(type) {
	case Record:
		for _, val := range v.values {
			p.collect(val)
		}
	case List:
		for _, elem := range v.elements {
			p.collect(elem)
		}
	case Variant:
		if v.value != nil {
			p.collect(v.value)
		}
	case BuiltInFunc:
		p.names[v.name] = true
	case Partial:
		p.names[v.name] = true
		for _, arg := range v.args {
			p.collect(arg)
		}
	case ScriptFunc:
		if v.ref != "" {
			return
		}
		if key, ok := home(v); ok {
			p.need(v.prog, key)
			return
		}
		p.code(v.prog, v.env, v.uses())
	}
}

// need returns the binding for key, a binder of prog, finding what it
// refers to the first time.
func (p *valuePrinter) need(prog *program, key bindingKey) *binding {
	if b := p.byKey[key]; b != nil {
		return b
	}
	b := &binding{bindingKey: key, prog: prog}
	p.byKey[key] = b
	p.bindings = append(p.bindings, b)

	bd := b.binder()
	if bd.where != ast.None {
		// Its annotation, or a sort binding's type.
		p.code(prog, key.frame, bd.types)
		if bd.sort {
			return b
		}
	}

	b.value = key.frame.slots[bd.slot]
	if p.defines(b) {
		p.code(prog, key.frame, b.value.(ScriptFunc).uses())
	} else {
		p.collect(b.value)
	}
	return b
}

// defines returns true if b's value is a function that b defines, which
// prints as its code.
func (p *valuePrinter) defines(b *binding) bool {
	sf, ok := b.value.(ScriptFunc)
	if !ok || sf.ref != "" || sf.prog != b.prog {
		return false
	}
	key, ok := home(sf)
	return ok && key == b.bindingKey
}

// code finds the bindings that the code with uses u, in frame f of prog,
// refers to.
func (p *valuePrinter) code(prog *program, f *frame, u int32) {
	us := prog.uses[u]
	for _, name := range us.names {
		p.idents[name] = true
	}
	for _, r := range us.free {
		if r.b < 0 {
			p.names[prog.src.TokenText(prog.src.Main(r.ident))] = true
			continue
		}
		bf := f
		for range r.depth {
			bf = bf.parent
		}
		p.refs[srcNode{prog.src, r.ident}] = p.need(prog, bindingKey{bf, r.b})
	}
}

// name names the bindings: each by its own name, unless an earlier binding
// or a builtin has it. Then it gets primes, up to a name that no code uses,
// so that no binder in the code shadows it.
func (p *valuePrinter) name() {
	taken := make(map[string]bool, len(p.bindings))
	for _, b := range p.bindings {
		own := b.binder().name
		name := own
		for p.names[name] || taken[name] || name != own && p.idents[name] {
			name += "'"
		}
		taken[name] = true
		b.printed = name
	}
}

func (p *valuePrinter) print(v Value) string {
	var s strings.Builder
	p.value(&s, v, printer.Expr)
	for _, b := range p.bindings {
		s.WriteString("\n; ")
		s.WriteString(b.printed)
		bd := b.binder()
		if bd.where == ast.None {
			s.WriteString(" = ")
			p.value(&s, b.value, printer.Body)
			continue
		}
		pr := p.printer(b.prog.src)
		_, _, typ, val := b.prog.src.Where(bd.where)
		if bd.sort || p.defines(b) {
			pr.Binding(typ, val, bd.sort)
			s.WriteString(pr.String())
			continue
		}
		if typ != ast.None {
			pr.WriteString(" : ")
			pr.Type(typ)
			s.WriteString(pr.String())
		}
		s.WriteString(" = ")
		p.value(&s, b.value, printer.Body)
	}
	return s.String()
}

// printer returns a Printer for code in src, which prints free names as
// the names of the bindings that they refer to.
func (p *valuePrinter) printer(src *ast.SourceExpr) *printer.Printer {
	return &printer.Printer{Src: src, Name: func(x ast.Node) string {
		if b := p.refs[srcNode{src, x}]; b != nil {
			return b.printed
		}
		return ""
	}}
}

// value prints v in ctx.
func (p *valuePrinter) value(s *strings.Builder, v Value, ctx printer.Context) {
	switch v := v.(type) {
	case Hole:
		s.WriteString("()")
	case Int:
		s.WriteString(strconv.FormatInt(int64(v), 10))
	case Float:
		f := strconv.FormatFloat(float64(v), 'f', -1, 64)
		s.WriteString(f)
		// A FLOAT has digits on both sides of its point.
		if strings.IndexByte(f, '.') < 0 {
			s.WriteString(".0")
		}
	case Text:
		s.WriteString(scanner.Quote(string(v)))
	case Byte:
		const chars = "0123456789ABCDEF"
		s.Write([]byte{'~', chars[v>>4], chars[v&0xf]})
	case Bytes:
		s.WriteString("~~")
		s.WriteString(base64.StdEncoding.EncodeToString(v))
	case Record:
		if len(v.values) == 0 {
			s.WriteString("{}")
			return
		}
		s.WriteString("{ ")
		for i, key := range v.shape.labels {
			if i > 0 {
				s.WriteString(", ")
			}
			s.WriteString(p.labels.Name(key))
			s.WriteString(" = ")
			p.value(s, v.values[i], printer.Expr)
		}
		s.WriteString(" }")
	case List:
		if len(v.elements) == 0 {
			s.WriteString("[]")
			return
		}
		s.WriteString("[ ")
		for i, elem := range v.elements {
			if i > 0 {
				s.WriteString(", ")
			}
			p.value(s, elem, printer.Expr)
		}
		s.WriteString(" ]")
	case Variant:
		if v.value == nil {
			s.WriteString("#" + p.labels.Name(v.tag))
			return
		}
		p.app(s, ctx, "#"+p.labels.Name(v.tag), v.value)
	case BuiltInFunc:
		s.WriteString(v.name)
	case Partial:
		p.app(s, ctx, v.name, v.args...)
	case ScriptFunc:
		if v.ref != "" {
			s.WriteString(v.ref)
		} else if key, ok := home(v); ok {
			s.WriteString(p.byKey[key].printed)
		} else {
			pr := p.printer(v.prog.src)
			pr.Expr(v.prog.at[v.node], ctx)
			s.WriteString(pr.String())
		}
	}
}

// app prints head applied to args, in parentheses if ctx is an argument.
func (p *valuePrinter) app(s *strings.Builder, ctx printer.Context, head string, args ...Value) {
	if ctx == printer.Arg {
		s.WriteByte('(')
		defer s.WriteByte(')')
	}
	s.WriteString(head)
	for _, arg := range args {
		s.WriteByte(' ')
		p.value(s, arg, printer.Arg)
	}
}
