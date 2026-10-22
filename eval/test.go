package eval

import (
	"bytes"
	"fmt"
	"math"
	"strconv"

	"github.com/Victorystick/scrapscript/types"
)

// A test scrap is an ordinary scrap whose value holds test cases, as
// lib/test.scrap makes them. Records group cases by label and lists hold
// them by index, nested as deep as they like. A case is a variant:
//
//	#eq { got : a, want : a }                          got equals want
//	#float-eq { got : float, want : float, within : float }
//	#pass
//	#fail text                                         fails, saying why
//
// Test reads the value by its type, which it checks before evaluating.

// A TestResult is the outcome of one case of a test scrap.
type TestResult struct {
	// Path names the case: the labels of its groups joined by ".", and the
	// indices of lists in brackets, as in "insert.keeps-order" or
	// "sizes[2]".
	Path   string
	Failed bool
	// Message says why the case failed, or is empty if it passed.
	Message string
}

// Test checks that the value of the scrap s holds test cases, evaluates
// it, and returns the outcome of each case, in the order of its labels and
// indices. It fails if s isn't a test scrap or its evaluation fails.
func (e *Environment) Test(s *Scrap) ([]TestResult, error) {
	res, err := e.infer(s)
	if err != nil {
		return nil, err
	}
	if res.Sort {
		return nil, fmt.Errorf("cannot test a type")
	}
	t := tester{e: e, seen: make(map[types.TypeRef]bool)}
	if err := t.check(res.Type, ""); err != nil {
		return nil, err
	}
	v, err := e.eval(s, res)
	if err != nil {
		return nil, err
	}
	t.run(res.Type, v, "")
	return t.results, nil
}

type tester struct {
	e       *Environment
	seen    map[types.TypeRef]bool
	results []TestResult
}

// check checks that typ is the type of test cases, reached by path.
func (t *tester) check(typ types.TypeRef, path string) error {
	s := t.e.reg.Shape(typ)
	if t.seen[s.Ref] {
		return nil
	}
	t.seen[s.Ref] = true
	switch s.Kind {
	case types.ShapeRecord:
		for _, l := range s.Labels {
			if err := t.check(l.Type, join(path, l.Name)); err != nil {
				return err
			}
		}
		return nil
	case types.ShapeList:
		return t.check(s.Elem, path+"[]")
	case types.ShapeEnum:
		for _, l := range s.Labels {
			if err := t.checkCase(l, path); err != nil {
				return err
			}
		}
		return nil
	}
	return t.errorf(path, "is not a test case, a record or a list of them: %s", t.e.reg.String(typ))
}

// checkCase checks the tag l of a case's type.
func (t *tester) checkCase(l types.Label, path string) error {
	payload := t.e.reg.Shape(l.Type)
	switch l.Name {
	case "eq":
		if payload.Kind != types.ShapeRecord || payload.Open || len(payload.Labels) != 2 ||
			payload.Labels[0].Name != "got" || payload.Labels[1].Name != "want" {
			return t.errorf(path, "#eq takes { got, want }, not %s", t.e.reg.String(l.Type))
		}
		for _, f := range payload.Labels {
			if t.hasFunc(f.Type, make(map[types.TypeRef]bool)) {
				return t.errorf(path, "cannot compare functions: %s", t.e.reg.String(f.Type))
			}
		}
		return nil
	case "float-eq":
		ok := payload.Kind == types.ShapeRecord && !payload.Open && len(payload.Labels) == 3
		for i, name := range []string{"got", "want", "within"} {
			ok = ok && payload.Labels[i].Name == name && t.e.reg.Resolve(payload.Labels[i].Type) == types.FloatRef
		}
		if !ok {
			return t.errorf(path, "#float-eq takes { got : float, want : float, within : float }, not %s", t.e.reg.String(l.Type))
		}
		return nil
	case "pass":
		if payload.Ref != types.HoleRef {
			return t.errorf(path, "#pass takes no payload, not %s", t.e.reg.String(l.Type))
		}
		return nil
	case "fail":
		if payload.Ref != types.TextRef {
			return t.errorf(path, "#fail takes text, not %s", t.e.reg.String(l.Type))
		}
		return nil
	}
	return t.errorf(path, "#%s is not a test case: they are #eq, #float-eq, #pass and #fail", l.Name)
}

// hasFunc returns true if values of type typ may hold functions.
func (t *tester) hasFunc(typ types.TypeRef, seen map[types.TypeRef]bool) bool {
	s := t.e.reg.Shape(typ)
	if seen[s.Ref] {
		return false
	}
	seen[s.Ref] = true
	switch s.Kind {
	case types.ShapeFunc:
		return true
	case types.ShapeList:
		return t.hasFunc(s.Elem, seen)
	case types.ShapeRecord, types.ShapeEnum:
		for _, l := range s.Labels {
			if t.hasFunc(l.Type, seen) {
				return true
			}
		}
	}
	return false
}

func (t *tester) errorf(path, format string, args ...any) error {
	if path == "" {
		return fmt.Errorf("the test scrap "+format, args...)
	}
	return fmt.Errorf(path+" "+format, args...)
}

// run records the outcome of the cases in v, of type typ, reached by path,
// which check has accepted.
func (t *tester) run(typ types.TypeRef, v value, path string) {
	s := t.e.reg.Shape(typ)
	switch s.Kind {
	case types.ShapeRecord:
		r := t.e.shapes.record(v)
		for _, l := range s.Labels {
			t.run(l.Type, r.values[r.shape.index(t.e.Label(l.Name))], join(path, l.Name))
		}
	case types.ShapeList:
		for i, elem := range v.asList() {
			t.run(s.Elem, elem, path+"["+strconv.Itoa(i)+"]")
		}
	case types.ShapeEnum:
		t.results = append(t.results, t.outcome(v, path))
	}
}

// outcome returns the outcome of the case v.
func (t *tester) outcome(v value, path string) TestResult {
	r := TestResult{Path: path}
	payload := v.payload()
	switch t.e.reg.Labels().Name(v.tag()) {
	case "eq":
		f := t.e.shapes.record(payload).values // got, want
		if !t.e.shapes.equal(f[0], f[1]) {
			r.Failed = true
			r.Message = "got  " + printValue(t.e.shapes, f[0]) + "\nwant " + printValue(t.e.shapes, f[1])
		}
	case "float-eq":
		f := t.e.shapes.record(payload).values // got, want, within
		got, want, within := f[0].asFloat(), f[1].asFloat(), f[2].asFloat()
		if !(math.Abs(got-want) <= within) {
			r.Failed = true
			r.Message = "got  " + printValue(t.e.shapes, f[0]) + "\nwant " + printValue(t.e.shapes, f[1]) +
				" within " + printValue(t.e.shapes, f[2])
		}
	case "fail":
		r.Failed, r.Message = true, payload.asText()
	}
	return r
}

// join returns the path to the field name of the group at path.
func join(path, name string) string {
	if path == "" {
		return name
	}
	return path + "." + name
}

// equal returns true if a and b, values of one type without functions, are
// the same. A tag without a payload equals the tag with (), since they
// match alike.
func (t *shapeTable) equal(a, b value) bool {
	if !a.valid() {
		a = hole
	}
	if !b.valid() {
		b = hole
	}
	k := a.kind()
	if k != b.kind() {
		return false
	}
	switch k {
	case kindHole:
		return true
	case kindInt, kindByte:
		return a.n == b.n
	case kindFloat:
		return a.asFloat() == b.asFloat()
	case kindText:
		return a.asText() == b.asText()
	case kindBytes:
		return bytes.Equal(a.asBytes(), b.asBytes())
	case kindList:
		return t.equalAll(a.asList(), b.asList())
	case kindRecord:
		return a.shapeID() == b.shapeID() && t.equalAll(a.fields(), b.fields())
	case kindVariant:
		return a.tag() == b.tag() && t.equal(a.payload(), b.payload())
	}
	return false
}

func (t *shapeTable) equalAll(as, bs []value) bool {
	if len(as) != len(bs) {
		return false
	}
	for i := range as {
		if !t.equal(as[i], bs[i]) {
			return false
		}
	}
	return true
}
