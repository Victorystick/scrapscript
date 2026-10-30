package main

import (
	"flag"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/Victorystick/scrapscript/eval"
	"github.com/Victorystick/scrapscript/gogen"
	"github.com/Victorystick/scrapscript/yards"
	"github.com/Victorystick/scrapscript/yards/httpyard"
)

// scrap-gen-go generates Go bindings for a platform's interface, given as a
// hash reference, source:<hex>, or as the path of a draft. It is meant for
// go:generate, which expands $ in arguments, so the reference may leave it
// out:
//
//	//go:generate go run github.com/Victorystick/scrapscript/cmd/scrap-gen-go source:<hex>
func main() {
	dir := flag.String("o", ".", "the directory to write the files to")
	pkg := flag.String("pkg", os.Getenv("GOPACKAGE"), "the Go package's name; $GOPACKAGE by default, as go:generate sets it")
	withMain := flag.Bool("main", true, "generate main.go too, which runs the host's command line and then run(p, args)")
	server := flag.String("server", "https://scraps.oseg.dev/", "the scrapyard to fetch scraps from")
	flag.Usage = func() {
		fmt.Fprintln(flag.CommandLine.Output(), "usage: scrap-gen-go [flags] <source:hex | path>")
		flag.PrintDefaults()
	}
	flag.Parse()
	if flag.NArg() != 1 {
		flag.Usage()
		os.Exit(2)
	}
	if *pkg == "" {
		*pkg = "main"
	}

	env := eval.NewEnvironment()
	yard := yards.Validate(httpyard.New(*server))
	if cached, err := yards.NewDefaultCacheFetcher(yard); err == nil {
		yard = cached
	}
	env.UseFetcher(yard)

	iface := must(readInterface(env, flag.Arg(0)))
	files := must(gogen.Generate(env, iface, gogen.Config{Package: *pkg, Main: *withMain}))
	for _, name := range slices.Sorted(maps.Keys(files)) {
		if err := os.WriteFile(filepath.Join(*dir, name), files[name], 0o644); err != nil {
			must(0, err)
		}
	}
}

func must[T any](val T, err error) T {
	if err != nil {
		fmt.Fprintln(os.Stderr, "scrap-gen-go:", err)
		os.Exit(1)
	}
	return val
}

func readInterface(env *eval.Environment, ref string) (*eval.Scrap, error) {
	if digest, ok := strings.CutPrefix(strings.TrimPrefix(ref, "$"), "source:"); ok {
		return env.Fetch("source", digest)
	}
	source, err := os.ReadFile(ref)
	if err != nil {
		return nil, err
	}
	return env.Read(source)
}
