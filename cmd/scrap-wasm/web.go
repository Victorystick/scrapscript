package main

import (
	"bytes"
	_ "embed"
	"errors"
	"strings"

	"github.com/Victorystick/scrapscript/eval"
	"github.com/Victorystick/scrapscript/label"
	"github.com/Victorystick/scrapscript/token"
)

// The interface of the web platform, which "web" checks apps against.
//
//go:embed web.scrap
var webInterface string

// webProgram returns the scrap that runs a step of req's app: it passes the
// message, if any, to update with the model of the state, or init, and
// gives the resulting model, and its view. The app, state and message are
// arguments, so that they don't see its names.
func webProgram(env *eval.Environment, req request) string {
	params, args := []string{"app"}, []string{req.Source}
	model := "app.init"
	if req.State != "" {
		params, args = append(params, "state"), append(args, req.State)
		model = "state"
	}
	result := "{ model, view = app.view model }"
	if req.Message != "" {
		msg := req.Message
		if req.Input != nil {
			msg = "(" + msg + "\n) " + env.Print(eval.Text(*req.Input))
		}
		params, args = append(params, "message"), append(args, msg)
		model = "app.update message " + model
		result = "{ message, model, view = app.view model }"
	}
	var b strings.Builder
	b.WriteString("(" + strings.Join(params, " -> ") + " -> " + model + " |> model -> " + result + ")")
	for _, arg := range args {
		// On a line of its own, as a comment ends with the line.
		b.WriteString("\n(" + arg + "\n)")
	}
	return b.String()
}

// web runs the step of an app that webProgram made s of, once the app
// implements the interface.
func web(env *eval.Environment, s *eval.Scrap, req request) response {
	app, err := env.Read([]byte(req.Source))
	if err != nil {
		return response{Error: token.Render(err, nil)}
	}
	iface, err := env.Read([]byte(webInterface))
	if err != nil {
		panic(err)
	}
	if err := env.Check(app, iface); err != nil {
		return response{Error: token.Render(err, nil)}
	}
	// Say which argument doesn't fit, rather than where in s.
	if req.State != "" {
		if err := fits(env, req, "app -> state -> [app.init, state]", req.State); err != nil {
			return response{Error: "the state is not a model of the app: " + token.Render(err, nil)}
		}
	}
	if req.Message != "" {
		fn := "app -> message -> app.update message"
		if req.Input != nil {
			fn = `app -> message -> app.update (message "")`
		}
		if err := fits(env, req, fn, req.Message); err != nil {
			return response{Error: "the app does not take the message: " + token.Render(err, nil)}
		}
	}
	v, err := env.Eval(s)
	if err != nil {
		return response{Error: token.Render(err, nil)}
	}
	r := env.Record(v)
	res := response{State: env.Print(r.Get(env.Label("model")))}
	if req.Message != "" {
		res.Message = env.Print(r.Get(env.Label("message")))
	}
	res.View = newViewer(env).node(r.Get(env.Label("view")))
	return res
}

// fits checks that fn, a function of req's app and arg, type checks with
// them. An error in fn itself has no line, which would only show fn.
func fits(env *eval.Environment, req request, fn, arg string) error {
	s, err := env.Read([]byte("(" + fn + ")\n(" + req.Source + "\n)\n(" + arg + "\n)"))
	if err != nil {
		return err
	}
	_, err = env.Infer(s)
	var e token.Error
	if errors.As(err, &e) && bytes.Equal(e.Src, s.Source()) {
		return errors.New(e.Msg)
	}
	return err
}

// An element is an element of a view, as a page builds it. Its text nodes
// are strings.
type element struct {
	Tag      string             `json:"tag"`
	Attrs    map[string]string  `json:"attrs,omitempty"`
	On       map[string]handler `json:"on,omitempty"`
	Children []any              `json:"children,omitempty"`
}

// A handler is the message that an element sends on an event, as a scrap,
// for the page to run the app with.
type handler struct {
	Message string `json:"message"`
	// Whether the message is a function, of the value of the event's target,
	// as of an input. The page passes the value as the input.
	Value bool `json:"value,omitempty"`
}

// A viewer reads views, which have the interface's html type.
type viewer struct {
	env                                           *eval.Environment
	text, attr, on, onValue                       label.ID
	tag, attrs, children, name, value, event, msg label.ID
}

func newViewer(env *eval.Environment) *viewer {
	l := env.Label
	return &viewer{
		env:  env,
		text: l("text"), attr: l("attr"), on: l("on"), onValue: l("on-value"),
		tag: l("tag"), attrs: l("attrs"), children: l("children"),
		name: l("name"), value: l("value"), event: l("event"), msg: l("msg"),
	}
}

// node returns the element or text that html is.
func (v *viewer) node(html eval.Value) any {
	n := html.Variant()
	if n.Tag() == v.text {
		return n.Payload().Text()
	}
	r := v.env.Record(n.Payload())
	e := element{Tag: r.Get(v.tag).Text()}
	for _, a := range r.Get(v.attrs).List().Elements() {
		a := a.Variant()
		p := v.env.Record(a.Payload())
		switch a.Tag() {
		case v.attr:
			if e.Attrs == nil {
				e.Attrs = make(map[string]string)
			}
			e.Attrs[p.Get(v.name).Text()] = p.Get(v.value).Text()
		case v.on, v.onValue:
			if e.On == nil {
				e.On = make(map[string]handler)
			}
			e.On[p.Get(v.event).Text()] = handler{
				Message: v.env.Print(p.Get(v.msg)),
				Value:   a.Tag() == v.onValue,
			}
		}
	}
	for _, c := range r.Get(v.children).List().Elements() {
		e.Children = append(e.Children, v.node(c))
	}
	return e
}
