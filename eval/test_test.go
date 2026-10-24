package eval_test

import (
	"os"
	"strings"
	"testing"

	"github.com/Victorystick/scrapscript/eval"
)

// testScrap runs the test scrap source, which may refer to lib/test.scrap
// as $"./test.scrap".
func testScrap(t *testing.T, source string) ([]eval.TestResult, error) {
	t.Helper()
	e := eval.NewEnvironment()
	e.UseDrafts(os.DirFS("../lib"))
	return e.Test(read(t, e, source))
}

func TestTest(t *testing.T) {
	results, err := testScrap(t, `{ insert =
    { keeps-order = t.eq [1, 2, 3] [1, 2, 3]
    , replaces = t.eq (#some "a") #none
    }
, sizes = list/map (n -> t.eq (list/length (list/repeat n ())) n) [0, 1, 7]
, close = t.float-eq 0.01 1.0 1.005
, far = t.float-eq 0.01 1.0 1.5
, mixed = [t.pass, t.fail "no", t.eq { a = #x, b = "" } { a = #x (), b = "" }]
, bare = #eq { got = 1, want = "1" }
}
; t = $"./test.scrap"`)
	if err != nil {
		t.Fatal(err)
	}
	want := []eval.TestResult{
		{Path: "bare", Failed: true, Message: "got  1\nwant \"1\""},
		{Path: "close"},
		{Path: "far", Failed: true, Message: "got  1.0\nwant 1.5 within 0.01"},
		{Path: "insert.keeps-order"},
		{Path: "insert.replaces", Failed: true, Message: "got  #some \"a\"\nwant #none"},
		{Path: "mixed[0]"},
		{Path: "mixed[1]", Failed: true, Message: "no"},
		{Path: "mixed[2]"},
		{Path: "sizes[0]"},
		{Path: "sizes[1]"},
		{Path: "sizes[2]"},
	}
	if len(results) != len(want) {
		t.Fatalf("got %d results, want %d: %v", len(results), len(want), results)
	}
	for i := range want {
		if results[i] != want[i] {
			t.Errorf("result %d: got %#v, want %#v", i, results[i], want[i])
		}
	}
}

// lib/ord.scrap's functions take their orderings as instances.
func TestOrdLibrary(t *testing.T) {
	results, err := testScrap(t, `{ max = t.eq (o.max 3 4) 4
, min = t.eq (o.min "b" "a") "a"
, sort = t.eq (o.sort [3, 1, 2]) [1, 2, 3]
, lists = t.eq (o.sort [[2], [1, 5], [1]]) [[1], [1, 5], [2]]
, reverse = t.eq (o.sort [1, 3, 2] ; _ = o.reverse o.int) [3, 2, 1]
}
; _ = o.int
; _ = o.text
; _ = o.list
; o = $"./ord.scrap"
; t = $"./test.scrap"`)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 5 {
		t.Fatalf("got %d results, want 5: %v", len(results), results)
	}
	for _, r := range results {
		if r.Failed {
			t.Errorf("%s: %s", r.Path, r.Message)
		}
	}
}

func TestTestRejects(t *testing.T) {
	failures := []struct{ source, error string }{
		{`1`, `the test scrap is not a test case`},
		{`{ a = { b = 1 } }`, `a.b is not a test case`},
		{`{ a = #some 1 }`, `a #some is not a test case`},
		{`{ a = t.eq (x -> x) (x -> x) } ; t = $"./test.scrap"`, `a cannot compare functions`},
		{`{ a = [t.eq [x -> x] []] } ; t = $"./test.scrap"`, `a[] cannot compare functions`},
		{`{ a = #eq 1 }`, `a #eq takes { got, want }`},
		{`{ a = #float-eq { got = 1, want = 1, within = 1 } }`, `a #float-eq takes`},
		{`{ a = #pass 1 }`, `a #pass takes no payload`},
		{`{ a = #fail 1 }`, `a #fail takes text`},
		{`t ; t : type = int`, `cannot test a type`},
	}
	for _, f := range failures {
		_, err := testScrap(t, f.source)
		if err == nil || !strings.Contains(err.Error(), f.error) {
			t.Errorf("%s: expected an error containing %q, got %v", f.source, f.error, err)
		}
	}
}
