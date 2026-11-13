package eval

import (
	"runtime"
	"testing"
	"time"
)

// TestFramesReused checks that a freed frame is taken by the next call of
// its size, and that the free frames are dropped after a collection.
func TestFramesReused(t *testing.T) {
	l := &limit{}
	a := l.slots(3)
	a[0] = intVal(1)
	l.free(a)
	if b := l.slots(3); &b[0] != &a[0] {
		t.Fatal("a freed frame was not reused")
	}
	l.free(a)
	if c := l.slots(4); len(c) != 4 || &c[0] == &a[0] {
		t.Fatal("a frame of another size was reused")
	}

	// Wait for a collection to be noticed.
	e := gcEpoch.Load()
	for deadline := time.Now().Add(5 * time.Second); gcEpoch.Load() == e; {
		if time.Now().After(deadline) {
			t.Fatal("no collection was noticed")
		}
		runtime.GC()
		time.Sleep(time.Millisecond)
	}
	l.free(make([]value, 5))
	if d := l.slots(3); &d[0] == &a[0] {
		t.Fatal("a frame freed before a collection was reused after it")
	}
}
