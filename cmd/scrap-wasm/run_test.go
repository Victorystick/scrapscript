package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func hash(src string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(src)))
}

func TestCheck(t *testing.T) {
	inc := `x -> x + 1`
	twice := `f -> x -> f (f x)`
	// Refers to inc directly, and to twice through use.
	use := `$source:` + hash(twice) + ` $source:` + hash(inc)
	both := map[string]string{hash(inc): inc, hash(twice): twice}

	examples := []struct {
		name    string
		req     request
		want    response
		wantErr string
	}{
		{"checks", request{Source: `1 + 2`}, response{Type: "int"}, ""},
		{"type error", request{Source: `1 + "a"`}, response{}, "cannot unify 'text' with 'int'"},
		{"parse error", request{Source: `1 +`}, response{}, "parse error"},
		{"path", request{Source: `$"./inc.scrap" 1`}, response{}, "cannot refer to a path here"},
		{"missing", request{Source: use}, response{Missing: sorted(hash(inc), hash(twice))}, ""},
		{"missing transitively", request{
			Source:  `$source:` + hash(use) + ` 1`,
			Sources: map[string]string{hash(use): use},
		}, response{Missing: sorted(hash(inc), hash(twice))}, ""},
		{"with imports", request{Source: use + ` 1`, Sources: both}, response{Type: "int"}, ""},
		{"ill-typed import", request{
			Source:  `$source:` + hash(`1 + "a"`),
			Sources: map[string]string{hash(`1 + "a"`): `1 + "a"`},
		}, response{}, "cannot unify 'text' with 'int'"},
		{"evaluates", request{Source: `1 + 2`, Command: "eval"}, response{Type: "int", Value: "3"}, ""},
		{"evaluates imports", request{Source: use + ` 1`, Sources: both, Command: "eval"}, response{Type: "int", Value: "3"}, ""},
		{"prints functions as scraps", request{Source: "inc\n; inc = x -> x + 1", Command: "eval"},
			response{Type: "int -> int", Value: "inc\n; inc = x -> x + 1"}, ""},
		{"evaluation fails", request{Source: `list/repeat -1 0`, Command: "eval"},
			response{Type: "list int", Error: "list/repeat: negative count -1"}, ""},
		{"type error before evaluation", request{Source: `1 + "a"`, Command: "eval"}, response{}, "cannot unify 'text' with 'int'"},
		{"tests", request{Source: `{ adds = #eq { got = 1 + 1, want = 2 }, fails = [#pass, #fail "why"] }`, Command: "test"},
			response{
				Type: "{ adds : [ #eq { got : int, want : int }, ..a ], fails : list [ #fail text, #pass, ..b ] }",
				Results: []result{
					{Path: "adds"},
					{Path: "fails[0]"},
					{Path: "fails[1]", Failed: true, Message: "why"},
				},
			}, ""},
		{"no tests", request{Source: `{}`, Command: "test"}, response{Type: "{}", Results: []result{}}, ""},
		{"not a test scrap", request{Source: `1`, Command: "test"}, response{}, "is not a test case"},
		{"unknown command", request{Source: `1`, Command: "push"}, response{}, `bad request: unknown command "push"`},
		{"wrong hash", request{
			Source:  `$source:` + hash(inc) + ` 1`,
			Sources: map[string]string{hash(inc): twice},
		}, response{}, "the source of $source:" + hash(inc) + " has a different hash"},
	}
	for _, ex := range examples {
		t.Run(ex.name, func(t *testing.T) {
			input, err := json.Marshal(ex.req)
			if err != nil {
				t.Fatal(err)
			}
			var got response
			if err := json.Unmarshal(handle(input), &got); err != nil {
				t.Fatal(err)
			}
			if ex.wantErr != "" {
				if !strings.Contains(got.Error, ex.wantErr) {
					t.Errorf("expected error %q, got %+v", ex.wantErr, got)
				}
				return
			}
			if !reflect.DeepEqual(got, ex.want) {
				t.Errorf("expected %+v, got %+v", ex.want, got)
			}
		})
	}
}

func TestBadRequest(t *testing.T) {
	if got := string(handle([]byte(`nope`))); !strings.Contains(got, `"error":"bad request:`) {
		t.Errorf("expected a bad request error, got %s", got)
	}
}

func sorted(xs ...string) []string {
	if xs[0] > xs[1] {
		xs[0], xs[1] = xs[1], xs[0]
	}
	return xs
}

// A test scrap without cases has an empty list of results, not none, which
// would look like a response to "type".
func TestNoTestsJSON(t *testing.T) {
	if got := string(handle([]byte(`{"source":"{}","command":"test"}`))); got != `{"type":"{}","results":[]}` {
		t.Errorf("got %s", got)
	}
}
