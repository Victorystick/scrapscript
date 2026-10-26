// scrap-web hosts the web platform, in a web page: it runs an app, whose
// interface is app.scrap, as Elm runs one. It is a WebAssembly module that
// a page runs requests with, as wasmhost describes, and keeps the app and
// its model between them. The page shows each view that it answers with,
// and sends it the events that the view's handlers handle. Build it with:
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

	"github.com/Victorystick/scrapscript/eval"
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
	// What "load" loads: the app, and a scrap of its model, if not init.
	Source string `json:"source"`
	State  string `json:"state"`
	// The generation of a view: the first that "load" makes, 1 by default,
	// or the one whose handlers "send" passes the events of. A page that
	// loads apps anew passes a generation after the last it showed, so that
	// it can't mistake events of an earlier view for the last's.
	Generation int     `json:"generation"`
	Events     []event `json:"events"`
	// The step that "goto" goes back to.
	Step int `json:"step"`
}

// An event is one that the handler of a view's element handled, with the
// value of its target, if the handler is a function of it.
type event struct {
	Handler int     `json:"handler"`
	Input   *string `json:"input"`
}

type response struct {
	Missing []string `json:"missing,omitempty"`
	Error   string   `json:"error,omitempty"`
	// Whether no app is loaded, as when the module is new.
	Unloaded   bool   `json:"unloaded,omitempty"`
	View       any    `json:"view,omitempty"`
	Generation int    `json:"generation,omitempty"`
	Steps      []step `json:"steps,omitempty"`
	State      string `json:"state,omitempty"`
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
	out, err := json.Marshal(res)
	if err != nil {
		panic(err)
	}
	return out
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
		return current.send(req.Generation, req.Events)
	case "state":
		return response{State: current.env.Print(current.model.v)}
	case "goto":
		return current.goTo(req.Step)
	}
	return response{Error: "bad request: unknown op " + strconv.Quote(req.Op)}
}

// The most models that a session keeps, to go back to.
const maxModels = 1000

// A session runs an app.
type session struct {
	env   *eval.Environment
	app   App
	model Model
	// The models that steps led to, the first of which is step first.
	models []Model
	first  int
	// The generation of the last view, and the handlers of its elements.
	generation int
	handlers   []Attr
}

// load loads the app in req's source, with the model in its state or
// init, and returns its session, and its first view. It fails as a
// response with no session.
func load(req request) (*session, response) {
	env := eval.NewEnvironment()
	req.Use(env)
	src, err := env.Read([]byte(req.Source))
	if err != nil {
		return nil, response{Error: token.Render(err, nil)}
	}
	scraps := []*eval.Scrap{src}
	var state *eval.Scrap
	if req.State != "" {
		if state, err = env.Read([]byte(req.State)); err != nil {
			return nil, response{Error: "the state: " + token.Render(err, nil)}
		}
		scraps = append(scraps, state)
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
		if err := fits(env, req.Source, req.State); err != nil {
			return nil, response{Error: "the state is not a model of the app: " + token.Render(err, nil)}
		}
		v, err := env.Eval(state)
		if err != nil {
			return nil, response{Error: "the state: " + token.Render(err, nil)}
		}
		model = Model{v}
	}
	first := max(req.Generation, 1)
	s := &session{env: env, app: app, model: model, models: []Model{model}, generation: first - 1}
	res := s.render()
	if res.Error != "" {
		return nil, res
	}
	return s, res
}

// fits checks that the state has the type of the models of the app, which
// checks. The state is a scrap of its own, as its type isn't the
// interface's to say.
func fits(env *eval.Environment, app, state string) error {
	s, err := env.Read([]byte("(app -> state -> [app.init, state])\n(" + app + "\n)\n(" + state + "\n)"))
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

// send updates the model with the message of each event, whose handlers
// are those of the view of the generation, which must be the last, and
// shows the view of the result. It ignores events of an earlier view, which
// the page sent before it showed the last, and stops at the first update
// that fails.
func (s *session) send(generation int, events []event) response {
	if generation != s.generation {
		return response{}
	}
	var steps []step
	var err error
	for _, e := range events {
		var msg Msg
		if msg, err = s.message(e); err != nil {
			break
		}
		model, uerr := s.app.Update(msg, s.model)
		if err = uerr; err != nil {
			break
		}
		s.model = model
		s.models = append(s.models, model)
		if len(s.models) > maxModels {
			s.models = s.models[1:]
			s.first++
		}
		steps = append(steps, step{s.first + len(s.models) - 1, s.env.Print(msg.v)})
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

// message returns the message of the event.
func (s *session) message(e event) (Msg, error) {
	if e.Handler < 0 || e.Handler >= len(s.handlers) {
		return Msg{}, fmt.Errorf("bad request: no handler %d", e.Handler)
	}
	switch a := s.handlers[e.Handler]; a.Tag() {
	case AttrOn:
		return a.On().Msg, nil
	default:
		if e.Input == nil {
			return Msg{}, fmt.Errorf("bad request: handler %d takes an input", e.Handler)
		}
		return a.OnValue().Msg(*e.Input)
	}
}

// goTo goes back, or forth, to the model of a step that the session keeps.
func (s *session) goTo(step int) response {
	i := step - s.first
	if i < 0 || i >= len(s.models) {
		return response{Error: fmt.Sprintf("bad request: no step %d", step)}
	}
	s.model = s.models[i]
	return s.render()
}

// render returns the view of the model, of the next generation, and keeps
// its handlers.
func (s *session) render() response {
	html, err := s.app.View(s.model)
	if err != nil {
		return response{Error: token.Render(err, nil)}
	}
	s.generation++
	s.handlers = s.handlers[:0]
	return response{View: s.node(html), Generation: s.generation}
}

// An element is an element of a view, as a page builds it. Its text nodes
// are strings.
type element struct {
	Tag      string             `json:"tag"`
	Attrs    map[string]string  `json:"attrs,omitempty"`
	On       map[string]handler `json:"on,omitempty"`
	Children []any              `json:"children,omitempty"`
}

// A handler is what a page sends of an event: the handler's index, and
// whether the value of the event's target is its input.
type handler struct {
	Handler int  `json:"handler"`
	Value   bool `json:"value,omitempty"`
}

// node returns the element or text that h is.
func (s *session) node(h Html) any {
	if h.Tag() == HtmlText {
		return h.Text()
	}
	el := h.El()
	e := element{Tag: el.Tag}
	for _, a := range el.Attrs {
		var event string
		switch a.Tag() {
		case AttrAttr:
			if e.Attrs == nil {
				e.Attrs = make(map[string]string)
			}
			e.Attrs[a.Attr().Name] = a.Attr().Value
			continue
		case AttrOn:
			event = a.On().Event
		case AttrOnValue:
			event = a.OnValue().Event
		}
		if e.On == nil {
			e.On = make(map[string]handler)
		}
		e.On[event] = handler{len(s.handlers), a.Tag() == AttrOnValue}
		s.handlers = append(s.handlers, a)
	}
	for _, c := range el.Children {
		e.Children = append(e.Children, s.node(c))
	}
	return e
}
