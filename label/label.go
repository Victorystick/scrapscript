// Package label interns record labels and enum tags to small integers, which
// the type checker, the evaluator and a host's bindings share through one
// Table per environment. IDs are only meaningful in the table that made
// them; they are never printed, hashed or part of a scrap's identity.
package label

import (
	"strings"
	"sync"
)

// An ID is an interned label or tag. Tags and record labels share one
// namespace, as rows do in the type graph.
type ID uint32

// None is the zero ID. It names nothing, so an unset ID is detectable.
const None ID = 0

// A Table interns names to IDs. Its zero value is empty and ready to use.
// It only grows, and is safe for concurrent use, so that hosts may build
// and read values from many goroutines. A Table must not be copied after
// first use.
type Table struct {
	mu    sync.RWMutex
	names []string // by ID; names[None] is unused
	ids   map[string]ID
}

// Of interns name and returns its ID.
func (t *Table) Of(name string) ID {
	if id, ok := t.Lookup(name); ok {
		return id
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if id, ok := t.ids[name]; ok {
		return id
	}
	if t.ids == nil {
		t.ids = make(map[string]ID)
		t.names = append(t.names, "")
	}
	id := ID(len(t.names))
	t.names = append(t.names, name)
	t.ids[name] = id
	return id
}

// Lookup returns name's ID without interning it, and false if it has none.
func (t *Table) Lookup(name string) (ID, bool) {
	t.mu.RLock()
	id, ok := t.ids[name]
	t.mu.RUnlock()
	return id, ok
}

// Name returns the name of id. It panics on an ID that t did not make.
func (t *Table) Name(id ID) string {
	t.mu.RLock()
	ok := id != None && int(id) < len(t.names)
	var name string
	if ok {
		name = t.names[id]
	}
	t.mu.RUnlock()
	if !ok {
		panic("label: unknown ID")
	}
	return name
}

// Compare orders IDs by name, not by number, so that sorted output is the
// same whatever order the names were interned in.
func (t *Table) Compare(a, b ID) int {
	return strings.Compare(t.Name(a), t.Name(b))
}
