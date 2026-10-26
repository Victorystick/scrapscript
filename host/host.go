// Package host is what the bindings that scrap-gen-go generates from a
// platform's interface share. It is lean, so that hosts without files or a
// network, as in WebAssembly, can use it; the command line that hosts on a
// computer have is in host/cli.
package host

import (
	"github.com/Victorystick/scrapscript/eval"
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
