package eval

import (
	"encoding/base64"
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/Victorystick/scrapscript/ast"
	"github.com/Victorystick/scrapscript/printer"
	"github.com/Victorystick/scrapscript/scanner"
)

// Print returns a scrap that evaluates to v and prints the same, as SPEC.md
// section 10, rule 6, requires.
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
func Print(v Value) string {
	p := valuePrinter{
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
	// Its Where node in frame.src, or None if frame isn't a where-group.
	where   ast.Node
	value   Value  // nil for a sort binding
	printed string // its name in the printed scrap
}

type bindingKey struct {
	frame *context
	name  string
	sort  bool
}

type srcNode struct {
	src  *ast.SourceExpr
	node ast.Node
}

type valuePrinter struct {
	bindings []*binding // in the order that they are found
	byKey    map[bindingKey]*binding
	// The bindings that the free Ident nodes of printed code refer to.
	refs map[srcNode]*binding
	// The names that print as themselves without being bound, builtins.
	names map[string]bool
	// The names of every Ident in printed code.
	idents map[string]bool
}

// home returns the name of the binding that defines a function, if its
// value is that very function.
func home(sf ScriptFunc) (string, bool) {
	for _, b := range sf.ctx.binds {
		_, id, _, val := sf.ctx.src.Where(b)
		if val == sf.node && !sf.ctx.src.Sort(b) {
			return sf.ctx.src.TokenText(id), true
		}
	}
	return "", false
}

// collect finds the bindings that v refers to.
func (p *valuePrinter) collect(v Value) {
	switch v := v.(type) {
	case Record:
		for _, key := range slices.Sorted(maps.Keys(v.values)) {
			p.collect(v.values[key])
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
		if name, ok := home(v); ok {
			p.need(bindingKey{v.ctx, name, false})
			return
		}
		p.code(v.ctx, v.node)
	}
}

// need returns the binding for key, finding what it refers to the first
// time.
func (p *valuePrinter) need(key bindingKey) *binding {
	if b := p.byKey[key]; b != nil {
		return b
	}
	b := &binding{bindingKey: key}
	p.byKey[key] = b
	p.bindings = append(p.bindings, b)

	src := key.frame.src
	for _, w := range key.frame.binds {
		_, id, _, _ := src.Where(w)
		if src.TokenText(id) == key.name && src.Sort(w) == key.sort {
			b.where = w
			break
		}
	}
	if b.where != ast.None {
		_, _, typ, val := src.Where(b.where)
		if typ != ast.None {
			p.typ(key.frame, typ)
		}
		if key.sort {
			p.typ(key.frame, val)
			return b
		}
	}

	b.value = key.frame.vars.Get(key.name)
	if p.defines(b) {
		_, _, _, val := src.Where(b.where)
		p.code(key.frame, val)
	} else {
		p.collect(b.value)
	}
	return b
}

// defines returns true if b's value is a function that b defines, which
// prints as its code.
func (p *valuePrinter) defines(b *binding) bool {
	sf, ok := b.value.(ScriptFunc)
	if !ok || sf.ref != "" || sf.ctx != b.frame || b.where == ast.None {
		return false
	}
	_, _, _, val := b.frame.src.Where(b.where)
	return val == sf.node
}

// resolve returns the binding that a free name in frame refers to, or nil
// if it is a builtin.
func (p *valuePrinter) resolve(frame *context, name string) *binding {
	for c := frame; c != nil; c = c.parent {
		v := c.vars.Get(name)
		if v == nil {
			continue
		}
		if bf, ok := v.(BuiltInFunc); ok && c.parent == nil && bf.name == name {
			p.names[name] = true
			return nil
		}
		return p.need(bindingKey{c, name, false})
	}
	p.names[name] = true
	return nil
}

// resolveType returns the sort binding that a free type name in frame
// refers to, or nil if it is a builtin type.
func (p *valuePrinter) resolveType(frame *context, name string) *binding {
	for c := frame; c != nil; c = c.parent {
		for _, w := range c.binds {
			_, id, _, _ := c.src.Where(w)
			if c.src.Sort(w) && c.src.TokenText(id) == name {
				return p.need(bindingKey{c, name, true})
			}
		}
	}
	p.names[name] = true
	return nil
}

// code finds the bindings that the free names of the code x in frame refer
// to.
func (p *valuePrinter) code(frame *context, x ast.Node) {
	w := walker{p: p, frame: frame, se: frame.src,
		values: make(map[string]int), types: make(map[string]int)}
	w.expr(x)
}

// typ finds the sort bindings that the free names of the type x in frame
// refer to.
func (p *valuePrinter) typ(frame *context, x ast.Node) {
	w := walker{p: p, frame: frame, se: frame.src,
		values: make(map[string]int), types: make(map[string]int)}
	w.typ(x)
}

// A walker finds the free names of code, and the bindings they refer to.
type walker struct {
	p     *valuePrinter
	frame *context
	se    *ast.SourceExpr
	// The names bound within the code, by how many binders bind them.
	values, types map[string]int
}

func (w *walker) ident(x ast.Node) string {
	name := w.se.TokenText(w.se.Main(x))
	w.p.idents[name] = true
	return name
}

func (w *walker) expr(x ast.Node) {
	se := w.se
	switch se.Kind(x) {
	case ast.Ident:
		if name := w.ident(x); w.values[name] == 0 {
			if b := w.p.resolve(w.frame, name); b != nil {
				w.p.refs[srcNode{se, x}] = b
			}
		}
	case ast.Binary:
		_, left, right := se.Binary(x)
		w.expr(left)
		w.expr(right)
	case ast.Call:
		fn, arg := se.Call(x)
		w.expr(fn)
		w.expr(arg)
	case ast.Func:
		arg, body := se.Func(x)
		defer w.bind(w.values, w.ident(arg))()
		w.expr(body)
	case ast.MatchFunc:
		for _, arm := range se.Arms(x) {
			w.arm(arm)
		}
	case ast.Variant:
		if _, payload := se.Variant(x); payload != ast.None {
			w.expr(payload)
		}
	case ast.Record:
		rest, fields := se.Record(x)
		if rest != ast.None {
			w.expr(rest)
		}
		for i := range fields.Len() {
			w.expr(fields.Value(i))
		}
	case ast.Access:
		rec, _ := se.Access(x)
		w.expr(rec)
	case ast.List:
		for _, elem := range se.List(x) {
			w.expr(elem)
		}
	case ast.Where:
		body, binds := se.Group(x)
		for _, b := range binds {
			_, id, _, _ := se.Where(b)
			names := w.values
			if se.Sort(b) {
				names = w.types
			}
			name := se.TokenText(id)
			w.p.idents[name] = true
			defer w.bind(names, name)()
		}
		for _, b := range binds {
			_, _, typ, val := se.Where(b)
			if typ != ast.None {
				w.typ(typ)
			}
			if se.Sort(b) {
				w.typ(val)
			} else {
				w.expr(val)
			}
		}
		w.expr(body)
	}
}

// arm walks a match arm, whose pattern binds names in its body.
func (w *walker) arm(arm ast.Node) {
	pat, body := w.se.Func(arm)
	var names []string
	w.pattern(pat, &names)
	for _, name := range names {
		defer w.bind(w.values, name)()
	}
	w.expr(body)
}

// pattern adds the names that the pattern x binds to names.
func (w *walker) pattern(x ast.Node, names *[]string) {
	se := w.se
	switch se.Kind(x) {
	case ast.Ident:
		*names = append(*names, w.ident(x))
	case ast.Variant:
		if _, payload := se.Variant(x); payload != ast.None {
			w.pattern(payload, names)
		}
	case ast.Record:
		rest, fields := se.Record(x)
		if rest != ast.None {
			w.pattern(rest, names)
		}
		for i := range fields.Len() {
			w.pattern(fields.Value(i), names)
		}
	case ast.List:
		for _, elem := range se.List(x) {
			w.pattern(elem, names)
		}
	case ast.Binary:
		_, left, right := se.Binary(x)
		w.pattern(left, names)
		w.pattern(right, names)
	}
}

func (w *walker) typ(x ast.Node) {
	se := w.se
	switch se.Kind(x) {
	case ast.Ident:
		if name := w.ident(x); w.types[name] == 0 {
			if b := w.p.resolveType(w.frame, name); b != nil {
				w.p.refs[srcNode{se, x}] = b
			}
		}
	case ast.Forall:
		param, t := se.Forall(x)
		name := se.TokenText(param)
		w.p.idents[name] = true
		defer w.bind(w.types, name)()
		w.typ(t)
	case ast.Func, ast.Call:
		l, r := se.Func(x)
		w.typ(l)
		w.typ(r)
	case ast.RecordType:
		tail, fields := se.Record(x)
		if tail != ast.None {
			w.typ(tail)
		}
		for i := range fields.Len() {
			w.typ(fields.Value(i))
		}
	case ast.Enum:
		tail, variants := se.Enum(x)
		if tail != ast.None {
			w.typ(tail)
		}
		for _, v := range variants {
			if _, payload := se.Variant(v); payload != ast.None {
				w.typ(payload)
			}
		}
	}
}

// bind binds name in names, until the returned function is called.
func (w *walker) bind(names map[string]int, name string) func() {
	if name == "_" {
		return func() {}
	}
	names[name]++
	return func() { names[name]-- }
}

// name names the bindings: each by its own name, unless an earlier binding
// or a builtin has it. Then it gets primes, up to a name that no code uses,
// so that no binder in the code shadows it.
func (p *valuePrinter) name() {
	taken := make(map[string]bool, len(p.bindings))
	for _, b := range p.bindings {
		name := b.name
		for p.names[name] || taken[name] || name != b.name && p.idents[name] {
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
		if b.where == ast.None {
			s.WriteString(" = ")
			p.value(&s, b.value, printer.Body)
			continue
		}
		pr := p.printer(b.frame.src)
		_, _, typ, val := b.frame.src.Where(b.where)
		if b.sort || p.defines(b) {
			pr.Binding(typ, val, b.sort)
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
		for i, key := range slices.Sorted(maps.Keys(v.values)) {
			if i > 0 {
				s.WriteString(", ")
			}
			s.WriteString(key)
			s.WriteString(" = ")
			p.value(s, v.values[key], printer.Expr)
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
			s.WriteString("#" + v.tag)
			return
		}
		p.app(s, ctx, "#"+v.tag, v.value)
	case BuiltInFunc:
		s.WriteString(v.name)
	case Partial:
		p.app(s, ctx, v.name, v.args...)
	case ScriptFunc:
		if v.ref != "" {
			s.WriteString(v.ref)
		} else if name, ok := home(v); ok {
			s.WriteString(p.byKey[bindingKey{v.ctx, name, false}].printed)
		} else {
			pr := p.printer(v.ctx.src)
			pr.Expr(v.node, ctx)
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
