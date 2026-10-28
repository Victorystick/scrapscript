package eval

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// nativeSources returns the programs that TestNative compiles: the
// evaluator's test cases that evaluate, and the benchmarks.
func nativeSources(t *testing.T) []string {
	var sources []string
	for _, ex := range expressions {
		sources = append(sources, ex.source)
	}
	for _, ex := range exp2str {
		sources = append(sources, ex.source)
	}
	for _, ex := range tailCalls {
		sources = append(sources, ex.source)
	}
	for _, ex := range evalSources {
		sources = append(sources, ex.source)
	}
	for _, f := range failures[len(failures)-3:] {
		sources = append(sources, f.source)
	}
	// Functions of drafts and of published scraps print from the sources
	// that the program embeds.
	sources = append(sources,
		`$"./echo.scrap"`,
		"$source:"+publishedInc,
		"$source:"+publishedInc+" 41",
		"{ inc = $source:"+publishedInc+", twice = f -> f >> f }",
	)
	for _, name := range []string{"testdata/expr.scrap", "testdata/cata.scrap"} {
		src, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		sources = append(sources, string(src))
	}
	return sources
}

// published holds a published scrap, for TestNative.
var published = sourceFS{publishedInc: "x -> x + 1"}

// publishedInc is the sha256 of the source of the published scrap.
const publishedInc = "d136bd5eb3538a2d41e58756f1ba8e366ba9289dd94092984d5a9ffe6b7fd537"

// TestNative checks that native code computes and prints what the
// evaluator does, by compiling each program to a package of one Go
// program, which prints them all.
func TestNative(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a Go program")
	}
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	var want []string
	var main strings.Builder
	main.WriteString("package main\n\nimport (\n\t\"fmt\"\n\t\"runtime/debug\"\n\n")
	sources := nativeSources(t)
	for i, source := range sources {
		env := NewEnvironment()
		env.UseDrafts(platformFiles)
		env.UseFetcher(published)
		s, err := env.Read([]byte(source))
		if err != nil {
			t.Fatal(err)
		}
		out, err := env.PrintScrap(s)
		if err != nil {
			out = "error: " + err.Error()
		}
		want = append(want, out)

		pkg := fmt.Sprintf("p%d", i)
		src, err := env.GenerateGo(s, GoConfig{Package: pkg})
		if err != nil {
			t.Fatalf("%s\n%s", source, err)
		}
		writeFile(t, filepath.Join(dir, pkg, "p.go"), src)
		fmt.Fprintf(&main, "\t%q\n", "scrapbuild/"+pkg)
	}
	main.WriteString(")\n\nfunc main() {\n")
	// Calls in tail position run in constant stack.
	main.WriteString("\tdebug.SetMaxStack(16 << 20)\n")
	for i := range sources {
		fmt.Fprintf(&main, "\tfmt.Print(\"\\x1e\")\n\tif v, err := p%d.Eval(); err != nil {\n", i)
		main.WriteString("\t\tfmt.Print(\"error: \", err)\n\t} else {\n")
		fmt.Fprintf(&main, "\t\ts, err := p%d.Print(v)\n\t\tfmt.Print(s, err)\n\t}\n", i)
	}
	main.WriteString("}\n")
	writeFile(t, filepath.Join(dir, "main.go"), []byte(main.String()))
	writeFile(t, filepath.Join(dir, "go.mod"), goMod(root))
	cmd := exec.Command("go", "run", ".")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s\n%s", err, out)
	}
	got := strings.Split(string(out), "\x1e")[1:]
	if len(got) != len(sources) {
		t.Fatalf("got %d results for %d programs:\n%s", len(got), len(sources), out)
	}
	for i, source := range sources {
		g := strings.TrimSuffix(got[i], "<nil>")
		if g != want[i] {
			t.Errorf("%s\nnative: %s\nwant:   %s", source, g, want[i])
		}
	}
}
