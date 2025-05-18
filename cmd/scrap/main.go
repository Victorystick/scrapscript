package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/Victorystick/scrapscript"
	"github.com/Victorystick/scrapscript/yards"
)

type Command func(args []string)

var commands = map[string]Command{
	"eval": eval,
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

func eval(args []string) {
	var input []byte
	var err error

	fetcher, err := yards.NewDefaultCacheFetcher(
		// TODO: make configurable
		yards.ByHttp("https://scraps.oseg.dev/"),
	)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return
	}

	input, err = io.ReadAll(os.Stdin)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return
	}

	val, err := scrapscript.Eval(input, fetcher)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return
	}

	if len(args) >= 2 && args[0] == "apply" {
		maybeFn, err := scrapscript.Eval([]byte(args[1]), fetcher)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return
		}

		val, err = scrapscript.Call(maybeFn, val)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return
		}
	}

	fmt.Println(val.String())
}
