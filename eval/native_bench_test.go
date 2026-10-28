package eval

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestNativeBenchGen writes BenchmarkEval's programs as native code to the
// directory $NATIVE_BENCH_DIR, as a Go module whose BenchmarkEval runs
// them, for bench/native.sh.
func TestNativeBenchGen(t *testing.T) {
	dir := os.Getenv("NATIVE_BENCH_DIR")
	if dir == "" {
		t.Skip()
	}
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	var bench strings.Builder
	bench.WriteString("package main\n\nimport (\n\t\"testing\"\n\n")
	for i, ex := range evalSources {
		env := NewEnvironment()
		env.UseDrafts(platformFiles)
		s, err := env.Read([]byte(ex.source))
		if err != nil {
			t.Fatal(err)
		}
		src, err := env.GenerateGo(s, GoConfig{Package: fmt.Sprintf("p%d", i)})
		if err != nil {
			t.Fatal(err)
		}
		writeFile(t, filepath.Join(dir, fmt.Sprintf("p%d", i), "p.go"), src)
		fmt.Fprintf(&bench, "\t%q\n", fmt.Sprintf("scrapbuild/p%d", i))
	}
	bench.WriteString(")\n\nfunc BenchmarkEval(b *testing.B) {\n")
	for i, ex := range evalSources {
		fmt.Fprintf(&bench, "\tb.Run(%q, func(b *testing.B) {\n\t\tb.ReportAllocs()\n\t\tfor b.Loop() {\n\t\t\tif _, err := p%d.Eval(); err != nil {\n\t\t\t\tb.Fatal(err)\n\t\t\t}\n\t\t}\n\t})\n", ex.name, i)
	}
	bench.WriteString("}\n")
	writeFile(t, filepath.Join(dir, "bench_test.go"), []byte(bench.String()))
	writeFile(t, filepath.Join(dir, "main.go"), []byte("package main\n\nfunc main() {}\n"))
	writeFile(t, filepath.Join(dir, "go.mod"), goMod(root))
}

// goMod returns the go.mod of a module of native code that builds against
// the scrapscript module in the directory root.
func goMod(root string) []byte {
	return fmt.Appendf(nil, "module scrapbuild\n\ngo 1.24.2\n\nrequire %[1]s v0.0.0\n\nreplace %[1]s => %[2]s\n",
		"github.com/Victorystick/scrapscript", root)
}

// writeFile writes the file at path, and the directories it is in.
func writeFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}
