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

// lib/monad.scrap's instances, and generic code that takes them.
func TestMonadLibrary(t *testing.T) {
	results, err := testScrap(t, `{ traverse =
    { all = t.eq (m.traverse m.option-monad text/to-int ["1", "2"]) (#some [1, 2])
    , stops = t.eq (m.traverse m.option-monad text/to-int ["1", "x"]) #none
    }
, sequence =
    { result = t.eq (m.sequence m.result-monad [#ok 1, #err "no", #err "late"]) (#err "no")
    , list = t.eq (m.sequence m.list-monad [[1, 2], [3]]) [[1, 3], [2, 3]]
    }
, ops =
    { map = t.eq (o.map (x -> x * 2) (#some 2)) (#some 4)
    , join = t.eq ((m.ops m.list-monad).join [[1], [2, 3]]) [1, 2, 3]
    }
, state = t.eq (number ["a", "b"] 10) { value = [{ n = 10, x = "a" }, { n = 11, x = "b" }], state = 12 }
}
; number : list text -> m.state int (list { n : int, x : text }) =
    m.traverse m.state-monad (x ->
      bind m.get <| n ->
      then (m.put (n + 1)) (pure { n = n, x = x }))
; { bind, pure } = m.state-monad
; { then } = m.ops m.state-monad
; o = m.ops m.option-monad
; m = $"./monad.scrap"
; t = $"./test.scrap"`)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range results {
		if r.Failed {
			t.Errorf("%s: %s", r.Path, r.Message)
		}
	}
	if len(results) != 7 {
		t.Errorf("got %d results, want 7: %v", len(results), results)
	}
}
