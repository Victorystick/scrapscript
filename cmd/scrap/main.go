package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"

	"github.com/Victorystick/scrapscript"
	"github.com/Victorystick/scrapscript/eval"
	"github.com/Victorystick/scrapscript/yards"
)

type Command func(args []string)

var commands = map[string]Command{
	"eval": evaluate,
	"type": inferType,
	// A trivial, path-only web-server.
	"serve": serve,
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

func serve(args []string) {
	input := must(io.ReadAll(os.Stdin))
	env := makeEnv()
	scrap := must(env.Read(input))
	fn := must(env.Eval(scrap))

	http.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		path := eval.Text(r.URL.Path)

		val, err := scrapscript.Call(fn, path)
		if err != nil {
			w.WriteHeader(500)
			w.Write([]byte(err.Error()))
			return
		}

		if rec, ok := val.(eval.Record); ok {
			if code, ok := rec.Get("code").(eval.Int); ok {
				if body, ok := rec.Get("body").(eval.Text); ok {
					w.WriteHeader((int)(code))
					w.Write([]byte(body))
					return
				}
			}
		}

		w.WriteHeader(500)
		w.Write([]byte("Bad return type: " + env.Type(val)))
	})

	log.Fatal(http.ListenAndServe(":8080", nil))
}
