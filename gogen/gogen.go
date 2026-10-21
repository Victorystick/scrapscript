// Package gogen generates Go bindings for a platform's interface: Go types
// for the interface's types, conversions between them and values, a loader
// that checks scripts against the interface, and the interface itself,
// embedded with the scraps it refers to.
package gogen

import (
	"bytes"
	"fmt"
	"go/format"
	"go/token"
	"maps"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"github.com/Victorystick/scrapscript/eval"
	"github.com/Victorystick/scrapscript/types"
)

// Config says what to generate.
type Config struct {
	// The name of the Go package.
	Package string
	// Whether to generate a main function too, which runs the host's
	// command line and then a function run(p, args) that the host
	// implements, where p is the script's value.
	Main bool
}

// Generate returns the files of the bindings of the interface iface, by
// name: bindings.go, and main.go if cfg.Main is set.
func Generate(env *eval.Environment, iface *eval.Scrap, cfg Config) (map[string][]byte, error) {
	if cfg.Main && cfg.Package != "main" {
		return nil, fmt.Errorf("can only generate main in package main, not %s", cfg.Package)
	}
	reg, res, err := env.TypeOf(iface)
	if err != nil {
		return nil, err
	}
	if !res.Sort {
		return nil, fmt.Errorf("an interface must be a type")
	}
	deps, err := env.Closure(iface)
	if err != nil {
		return nil, err
	}

	g := &generator{
		reg:        reg,
		params:     make(map[types.TypeRef]string),
		names:      make(map[string]bool),
		byRef:      make(map[types.TypeRef]*decl),
		byKey:      make(map[string]*decl),
		labels:     make(map[string]string),
		labelNames: map[string]bool{"env": true},
	}
	for _, name := range []string{"Interface", "Load"} {
		g.names[name] = true
	}
	for i, p := range res.Params {
		if p.Row {
			return nil, fmt.Errorf("cannot bind row parameter %s", p.Name)
		}
		name := camel(p.Name)
		if name == "" {
			name = fmt.Sprintf("T%d", i)
		}
		if err := g.claim(name); err != nil {
			return nil, err
		}
		g.params[reg.Resolve(p.Var)] = name
		g.paramOrder = append(g.paramOrder, p)
	}

	topName := "Platform"
	if s := reg.Shape(res.Type); s.Alias != "" && !s.Imported {
		topName = camel(s.Alias)
	}
	top, err := g.typ(res.Type, topName)
	if err != nil {
		return nil, err
	}

	sources := map[string]string{iface.Sha256(): string(iface.Source())}
	for _, d := range deps {
		sources[d.Sha256()] = string(d.Source())
	}
	files := map[string][]byte{}
	if files["bindings.go"], err = g.bindings(cfg.Package, iface.Sha256(), sources, top); err != nil {
		return nil, err
	}
	if cfg.Main {
		files["main.go"] = []byte(mainFile)
	}
	return files, nil
}

type generator struct {
	reg *types.Registry
	// The Go names of the interface's parameters, by their variables.
	params     map[types.TypeRef]string
	paramOrder []types.Param
	// The Go identifiers taken at the top level.
	names map[string]bool
	// The declared types, in the order declared, by the ref of their type,
	// and by the alias and arguments they are expanded from.
	decls []*decl
	byRef map[types.TypeRef]*decl
	byKey map[string]*decl
	// The fields of the labels struct, by the label or tag they hold the ID
	// of, and the field names taken, env among them.
	labels     map[string]string
	labelNames map[string]bool
	// The fields of the labels struct that hold the shapes of records, in
	// the order of the records' declarations.
	shapes []shapeField
	// Whether the bindings use the bytes package.
	usesBytes bool
}

// label returns an expression of the ID of a record label or tag, a field
// of the labels struct l, which the bindings resolve once per environment.
func (g *generator) label(name string) string {
	if f, ok := g.labels[name]; ok {
		return "l." + f
	}
	f := lower(camel(name))
	if f == "" || !unicode.IsLetter([]rune(f)[0]) {
		f = "l" + camel(name)
	}
	if token.IsKeyword(f) {
		f += "_"
	}
	f = g.labelsField(f)
	g.labels[name] = f
	return "l." + f
}

// A shapeField is a field of the labels struct that holds the shape of a
// record declaration.
type shapeField struct {
	name string
	decl *decl
}

// shape returns an expression of the shape of the record d, a field of the
// labels struct l.
func (g *generator) shape(d *decl) string {
	f := g.labelsField(lower(d.name) + "Shape")
	g.shapes = append(g.shapes, shapeField{f, d})
	return "l." + f
}

// labelsField claims a field name of the labels struct, starting from base.
func (g *generator) labelsField(base string) string {
	f := base
	for i := 2; g.labelNames[f]; i++ {
		f = fmt.Sprintf("%s%d", base, i)
	}
	g.labelNames[f] = true
	return f
}

// A gotype is a Go type that a type binds to.
type gotype struct {
	expr   string
	prim   string    // a primitive's name, or ""
	param  bool      // a parameter's wrapper
	elem   *gotype   // a list's element
	args   []*gotype // an uncurried function's arguments
	result *gotype   // and its result
	decl   *decl     // a record or enum
	hint   string    // a function's name, for printing
}

// A decl is a declared Go type: a record's struct or an enum's.
type decl struct {
	name   string
	enum   bool
	fields []field
}

// A field is a record's field or an enum's tag.
type field struct {
	label string
	name  string  // the Go field, or the tag's accessor
	typ   *gotype // nil for a tag without a payload
}

func (g *generator) claim(name string) error {
	if g.names[name] {
		return fmt.Errorf("the interface's types would declare %s twice", name)
	}
	g.names[name] = true
	return nil
}

// typ returns the Go type of ref, declaring the types it needs. The hint
// names a record or enum that has no alias.
func (g *generator) typ(ref types.TypeRef, hint string) (*gotype, error) {
	s := g.reg.Shape(ref)
	switch s.Kind {
	case types.ShapePrimitive:
		return primitive(s.Name)
	case types.ShapeVariable:
		if name, ok := g.params[s.Ref]; ok {
			return &gotype{expr: name, param: true}, nil
		}
		return nil, fmt.Errorf("cannot bind %s: only the interface's parameters can be type variables", g.reg.String(ref))
	case types.ShapeList:
		elem, err := g.typ(s.Elem, hint+"Elem")
		if err != nil {
			return nil, err
		}
		return &gotype{expr: "[]" + elem.expr, elem: elem}, nil
	case types.ShapeFunc:
		return g.fn(s, hint)
	}
	return g.named(s, hint)
}

// fn returns the Go type of a function, uncurried: it takes the arguments
// of consecutive arrows at once.
func (g *generator) fn(s types.Shape, hint string) (*gotype, error) {
	t := &gotype{hint: hint}
	var args []string
	for s.Kind == types.ShapeFunc {
		arg, err := g.typ(s.Arg, fmt.Sprintf("%sArg%d", hint, len(t.args)))
		if err != nil {
			return nil, err
		}
		t.args = append(t.args, arg)
		args = append(args, arg.expr)
		s = g.reg.Shape(s.Result)
	}
	result, err := g.typ(s.Ref, hint+"Result")
	if err != nil {
		return nil, err
	}
	t.result = result
	t.expr = fmt.Sprintf("func(%s) (%s, error)", strings.Join(args, ", "), result.expr)
	return t, nil
}

// named returns the declared Go type of a record or enum, declaring it the
// first time.
func (g *generator) named(s types.Shape, hint string) (*gotype, error) {
	if d, ok := g.byRef[s.Ref]; ok {
		return &gotype{expr: d.name, decl: d}, nil
	}
	name := hint
	key := ""
	if s.Alias != "" && !s.Imported {
		name = camel(s.Alias)
		key = g.key(s)
		if d, ok := g.byKey[key]; ok {
			g.byRef[s.Ref] = d
			return &gotype{expr: d.name, decl: d}, nil
		}
		if g.names[name] {
			return nil, fmt.Errorf("cannot bind %s: the alias %s is used at more than one type", g.reg.String(s.Ref), s.Alias)
		}
	}
	if s.Open {
		return nil, fmt.Errorf("cannot bind %s: its row is open", g.reg.String(s.Ref))
	}
	if err := g.claim(name); err != nil {
		return nil, err
	}
	d := &decl{name: name, enum: s.Kind == types.ShapeEnum}
	g.byRef[s.Ref] = d
	if key != "" {
		g.byKey[key] = d
	}
	g.decls = append(g.decls, d)

	seen := map[string]bool{"Tag": d.enum}
	for _, l := range s.Labels {
		f := field{label: l.Name, name: camel(l.Name)}
		if f.name == "" || seen[f.name] {
			return nil, fmt.Errorf("cannot bind %s: the label %s has no distinct Go name", g.reg.String(s.Ref), l.Name)
		}
		seen[f.name] = true
		if d.enum {
			if err := g.claim(name + f.name); err != nil {
				return nil, err
			}
			if err := g.claim("New" + name + f.name); err != nil {
				return nil, err
			}
		}
		if !d.enum || g.reg.Resolve(l.Type) != types.HoleRef {
			hint := name + f.name
			if d.enum {
				hint += "Payload"
			}
			typ, err := g.typ(l.Type, hint)
			if err != nil {
				return nil, err
			}
			f.typ = typ
		}
		d.fields = append(d.fields, f)
	}
	return &gotype{expr: name, decl: d}, nil
}

// key identifies the alias of s and the arguments it is applied to.
func (g *generator) key(s types.Shape) string {
	parts := []string{s.Alias}
	for _, arg := range s.Args {
		if name, ok := g.params[g.reg.Resolve(arg)]; ok {
			parts = append(parts, "param "+name)
		} else {
			parts = append(parts, g.reg.String(arg))
		}
	}
	return strings.Join(parts, "\x00")
}

func primitive(name string) (*gotype, error) {
	exprs := map[string]string{
		"()": "struct{}", "int": "int64", "float": "float64",
		"text": "string", "byte": "byte", "bytes": "[]byte",
	}
	expr, ok := exprs[name]
	if !ok {
		return nil, fmt.Errorf("cannot bind the type %s", name)
	}
	return &gotype{expr: expr, prim: name}, nil
}

// from returns a Go expression that converts the value v to t.
func (g *generator) from(t *gotype, v string) string {
	switch {
	case t.prim != "":
		switch t.prim {
		case "()":
			return "struct{}{}"
		case "int":
			return fmt.Sprintf("%s.Int()", v)
		case "float":
			return fmt.Sprintf("%s.Float()", v)
		case "text":
			return fmt.Sprintf("%s.Text()", v)
		case "byte":
			return fmt.Sprintf("%s.Byte()", v)
		case "bytes":
			g.usesBytes = true
			return fmt.Sprintf("bytes.Clone(%s.Bytes())", v)
		}
	case t.param:
		return fmt.Sprintf("%s{%s}", t.expr, v)
	case t.elem != nil:
		return fmt.Sprintf("host.ListFrom(%s, func(v eval.Value) %s { return %s })", v, t.elem.expr, g.from(t.elem, "v"))
	case t.decl != nil:
		return fmt.Sprintf("%sFromValue(l, %s)", lower(t.decl.name), v)
	}
	// A function, which calls the value with its arguments converted.
	var params, args []string
	for i, a := range t.args {
		params = append(params, fmt.Sprintf("a%d %s", i, a.expr))
		args = append(args, g.to(a, fmt.Sprintf("a%d", i)))
	}
	return fmt.Sprintf(`func(fn eval.Value) %s {
	return func(%s) (%s, error) {
		v, err := eval.Call(fn, %s)
		if err != nil {
			var zero %s
			return zero, err
		}
		return %s, nil
	}
}(%s)`, t.expr, strings.Join(params, ", "), t.result.expr, strings.Join(args, ", "), t.result.expr, g.from(t.result, "v"), v)
}

// to returns a Go expression that converts x, of type t, to a value.
func (g *generator) to(t *gotype, x string) string {
	switch {
	case t.prim != "":
		switch t.prim {
		case "()":
			return "eval.Hole()"
		case "int":
			return fmt.Sprintf("eval.Int(%s)", x)
		case "float":
			return fmt.Sprintf("eval.Float(%s)", x)
		case "text":
			return fmt.Sprintf("eval.Text(%s)", x)
		case "byte":
			return fmt.Sprintf("eval.Byte(%s)", x)
		case "bytes":
			g.usesBytes = true
			return fmt.Sprintf("eval.Bytes(bytes.Clone(%s))", x)
		}
	case t.param:
		return x + ".v"
	case t.elem != nil:
		return fmt.Sprintf("host.ListTo(%s, func(x %s) eval.Value { return %s })", x, t.elem.expr, g.to(t.elem, "x"))
	case t.decl != nil:
		return x + ".toValue(l)"
	}
	// A function of the host, which takes its arguments at once.
	var args []string
	for i, a := range t.args {
		args = append(args, g.from(a, fmt.Sprintf("args[%d]", i)))
	}
	return fmt.Sprintf(`eval.NewFunc(%q, %d, func(args []eval.Value) (eval.Value, error) {
	r, err := %s(%s)
	if err != nil {
		return eval.Value{}, err
	}
	return %s, nil
})`, lower(t.hint), len(t.args), x, strings.Join(args, ", "), g.to(t.result, "r"))
}

func (g *generator) bindings(pkg, hash string, sources map[string]string, top *gotype) ([]byte, error) {
	var body bytes.Buffer
	w := func(format string, args ...any) { fmt.Fprintf(&body, format, args...) }

	w("// Interface is the platform's interface, %s, and the scraps it refers to.\n", "$source:"+hash)
	w("var Interface = host.Interface{\n\tHash: %q,\n\tSources: map[string]string{\n", hash)
	for _, h := range append([]string{hash}, sortedWithout(sources, hash)...) {
		w("\t\t%q: %s,\n", h, literal(sources[h]))
	}
	w("\t},\n}\n\n")

	w("// Load checks that the scrap app implements the interface, and returns its\n")
	w("// value. The environment must find the scraps that the interface refers to,\n")
	w("// as with Interface.Fetcher.\n")
	w("func Load(env *eval.Environment, app *eval.Scrap) (%s, error) {\n", top.expr)
	w("\tvar zero %s\n", top.expr)
	w("\tiface, err := env.Read([]byte(Interface.Source()))\n\tif err != nil {\n\t\treturn zero, err\n\t}\n")
	w("\tif err := env.Check(app, iface); err != nil {\n\t\treturn zero, err\n\t}\n")
	w("\tv, err := env.Eval(app)\n\tif err != nil {\n\t\treturn zero, err\n\t}\n")
	// Records and enums, the only types with labels, convert through l.
	if len(g.decls) > 0 {
		w("\tl := newLabels(env)\n")
	}
	w("\treturn %s, nil\n}\n", g.from(top, "v"))

	for _, p := range g.paramOrder {
		name := g.params[g.reg.Resolve(p.Var)]
		w("\n// %s is a value of the type parameter %s, which only scripts can inspect.\n", name, p.Name)
		w("type %s struct{ v eval.Value }\n", name)
	}

	for _, d := range g.decls {
		if d.enum {
			g.enum(w, d)
		} else {
			g.record(w, d)
		}
	}
	if len(g.decls) > 0 {
		g.labelsType(w)
	}

	var head bytes.Buffer
	fmt.Fprintf(&head, "// Code generated by scrap-gen-go from $source:%s. DO NOT EDIT.\n\n", hash)
	fmt.Fprintf(&head, "package %s\n\nimport (\n", pkg)
	if g.usesBytes {
		head.WriteString("\t\"bytes\"\n\n")
	}
	head.WriteString("\t\"github.com/Victorystick/scrapscript/eval\"\n")
	head.WriteString("\t\"github.com/Victorystick/scrapscript/host\"\n")
	if len(g.decls) > 0 {
		head.WriteString("\t\"github.com/Victorystick/scrapscript/label\"\n")
	}
	head.WriteString(")\n\n")
	head.Write(body.Bytes())

	src, err := format.Source(head.Bytes())
	if err != nil {
		return nil, fmt.Errorf("generated invalid Go: %w\n%s", err, head.Bytes())
	}
	return src, nil
}

// labelsType writes the labels struct, of the IDs of the labels and tags
// that the conversions use and the shapes of the records they make, and
// newLabels, which resolves them in an environment.
func (g *generator) labelsType(w func(string, ...any)) {
	names := slices.Sorted(maps.Keys(g.labels))
	w("\n// labels are the IDs of the interface's record labels and tags in the\n")
	w("// environment that Load checked the script in, which values hold, and\n")
	w("// the shapes of its records there, and the environment, which reads\n")
	w("// records.\n")
	w("type labels struct {\n\tenv *eval.Environment\n")
	for _, name := range names {
		w("\t%s label.ID\n", g.labels[name])
	}
	for _, s := range g.shapes {
		w("\t%s *eval.Shape\n", s.name)
	}
	w("}\n\nfunc newLabels(env *eval.Environment) *labels {\n\tl := &labels{\n\t\tenv: env,\n")
	for _, name := range names {
		w("\t\t%s: env.Label(%q),\n", g.labels[name], name)
	}
	w("\t}\n")
	for _, s := range g.shapes {
		w("\tl.%s = env.Shape(", s.name)
		for i, f := range s.decl.fields {
			if i > 0 {
				w(", ")
			}
			w("l.%s", g.labels[f.label])
		}
		w(")\n")
	}
	w("\treturn l\n}\n")
}

func (g *generator) record(w func(string, ...any), d *decl) {
	w("\ntype %s struct {\n", d.name)
	for _, f := range d.fields {
		w("\t%s %s\n", f.name, f.typ.expr)
	}
	w("}\n")

	w("\nfunc %sFromValue(l *labels, v eval.Value) %s {\n", lower(d.name), d.name)
	if len(d.fields) == 0 {
		w("\treturn %s{}\n}\n", d.name)
	} else {
		w("\tr := l.env.Record(v)\n\treturn %s{\n", d.name)
		for _, f := range d.fields {
			w("\t\t%s: %s,\n", f.name, g.from(f.typ, fmt.Sprintf("r.Get(%s)", g.label(f.label))))
		}
		w("\t}\n}\n")
	}

	w("\nfunc (x %s) toValue(l *labels) eval.Value {\n\treturn %s.New(\n", d.name, g.shape(d))
	for _, f := range d.fields {
		g.label(f.label)
		w("\t\t%s,\n", g.to(f.typ, "x."+f.name))
	}
	w("\t)\n}\n")
}

// enum writes an enum's declarations: a struct of its tag and the payload
// of each tag that has one, which records and enums hold by pointer, so
// that recursive types have a size.
func (g *generator) enum(w func(string, ...any), d *decl) {
	tag := d.name + "Tag"
	tags := lower(d.name) + "Tags"
	w("\ntype %s uint8\n\nconst (\n", tag)
	for i, f := range d.fields {
		if i == 0 {
			w("\t%s%s %s = iota\n", d.name, f.name, tag)
		} else {
			w("\t%s%s\n", d.name, f.name)
		}
	}
	w(")\n\nvar %s = [...]string{\n", tags)
	for _, f := range d.fields {
		w("\t%s%s: %q,\n", d.name, f.name, f.label)
	}
	w("}\n\nfunc (t %s) String() string { return \"#\" + %s[t] }\n", tag, tags)

	w("\ntype %s struct {\n\ttag %s\n", d.name, tag)
	for _, f := range d.fields {
		if f.typ != nil {
			w("\t%s %s\n", storage(f), pointer(f.typ))
		}
	}
	w("}\n")

	w("\nfunc (x %s) Tag() %s { return x.tag }\n", d.name, tag)
	w("\nfunc (x %s) must(t %s) {\n\tif x.tag != t {\n", d.name, tag)
	w("\t\tpanic(\"%s: \" + t.String() + \" of \" + x.tag.String())\n\t}\n}\n", d.name)
	for _, f := range d.fields {
		const_ := d.name + f.name
		if f.typ == nil {
			w("\nfunc New%s() %s { return %s{tag: %s} }\n", const_, d.name, d.name, const_)
			continue
		}
		val := "x"
		if isDecl(f.typ) {
			val = "&x"
		}
		w("\nfunc New%s(x %s) %s { return %s{tag: %s, %s: %s} }\n", const_, f.typ.expr, d.name, d.name, const_, storage(f), val)
		w("\n// %s returns the payload of #%s, and panics on another tag.\n", f.name, f.label)
		w("func (x %s) %s() %s {\n\tx.must(%s)\n\treturn %sx.%s\n}\n", d.name, f.name, f.typ.expr, const_, deref(f.typ), storage(f))
	}

	w("\nfunc %sFromValue(l *labels, v eval.Value) %s {\n\tt := v.Variant()\n\tswitch t.Tag() {\n", lower(d.name), d.name)
	for _, f := range d.fields {
		if f.typ == nil {
			w("\tcase %s:\n\t\treturn New%s%s()\n", g.label(f.label), d.name, f.name)
		} else {
			w("\tcase %s:\n\t\treturn New%s%s(%s)\n", g.label(f.label), d.name, f.name, g.from(f.typ, "t.Payload()"))
		}
	}
	w("\t}\n\tpanic(\"%s: unknown tag\")\n}\n", d.name)

	w("\nfunc (x %s) toValue(l *labels) eval.Value {\n\tswitch x.tag {\n", d.name)
	for _, f := range d.fields {
		if f.typ != nil {
			w("\tcase %s%s:\n\t\treturn eval.NewVariant(%s, %s)\n", d.name, f.name, g.label(f.label), g.to(f.typ, payload(f)))
		} else {
			w("\tcase %s%s:\n\t\treturn eval.NewVariant(%s, eval.Value{})\n", d.name, f.name, g.label(f.label))
		}
	}
	w("\t}\n\tpanic(\"%s: invalid tag\")\n}\n", d.name)
}

func isDecl(t *gotype) bool { return t.decl != nil }

func pointer(t *gotype) string {
	if isDecl(t) {
		return "*" + t.expr
	}
	return t.expr
}

// payload returns an expression of a tag's payload in an enum x.
func payload(f field) string {
	if isDecl(f.typ) {
		return "(*x." + storage(f) + ")"
	}
	return "x." + storage(f)
}

func deref(t *gotype) string {
	if isDecl(t) {
		return "*"
	}
	return ""
}

// storage returns the name of the struct field that holds a tag's payload.
func storage(f field) string {
	name := lower(f.name)
	if name == "tag" || token.IsKeyword(name) {
		name += "_"
	}
	return name
}

// camel returns a scrap name in CamelCase, as an exported Go name:
// next-action is NextAction, and x' is XPrime.
func camel(name string) string {
	name = strings.ReplaceAll(name, "'", "-prime")
	var b strings.Builder
	for part := range strings.FieldsFuncSeq(name, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}) {
		rs := []rune(part)
		b.WriteRune(unicode.ToUpper(rs[0]))
		b.WriteString(string(rs[1:]))
	}
	return b.String()
}

// lower returns name with its first letter lowered.
func lower(name string) string {
	rs := []rune(name)
	if len(rs) == 0 {
		return name
	}
	rs[0] = unicode.ToLower(rs[0])
	return string(rs)
}

// literal returns a Go string literal of s, raw if it can be.
func literal(s string) string {
	if !strings.ContainsAny(s, "`\r") {
		return "`" + s + "`"
	}
	return strconv.Quote(s)
}

func sortedWithout(m map[string]string, skip string) []string {
	keys := slices.Sorted(maps.Keys(m))
	return slices.DeleteFunc(keys, func(k string) bool { return k == skip })
}

const mainFile = `// Code generated by scrap-gen-go. DO NOT EDIT.

package main

import (
	"os"

	"github.com/Victorystick/scrapscript/eval"
	"github.com/Victorystick/scrapscript/host"
)

// main runs the host's command line, and then run, which the host
// implements, with the value of the script that it names, once the script
// is checked against the interface.
func main() {
	os.Exit(host.Main(Interface, func(env *eval.Environment, script *eval.Scrap, args []string) (int, error) {
		p, err := Load(env, script)
		if err != nil {
			return 0, err
		}
		return run(p, args)
	}))
}
`
