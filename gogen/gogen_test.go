package gogen

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/Victorystick/scrapscript/eval"
)

// TestGenerated checks that the generated files in the repository are what
// the generator generates now, as go generate would.
func TestGenerated(t *testing.T) {
	cases := []struct {
		dir, iface string
		cfg        Config
	}{
		{"internal/rich", "rich.scrap", Config{Package: "rich"}},
		{"../cmd/example/scrap-terminal", "program.scrap", Config{Package: "main", Main: true}},
	}
	for _, c := range cases {
		source, err := os.ReadFile(filepath.Join(c.dir, c.iface))
		if err != nil {
			t.Fatal(err)
		}
		env := eval.NewEnvironment()
		iface, err := env.Read(source)
		if err != nil {
			t.Fatal(err)
		}
		files, err := Generate(env, iface, c.cfg)
		if err != nil {
			t.Fatalf("%s: %v", c.dir, err)
		}
		for name, got := range files {
			want, err := os.ReadFile(filepath.Join(c.dir, name))
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != string(want) {
				t.Errorf("%s/%s is out of date; run go generate", c.dir, name)
			}
		}
	}
}

func TestGenerateFailures(t *testing.T) {
	failures := []struct{ source, error string }{
		{`r ; r : row -> type = a => { b : { a : int, ..a } }`, `cannot bind row parameter`},
		{`p ; p : type = { a : box int, b : box text } ; box : type -> type = a => { v : a }`,
			`the alias box is used at more than one type`},
		{`1`, `an interface must be a type`},
		{`p ; p : type = $"./q.scrap"`, `cannot embed a scrap that refers to a path`},
	}
	for _, f := range failures {
		env := eval.NewEnvironment()
		env.UseDrafts(fstest.MapFS{"q.scrap": {Data: []byte(`q ; q : type = int`)}})
		iface, err := env.Read([]byte(f.source))
		if err == nil {
			_, err = Generate(env, iface, Config{Package: "p"})
		}
		if err == nil || !strings.Contains(err.Error(), f.error) {
			t.Errorf("%s: expected an error containing %q, got %v", f.source, f.error, err)
		}
	}

	env := eval.NewEnvironment()
	iface, _ := env.Read([]byte(`p ; p : type = int`))
	if _, err := Generate(env, iface, Config{Package: "p", Main: true}); err == nil {
		t.Errorf("expected main outside package main to fail")
	}
}

func TestCamel(t *testing.T) {
	for in, want := range map[string]string{
		"next-action": "NextAction", "eof": "Eof", "x'": "XPrime", "a/b-c": "ABC",
	} {
		if got := camel(in); got != want {
			t.Errorf("camel(%q) = %q, want %q", in, got, want)
		}
	}
}
