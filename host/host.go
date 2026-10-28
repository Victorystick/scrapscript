// Package host is what hosts of platforms share: the command line that
// every host has, and helpers for the bindings that scrap-gen-go
// generates from a platform's interface.
package host

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"iter"
	"os"
	"path/filepath"
	"strings"

	"github.com/Victorystick/scrapscript/eval"
	"github.com/Victorystick/scrapscript/highlight"
	"github.com/Victorystick/scrapscript/token"
	"github.com/Victorystick/scrapscript/yards"
)

// An Interface is a platform's interface, as a host embeds it.
type Interface struct {
	// The sha256 hash of the interface's source, in hex.
	Hash string
	// The sources of the interface and the scraps it refers to, by hash.
	Sources map[string]string
}

// Ref returns the reference by which scraps refer to the interface.
func (i Interface) Ref() string {
	return "$source:" + i.Hash
}

// Source returns the source of the interface.
func (i Interface) Source() string {
	return i.Sources[i.Hash]
}

// Fetcher returns a Fetcher of the interface and the scraps it refers to.
func (i Interface) Fetcher() yards.Fetcher {
	return yards.ByMap(i.Sources)
}

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
func Main(iface Interface, run Run) int {
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

// ListFrom converts a list value with the conversion of its elements.
func ListFrom[T any](v eval.Value, from func(eval.Value) T) []T {
	elems := v.List().Elements()
	xs := make([]T, len(elems))
	for i, e := range elems {
		xs[i] = from(e)
	}
	return xs
}

// ListTo converts a slice to a list value with the conversion of its
// elements.
func ListTo[T any](xs []T, to func(T) eval.Value) eval.Value {
	elems := make([]eval.Value, len(xs))
	for i, x := range xs {
		elems[i] = to(x)
	}
	return eval.NewList(elems...)
}

// A ListRef is a list as a script holds it, whose elements convert to T
// only as they are read, and which goes back to a script as it is.
type ListRef[T any] struct {
	v     eval.Value
	elems []eval.Value
	at    func(eval.Value) T
}

// NewListRef returns the list value v, whose elements at converts.
func NewListRef[T any](v eval.Value, at func(eval.Value) T) ListRef[T] {
	return ListRef[T]{v, v.List().Elements(), at}
}

// Value returns the list as a value.
func (x ListRef[T]) Value() eval.Value { return x.v }

// Len returns the number of elements.
func (x ListRef[T]) Len() int { return len(x.elems) }

// At returns the ith element.
func (x ListRef[T]) At(i int) T { return x.at(x.elems[i]) }

// All returns the elements in order, with their indices.
func (x ListRef[T]) All() iter.Seq2[int, T] {
	return func(yield func(int, T) bool) {
		for i, e := range x.elems {
			if !yield(i, x.at(e)) {
				return
			}
		}
	}
}

// Load converts all the elements.
func (x ListRef[T]) Load() []T { return ListFrom(x.v, x.at) }
