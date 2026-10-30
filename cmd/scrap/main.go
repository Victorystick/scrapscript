package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/Victorystick/scrapscript/eval"
	"github.com/Victorystick/scrapscript/highlight"
	"github.com/Victorystick/scrapscript/token"
	"github.com/Victorystick/scrapscript/yards"
	"github.com/Victorystick/scrapscript/yards/httpyard"
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
}

var (
	server  = flag.String("server", "https://scraps.oseg.dev/", "The scrapyard server to use")
	root    = flag.String("root", ".", "The directory the script reads drafts from, as if it were a file there")
	cache   = flag.String("cache", "", "The directory to cache fetched scraps in (default: the user cache directory)")
	timeout = flag.Duration("timeout", 0, "How long to evaluate for, such as 5s, before stopping with an error (default: no limit)")
)

// ctx ends --timeout after the command starts.
var ctx = context.Background()

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
	if *timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, *timeout)
		defer cancel()
	}
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

	pusher := httpyard.New(*server)
	env.UsePusher(pusher)
	env.UseDrafts(os.DirFS(*root))
	if *timeout > 0 {
		env.UseContext(ctx)
	}
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
