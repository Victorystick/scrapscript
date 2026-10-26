package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/Victorystick/scrapscript/host/wasmhost"
)

// The drafts of a counter, which says hi to whom the input names.
var counter = map[string]string{
	"main.scrap": `{ init, update, view }
; init = { count = 0, name = "" }
; update =
  | #inc -> m -> { ..m, count = m.count + 1 }
  | #name n -> m -> { ..m, name = n }
; view = m -> #el { tag = "p", attrs = [#attr { name = "class", value = "counter" }], children =
  [ #text ("hi " ++ m.name)
  , #el { tag = "input", attrs = [#attr { name = "value", value = m.name }, #on-value { event = "input", msg = n -> #name n }], children = [] }
  , #el { tag = "button", attrs = [#on { event = "click", msg = #inc }], children = [#text (int/to-text m.count)] }
  ] }`,
	"state.scrap":      `{ count = 5, name = "you" }`,
	"text-count.scrap": `{ count = "5", name = "you" }`,
}

// view returns the JSON of the counter's view.
func view(count, name string) string {
	return `{"tag":"p","attrs":{"class":"counter"},"children":["hi ` + name + `",` +
		`{"tag":"input","attrs":{"value":"` + name + `"},"on":{"input":{"handler":0,"value":true}}},` +
		`{"tag":"button","on":{"click":{"handler":1}},"children":["` + count + `"]}]}`
}

// run runs the requests, as JSON, in a new module, and returns the
// responses.
func run(t *testing.T, requests ...string) []string {
	t.Helper()
	current = nil
	var out []string
	for _, r := range requests {
		out = append(out, string(handle([]byte(r))))
	}
	return out
}

// loadReq returns a request that loads the counter with the state.
func loadReq(state string) string {
	req := request{Op: "load", Source: `$"./main.scrap"`, State: state, Scraps: wasmhost.Scraps{Drafts: counter}}
	b, err := json.Marshal(req)
	if err != nil {
		panic(err)
	}
	return string(b)
}

func check(t *testing.T, got []string, want ...string) {
	t.Helper()
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("response %d: expected\n%s\ngot\n%s", i, want[i], got[i])
		}
	}
}

func TestHost(t *testing.T) {
	t.Run("init", func(t *testing.T) {
		check(t, run(t, loadReq(""), `{"op":"state"}`),
			`{"view":`+view("0", "")+`,"generation":1}`,
			`{"state":"{ count = 0, name = \"\" }"}`)
	})
	t.Run("state", func(t *testing.T) {
		check(t, run(t, loadReq(`$"./state.scrap"`)), `{"view":`+view("5", "you")+`,"generation":1}`)
	})
	t.Run("events", func(t *testing.T) {
		check(t, run(t, loadReq(""),
			`{"op":"send","events":[{"generation":1,"handler":1},{"generation":1,"handler":0,"input":"\"a\""},{"generation":1,"handler":1}]}`,
			`{"op":"state"}`,
		),
			`{"view":`+view("0", "")+`,"generation":1}`,
			`{"view":`+view("2", `\"a\"`)+`,"generation":2,"steps":[`+
				`{"step":1,"message":"#inc"},{"step":2,"message":"#name \"\\\"a\\\"\""},{"step":3,"message":"#inc"}]}`,
			`{"state":"{ count = 2, name = \"\\\"a\\\"\" }"}`)
	})
	t.Run("events of an earlier view", func(t *testing.T) {
		// As the page sends an event before it shows the last view.
		check(t, run(t, loadReq(""),
			`{"op":"send","events":[{"generation":1,"handler":1}]}`,
			`{"op":"send","events":[{"generation":1,"handler":1},{"generation":2,"handler":1}]}`,
		)[1:],
			`{"view":`+view("1", "")+`,"generation":2,"steps":[{"step":1,"message":"#inc"}]}`,
			`{"view":`+view("3", "")+`,"generation":3,"steps":[{"step":2,"message":"#inc"},{"step":3,"message":"#inc"}]}`)
	})
	t.Run("events of views it no longer keeps", func(t *testing.T) {
		reqs := []string{loadReq("")}
		for g := 1; g <= maxViews; g++ {
			reqs = append(reqs, fmt.Sprintf(`{"op":"send","events":[{"generation":%d,"handler":1}]}`, g))
		}
		reqs = append(reqs, `{"op":"send","events":[{"generation":1,"handler":1}]}`, `{"op":"send","events":[{"generation":2,"handler":1}]}`)
		got := run(t, reqs...)
		check(t, got[len(got)-2:], `{}`, `{"view":`+view(fmt.Sprint(maxViews+1), "")+`,"generation":18,"steps":[{"step":17,"message":"#inc"}]}`)
	})
	t.Run("goto", func(t *testing.T) {
		check(t, run(t, loadReq(""),
			`{"op":"send","events":[{"generation":1,"handler":1},{"generation":1,"handler":1}]}`,
			`{"op":"goto","step":1}`,
			`{"op":"send","events":[{"generation":3,"handler":1}]}`,
		)[2:],
			`{"view":`+view("1", "")+`,"generation":3}`,
			`{"view":`+view("2", "")+`,"generation":4,"steps":[{"step":3,"message":"#inc"}]}`)
	})
	t.Run("generations go on", func(t *testing.T) {
		req, _ := json.Marshal(request{Op: "load", Source: `$"./main.scrap"`, Generation: 7, Scraps: wasmhost.Scraps{Drafts: counter}})
		check(t, run(t, string(req)), `{"view":`+view("0", "")+`,"generation":7}`)
	})
	t.Run("unloaded", func(t *testing.T) {
		check(t, run(t, `{"op":"state"}`), `{"unloaded":true}`)
	})
	t.Run("a failed load keeps the app", func(t *testing.T) {
		check(t, run(t, loadReq(""), loadReq(`$"./text-count.scrap"`), `{"op":"state"}`)[2:],
			`{"state":"{ count = 0, name = \"\" }"}`)
	})
}

func TestHostErrors(t *testing.T) {
	examples := []struct {
		name     string
		requests []string
		wantErr  string
	}{
		{"not an app", []string{`{"op":"load","source":"1"}`}, "does not implement the interface: 'int' is not 'app a b'"},
		{"state of another type", []string{loadReq(`$"./text-count.scrap"`)},
			"the state is not a model of the app: cannot unify 'int' with 'text'"},
		{"bad state", []string{loadReq(`1 + "a"`)}, "cannot unify 'text' with 'int'"},
		{"no such handler", []string{loadReq(""), `{"op":"send","events":[{"generation":1,"handler":2}]}`}, "bad request: no handler 2"},
		{"no input", []string{loadReq(""), `{"op":"send","events":[{"generation":1,"handler":0}]}`}, "bad request: handler 0 takes an input"},
		{"no such step", []string{loadReq(""), `{"op":"goto","step":1}`}, "bad request: no step 1"},
		{"unknown op", []string{loadReq(""), `{"op":"push"}`}, `bad request: unknown op "push"`},
	}
	for _, ex := range examples {
		t.Run(ex.name, func(t *testing.T) {
			got := run(t, ex.requests...)
			var res response
			if err := json.Unmarshal([]byte(got[len(got)-1]), &res); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(res.Error, ex.wantErr) {
				t.Errorf("expected error %q, got %+v", ex.wantErr, res)
			}
		})
	}
}

// A failing update stops the events, after showing what those before it
// led to.
func TestFailingUpdate(t *testing.T) {
	drafts := map[string]string{"main.scrap": strings.Replace(counter["main.scrap"],
		`| #inc -> m -> { ..m, count = m.count + 1 }`,
		`| #inc -> m -> (list/repeat (m.count - 1) 0 |> _ -> { ..m, count = m.count + 1 })`, 1)}
	req, _ := json.Marshal(request{Op: "load", Source: `$"./main.scrap"`, State: `{ count = 0, name = "" }`, Scraps: wasmhost.Scraps{Drafts: drafts}})
	got := run(t, string(req), `{"op":"send","events":[{"generation":1,"handler":0,"input":"x"},{"generation":1,"handler":1},{"generation":1,"handler":1}]}`)
	var res response
	if err := json.Unmarshal([]byte(got[1]), &res); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Error, "list/repeat: negative count -1") || len(res.Steps) != 1 || res.Generation != 2 {
		t.Errorf("got %s", got[1])
	}
}

// The scraps that an app refers to by hash are missing, as for any scrap.
func TestMissing(t *testing.T) {
	inc := `x -> x + 1`
	hash := fmt.Sprintf("%x", sha256.Sum256([]byte(inc)))
	got := run(t, loadReq(`$source:`+hash))
	if want := `{"missing":["` + hash + `"]}`; got[0] != want {
		t.Errorf("expected %s, got %s", want, got[0])
	}
}
