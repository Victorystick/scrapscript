package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/Victorystick/scrapscript"
	"github.com/Victorystick/scrapscript/eval"
	"github.com/Victorystick/scrapscript/parser"
	"github.com/Victorystick/scrapscript/token"
	"github.com/Victorystick/scrapscript/types"
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

func evaluate(args []string) {
	fetcher := must(yards.NewDefaultCacheFetcher(
		// Don't cache invalid scraps, but trust the local cache for now.
		yards.Validate(
			// TODO: make configurable
			yards.ByHttp("https://scraps.oseg.dev/")),
	))

	input := must(io.ReadAll(os.Stdin))
	env := eval.NewEnvironment(fetcher)
	val := must(env.Eval(input))

	if len(args) >= 2 && args[0] == "apply" {
		fn := must(env.Eval([]byte(args[1])))
		val = must(scrapscript.Call(fn, val))
	}

	fmt.Println(env.Scrap(val))
}

func inferType(args []string) {
	input := must(io.ReadAll(os.Stdin))
	source := token.NewSource(input)

	se := must(parser.Parse(&source))
	str, err := types.Infer(se)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println(str)
}
