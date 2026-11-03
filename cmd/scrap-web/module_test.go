package main

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/Victorystick/scrapscript/eval"
)

// spin loops forever on #spin, and in init when its flags aren't null.
var spin = `app
; app : web int msg =
    { init = | #null -> { model = 0, cmds = [] }
             | _     -> { model = loop 0, cmds = [] }
    , update = | #inc  -> n -> { model = n + 1, cmds = [] }
               | #spin -> n -> { model = loop n, cmds = [] }
    , view = n -> #el { tag = "button", attrs = [#on-click #inc, #on { event = "dblclick", msg = #spin }], children = [#text (int/to-text n)] }
    , subscriptions = _ -> []
    }
; msg : type = [#inc, #spin]
; loop = n -> loop n
` + web

func startApp(t *testing.T, m *module, req startRequest) startResponse {
	t.Helper()
	in, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	var res startResponse
	if err := json.Unmarshal(m.start(in), &res); err != nil {
		t.Fatal(err)
	}
	return res
}

// encodeEvent encodes an event as the page does.
func encodeEvent(e Event) []byte {
	b := binary.LittleEndian.AppendUint32(nil, e.Node)
	flags := uint32(0)
	if e.Checked {
		flags = 1
	}
	b = binary.LittleEndian.AppendUint32(b, flags)
	b = binary.LittleEndian.AppendUint32(b, uint32(len(e.Name)))
	return append(append(b, e.Name...), e.Value...)
}

func TestModule(t *testing.T) {
	var m module
	res := startApp(t, &m, startRequest{Source: form})
	if res.Handle != 1 || res.Error != "" {
		t.Fatalf("got %+v", res)
	}
	d := newDOM(t)
	show := func() string {
		t.Helper()
		p, err := m.render(res.Handle)
		if err != nil {
			t.Fatal(err)
		}
		d.apply(decode(t, p))
		var ids []uint32
		return d.print(&ids)
	}
	if got := show(); !strings.Contains(got, `<button on:click>"0"</button>`) {
		t.Errorf("got %s", got)
	}
	// The input is node 2, and the button 4.
	for _, e := range []Event{{Node: 2, Name: "input", Value: "hé"}, {Node: 4, Name: "click"}} {
		if err := m.event(res.Handle, encodeEvent(e)); err != nil {
			t.Fatal(err)
		}
	}
	if got := show(); !strings.Contains(got, `p:value="hé"`) || !strings.Contains(got, `"1"`) {
		t.Errorf("got %s", got)
	}

	// Bad events, and apps that aren't.
	for _, in := range [][]byte{nil, encodeEvent(Event{Node: 2, Name: "input"})[:13]} {
		if err := m.event(res.Handle, in); err == nil || !strings.Contains(err.Error(), "bad event") {
			t.Errorf("got %v for %v", err, in)
		}
	}
	m.stop(res.Handle)
	if _, err := m.render(res.Handle); err == nil {
		t.Errorf("rendered a stopped app")
	}
	if err := m.event(7, encodeEvent(Event{})); err == nil {
		t.Errorf("passed an event to no app")
	}

	// Handles aren't reused.
	if res := startApp(t, &m, startRequest{Source: counter}); res.Handle != 2 {
		t.Errorf("got %+v", res)
	}
}

func TestModuleStart(t *testing.T) {
	inc := `x -> x + 1`
	incHash := fmt.Sprintf("%x", sha256.Sum256([]byte(inc)))
	withInc := strings.Replace(counter, "n + 1", "$source:"+incHash+" n", 1)

	var m module
	for _, c := range []struct {
		name string
		req  startRequest
		want string
	}{
		{"parse error", startRequest{Source: "1 +"}, "parse error"},
		{"not an app", startRequest{Source: "1"}, "does not implement"},
		{"missing", startRequest{Source: withInc}, "missing " + incHash},
		{"with the import", startRequest{Source: withInc, Sources: map[string]string{incHash: inc}}, "handle"},
		{"wrong hash", startRequest{Source: withInc, Sources: map[string]string{incHash: "x -> x"}}, "wrong hash"},
		{"flags", startRequest{Source: spin, Flags: json.RawMessage(`null`), Timeout: 50}, "handle"},
		{"init runs too long", startRequest{Source: spin, Flags: json.RawMessage(`1`), Timeout: 50}, "deadline"},
	} {
		res := startApp(t, &m, c.req)
		var got string
		switch {
		case res.Handle > 0:
			got = "handle"
		case len(res.Missing) > 0:
			got = "missing " + strings.Join(res.Missing, " ")
		default:
			got = res.Error
		}
		if !strings.Contains(got, c.want) {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

func TestModuleBadRequests(t *testing.T) {
	var m module
	for in, want := range map[string]string{
		`{"source": 1}`:                 "bad request",
		`{"source": "1", "flags": [1,}`: "bad request",
	} {
		var res startResponse
		if err := json.Unmarshal(m.start([]byte(in)), &res); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(res.Error, want) {
			t.Errorf("%s: got %+v, want %q", in, res, want)
		}
	}
}

// A step that runs too long fails, and the app goes on.
func TestModuleTimeout(t *testing.T) {
	var m module
	res := startApp(t, &m, startRequest{Source: spin, Timeout: 50})
	if res.Handle == 0 {
		t.Fatalf("got %+v", res)
	}
	if _, err := m.render(res.Handle); err != nil {
		t.Fatal(err)
	}
	if err := m.event(res.Handle, encodeEvent(Event{Node: 1, Name: "dblclick"})); err == nil || !strings.Contains(err.Error(), "deadline") {
		t.Errorf("got %v", err)
	}
	if err := m.event(res.Handle, encodeEvent(Event{Node: 1, Name: "click"})); err != nil {
		t.Error(err)
	}
	p, err := m.render(res.Handle)
	if err != nil {
		t.Fatal(err)
	}
	if ops := decode(t, p); len(ops) != 1 || ops[0].String() != "SET_TEXT 2 1" {
		t.Errorf("got %v", ops)
	}
}

func TestParseJSON(t *testing.T) {
	l := newBindings(eval.NewEnvironment())
	for _, c := range []struct{ in, want string }{
		{`null`, `null`},
		{` [true, false, -1.5e3, "a\"é"] `, `[true,false,-1500,"a\"é"]`},
		{`{"z": {}, "a": [], "m": null}`, `{"z":{},"a":[],"m":null}`},
	} {
		j, err := parseJSON([]byte(c.in))
		if err != nil {
			t.Errorf("%s: %v", c.in, err)
			continue
		}
		if got := string(appendJSON(nil, JsonRef{ref{l, j.toValue(l)}})); got != c.want {
			t.Errorf("%s: got %s, want %s", c.in, got, c.want)
		}
	}
	for _, in := range []string{``, `[1,`, `{"a" 1}`, `1 2`, `nul`} {
		if _, err := parseJSON([]byte(in)); err == nil {
			t.Errorf("%q: parsed", in)
		}
	}
}
