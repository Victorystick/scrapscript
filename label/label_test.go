package label

import (
	"sync"
	"testing"
)

func TestTable(t *testing.T) {
	var tab Table
	if _, ok := tab.Lookup("a"); ok {
		t.Fatal("empty table has a")
	}
	b, a := tab.Of("b"), tab.Of("a")
	if a == None || b == None || a == b {
		t.Fatalf("ids a=%d b=%d", a, b)
	}
	if tab.Of("a") != a {
		t.Error("interning twice gave a new id")
	}
	if id, ok := tab.Lookup("b"); !ok || id != b {
		t.Errorf("Lookup(b) = %d, %v", id, ok)
	}
	if tab.Name(a) != "a" || tab.Name(b) != "b" {
		t.Errorf("names %q %q", tab.Name(a), tab.Name(b))
	}
	if tab.Compare(a, b) >= 0 || tab.Compare(b, a) <= 0 || tab.Compare(a, a) != 0 {
		t.Error("Compare doesn't order by name")
	}
}

func TestNameUnknown(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("Name(None) didn't panic")
		}
	}()
	var tab Table
	tab.Name(None)
}

func TestConcurrent(t *testing.T) {
	var tab Table
	var wg sync.WaitGroup
	ids := make([]ID, 8)
	for i := range ids {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ids[i] = tab.Of("x")
			tab.Name(ids[i])
		}()
	}
	wg.Wait()
	for _, id := range ids {
		if id != ids[0] {
			t.Fatalf("ids differ: %v", ids)
		}
	}
}
