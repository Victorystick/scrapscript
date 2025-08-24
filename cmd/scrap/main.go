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

type Command func(args []string)

var commands = map[string]Command{
	"eval": evaluate,
	"type": inferType,
}

func main() {
	flag.Parse()

	cmd, ok := commands[flag.Arg(0)]
	if !ok {
		flag.Usage()
		for name := range commands {
			fmt.Fprintln(os.Stderr, name)
		}
		os.Exit(2)
	}

	cmd(flag.Args()[1:])
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
	env.UseFetcher(must(yards.NewDefaultCacheFetcher(
		// Don't cache invalid scraps, but trust the local cache for now.
		yards.Validate(
			// TODO: make configurable
			yards.ByHttp("https://scraps.oseg.dev/")),
	)))
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
