package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"

	"github.com/Victorystick/scrapscript/eval"
	"github.com/Victorystick/scrapscript/highlight"
	"github.com/Victorystick/scrapscript/token"
	"github.com/Victorystick/scrapscript/yards"
)

type Command struct {
	name string
	desc string
	fn   func(args []string)
}

var commands = []Command{
	{name: "eval", desc: "evaluates it", fn: evaluate},
	{name: "type", desc: "infers its type", fn: inferType},
	{name: "test", desc: "runs the test cases it holds", fn: runTests},
	{name: "push", desc: "pushes it to the server", fn: pushScrap},
	{name: "hash", desc: "prints its sha256 hash", fn: hashScrap},
	{name: "build", desc: "compiles it to a program that prints its value, with go build", fn: buildScrap},
}

var (
	server = flag.String("server", "https://scraps.oseg.dev/", "The scrapyard server to use")
	root   = flag.String("root", ".", "The directory the script reads drafts from, as if it were a file there")
	cache  = flag.String("cache", "", "The directory to cache fetched scraps in (default: the user cache directory)")
	output = flag.String("o", "scrap.out", "build: the program to write")
	gen    = flag.String("gen", "", "build: the directory to write the Go module to, instead of a temporary one, which it keeps")
	module = flag.String("module", "", "build: the directory of the scrapscript module to build against (default: found with go list)")
)

func main() {
	flag.Parse()

	name := flag.Arg(0)
	var cmd *Command
	for i := range commands {
		if commands[i].name == name {
			cmd = &commands[i]
			break
		}
	}

	if cmd == nil {
		fmt.Fprintln(os.Stderr, os.Args[0], "reads a script from stdin, parses it and does one of", len(commands), "things:")
		fmt.Fprintln(os.Stderr)
		for _, cmd := range commands {
			fmt.Fprintf(os.Stderr, "%s %s - %s\n", os.Args[0], cmd.name, cmd.desc)
		}
		fmt.Fprintln(os.Stderr, "\nFlags:")
		flag.PrintDefaults()
		os.Exit(2)
	}

	// Flags may also follow the command.
	flag.CommandLine.Parse(flag.Args()[1:])
	cmd.fn(flag.Args())
}

func must[T any](val T, err error) T {
	if err != nil {
		fmt.Fprintln(os.Stderr, token.Render(err, highlighter(os.Stderr)))
		os.Exit(1)
	}
	return val
}

func makeEnv() *eval.Environment {
	env := eval.NewEnvironment()

	pusher := yards.ByHttp(*server)
	env.UsePusher(pusher)
	env.UseDrafts(os.DirFS(*root))
	// Don't cache invalid scraps, but trust the local cache for now.
	fetcher := yards.Validate(pusher)
	if *cache != "" {
		env.UseFetcher(must(yards.NewCacheFetcher(*cache, fetcher)))
	} else {
		env.UseFetcher(must(yards.NewDefaultCacheFetcher(fetcher)))
	}
	return env
}

func evaluate(args []string) {
	input := must(io.ReadAll(os.Stdin))
	env := makeEnv()
	scrap := must(env.Read(input))

	if len(args) >= 2 && args[0] == "apply" {
		fn := must(env.Read([]byte(args[1])))
		fmt.Println(env.Print(must(env.Apply(fn, scrap))))
		return
	}
	fmt.Println(must(env.PrintScrap(scrap)))
}

func inferType(args []string) {
	input := must(io.ReadAll(os.Stdin))
	env := makeEnv()
	scrap := must(env.Read(input))
	fmt.Println(must(env.Infer(scrap)))
}

// runTests prints each failing case of a test scrap and a summary, and
// exits with status 1 if any case failed.
func runTests(args []string) {
	input := must(io.ReadAll(os.Stdin))
	env := makeEnv()
	scrap := must(env.Read(input))
	results := must(env.Test(scrap))
	failed := 0
	for _, r := range results {
		if !r.Failed {
			continue
		}
		failed++
		fmt.Println(strings.TrimSpace("FAIL " + r.Path))
		fmt.Println("    " + strings.ReplaceAll(r.Message, "\n", "\n    "))
	}
	fmt.Printf("%d failed, %d passed\n", failed, len(results)-failed)
	if failed > 0 {
		os.Exit(1)
	}
}

func pushScrap(args []string) {
	input := must(io.ReadAll(os.Stdin))
	env := makeEnv()
	scrap := must(env.Read(input))
	key := must(env.Push(scrap))
	fmt.Println(key)
}

func hashScrap(args []string) {
	input := must(io.ReadAll(os.Stdin))
	env := makeEnv()
	scrap := must(env.Read(input))
	fmt.Println(scrap.Sha256())
}

// highlighter returns what highlights errors for f, if it is a terminal.
func highlighter(f *os.File) token.Highlighter {
	if info, err := f.Stat(); err == nil && info.Mode()&os.ModeCharDevice != 0 {
		return highlight.ANSI
	}
	return nil
}

// buildScrap compiles the scrap to a Go program, which prints its value as
// eval does, and builds it with go build. The scraps it refers to are
// fetched now, and embedded in the program.
func buildScrap(args []string) {
	input := must(io.ReadAll(os.Stdin))
	env := makeEnv()
	scrap := must(env.Read(input))
	src := must(env.GenerateGo(scrap, eval.GoConfig{Package: "main", Main: true}))

	dir := *gen
	if dir == "" {
		dir = must(os.MkdirTemp("", "scrap-build-"))
		defer os.RemoveAll(dir)
	} else {
		must(0, os.MkdirAll(dir, 0o755))
	}
	must(0, os.WriteFile(filepath.Join(dir, "main.go"), src, 0o644))
	mod, tidy := goMod()
	must(0, os.WriteFile(filepath.Join(dir, "go.mod"), []byte(mod), 0o644))

	goCmd := func(args ...string) {
		cmd := exec.Command("go", args...)
		cmd.Dir, cmd.Stdout, cmd.Stderr = dir, os.Stdout, os.Stderr
		must(0, cmd.Run())
	}
	if tidy {
		goCmd("mod", "tidy")
	}
	goCmd("build", "-o", must(filepath.Abs(*output)), ".")
}

const modulePath = "github.com/Victorystick/scrapscript"

// goMod returns the go.mod of a built program, which requires the
// scrapscript module: in the directory that -module or go list names, or
// else the version that this command was built from, in which case the
// module needs go mod tidy to fetch it.
func goMod() (mod string, tidy bool) {
	// The go version of the toolchain that built this command, which is at
	// least the module's.
	version := strings.TrimPrefix(runtime.Version(), "go")
	if !strings.HasPrefix(version, "1.") {
		version = "1.24.2"
	}
	mod = "module scrapbuild\n\ngo " + version + "\n\n"
	dir := *module
	if dir == "" {
		if out, err := exec.Command("go", "list", "-m", "-f", "{{.Dir}}", modulePath).Output(); err == nil {
			dir = strings.TrimSpace(string(out))
		}
	}
	if dir != "" {
		dir = must(filepath.Abs(dir))
		return mod + fmt.Sprintf("require %s v0.0.0\n\nreplace %s => %s\n", modulePath, modulePath, dir), false
	}
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Path == modulePath {
		if v := info.Main.Version; v != "" && v != "(devel)" && !strings.HasSuffix(v, "+dirty") {
			return mod + fmt.Sprintf("require %s %s\n", modulePath, v), true
		}
	}
	must(0, fmt.Errorf("cannot find the scrapscript module to build against; name its directory with -module"))
	return "", false
}
