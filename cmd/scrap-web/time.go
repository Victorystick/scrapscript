package main

import (
	"encoding/json"
	"errors"
	"math/rand/v2"
	"slices"
	"time"

	"github.com/Victorystick/scrapscript/eval"
	"github.com/Victorystick/scrapscript/token"
)

// An app's subscriptions, as the page last asked for them: the list, for
// its continuations, the model it is of, and what the page runs for it.
type subscriptions struct {
	list  ListRef[SubRef]
	model Model
	asked bool
	timers
}

// The timers that the page runs for an app's subscriptions: an interval
// for each distinct #every, and animation frames if any #animation-frame.
type timers struct {
	Every []int64 `json:"every"` // in milliseconds, ascending
	Frame bool    `json:"frame"`
}

// subscriptions asks app h for its subscriptions, for its model now, and
// returns the timers that the page should run for them, as JSON, or
// nothing if they are as last returned. The page asks once per render,
// so the subscriptions of the same model as last time are not asked for
// again.
func (m *module) subscriptions(h int) ([]byte, error) {
	a, err := m.app(h)
	if err != nil {
		return nil, err
	}
	if a.subs.asked && eval.Same(a.s.model.v, a.subs.model.v) {
		return nil, nil
	}
	a.step()
	list, err := a.s.Subscriptions()
	if err != nil {
		return nil, errors.New(token.Render(err, nil))
	}
	a.subs.list, a.subs.model, a.subs.asked = list, a.s.model, true
	t := timers{Every: []int64{}}
	for _, s := range list.All() {
		switch s.Tag() {
		case SubEvery:
			t.Every = append(t.Every, s.Every().Ms())
		case SubAnimationFrame:
			t.Frame = true
		}
	}
	slices.Sort(t.Every)
	t.Every = slices.Compact(t.Every)
	if t.Frame == a.subs.Frame && slices.Equal(t.Every, a.subs.Every) {
		return nil, nil
	}
	a.subs.timers = t
	return json.Marshal(t)
}

// tick passes app h the time now, in milliseconds since the epoch, for
// each of its #every subscriptions of ms milliseconds, as the page last
// asked for them: an update each, in order. The updates go on after one
// fails, and tick returns the first error.
func (m *module) tick(h int, ms int64) error {
	a, err := m.app(h)
	if err != nil {
		return err
	}
	return a.fire(SubEvery, func(s SubRef) (Msg, error) {
		e := s.Every()
		if e.Ms() != ms {
			return Msg{}, errSkip
		}
		return e.Msg(m.clock().UnixMilli())
	})
}

// frame passes app h the milliseconds since the last animation frame, for
// each of its #animation-frame subscriptions, as tick does.
func (m *module) frame(h int, delta float64) error {
	a, err := m.app(h)
	if err != nil {
		return err
	}
	return a.fire(SubAnimationFrame, func(s SubRef) (Msg, error) {
		return s.AnimationFrame()(delta)
	})
}

var errSkip = errors.New("skip")

// fire updates the app with the message of each subscription with the
// tag, unless msg skips it.
func (a *app) fire(tag SubTag, msg func(SubRef) (Msg, error)) error {
	var first error
	for _, s := range a.subs.list.All() {
		if s.Tag() != tag {
			continue
		}
		a.step()
		m, err := msg(s)
		if err == errSkip {
			continue
		}
		if err == nil {
			err = a.s.Update(m)
		}
		if err != nil && first == nil {
			first = errors.New(token.Render(err, nil))
		}
	}
	return first
}

// wake runs the continuation of app h's command with the id, which isn't
// a request, with the time or a random number, and update with its
// message. A command that the app doesn't await is ignored.
func (m *module) wake(h int, id uint32) error {
	a, err := m.app(h)
	if err != nil {
		return err
	}
	cmd, ok := a.pending[id]
	if !ok || cmd.Tag() == CmdHttp {
		return nil
	}
	delete(a.pending, id)
	a.step()
	var msg Msg
	switch cmd.Tag() {
	case CmdNow:
		msg, err = cmd.Now()(m.clock().UnixMilli())
	case CmdRandomInt:
		r := cmd.RandomInt()
		msg, err = r.Msg(randomInt(m.random(), r.Min(), r.Max()))
	case CmdRandomFloat:
		msg, err = cmd.RandomFloat()(m.random().Float64())
	}
	if err == nil {
		err = a.s.Update(msg)
	}
	if err != nil {
		return errors.New(token.Render(err, nil))
	}
	return nil
}

// randomInt returns a random integer from lo to hi, both included, or
// from hi to lo if hi is the lesser, as in Elm.
func randomInt(r *rand.Rand, lo, hi int64) int64 {
	if lo > hi {
		lo, hi = hi, lo
	}
	span := uint64(hi) - uint64(lo) + 1
	if span == 0 { // every int64
		return int64(r.Uint64())
	}
	return lo + int64(r.Uint64N(span))
}

func (m *module) clock() time.Time {
	if m.now != nil {
		return m.now()
	}
	return time.Now()
}

func (m *module) random() *rand.Rand {
	if m.rand == nil {
		m.rand = rand.New(rand.NewPCG(rand.Uint64(), rand.Uint64()))
	}
	return m.rand
}
