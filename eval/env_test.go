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
		} else if val.String() != ex.result {
			t.Errorf("%s: expected %s, got %s", ex.source, ex.result, val)
		}
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
		case val.String() != ex.result:
			t.Errorf("%s <| %s: got %s, want %s", ex.fn, ex.arg, val, ex.result)
		}
	}
}
