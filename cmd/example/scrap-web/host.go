// scrap-web hosts the web platform, in a web page: it runs an app, whose
// interface is app.scrap, as Elm runs one. It is a WebAssembly module that
// a page runs requests with, as wasmhost describes, and keeps the app and
// its model between them. The page shows the view that it patches (see
// dom.go), and sends it the events that the view's handlers handle. Build
// it with:
//
//	GOOS=wasip1 GOARCH=wasm go build -buildmode=c-shared -o scrap-web.wasm ./cmd/example/scrap-web
//
// See README.md for the requests.
package main

//go:generate go run ../../scrap-gen-go -main=false app.scrap

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/Victorystick/scrapscript/eval"
	"github.com/Victorystick/scrapscript/host"
	"github.com/Victorystick/scrapscript/host/wasmhost"
	"github.com/Victorystick/scrapscript/token"
)

func main() {}

func init() {
	wasmhost.Serve(handle)
}

type request struct {
	wasmhost.Scraps
	Op string `json:"op"`
	// What "load" loads: the app, and a scrap of its model, if not init,
	// and the messages, as scraps, to update it with, as a page keeps them
	// to load a changed app with.
	Source   string   `json:"source"`
	State    string   `json:"state"`
	Messages []string `json:"messages"`
	// The generation of the first view that "load" makes, 1 by default. A
	// page that loads apps anew passes one after the last it showed, so
	// that it can't mistake events of an earlier view for the last's.
	Generation int `json:"generation"`
	// The events that "send" updates the model with.
	Events []event `json:"events"`
	// The step that "goto" goes to, that "state" prints the model of, and
	// that "load" shows, rather than the last.
	Step *int `json:"step"`
}

// An event is one that an element of the view of a generation handled,
// with the value of its target, for a handler that is a function of it.
type event struct {
	Generation int     `json:"generation"`
	Node       int     `json:"node"`
	Event      string  `json:"event"`
	Input      *string `json:"input"`
}

type response struct {
	Missing []string `json:"missing,omitempty"`
	Error   string   `json:"error,omitempty"`
	// Whether no app is loaded, as when the module is new.
	Unloaded bool `json:"unloaded,omitempty"`
	// The patch that shows the view, as dom.go describes, and its
	// generation.
	Patch      json.RawMessage `json:"patch,omitempty"`
	Generation int             `json:"generation,omitempty"`
	// How many of the messages that "load" was given it updated the model
	// with, as all but those after one that failed.
	Replayed int    `json:"replayed,omitempty"`
	Steps    []step `json:"steps,omitempty"`
	State    string `json:"state,omitempty"`
}

// A step is an update of the model, by a message, as a scrap. Step 0 is
// the model that the app was loaded with.
type step struct {
	Step    int    `json:"step"`
	Message string `json:"message"`
}

// handle answers the JSON request in input.
func handle(input []byte) []byte {
	var req request
	var res response
	if err := json.Unmarshal(input, &req); err != nil {
		res.Error = "bad request: " + err.Error()
	} else {
		res = respond(req)
	}
	return res.append(nil)
}

// append appends the response as JSON. Unlike encoding/json, it copies the
// patch, which is most of a response, as it is.
func (r *response) append(b []byte) []byte {
	b = append(b, '{')
	field := func(name string) {
		if len(b) > 1 {
			b = append(b, ',')
		}
		b = append(b, '"')
		b = append(b, name...)
		b = append(b, '"', ':')
	}
	if r.Missing != nil {
		field("missing")
		b = append(b, '[')
		for i, m := range r.Missing {
			if i > 0 {
				b = append(b, ',')
			}
			b = appendString(b, m)
		}
		b = append(b, ']')
	}
	if r.Error != "" {
		field("error")
		b = appendString(b, r.Error)
	}
	if r.Unloaded {
		field("unloaded")
		b = append(b, "true"...)
	}
	if r.Patch != nil {
		field("patch")
		b = append(b, r.Patch...)
	}
	if r.Generation != 0 {
		field("generation")
		b = strconv.AppendInt(b, int64(r.Generation), 10)
	}
	if r.Replayed != 0 {
		field("replayed")
		b = strconv.AppendInt(b, int64(r.Replayed), 10)
	}
	if r.Steps != nil {
		field("steps")
		b = append(b, '[')
		for i, s := range r.Steps {
			if i > 0 {
				b = append(b, ',')
			}
			b = append(b, `{"step":`...)
			b = strconv.AppendInt(b, int64(s.Step), 10)
			b = append(b, `,"message":`...)
			b = appendString(b, s.Message)
			b = append(b, '}')
		}
		b = append(b, ']')
	}
	if r.State != "" {
		field("state")
		b = appendString(b, r.State)
	}
	return append(b, '}')
}

// The loaded app, if any.
var current *session

func respond(req request) response {
	if req.Op == "load" {
		s, res := load(req)
		if s != nil {
			current = s
		}
		return res
	}
	if current == nil {
		return response{Unloaded: true}
	}
	switch req.Op {
	case "send":
		return current.send(req.Events)
	case "state":
		i := current.at
		if req.Step != nil {
			var err error
			if i, err = current.index(*req.Step); err != nil {
				return response{Error: err.Error()}
			}
		}
		return response{State: current.env.Print(current.models[i].v)}
	case "goto":
		if req.Step == nil {
			return response{Error: "bad request: no step"}
		}
		return current.goTo(*req.Step)
	case "view":
		return response{Patch: reshow(), Generation: current.generation}
	}
	return response{Error: "bad request: unknown op " + strconv.Quote(req.Op)}
}

// The most models that a session keeps, to go back to, and views whose
// handlers it keeps, for the events that a page sends before it shows the
// last view.
const (
	maxModels = 1000
	maxViews  = 16
)

// A session runs an app.
type session struct {
	env *eval.Environment
	app App
	// The models that steps led to, the first of which is step first, and
	// the index of the one shown, which an update updates, dropping those
	// after it.
	models []Model
	first  int
	at     int
	// The generation of the last view, and the handlers of the last views,
	// the last last.
	generation int
	handlers   [][]handler
}

// load loads the app in req's source, with the model in its state or
// init, updated with its messages, and returns its session, and its first
// view. It fails as a response with no session.
func load(req request) (*session, response) {
	env := eval.NewEnvironment()
	req.Use(env)
	src, err := env.Read([]byte(req.Source))
	if err != nil {
		return nil, response{Error: token.Render(err, nil)}
	}
	scraps := []*eval.Scrap{src}
	var state, messages *eval.Scrap
	if req.State != "" {
		if state, err = env.Read([]byte(req.State)); err != nil {
			return nil, response{Error: "the state: " + token.Render(err, nil)}
		}
		scraps = append(scraps, state)
	}
	if len(req.Messages) > 0 {
		if messages, err = env.Read([]byte(list(req.Messages))); err != nil {
			return nil, response{Error: "the messages: " + token.Render(err, nil)}
		}
		scraps = append(scraps, messages)
	}
	missing, err := req.Missing(env, scraps...)
	if err != nil {
		return nil, response{Error: token.Render(err, nil)}
	}
	if len(missing) > 0 {
		return nil, response{Missing: missing}
	}
	app, err := Load(env, src)
	if err != nil {
		return nil, response{Error: token.Render(err, nil)}
	}
	model := app.Init
	if state != nil {
		if err := fits(env, req.Source, req.State, nil); err != nil {
			return nil, response{Error: "the state is not a model of the app: " + token.Render(err, nil)}
		}
		v, err := env.Eval(state)
		if err != nil {
			return nil, response{Error: "the state: " + token.Render(err, nil)}
		}
		model = Model{v}
	}
	s := &session{env: env, app: app, models: []Model{model}, generation: max(req.Generation, 1) - 1}
	replayed, replayErr := s.replay(req, messages)
	if req.Step != nil {
		if s.at, err = s.index(*req.Step); err != nil {
			s.at = len(s.models) - 1
		}
	}
	res := s.render()
	if res.Error != "" {
		return nil, res
	}
	res.Replayed = replayed
	if replayErr != nil {
		res.Error = token.Render(replayErr, nil)
	}
	return s, res
}

// replay updates the session's model with the messages, as many as fit
// the app and update it, and returns how many did, and why the rest
// didn't.
func (s *session) replay(req request, messages *eval.Scrap) (int, error) {
	defer func() { s.at = len(s.models) - 1 }()
	if messages == nil {
		return 0, nil
	}
	var unfit error
	if n := len(req.Messages); fits(s.env, req.Source, req.State, req.Messages) != nil {
		// That only some messages fit is rare, as after an edit of the
		// app's messages, so look for how many, rather than check each.
		lo, hi := 0, n-1
		for lo < hi {
			if m := (lo + hi + 1) / 2; fits(s.env, req.Source, req.State, req.Messages[:m]) == nil {
				lo = m
			} else {
				hi = m - 1
			}
		}
		err := fits(s.env, req.Source, req.State, req.Messages[:lo+1])
		unfit = fmt.Errorf("message %d is not a message of the app: %w", lo+1, err)
		if lo == 0 {
			return 0, unfit
		}
		if messages, err = s.env.Read([]byte(list(req.Messages[:lo]))); err != nil {
			return 0, err
		}
	}
	v, err := s.env.Eval(messages)
	if err != nil {
		return 0, err
	}
	msgs := host.ListFrom(v, func(v eval.Value) Msg { return Msg{v} })
	for i, msg := range msgs {
		model, err := s.app.Update(msg, s.models[len(s.models)-1])
		if err != nil {
			return i, fmt.Errorf("updating with message %d: %w", i+1, err)
		}
		s.keep(model)
	}
	return len(msgs), unfit
}

// list returns the list of the scraps.
func list(scraps []string) string {
	var b strings.Builder
	b.WriteString("[")
	for i, m := range scraps {
		if i > 0 {
			b.WriteString(",")
		}
		// On lines of their own, as a scrap may end with a comment.
		b.WriteString("(\n")
		b.WriteString(m)
		b.WriteString("\n)")
	}
	b.WriteString("]")
	return b.String()
}

// fits checks that the state, if any, has the type of the models of the
// app, which checks, and the messages that of its messages. They are
// scraps of their own, as their types aren't the interface's to say.
func fits(env *eval.Environment, app, state string, messages []string) error {
	var src string
	switch {
	case messages == nil:
		src = "(app -> state -> [app.init, state])\n(" + app + "\n)\n(" + state + "\n)"
	case state == "":
		src = "(app -> ms -> list/map (m -> app.update m app.init) ms)\n(" + app + "\n)\n" + list(messages)
	default:
		src = "(app -> state -> ms -> [[app.init, state], list/map (m -> app.update m state) ms])\n(" +
			app + "\n)\n(" + state + "\n)\n" + list(messages)
	}
	s, err := env.Read([]byte(src))
	if err != nil {
		return err
	}
	_, err = env.Infer(s)
	// An error in the function itself has no line, which would only show it.
	var e token.Error
	if errors.As(err, &e) && bytes.Equal(e.Src, s.Source()) {
		return errors.New(e.Msg)
	}
	return err
}

// keep keeps the model that an update led to, as the last.
func (s *session) keep(model Model) {
	s.models = append(s.models, model)
	if len(s.models) > maxModels {
		s.models = s.models[1:]
		s.first++
	}
}

// index returns the index of the model of a step that the session keeps.
func (s *session) index(step int) (int, error) {
	i := step - s.first
	if i < 0 || i >= len(s.models) {
		return 0, fmt.Errorf("bad request: no step %d", step)
	}
	return i, nil
}

// send updates the model with the message of each event's handler, as of
// the view of its generation, as Elm does with the handlers that were on
// the page when the event happened, and shows the view of the result. It
// ignores events of views that it no longer keeps, and stops at the first
// update that fails. If the model is one before the last, as after goto,
// the first update drops those after it.
func (s *session) send(events []event) response {
	var steps []step
	var err error
	for _, e := range events {
		var msg Msg
		var ok bool
		if msg, ok, err = s.message(e); !ok {
			continue
		} else if err != nil {
			break
		}
		model, uerr := s.app.Update(msg, s.models[s.at])
		if err = uerr; err != nil {
			break
		}
		s.models = s.models[:s.at+1]
		s.keep(model)
		s.at = len(s.models) - 1
		steps = append(steps, step{s.first + s.at, s.env.Print(msg.v)})
	}
	var res response
	if len(steps) > 0 {
		res = s.render()
	}
	res.Steps = steps
	if err != nil {
		res.Error = token.Render(err, nil)
	}
	return res
}

// message returns the message of the event, and whether its view is one
// that the session keeps.
func (s *session) message(e event) (Msg, bool, error) {
	i := len(s.handlers) - 1 - (s.generation - e.Generation)
	if i < 0 || i >= len(s.handlers) {
		return Msg{}, false, nil
	}
	// The last handler of an event wins, as the last attribute of a name
	// does.
	handlers := s.handlers[i]
	for j := len(handlers) - 1; j >= 0; j-- {
		h := handlers[j]
		if h.node != e.Node {
			continue
		}
		switch a := h.attr; a.Tag() {
		case AttrOn:
			if a.On().Event == e.Event {
				return a.On().Msg, true, nil
			}
		case AttrOnValue:
			if a.OnValue().Event != e.Event {
				continue
			}
			if e.Input == nil {
				return Msg{}, true, fmt.Errorf("bad request: the %s handler of node %d takes an input", e.Event, e.Node)
			}
			msg, err := a.OnValue().Msg(*e.Input)
			return msg, true, err
		}
	}
	return Msg{}, true, fmt.Errorf("bad request: node %d has no %s handler", e.Node, e.Event)
}

// goTo goes back, or forth, to the model of a step that the session keeps.
func (s *session) goTo(step int) response {
	i, err := s.index(step)
	if err != nil {
		return response{Error: err.Error()}
	}
	s.at = i
	return s.render()
}

// render patches the view to the model's, of the next generation, and
// keeps its handlers.
func (s *session) render() response {
	html, err := s.app.View(s.models[s.at])
	if err != nil {
		return response{Error: token.Render(err, nil)}
	}
	patch, handlers := patch(html)
	s.generation++
	if len(s.handlers) == maxViews {
		s.handlers = s.handlers[1:]
	}
	s.handlers = append(s.handlers, handlers)
	return response{Patch: patch, Generation: s.generation}
}
