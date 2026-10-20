package eval

import (
	"encoding/base64"
	"slices"
	"strconv"
	"strings"

	"github.com/Victorystick/scrapscript/ast"
	"github.com/Victorystick/scrapscript/label"
	"github.com/Victorystick/scrapscript/printer"
	"github.com/Victorystick/scrapscript/scanner"
	"github.com/Victorystick/scrapscript/token"
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
// A lambda's code takes the values of its free names from its captures. A
// binder with the same value, whichever closure captured it, is one
// binding: values that are the same, with functions the same closure.
//
// A record that a binding's value is the literal of keeps its type
// members, which the checker found in the binding's static part. A binding
// that only types refer to, as avl in avl.tree, prints as its static part:
// an import as itself, a record literal as its members.
//
// The bindings come from many scopes, but print as one where-group, so a
// name that is already taken gets primes: a, a', a”.
func printValue(labels *label.Table, v Value) string {
	p := newValuePrinter(labels)
	p.collect(v)
	p.merge()
	p.name()
	return p.print(v)
}

// printScrap prints v, the value of prog, as printValue does, with the
// members of prog's static part. An import's are those of its scrap.
func printScrap(labels *label.Table, v Value, prog *program) string {
	x := prog.src.Expr
	for prog.src.Kind(x) == ast.Import {
		prog = prog.imported(x)
		x = prog.src.Expr
	}
	u := prog.rootStatic
	if prog.src.Kind(x) == ast.Where {
		u = prog.whereUses(x)
	}
	if u < 0 {
		return printValue(labels, v)
	}
	p := newValuePrinter(labels)
	p.collect(v)
	if prog.rootStatic >= 0 {
		p.refs(prog, prog.rootStatic, nil)
	}
	wheres(prog, x, func(u int32) { p.refs(prog, u, nil) })
	p.merge()
	p.name()
	var s strings.Builder
	p.withMembers(&s, prog, p.staticNames(prog, prog.rootStatic, x), x, v)
	p.where(&s)
	return s.String()
}

// A binding is a variable, or a sort binding, that a printed value refers
// to and so binds.
type binding struct {
	binderKey
	// nil for a sort binding, and for a variable that only types refer to,
	// which prints as its static part
	value   Value
	printed string // its name in the printed scrap
	// A variable's binding with a value that has its members, which one
	// that only types refer to prints as instead.
	merged *binding
}

// A binderKey is a binder of a program.
type binderKey struct {
	prog *program
	b    int32 // the index in binders of prog
}

func newValuePrinter(labels *label.Table) *valuePrinter {
	return &valuePrinter{
		labels:   labels,
		byBinder: make(map[binderKey][]*binding),
		names:    make(map[string]bool),
		idents:   make(map[string]bool),
	}
}

type valuePrinter struct {
	labels   *label.Table
	bindings []*binding // in the order that they are found
	// The bindings of each binder, one per value.
	byBinder map[binderKey][]*binding
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
	if sf.composition() {
		_, _, u := sf.prog.compose(sf.node)
		return u
	}
	_, u, _, _ := sf.prog.lambda(sf.node)
	return u
}

// home returns the binder that defines sf, if its value is sf itself.
func home(sf ScriptFunc) (binderKey, bool) {
	h := sf.prog.uses[sf.uses()].home
	return binderKey{sf.prog, h}, h >= 0 && sf.ref == ""
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
		p.names[v.fn.name] = true
		for _, arg := range v.args {
			p.collect(arg)
		}
	case ScriptFunc:
		for _, arg := range v.args {
			p.collect(arg)
		}
		v.args = nil
		if v.ref != "" {
			return
		}
		if key, ok := home(v); ok {
			p.need(key, v)
			return
		}
		p.code(v)
	}
}

// find returns the binding of key with the value v, or nil.
func (p *valuePrinter) find(key binderKey, v Value) *binding {
	for _, b := range p.byBinder[key] {
		if same(b.value, v) {
			return b
		}
	}
	return nil
}

// need returns the binding of key with the value v, nil for a sort
// binding, finding what it refers to the first time.
func (p *valuePrinter) need(key binderKey, v Value) *binding {
	if b := p.find(key, v); b != nil {
		return b
	}
	b := &binding{binderKey: key, value: v}
	p.byBinder[key] = append(p.byBinder[key], b)
	p.bindings = append(p.bindings, b)

	bd := b.binder()
	if bd.where != ast.None {
		// Its annotation, or a sort binding's type.
		p.refs(key.prog, bd.types, nil)
		if bd.sort {
			return b
		}
		if bd.static >= 0 {
			_, _, _, val := key.prog.src.Where(bd.where)
			if v == nil || p.members(b) {
				p.refs(key.prog, bd.static, nil)
				wheres(key.prog, val, func(u int32) { p.refs(key.prog, u, nil) })
			}
			if v == nil || key.prog.src.Kind(val) == ast.Import {
				return b
			}
		}
	}
	if p.defines(b) {
		p.code(v.(ScriptFunc))
	} else {
		p.collect(v)
	}
	return b
}

// wheres calls f with the uses of the static part of each where-group in
// the static part of x, which have uses of their own.
func wheres(prog *program, x ast.Node, f func(u int32)) {
	se := prog.src
	switch se.Kind(x) {
	case ast.Record:
		_, fields := se.Record(x)
		for i := range fields.Len() {
			if v := fields.Value(i); prog.static.Has(v) {
				wheres(prog, v, f)
			}
		}
	case ast.Where:
		body, _ := se.Group(x)
		f(prog.whereUses(x))
		wheres(prog, body, f)
	}
}

// staticNames returns the printed names of the free names of the static
// part of x in prog, whose uses are u, or -1 for none.
func (p *valuePrinter) staticNames(prog *program, u int32, x ast.Node) map[ast.Node]string {
	names := make(map[ast.Node]string)
	if u >= 0 {
		p.renames(names, prog, u, nil)
	}
	wheres(prog, x, func(u int32) { p.renames(names, prog, u, nil) })
	return names
}

// members returns true if b's value, with a static part, prints with its
// members: as the import or record literal that it is the value of.
func (p *valuePrinter) members(b *binding) bool {
	bd := b.binder()
	if b.value == nil || bd.static < 0 {
		return false
	}
	_, _, _, val := b.prog.src.Where(bd.where)
	switch b.prog.src.Kind(val) {
	case ast.Import, ast.Record:
		return true
	}
	return false
}

// merge lets the bindings that only types refer to print as a binding of
// the same binder whose value prints with its members, if there is one.
func (p *valuePrinter) merge() {
	kept := p.bindings[:0]
	for _, b := range p.bindings {
		if b.value == nil && !b.binder().sort {
			for _, o := range p.byBinder[b.binderKey] {
				if p.members(o) {
					b.merged = o
					break
				}
			}
		}
		if b.merged == nil {
			kept = append(kept, b)
		}
	}
	p.bindings = kept
}

// defines returns true if b's value is a function that b defines, which
// prints as its code.
func (p *valuePrinter) defines(b *binding) bool {
	sf, ok := b.value.(ScriptFunc)
	if !ok || len(sf.args) > 0 {
		return false
	}
	key, ok := home(sf)
	return ok && key == b.binderKey
}

// code finds the bindings that the code of sf refers to. A composition's
// code is its functions.
func (p *valuePrinter) code(sf ScriptFunc) {
	if sf.composition() {
		p.collect(sf.caps[0])
		p.collect(sf.caps[1])
		return
	}
	p.refs(sf.prog, sf.uses(), sf.caps)
}

// refs finds the bindings that the code with uses u in prog refers to, the
// values of its free names taken from caps.
func (p *valuePrinter) refs(prog *program, u int32, caps []Value) {
	us := prog.uses[u]
	for _, name := range us.names {
		p.idents[name] = true
	}
	for _, r := range us.free {
		if r.b < 0 {
			p.names[prog.src.TokenText(prog.src.Main(r.ident))] = true
			continue
		}
		var v Value
		if r.cap >= 0 {
			v = caps[r.cap]
		}
		p.need(binderKey{prog, r.b}, v)
	}
}

// renames returns the printed names of the free names of the code with
// uses u in prog, by Ident, with their values taken from caps, adding them
// to into.
func (p *valuePrinter) renames(into map[ast.Node]string, prog *program, u int32, caps []Value) map[ast.Node]string {
	if into == nil {
		into = make(map[ast.Node]string)
	}
	for _, r := range prog.uses[u].free {
		if r.b < 0 {
			continue
		}
		var v Value
		if r.cap >= 0 {
			v = caps[r.cap]
		}
		into[r.ident] = p.find(binderKey{prog, r.b}, v).printed
	}
	return into
}

// same returns true if a and b are the same value, as far as printing is
// concerned: equal, with functions the same closure.
func same(a, b Value) bool {
	switch a := a.(type) {
	case nil:
		return b == nil
	case ScriptFunc:
		o, ok := b.(ScriptFunc)
		return ok && a.prog == o.prog && a.node == o.node && a.ref == o.ref &&
			len(a.caps) == len(o.caps) && (len(a.caps) == 0 || &a.caps[0] == &o.caps[0]) &&
			slices.EqualFunc(a.args, o.args, same)
	case Partial:
		o, ok := b.(Partial)
		return ok && a.fn.name == o.fn.name && slices.EqualFunc(a.args, o.args, same)
	case Record:
		o, ok := b.(Record)
		return ok && a.shape == o.shape && slices.EqualFunc(a.values, o.values, same)
	case List:
		o, ok := b.(List)
		return ok && slices.EqualFunc(a.elements, o.elements, same)
	case Variant:
		o, ok := b.(Variant)
		return ok && a.tag == o.tag && same(a.value, o.value)
	}
	return a.eq(b)
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
	for _, bs := range p.byBinder {
		for _, b := range bs {
			if b.merged != nil {
				b.printed = b.merged.printed
			}
		}
	}
}

func (p *valuePrinter) print(v Value) string {
	var s strings.Builder
	p.value(&s, v, printer.Expr)
	p.where(&s)
	return s.String()
}

// where prints the bindings as a where-group.
func (p *valuePrinter) where(s *strings.Builder) {
	for _, b := range p.bindings {
		s.WriteString("\n; ")
		s.WriteString(b.printed)
		bd := b.binder()
		if bd.where == ast.None {
			s.WriteString(" = ")
			p.value(s, b.value, printer.Body)
			continue
		}
		names := p.renames(nil, b.prog, bd.types, nil)
		pr := p.printer(b.prog.src, names)
		_, _, typ, val := b.prog.src.Where(bd.where)
		if p.defines(b) {
			if sf := b.value.(ScriptFunc); !sf.composition() {
				p.renames(names, sf.prog, sf.uses(), sf.caps)
				pr.Binding(typ, val, false)
				s.WriteString(pr.String())
				continue
			}
		} else if bd.sort {
			pr.Binding(typ, val, true)
			s.WriteString(pr.String())
			continue
		}
		if typ != ast.None {
			pr.WriteString(" : ")
			pr.Type(typ)
			s.WriteString(pr.String())
		}
		s.WriteString(" = ")
		v := b.value
		if p.defines(b) {
			// A composition that b defines prints as its functions.
			p.compose(s, v.(ScriptFunc), printer.Body)
			continue
		}
		if v == nil || p.members(b) {
			names := p.staticNames(b.prog, bd.static, val)
			p.withMembers(s, b.prog, names, val, v)
			continue
		}
		p.value(s, v, printer.Body)
	}
}

// withMembers prints v, the value of x in prog, with the members of x's
// static part. An import prints as itself, a record literal as v's fields
// and its members. Without v, x prints as its static part alone. names are
// the printed names of the static part's free names.
func (p *valuePrinter) withMembers(s *strings.Builder, prog *program, names map[ast.Node]string, x ast.Node, v Value) {
	se := prog.src
	switch se.Kind(x) {
	case ast.Record:
		_, fields := se.Record(x)
		r, valued := v.(Record)
		n := 0
		sep := func() {
			if n == 0 {
				s.WriteString("{ ")
			} else {
				s.WriteString(", ")
			}
			n++
		}
		for i := 0; valued && i < len(r.values); i++ {
			key := r.shape.labels[i]
			sep()
			name := p.labels.Name(key)
			s.WriteString(name)
			s.WriteString(" = ")
			if f := field(se, fields, name); f != ast.None && prog.static.Has(f) {
				p.withMembers(s, prog, names, f, r.values[i])
			} else {
				p.value(s, r.values[i], printer.Expr)
			}
		}
		for i := range fields.Len() {
			f := fields.Value(i)
			if !prog.static.Has(f) || valued && r.shape.index(p.labels.Of(se.TokenText(fields.Key(i)))) >= 0 {
				continue
			}
			// A member, which the value leaves out.
			sep()
			s.WriteString(se.TokenText(fields.Key(i)))
			s.WriteString(" = ")
			p.withMembers(s, prog, names, f, nil)
		}
		if n == 0 {
			s.WriteString("{}")
		} else {
			s.WriteString(" }")
		}
	case ast.Where:
		body, _ := se.Group(x)
		p.withMembers(s, prog, names, body, v)
	case ast.Ident, ast.Access, ast.Import:
		if v == nil || se.Kind(x) == ast.Import {
			pr := p.printer(se, names)
			pr.Expr(x, printer.Expr)
			s.WriteString(pr.String())
			return
		}
		fallthrough
	default:
		p.value(s, v, printer.Expr)
	}
}

// field returns the value of the field name of a record literal, or None.
func field(se *ast.SourceExpr, fields ast.Fields, name string) ast.Node {
	for i := range fields.Len() {
		if se.TokenText(fields.Key(i)) == name {
			return fields.Value(i)
		}
	}
	return ast.None
}

// printer returns a Printer for code in src, which prints the free names
// in names as the names of the bindings that they refer to.
func (p *valuePrinter) printer(src *ast.SourceExpr, names map[ast.Node]string) *printer.Printer {
	return &printer.Printer{Src: src, Name: func(x ast.Node) string {
		return names[x]
	}}
}

// compose prints the composition sf as its functions, in its source's
// order.
func (p *valuePrinter) compose(s *strings.Builder, sf ScriptFunc, ctx printer.Context) {
	if ctx == printer.Arg {
		s.WriteByte('(')
		defer s.WriteByte(')')
	}
	first, second, op := sf.caps[0], sf.caps[1], " >> "
	if bin, _, _ := sf.prog.src.Binary(sf.prog.at[sf.node]); bin == token.LCOMP {
		first, second, op = second, first, " << "
	}
	p.value(s, first, printer.Arg)
	s.WriteString(op)
	p.value(s, second, printer.Arg)
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
		p.app(s, ctx, v.fn.name, v.args...)
	case ScriptFunc:
		if len(v.args) > 0 {
			// A function applied to some of its arguments prints as such.
			fn := v
			fn.args = nil
			var head strings.Builder
			p.value(&head, fn, printer.Arg)
			p.app(s, ctx, head.String(), v.args...)
		} else if v.ref != "" {
			s.WriteString(v.ref)
		} else if key, ok := home(v); ok {
			s.WriteString(p.find(key, v).printed)
		} else if v.composition() {
			p.compose(s, v, ctx)
		} else {
			pr := p.printer(v.prog.src, p.renames(nil, v.prog, v.uses(), v.caps))
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
