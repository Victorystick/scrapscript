package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/Victorystick/scrapscript"
	"github.com/Victorystick/scrapscript/eval"
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
	{name: "push", desc: "pushes it to the server", fn: pushScrap},
	{name: "hash", desc: "prints its sha256 hash", fn: hashScrap},
}

var (
	server = flag.String("server", "https://scraps.oseg.dev/", "The scrapyard server to use")
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

	cmd.fn(flag.Args()[1:])
}

func must[T any](val T, err error) T {
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	return val
}

func makeEnv() *eval.Environment {
	env := eval.NewEnvironment()

	pusher := yards.ByHttp(*server)
	env.UsePusher(pusher)
	env.UseFetcher(must(yards.NewDefaultCacheFetcher(
		// Don't cache invalid scraps, but trust the local cache for now.
		yards.Validate(pusher)),
	))
	return env
}

func evaluate(args []string) {
	input := must(io.ReadAll(os.Stdin))
	env := makeEnv()
	scrap := must(env.Read(input))
	val := must(env.Eval(scrap))

	if len(args) >= 2 && args[0] == "apply" {
		scrap = must(env.Read([]byte(args[1])))
		fn := must(env.Eval(scrap))
		val = must(scrapscript.Call(fn, val))
	}

	fmt.Println(env.Scrap(val))
}

func inferType(args []string) {
	input := must(io.ReadAll(os.Stdin))
	env := makeEnv()
	scrap := must(env.Read(input))
	fmt.Println(must(env.Infer(scrap)))
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
