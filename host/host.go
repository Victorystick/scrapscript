// Package host is what hosts of platforms share: helpers for the bindings
// that scrap-gen-go generates from a platform's interface. It reaches
// outside the process only through what an Environment is given, so that
// hosts built for WebAssembly need no network; the command line is in
// package cli.
package host

import (
	"errors"
	"iter"

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

// Fetcher returns a Fetcher of the interface and the scraps it refers to.
func (i Interface) Fetcher() eval.Fetcher {
	return sources(i.Sources)
}

// sources is a Fetcher of scraps by their sha256 hash in hex.
type sources map[string]string

var errNotFound = errors.New("no scrap found")

func (s sources) FetchSha256(key string) ([]byte, error) {
	if src, ok := s[key]; ok {
		return []byte(src), nil
	}
	return nil, errNotFound
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
