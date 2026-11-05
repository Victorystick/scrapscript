package main

import (
	"encoding/binary"
	"strings"
	"testing"
)

// fetcher makes requests of each kind, and logs what comes back in a
// property of its view; a click makes three more, the last of which fails
// in its continuation.
var fetcher = `app
; app : web (list json) msg =
    { init = _ -> { model = [], cmds =
        [ #http { method = "GET", url = "/t", headers = [{ name = "accept", value = "text/plain" }], body = #empty, expect = #text (r -> #got-text r) }
        , #http { method = "POST", url = "/b", headers = [], body = #bytes ~~AAH/, expect = #bytes (r -> #got-bytes r) }
        , #http { method = "PUT", url = "/j", headers = [], body = #json (#object [{ key = "a", value = #list [#number 1.5, #null, #bool #true, #text "q\""] }]), expect = #json (r -> #got-json r) }
        , #http { method = "POST", url = "/s", headers = [{ name = "Content-Type", value = "text/x" }], body = #json #null, expect = #json (r -> #got-json r) }
        , #now (_ -> #again)
        ] }
    , update =
        | #got-text (#ok r)  -> log -> step (log +< #list [#text "text", #text (int/to-text r.status), #list (list/map (h -> #text (h.name ++ ":" ++ h.value)) r.headers), #text r.body, #text (int/to-text (text/length r.body))])
        | #got-bytes (#ok r) -> log -> step (log +< #list [#text "bytes", #text (int/to-text r.status), #text (int/to-text (bytes/length r.body))])
        | #got-json (#ok r)  -> log -> step (log +< #list [#text "json", #text (int/to-text r.status), r.body])
        | #got-text (#error e)  -> log -> step (log +< #text e)
        | #got-bytes (#error e) -> log -> step (log +< #text e)
        | #got-json (#error e)  -> log -> step (log +< #text e)
        | #again -> log -> { model = log, cmds =
            [ #http { method = "GET", url = "/again", headers = [], body = #empty, expect = #text (r -> #got-text r) }
            , #http { method = "GET", url = "/missing", headers = [], body = #empty, expect = #bytes (r -> #got-bytes r) }
            , #http { method = "GET", url = "/deep", headers = [], body = #empty, expect = #json (_ -> #got-json (#error (int/to-text (deep 0)))) }
            ] }
    , view = log -> #el { tag = "p", attrs = [#prop { name = "log", value = #list log }, #on-click #again], children = [] }
    , subscriptions = _ -> []
    }
; step = log -> { model = log, cmds = [] }
; deep = n -> 1 + deep n
; msg : type =
    [ #got-text (result text)
    , #got-bytes (result bytes)
    , #got-json (result json)
    , #again
    ]
; result : type -> type = body => [#ok { status : int, headers : list { name : text, value : text }, body : body }, #error text]
; json : type = [#null, #bool [#false, #true], #number float, #text text, #list (list json), #object (list { key : text, value : json })]
` + web

// encodeResponse encodes a response as the page does.
func encodeResponse(id, status uint32, headers []Header, body string) []byte {
	b := binary.LittleEndian.AppendUint32(nil, id)
	b = binary.LittleEndian.AppendUint32(b, status)
	b = binary.LittleEndian.AppendUint32(b, uint32(len(headers)))
	for _, h := range headers {
		b = binary.LittleEndian.AppendUint32(b, uint32(len(h.Name)))
		b = binary.LittleEndian.AppendUint32(b, uint32(len(h.Value)))
	}
	for _, h := range headers {
		b = append(append(b, h.Name...), h.Value...)
	}
	return append(b, body...)
}

func TestHTTP(t *testing.T) {
	var m module
	res := startApp(t, &m, startRequest{Source: fetcher})
	if res.Handle == 0 {
		t.Fatalf("got %+v", res)
	}
	h := res.Handle
	commands := func(want string) {
		t.Helper()
		got, err := m.commands(h)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != want {
			t.Errorf("got commands\n%s\nwant\n%s", got, want)
		}
	}
	respond := func(input []byte) {
		t.Helper()
		if err := m.response(h, input); err != nil {
			t.Fatal(err)
		}
	}
	log := func(want string) {
		t.Helper()
		view, err := m.apps[h-1].s.View()
		if err != nil {
			t.Fatal(err)
		}
		if got := string(appendJSON(nil, view.El().Attrs().At(0).Prop().Value())); got != want {
			t.Errorf("got log\n%s\nwant\n%s", got, want)
		}
	}

	// Init's commands, with ids: requests, and #now. A JSON body is text,
	// with its content type unless the request gives one.
	commands(`[{"id":1,"http":{"method":"GET","url":"/t","headers":[["accept","text/plain"]]}},` +
		`{"id":2,"http":{"method":"POST","url":"/b","headers":[],"bytes":"AAH/"}},` +
		`{"id":3,"http":{"method":"PUT","url":"/j","headers":[["content-type","application/json"]],"text":"{\"a\":[1.5,null,true,\"q\\\"\"]}"}},` +
		`{"id":4,"http":{"method":"POST","url":"/s","headers":[["Content-Type","text/x"]],"text":"null"}},` +
		`{"id":5}]`)
	commands(``)
	// Waking a request does nothing.
	if err := m.wake(h, 1); err != nil {
		t.Fatal(err)
	}

	// Responses, in any order. Text is UTF-8 with invalid bytes replaced;
	// as in Elm, a status that isn't 2xx is an error, as is a body that
	// isn't the JSON expected.
	respond(encodeResponse(3, 200, nil, ` {"b": [2, {}], "a": "x"} `))
	respond(encodeResponse(1, 200, []Header{{"content-type", "text/plain"}, {"x", ""}}, "hi \xff"))
	respond(encodeResponse(2, 201, nil, "\x00\x01\x02"))
	respond(encodeResponse(4, 200, nil, "{nope"))
	log(`[["json","200",{"b":[2,{}],"a":"x"}],` +
		`["text","200",["content-type:text/plain","x:"],"hi �","6"],` +
		`["bytes","201","3"],` +
		`"bad body: invalid character 'n'"]`)

	// Responses to requests that the app doesn't await are ignored, as
	// are those to commands that aren't requests.
	respond(encodeResponse(5, 200, nil, ""))
	respond(encodeResponse(1, 200, nil, "again"))
	respond(encodeResponse(99, 200, nil, ""))
	commands(``)

	// An event's requests.
	if _, err := m.render(h); err != nil {
		t.Fatal(err)
	}
	if err := m.event(h, encodeEvent(Event{Node: 1, Name: "click"})); err != nil {
		t.Fatal(err)
	}
	commands(`[{"id":6,"http":{"method":"GET","url":"/again","headers":[]}},` +
		`{"id":7,"http":{"method":"GET","url":"/missing","headers":[]}},` +
		`{"id":8,"http":{"method":"GET","url":"/deep","headers":[]}}]`)

	// A failed request, and a bad status.
	before := `[["json","200",{"b":[2,{}],"a":"x"}],["text","200",["content-type:text/plain","x:"],"hi �","6"],["bytes","201","3"],"bad body: invalid character 'n'"`
	respond(encodeResponse(6, 0, nil, "Failed to fetch"))
	respond(encodeResponse(7, 404, nil, "not here"))
	before += `,"Failed to fetch","bad status: 404"`
	log(before + `]`)

	// A continuation that fails is a step that fails: the model stays as
	// it was, and the request is done.
	if err := m.response(h, encodeResponse(8, 500, nil, "")); err == nil || !strings.Contains(err.Error(), "calls nested deeper") {
		t.Errorf("got %v, want the call depth error", err)
	}
	log(before + `]`)
	respond(encodeResponse(8, 200, nil, ""))

	// Bad responses.
	for _, in := range [][]byte{nil, encodeResponse(1, 200, nil, "")[:11], encodeResponse(1, 200, []Header{{"a", "b"}}, "")[:21]} {
		if err := m.response(h, in); err == nil || !strings.Contains(err.Error(), "bad response") {
			t.Errorf("got %v for %v", err, in)
		}
	}
	// A header count too large for the input.
	if err := m.response(h, encodeResponse(1, 200, nil, "")[:8:8]); err == nil {
		t.Error("took a response without a header count")
	}
	if err := m.response(h, append(encodeResponse(1, 200, nil, "")[:8], 0xff, 0xff, 0xff, 0xff)); err == nil || !strings.Contains(err.Error(), "bad response") {
		t.Errorf("got %v for a huge header count", err)
	}

	m.stop(h)
	if _, err := m.commands(h); err == nil {
		t.Error("took the commands of a stopped app")
	}
	if err := m.response(h, encodeResponse(1, 200, nil, "")); err == nil {
		t.Error("passed a response to a stopped app")
	}
}
