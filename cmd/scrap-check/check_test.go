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
