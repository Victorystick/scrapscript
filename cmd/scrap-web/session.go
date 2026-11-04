package main

import (
	"github.com/Victorystick/scrapscript/eval"
)

// A Session runs an app: it holds the app's model, which it passes from one
// update to the next as it is, and queues the commands that init and
// update return until the host takes them.
//
// Messages come to the host from the app alone: from the handlers of its
// view, and from the continuations of its commands and subscriptions. The
// host passes each to Update, and decides when to render, by asking for
// the View and the Subscriptions once for any number of updates.
//
// A step that fails, by an error in the app or by running past the
// environment's deadline, changes nothing: the model and the queue stay as
// they were. Whether the app goes on is the host's choice.
type Session struct {
	app   AppRef
	model Model
	cmds  []CmdRef
	r     renderer
}

// Start checks that script is an app, evaluates it, and runs its init with
// the flags, whose commands it queues. The environment must find the
// scraps that the interface refers to, as with Interface.Fetcher.
func Start(env *eval.Environment, script *eval.Scrap, flags Json) (*Session, error) {
	app, err := Load(env, script)
	if err != nil {
		return nil, err
	}
	step, err := app.Init(flags.Ref(app.Bindings()))
	if err != nil {
		return nil, err
	}
	s := &Session{app: app}
	s.take(step)
	return s, nil
}

// Update runs the app's update with msg, and queues its commands.
func (s *Session) Update(msg Msg) error {
	step, err := s.app.Update(msg, s.model)
	if err != nil {
		return err
	}
	s.take(step)
	return nil
}

// take makes the step's model the session's, and queues its commands.
func (s *Session) take(step StepRef) {
	s.model = step.Model()
	for _, c := range step.Cmds().All() {
		s.cmds = append(s.cmds, c)
	}
}

// Commands returns the queued commands, oldest first, and empties the
// queue.
func (s *Session) Commands() []CmdRef {
	cmds := s.cmds
	s.cmds = nil
	return cmds
}

// View returns the app's view of its model.
func (s *Session) View() (NodeRef, error) {
	return s.app.View(s.model)
}

// Render returns the patch that takes the page from the last view that it
// rendered to the app's view now, valid until the next Render; the first
// fills the mount element. If the view fails, the page stays as it was.
func (s *Session) Render() ([]byte, error) {
	view, err := s.View()
	if err != nil {
		return nil, err
	}
	return s.r.render(view), nil
}

// An Event is what the page reports when an event that a node listens to
// fires: the node's id and the event's name, and the value and checked
// state of the element it fired on, which inputs and checkboxes have.
type Event struct {
	Node    uint32
	Name    string
	Value   string
	Checked bool
}

// Event runs update with the message of the handler of e, of the view as
// last rendered. An event without a handler is ignored: it can come just
// after a render removed it.
func (s *Session) Event(e Event) error {
	a, ok := s.r.handler(e.Node, e.Name)
	if !ok {
		return nil
	}
	var msg Msg
	var err error
	switch a.Tag() {
	case AttrOnClick:
		msg = a.OnClick()
	case AttrOnSubmit:
		msg = a.OnSubmit()
	case AttrOn:
		msg = a.On().Msg()
	case AttrOnInput:
		msg, err = a.OnInput()(e.Value)
	case AttrOnCheck:
		b := NewBoolFalse()
		if e.Checked {
			b = NewBoolTrue()
		}
		msg, err = a.OnCheck()(b.Ref(s.app.Bindings()))
	}
	if err != nil {
		return err
	}
	return s.Update(msg)
}

// Subscriptions returns what the app listens to, for its model.
func (s *Session) Subscriptions() (ListRef[SubRef], error) {
	return s.app.Subscriptions(s.model)
}
