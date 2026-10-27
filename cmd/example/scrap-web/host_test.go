package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strconv"
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

// html returns the HTML of the counter's view.
func html(count, name string) string {
	return `<p class="counter">hi ` + name + `<input value=` + strconv.Quote(name) + ` on:input></input><button on:click>` + count + `</button></p>`
}

// A page runs requests in a new module, and shows their views in a frame.
type page struct {
	t          *testing.T
	frame      *frame
	generation int
}

func newPage(t *testing.T) *page {
	current = nil
	resetView()
	return &page{t: t, frame: newFrame()}
}

// run runs the request, and shows the view that it patches.
func (p *page) run(req string) response {
	p.t.Helper()
	var res response
	if err := json.Unmarshal(handle([]byte(req)), &res); err != nil {
		p.t.Fatal(err)
	}
	if res.Patch != nil {
		p.frame.apply(p.t, res.Patch)
		p.generation = res.Generation
	}
	return res
}

// event returns an event of the n-th element with the tag in the view
// shown, with the input, if any.
func (p *page) event(tag string, n int, event string, input ...string) string {
	p.t.Helper()
	e := fmt.Sprintf(`{"generation":%d,"node":%d,"event":%q`, p.generation, p.frame.find(p.t, tag, n), event)
	if len(input) > 0 {
		in, _ := json.Marshal(input[0])
		e += `,"input":` + string(in)
	}
	return e + "}"
}

// send runs a request that sends the events.
func (p *page) send(events ...string) response {
	p.t.Helper()
	return p.run(`{"op":"send","events":[` + strings.Join(events, ",") + `]}`)
}

// loadReq returns a request that loads the counter with the state.
func loadReq(state string, messages ...string) string {
	req := request{Op: "load", Source: `$"./main.scrap"`, State: state, Messages: messages, Scraps: wasmhost.Scraps{Drafts: counter}}
	b, err := json.Marshal(req)
	if err != nil {
		panic(err)
	}
	return string(b)
}

func (p *page) shows(want string) {
	p.t.Helper()
	if got := p.frame.html(); got != want {
		p.t.Errorf("expected the page to show\n%s\ngot\n%s", want, got)
	}
}

func (p *page) state(want string) {
	p.t.Helper()
	if got := p.run(`{"op":"state"}`).State; got != want {
		p.t.Errorf("expected the state %s, got %s", want, got)
	}
}

func steps(res response) string {
	var s []string
	for _, st := range res.Steps {
		s = append(s, fmt.Sprintf("%d %s", st.Step, st.Message))
	}
	return strings.Join(s, ", ")
}

func TestHost(t *testing.T) {
	t.Run("init", func(t *testing.T) {
		p := newPage(t)
		res := p.run(loadReq(""))
		if res.Generation != 1 || !strings.HasPrefix(string(res.Patch), "[0,1,0,0,") {
			t.Errorf("expected generation 1 of a new view, got %+v", res)
		}
		p.shows(html("0", ""))
		p.state(`{ count = 0, name = "" }`)
	})
	t.Run("state", func(t *testing.T) {
		p := newPage(t)
		p.run(loadReq(`$"./state.scrap"`))
		p.shows(html("5", "you"))
	})
	t.Run("events", func(t *testing.T) {
		p := newPage(t)
		p.run(loadReq(""))
		res := p.send(p.event("button", 0, "click"), p.event("input", 0, "input", `"a"`), p.event("button", 0, "click"))
		if got, want := steps(res), `1 #inc, 2 #name "\"a\"", 3 #inc`; got != want {
			t.Errorf("expected steps %s, got %s", want, got)
		}
		if res.Generation != 2 {
			t.Errorf("expected generation 2, got %d", res.Generation)
		}
		p.shows(html("2", `"a"`))
		p.state(`{ count = 2, name = "\"a\"" }`)
	})
	t.Run("a patch changes what changed", func(t *testing.T) {
		p := newPage(t)
		p.run(loadReq(""))
		res := p.send(p.event("button", 0, "click"))
		// The p is node 1, its text 2, the input 3, the button 4, and its
		// text 5.
		if got, want := string(res.Patch), `[4,5,"1"]`; got != want {
			t.Errorf("expected the patch %s, got %s", want, got)
		}
	})
	t.Run("events of an earlier view", func(t *testing.T) {
		// As the page sends an event before it shows the last view.
		p := newPage(t)
		p.run(loadReq(""))
		first := p.event("button", 0, "click")
		p.send(first)
		res := p.send(first, p.event("button", 0, "click"))
		if got, want := steps(res), "2 #inc, 3 #inc"; got != want {
			t.Errorf("expected steps %s, got %s", want, got)
		}
		p.shows(html("3", ""))
	})
	t.Run("events of views it no longer keeps", func(t *testing.T) {
		p := newPage(t)
		p.run(loadReq(""))
		first := p.event("button", 0, "click")
		for range maxViews {
			p.send(p.event("button", 0, "click"))
		}
		if res := p.send(first); res.Steps != nil || res.Error != "" {
			t.Errorf("expected no steps, got %+v", res)
		}
		p.shows(html(fmt.Sprint(maxViews), ""))
	})
	t.Run("goto", func(t *testing.T) {
		p := newPage(t)
		p.run(loadReq(""))
		p.send(p.event("button", 0, "click"), p.event("button", 0, "click"))
		p.run(`{"op":"goto","step":1}`)
		p.shows(html("1", ""))
		p.state(`{ count = 1, name = "" }`)
		if got := p.run(`{"op":"state","step":2}`).State; got != `{ count = 2, name = "" }` {
			t.Errorf("expected the state of step 2, got %s", got)
		}
		// An update after goto drops the steps after it.
		res := p.send(p.event("input", 0, "input", "x"))
		if got, want := steps(res), `2 #name "x"`; got != want {
			t.Errorf("expected steps %s, got %s", want, got)
		}
		p.shows(html("1", "x"))
		if res := p.run(`{"op":"goto","step":3}`); res.Error != "bad request: no step 3" {
			t.Errorf("expected no step 3, got %+v", res)
		}
	})
	t.Run("replay", func(t *testing.T) {
		p := newPage(t)
		res := p.run(loadReq(`$"./state.scrap"`, "#inc", `#name "me"`, "#inc"))
		if res.Replayed != 3 || res.Error != "" {
			t.Errorf("expected 3 messages replayed, got %+v", res)
		}
		p.shows(html("7", "me"))
		if res := p.send(p.event("button", 0, "click")); steps(res) != "4 #inc" {
			t.Errorf("expected step 4, got %s", steps(res))
		}
		p.run(`{"op":"goto","step":1}`)
		p.shows(html("6", "you"))
	})
	t.Run("replay to a step", func(t *testing.T) {
		p := newPage(t)
		one := 1
		req, _ := json.Marshal(request{Op: "load", Source: `$"./main.scrap"`, Messages: []string{"#inc", "#inc"}, Step: &one, Scraps: wasmhost.Scraps{Drafts: counter}})
		res := p.run(string(req))
		if res.Replayed != 2 {
			t.Errorf("expected 2 messages replayed, got %+v", res)
		}
		p.shows(html("1", ""))
	})
	t.Run("replay of messages that no longer fit", func(t *testing.T) {
		p := newPage(t)
		res := p.run(loadReq("", "#inc", "#inc", "#dec", "#inc"))
		if res.Replayed != 2 || !strings.Contains(res.Error, "message 3 is not a message of the app") {
			t.Errorf("expected 2 messages replayed, got %+v", res)
		}
		p.shows(html("2", ""))
		res = p.run(loadReq("", "#dec"))
		if res.Replayed != 0 || res.Patch == nil || !strings.Contains(res.Error, "message 1 is not a message of the app") {
			t.Errorf("expected none replayed, got %+v", res)
		}
	})
	t.Run("a new version of the app patches the view", func(t *testing.T) {
		p := newPage(t)
		p.run(loadReq(""))
		drafts := map[string]string{"main.scrap": strings.Replace(counter["main.scrap"], `"hi "`, `"hello "`, 1)}
		req, _ := json.Marshal(request{Op: "load", Source: `$"./main.scrap"`, Generation: 2, Scraps: wasmhost.Scraps{Drafts: drafts}})
		if got, want := string(p.run(string(req)).Patch), `[4,2,"hello "]`; got != want {
			t.Errorf("expected the patch %s, got %s", want, got)
		}
	})
	t.Run("view", func(t *testing.T) {
		p := newPage(t)
		p.run(loadReq(""))
		p.send(p.event("button", 0, "click"))
		// As for a frame that reloaded.
		p.frame = newFrame()
		p.frame.apply(t, p.run(`{"op":"view"}`).Patch)
		p.shows(html("1", ""))
		if p.generation != 2 {
			t.Errorf("expected generation 2, got %d", p.generation)
		}
	})
	t.Run("generations go on", func(t *testing.T) {
		p := newPage(t)
		req, _ := json.Marshal(request{Op: "load", Source: `$"./main.scrap"`, Generation: 7, Scraps: wasmhost.Scraps{Drafts: counter}})
		if res := p.run(string(req)); res.Generation != 7 {
			t.Errorf("expected generation 7, got %d", res.Generation)
		}
	})
	t.Run("unloaded", func(t *testing.T) {
		p := newPage(t)
		if res := p.run(`{"op":"state"}`); !res.Unloaded {
			t.Errorf("expected unloaded, got %+v", res)
		}
	})
	t.Run("a failed load keeps the app", func(t *testing.T) {
		p := newPage(t)
		p.run(loadReq(""))
		p.run(loadReq(`$"./text-count.scrap"`))
		p.state(`{ count = 0, name = "" }`)
	})
}

func TestHostErrors(t *testing.T) {
	click := `{"op":"send","events":[{"generation":1,"node":1,"event":"click"}]}`
	examples := []struct {
		name     string
		requests []string
		wantErr  string
	}{
		{"not an app", []string{`{"op":"load","source":"1"}`}, "does not implement the interface: 'int' is not 'app a b'"},
		{"state of another type", []string{loadReq(`$"./text-count.scrap"`)},
			"the state is not a model of the app: cannot unify 'int' with 'text'"},
		{"bad state", []string{loadReq(`1 + "a"`)}, "cannot unify 'text' with 'int'"},
		{"bad messages", []string{loadReq("", "#inc", "(")}, "the messages: parse error"},
		{"no such handler", []string{loadReq(""), click}, "bad request: node 1 has no click handler"},
		{"no input", []string{loadReq(""), `{"op":"send","events":[{"generation":1,"node":3,"event":"input"}]}`},
			"bad request: the input handler of node 3 takes an input"},
		{"no such step", []string{loadReq(""), `{"op":"goto","step":1}`}, "bad request: no step 1"},
		{"no step", []string{loadReq(""), `{"op":"goto"}`}, "bad request: no step"},
		{"unknown op", []string{loadReq(""), `{"op":"push"}`}, `bad request: unknown op "push"`},
	}
	for _, ex := range examples {
		t.Run(ex.name, func(t *testing.T) {
			p := newPage(t)
			var res response
			for _, r := range ex.requests {
				res = p.run(r)
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
	p := newPage(t)
	p.run(string(req))
	res := p.send(p.event("input", 0, "input", "x"), p.event("button", 0, "click"), p.event("button", 0, "click"))
	if !strings.Contains(res.Error, "list/repeat: negative count -1") || len(res.Steps) != 1 || res.Generation != 2 {
		t.Errorf("got %+v", res)
	}
	p.shows(html("0", "x"))
}

// The scraps that an app refers to by hash are missing, as for any scrap.
func TestMissing(t *testing.T) {
	inc := `x -> x + 1`
	hash := fmt.Sprintf("%x", sha256.Sum256([]byte(inc)))
	got := string(handle([]byte(loadReq(`$source:` + hash))))
	if want := `{"missing":["` + hash + `"]}`; got != want {
		t.Errorf("expected %s, got %s", want, got)
	}
}
