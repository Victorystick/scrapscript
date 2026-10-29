// Package cli is the command line that every host of a platform has, which
// the main.go that scrap-gen-go generates runs. It reads scripts from files
// and the scrapyard, so it is kept apart from package host, which the
// bindings themselves use, and which a host built for WebAssembly needs
// without the network.
package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/Victorystick/scrapscript/eval"
	"github.com/Victorystick/scrapscript/highlight"
	"github.com/Victorystick/scrapscript/host"
	"github.com/Victorystick/scrapscript/token"
	"github.com/Victorystick/scrapscript/yards"
)

// A Run runs a script, which the host has read from the command line,
// with the arguments that follow it, and returns the status to exit with.
type Run func(env *eval.Environment, script *eval.Scrap, args []string) (int, error)

// Main runs the command line of a host of the interface iface, and
// returns the status to exit with:
//
//	name [flags] <script> [args...]
//
// The script is a path to a draft, or a reference source:<hex>. Scraps that
// it refers to by hash are looked up in the interface's scraps first, and
// then in the scrapyard.
func Main(iface host.Interface, run Run) int {
	name := filepath.Base(os.Args[0])
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	printInterface := fs.Bool("interface", false, "print the source of the interface, and exit")
	server := fs.String("server", "https://scraps.oseg.dev/", "the scrapyard to fetch scraps from")
	fs.SetOutput(io.Discard)
	usage := func(w io.Writer) {
		fmt.Fprintf(w, "%s is a host which embeds %s with the type\n\n", name, iface.Ref())
		fmt.Fprintln(w, strings.TrimRight(iface.Source(), "\n"))
		fmt.Fprintf(w, "\nUsage: %s [flags] <script> [args...]\n\n", name)
		fmt.Fprintln(w, "The script is a path to a draft, or a reference source:<hex>.")
		fmt.Fprintln(w, "\nFlags:")
		fs.SetOutput(w)
		fs.PrintDefaults()
	}

	err := fs.Parse(os.Args[1:])
	switch {
	case errors.Is(err, flag.ErrHelp):
		usage(os.Stdout)
		return 0
	case err != nil:
		fmt.Fprintf(os.Stderr, "%s: %v\n\n", name, err)
		usage(os.Stderr)
		return 2
	case *printInterface:
		fmt.Print(iface.Source())
		return 0
	case fs.NArg() == 0:
		usage(os.Stderr)
		return 2
	}

	env := eval.NewEnvironment()
	yard := yards.Validate(yards.ByHttp(*server))
	if cached, err := yards.NewDefaultCacheFetcher(yard); err == nil {
		yard = cached
	}
	env.UseFetcher(yards.InOrder(iface.Fetcher(), yard))

	script, err := readScript(env, fs.Arg(0))
	if err == nil {
		var status int
		if status, err = run(env, script, fs.Args()[1:]); err == nil {
			return status
		}
	}
	fmt.Fprintf(os.Stderr, "%s: %s\n", name, token.Render(err, highlighter(os.Stderr)))
	return 1
}

// highlighter returns what highlights errors for f, if it is a terminal.
func highlighter(f *os.File) token.Highlighter {
	if info, err := f.Stat(); err == nil && info.Mode()&os.ModeCharDevice != 0 {
		return highlight.ANSI
	}
	return nil
}

// readScript reads the script that arg refers to, as a reference or a path.
func readScript(env *eval.Environment, arg string) (*eval.Scrap, error) {
	if digest, ok := strings.CutPrefix(strings.TrimPrefix(arg, "$"), "source:"); ok {
		return env.Fetch("source", digest)
	}
	abs, err := filepath.Abs(arg)
	if err != nil {
		return nil, err
	}
	// The script is a draft at the root of its directory, which its own
	// paths are relative to.
	env.UseDrafts(os.DirFS(filepath.Dir(abs)))
	base := filepath.Base(abs)
	if strings.ContainsAny(base, `"\`) {
		return nil, fmt.Errorf("cannot read a script named %s", base)
	}
	return env.Read([]byte(`$"./` + base + `"`))
}
