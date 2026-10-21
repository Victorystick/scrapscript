package eval

import (
	"strings"
	"testing"
	"testing/fstest"
)

func TestInferBuiltin(t *testing.T) {
	examples := []struct {
		source string
		result string
	}{
		// numeric conversions
		{`round`, `float -> int`},
		{`ceil`, `float -> int`},
		{`floor`, `float -> int`},
		{`to-float`, `int -> float`},

		// byte <-> text conversion
		{`bytes/to-utf8-text`, `bytes -> text`},
		{`bytes/from-utf8-text`, `text -> bytes`},

		// int
		{`int/cmp`, `int -> int -> [ #eq, #gt, #lt ]`},
		{`int/div`, `int -> int -> [ #none, #some int ]`},
		{`int/to-text`, `int -> text`},
		{`text/cmp`, `text -> text -> [ #eq, #gt, #lt ]`},
		{`text/split`, `text -> text -> list text`},
		{`text/to-int`, `text -> [ #none, #some int ]`},
		{`list/sort`, `(a -> a -> [ #eq, #gt, #lt ]) -> list a -> list a`},

		// list
		{`list/length`, `list a -> int`},
		{`list/map`, `(a -> b) -> list a -> list b`},
		{`list/map (a -> a + 1)`, `list int -> list int`},
		{`list/fold`, `a -> (a -> b -> a) -> list b -> a`},
		{`list/repeat`, `int -> a -> list a`},

		// text
		{`text/length`, `text -> int`},
		{`text/repeat`, `int -> text -> text`},
		{`text/join`, `text -> list text -> text`},

		{`list/fold 0 (a -> b -> a + text/length b)`, `list text -> int`},
		{`list/fold 0 (a -> b -> a + text/length b) ["hey", "beautiful"]`, `int`},

		{`| 0 -> [0] | n -> seq (n - 1) +< n ; seq = x -> [x]`, `int -> list int`},
		{`seq ; seq = | 0 -> [0] | n -> seq (n - 1) +< n`, `int -> list int`},
	}

	for _, ex := range examples {
		env := NewEnvironment()
		scrap, err := env.Read([]byte(ex.source))

		if err != nil {
			t.Error(err)
			continue
		}

		res, err := env.Infer(scrap)

		if err != nil {
			t.Error(err)
		} else {
			if res != ex.result {
				t.Errorf("Invalid type for '%s'\n  expected: %s\n       got: %s", ex.source, ex.result, res)
			}
		}
	}
}

func TestInferFailure(t *testing.T) {
	env := NewEnvironment()
	scrap, err := env.Read([]byte(`1 + "a"`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := env.Infer(scrap); err == nil {
		t.Error("Expected a type error")
	}
}

// pathEnv returns an environment whose drafts are the given files, in memory.
func pathEnv(files map[string]string) *Environment {
	fsys := make(fstest.MapFS, len(files))
	for name, source := range files {
		fsys[name] = &fstest.MapFile{Data: []byte(source)}
	}
	env := NewEnvironment()
	env.UseDrafts(fsys)
	return env
}

var pathFiles = map[string]string{
	"inc.scrap":     `x -> x + 1`,
	"choice.scrap":  `choice ; choice : type = [#a int, #b]`,
	"option.scrap":  `option ; option = a => [#some a, #none]`,
	"sub/two.scrap": `$"../inc.scrap" 1`,
	// Paths are relative to the draft that has them.
	"x.scrap":        `$"./subdir/y.scrap"`,
	"subdir/y.scrap": `$"../other/z.scrap"`,
	"other/z.scrap":  `42`,
	"a.scrap":        `$"./b.scrap"`,
	"b.scrap":        `$"./a.scrap"`,
}

func TestPathImports(t *testing.T) {
	examples := []struct{ source, result string }{
		{`$"./inc.scrap" 1`, `2`},
		{`or-zero (#a 3) ; or-zero : $"./choice.scrap" -> int = | #a n -> n | #b -> 0`, `3`},
		// Paths are relative to the draft that has them; standard input is an
		// unnamed draft at the root.
		{`$"./sub/two.scrap"`, `2`},
		{`$"./x.scrap"`, `42`},
		// An imported type constructor.
		{`or-zero (#some 3) ; or-zero : $"./option.scrap" int -> int = | #some n -> n | #none -> 0`, `3`},
		{`$"sub/../inc.scrap" 2`, `3`},
	}

	for _, ex := range examples {
		env := pathEnv(pathFiles)
		val, err := eval(env, ex.source)
		if err != nil {
			t.Errorf("%s: %v", ex.source, err)
		} else if got := env.Print(val); got != ex.result {
			t.Errorf("%s: expected %s, got %s", ex.source, ex.result, got)
		}
	}
}

func TestScrapsAreBoundOnce(t *testing.T) {
	env := pathEnv(pathFiles)
	// sub/two.scrap refers to inc.scrap too.
	scrap, err := env.Read([]byte(`$"./inc.scrap" $"./sub/two.scrap"`))
	if err != nil {
		t.Fatal(err)
	}
	val, err := env.Eval(scrap)
	if err != nil {
		t.Fatal(err)
	}
	if got := env.Print(val); got != "3" {
		t.Errorf("expected 3, got %s", got)
	}
	if n := len(scrap.prog.srcs); n != 3 {
		t.Errorf("expected 3 sources, got %d", n)
	}
	if _, binds := scrap.prog.where(scrap.prog.root); len(binds) != 4 {
		t.Errorf("expected a binding per scrap referred to, 2, got %d", len(binds)/2)
	}
}

func TestErrorsInScrapsReferredTo(t *testing.T) {
	env := pathEnv(map[string]string{"pick.scrap": `| #a -> 1`})
	scrap, err := env.Read([]byte(`$"./pick.scrap"`))
	if err != nil {
		t.Fatal(err)
	}
	fn, err := env.Eval(scrap)
	if err != nil {
		t.Fatal(err)
	}
	// Hosts call functions unchecked, so this can fail at runtime, in the
	// scrap that the function is from.
	_, err = Call(fn, NewVariant(env.Label("b"), Value{}))
	if err == nil || !strings.Contains(err.Error(), "| #a -> 1") {
		t.Errorf("expected an error in pick.scrap, got %v", err)
	}
}

func TestPathImportFailures(t *testing.T) {
	examples := []struct{ source, error string }{
		{`$"./a.scrap"`, `$"./a.scrap" refers back to this scrap`},
		{`$"../inc.scrap"`, `cannot refer to a path outside the current directory`},
		{`$"/inc.scrap"`, `cannot refer to an absolute path`},
		{`$"./missing.scrap"`, `file does not exist`},
		{`$"sub/../../inc.scrap"`, `cannot refer to a path outside the current directory`},
		{`$"./choice.scrap" + 1`, `cannot use type $"./choice.scrap" as a value`},
	}

	for _, ex := range examples {
		env := pathEnv(pathFiles)
		if val, err := eval(env, ex.source); err == nil {
			t.Errorf("%s: expected an error, got %s", ex.source, val)
		} else if !strings.Contains(err.Error(), ex.error) {
			t.Errorf("%s: expected '%s' in error:\n%s", ex.source, ex.error, err)
		}
	}
}

func TestPathsNeedADirectory(t *testing.T) {
	_, err := eval(NewEnvironment(), `$"./inc.scrap"`)
	if err == nil || !strings.Contains(err.Error(), "cannot refer to a path here") {
		t.Errorf("Expected 'cannot refer to a path here', got %v", err)
	}
}

func TestPublishedScrapsCannotReferToPaths(t *testing.T) {
	env := pathEnv(pathFiles)
	hash := "a948904f2f0f479b8f8197694b30184b0d2ed1c1cd2a1ec0fb85d299a192a445"
	env.UseFetcher(MapFetcher{hash: `$"./inc.scrap"`})

	_, err := eval(env, `$source:`+hash)
	if err == nil || !strings.Contains(err.Error(), "a published scrap cannot refer to a path") {
		t.Errorf("Expected 'a published scrap cannot refer to a path', got %v", err)
	}
}

func TestHashSchemes(t *testing.T) {
	examples := []struct{ source, message string }{
		{`$core:0a`, "core hashes are not supported yet"},
		{`$sha256:0a`, "unknown hash scheme sha256"},
		{`$source:0a`, "a source hash has 64 hex digits, not 2"},
	}
	for _, ex := range examples {
		_, err := eval(NewEnvironment(), ex.source)
		if err == nil || !strings.Contains(err.Error(), ex.message) {
			t.Errorf("%s: expected '%s', got %v", ex.source, ex.message, err)
		}
	}
}

type nopPusher struct{}

func (nopPusher) PushScrap(data []byte) (string, error) { return "", nil }

func TestReadDraft(t *testing.T) {
	eval := func(env *Environment, s *Scrap, err error) string {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		v, err := env.Eval(s)
		if err != nil {
			t.Fatal(err)
		}
		return env.Print(v)
	}

	// Its paths are relative to its name.
	env := pathEnv(pathFiles)
	main, err := env.ReadDraft("sub/main.scrap", []byte(`$"../inc.scrap" 41`))
	if got := eval(env, main, err); got != "42" {
		t.Errorf("Expected 42, got %s", got)
	}

	// Other drafts get it instead of the file.
	env = pathEnv(pathFiles)
	if _, err := env.ReadDraft("inc.scrap", []byte(`x -> x + 2`)); err != nil {
		t.Fatal(err)
	}
	two, err := env.Read([]byte(`$"./sub/two.scrap"`))
	if got := eval(env, two, err); got != "3" {
		t.Errorf("Expected 3, got %s", got)
	}

	if _, err := env.ReadDraft("inc.scrap", []byte(`1`)); err == nil || err.Error() != "inc.scrap is already read" {
		t.Errorf("Expected 'inc.scrap is already read', got %v", err)
	}
	if _, err := env.ReadDraft("../inc.scrap", []byte(`1`)); err == nil || err.Error() != "../inc.scrap is not a path within the drafts" {
		t.Errorf("Expected '../inc.scrap is not a path within the drafts', got %v", err)
	}
}

func TestDraftsWithPathsCannotBePushed(t *testing.T) {
	env := NewEnvironment()
	env.UsePusher(nopPusher{})
	scrap, err := env.Read([]byte(`$"./inc.scrap" 1`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := env.Push(scrap); err == nil || !strings.Contains(err.Error(), "cannot push a draft that refers to a path") {
		t.Errorf("Expected 'cannot push a draft that refers to a path', got %v", err)
	}
}

func TestApply(t *testing.T) {
	examples := []struct{ fn, arg, result, err string }{
		{`n -> n + 1`, `1`, `2`, ``},
		{`list/map (x -> x * 2)`, `[1, 2]`, `[ 2, 4 ]`, ``},
		{`x -> x`, `y -> y`, `y -> y`, ``},
		// The application is checked, not only each side.
		{`x -> x ++ "a"`, `1`, ``, "cannot apply: passed 'int' but expected 'text'"},
		{`list/map (x -> x + 1)`, `["a"]`, ``, "cannot apply: passed 'list text' but expected 'list int'"},
		{`1`, `1`, ``, "cannot apply: 'int' is not a function"},
		// Aliases are defined after the message; different ones with the same
		// name get primes.
		{`f ; f : io -> int = _ -> 1 ; io : type = [#err text, #ok]`,
			`(g ; g : io -> int = _ -> 2 ; io : type = [#err text, #ok])`, ``,
			"cannot apply: passed 'io -> int' but expected 'io''\n; io : type = [ #err text, #ok ]\n; io' : type = [ #err text, #ok ]"},
		{`get ; get : pair -> int = p -> p.a ; pair : type = { a : int, b : int }`, `{ a = "x", b = 2 }`, ``,
			"cannot apply: passed '{ a : text, b : int }' but expected 'pair'\n; pair : type = { a : int, b : int }"},
		// Variables are named across both types.
		{`f ; f : a => (a -> a) -> int = _ -> 1`, `x -> [x]`, ``,
			"cannot apply: passed 'a -> list a' but expected 'b -> b'"},
		// Runtime failures are errors too.
		{`n -> list/repeat n 1`, `-1`, ``, `list/repeat: negative count -1`},
		{`t ; t : type = int`, `1`, ``, `cannot evaluate a type`},
	}
	for _, ex := range examples {
		env := NewEnvironment()
		fn, err := env.Read([]byte(ex.fn))
		if err != nil {
			t.Fatal(err)
		}
		arg, err := env.Read([]byte(ex.arg))
		if err != nil {
			t.Fatal(err)
		}
		val, err := env.Apply(fn, arg)
		switch {
		case ex.err != "":
			if err == nil || !strings.Contains(err.Error(), ex.err) {
				t.Errorf("%s <| %s: got %v, %v; want error %q", ex.fn, ex.arg, val, err, ex.err)
			}
		case err != nil:
			t.Errorf("%s <| %s: %v", ex.fn, ex.arg, err)
		case env.Print(val) != ex.result:
			t.Errorf("%s <| %s: got %s, want %s", ex.fn, ex.arg, env.Print(val), ex.result)
		}
	}
}

// Calls to the functions that a scrap exports as fields of its record are
// inlined, through an import or a name bound to one, and still print as
// calls.
func TestInliningAcrossScraps(t *testing.T) {
	env := pathEnv(map[string]string{
		"lib.scrap": `{ inc = inc, add = add } ; inc = x -> x + n ; add = a -> b -> inc a + b ; n = 1`,
	})
	scrap, err := env.Read([]byte(`[lib.inc 2, lib.add 3 4, $"./lib.scrap".inc 5, f 6] ; f = y -> lib.inc y ; lib = $"./lib.scrap"`))
	if err != nil {
		t.Fatal(err)
	}
	got, err := env.PrintScrap(scrap)
	if err != nil {
		t.Fatal(err)
	}
	if want := "[ 3, 8, 6, 7 ]"; got != want {
		t.Errorf("expected %s, got %s", want, got)
	}
	p := scrap.prog
	for n, o := range p.ops {
		if (o == opCall || o == opCallN) && p.source(cnode(n)) == &scrap.expr {
			t.Errorf("a call is left in the root: %s", scrap.expr.NodeText(p.at[n]))
		}
	}

	scrap, err = env.Read([]byte(`y -> lib.add y 1 ; lib = $"./lib.scrap"`))
	if err != nil {
		t.Fatal(err)
	}
	got, err = env.PrintScrap(scrap)
	if err != nil {
		t.Fatal(err)
	}
	// A draft prints as its value.
	if want := "y -> lib.add y 1\n; lib = { add = add, inc = inc }\n; add = a -> b -> inc a + b\n; inc = x -> x + n\n; n = 1"; got != want {
		t.Errorf("expected %q, got %q", want, got)
	}
}
