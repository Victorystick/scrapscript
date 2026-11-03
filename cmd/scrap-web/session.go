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

// Subscriptions returns what the app listens to, for its model.
func (s *Session) Subscriptions() (ListRef[SubRef], error) {
	return s.app.Subscriptions(s.model)
}
