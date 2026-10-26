package main

import (
	"encoding/json"
	"strings"
	"testing"
)

// The drafts of a counter, which says hi to whom the input names.
var counter = map[string]string{
	"html.scrap": `{ el, text, attr, on }
; el = tag -> attrs -> children -> #el { tag, attrs, children }
; text = t -> #text t
; attr = name -> value -> #attr { name, value }
; on = event -> msg -> #on { event, msg }`,
	"main.scrap": `{ init, update, view }
; init = { count = 0, name = "" }
; update =
  | #inc -> m -> { ..m, count = m.count + 1 }
  | #name n -> m -> { ..m, name = n }
; view = m -> h.el "p" [h.attr "class" "counter"]
  [ h.text ("hi " ++ m.name ++ suffix)
  , h.el "input" [h.attr "value" m.name, #on-value { event = "input", msg = n -> #name n }] []
  , h.el "button" [h.on "click" #inc] [h.text (int/to-text m.count)]
  ]
; suffix = "!"
; h = $"./html.scrap"`,
	"state.scrap":      `{ count = 5, name = "you" }`,
	"text-count.scrap": `{ count = "5", name = "you" }`,
}

// view returns the JSON of the counter's view of the model.
func view(count, name string) string {
	return `{"tag":"p","attrs":{"class":"counter"},"children":["hi ` + name + `!",` +
		`{"tag":"input","attrs":{"value":"` + name + `"},"on":{"input":{"message":"n -` + "\\u003e" + ` #name n","value":true}}},` +
		`{"tag":"button","on":{"click":{"message":"#inc"}},"children":["` + count + `"]}]}`
}

func TestWeb(t *testing.T) {
	input := `"a"`
	examples := []struct {
		name string
		req  request
		want string
	}{
		{"init", request{}, `{"state":"{ count = 0, name = \"\" }","view":` + view("0", "") + `}`},
		{"state", request{State: `$"./state.scrap"`},
			`{"state":"{ count = 5, name = \"you\" }","view":` + view("5", "you") + `}`},
		{"message", request{State: `$"./state.scrap"`, Message: `#inc`},
			`{"state":"{ count = 6, name = \"you\" }","message":"#inc","view":` + view("6", "you") + `}`},
		{"message to init", request{Message: `#inc`},
			`{"state":"{ count = 1, name = \"\" }","message":"#inc","view":` + view("1", "") + `}`},
		{"input", request{State: `$"./state.scrap"`, Message: `n -> #name n`, Input: &input},
			`{"state":"{ count = 5, name = \"\\\"a\\\"\" }","message":"#name \"\\\"a\\\"\"","view":` + view("5", `\"a\"`) + `}`},
	}
	for _, ex := range examples {
		t.Run(ex.name, func(t *testing.T) {
			ex.req.Command, ex.req.Source, ex.req.Drafts = "web", `$"./main.scrap"`, counter
			input, err := json.Marshal(ex.req)
			if err != nil {
				t.Fatal(err)
			}
			if got := string(handle(input)); got != ex.want {
				t.Errorf("expected\n%s\ngot\n%s", ex.want, got)
			}
		})
	}
}

func TestWebErrors(t *testing.T) {
	examples := []struct {
		name    string
		req     request
		wantErr string
	}{
		{"not an app", request{Source: `1`}, "does not implement the interface: 'int' is not 'app a b'"},
		{"state of another type", request{State: `$"./text-count.scrap"`},
			"the state is not a model of the app: cannot unify 'int' with 'text'"},
		{"bad state", request{State: `1 + "a"`}, "cannot unify 'text' with 'int'"},
		{"unknown message", request{Message: `#dec`},
			"the app does not take the message: cannot unify '[ #inc, #name a ]' with '[ #dec, ..b ]'"},
		{"input to a message", request{Message: `#inc`, Input: new(string)},
			"the app does not take the message: cannot unify 'text -> [ #inc, #name a ]' with '[ #inc, ..b ]'"},
		{"failing update", request{Source: `{ ..$"./main.scrap", update = _ -> m -> list/repeat -1 m |> _ -> m }`, Message: `#inc`},
			"list/repeat: negative count -1"},
	}
	for _, ex := range examples {
		t.Run(ex.name, func(t *testing.T) {
			ex.req.Command, ex.req.Drafts = "web", counter
			if ex.req.Source == "" {
				ex.req.Source = `$"./main.scrap"`
			}
			input, err := json.Marshal(ex.req)
			if err != nil {
				t.Fatal(err)
			}
			var got response
			if err := json.Unmarshal(handle(input), &got); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(got.Error, ex.wantErr) {
				t.Errorf("expected error %q, got %+v", ex.wantErr, got)
			}
		})
	}
}

// The scraps that an app refers to by hash are missing as for any scrap.
func TestWebMissing(t *testing.T) {
	inc := `x -> x + 1`
	req := request{Command: "web", Source: `$"./main.scrap"`, Drafts: counter, Message: `$source:` + hash(inc)}
	input, _ := json.Marshal(req)
	if got, want := string(handle(input)), `{"missing":["`+hash(inc)+`"]}`; got != want {
		t.Errorf("expected %s, got %s", want, got)
	}
}
