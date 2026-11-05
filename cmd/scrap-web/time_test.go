package main

import (
	"fmt"
	"math"
	"math/rand/v2"
	"strings"
	"testing"
	"time"

	"github.com/Victorystick/scrapscript/eval"
)

// ticker logs the time and random numbers that its commands get, and the
// messages of its subscriptions, which change with its mode, as clicks
// advance it.
var ticker = `app
; app : web model msg =
    { init = _ -> { model = { log = [], mode = 0 }, cmds =
        [ #now (t -> #at t)
        , #random-int { min = 6, max = 1, msg = n -> #roll n }
        , #random-float (f -> #float f)
        ] }
    , update =
        | #at t    -> m -> add (#text ("now " ++ int/to-text t)) m
        | #roll n  -> m -> add (#text ("roll " ++ int/to-text n)) m
        | #float f -> m -> add (#number f) m
        | #tick t  -> m -> add (#text t) m
        | #frame d -> m -> add (#number d) m
        | #next    -> m -> { model = { ..m, mode = m.mode + 1 }, cmds = [] }
    , view = m -> #el { tag = "p", attrs = [#prop { name = "log", value = #list m.log }, #on-click #next], children = [] }
    , subscriptions = m -> subs m.mode
    }
; add = x -> m -> { model = { ..m, log = m.log +< x }, cmds = [] }
; at = name -> t -> #tick (name ++ " " ++ int/to-text t)
; subs =
    | 0 -> [#every { ms = 1000, msg = at "a" }, #every { ms = 500, msg = at "b" }, #every { ms = 1000, msg = at "c" }]
    | 1 -> [#animation-frame (d -> #frame d), #every { ms = 1000, msg = at "d" }]
    | 2 -> [#every { ms = 1000, msg = _ -> #tick (int/to-text (deep 0)) }, #every { ms = 1000, msg = at "e" }]
    | _ -> []
; deep = n -> 1 + deep n
; model : type = { log : list json, mode : int }
; msg : type = [#at int, #roll int, #float float, #tick text, #frame float, #next]
; json : type = [#null, #bool [#false, #true], #number float, #text text, #list (list json), #object (list { key : text, value : json })]
` + web

func TestTime(t *testing.T) {
	m := module{
		now:  func() time.Time { return time.UnixMilli(1234) },
		rand: rand.New(rand.NewPCG(1, 2)),
	}
	res := startApp(t, &m, startRequest{Source: ticker})
	if res.Handle == 0 {
		t.Fatalf("got %+v", res)
	}
	h := res.Handle
	check := func(what string, got []byte, err error, want string) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != want {
			t.Errorf("got %s\n%s\nwant\n%s", what, got, want)
		}
	}
	subscriptions := func(want string) {
		t.Helper()
		got, err := m.subscriptions(h)
		check("timers", got, err, want)
	}
	var log []string
	logged := func(entries ...string) {
		t.Helper()
		log = append(log, entries...)
		view, err := m.apps[h-1].s.View()
		if err != nil {
			t.Fatal(err)
		}
		got := appendJSON(nil, view.El().Attrs().At(0).Prop().Value())
		check("log", got, nil, "["+strings.Join(log, ",")+"]")
	}
	ok := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	click := func() {
		t.Helper()
		_, err := m.render(h)
		ok(err)
		ok(m.event(h, encodeEvent(Event{Node: 1, Name: "click"})))
	}

	// Commands that aren't requests, which the page wakes, in any order.
	// Random numbers come in the order they're woken.
	cmds, err := m.commands(h)
	check("commands", cmds, err, `[{"id":1},{"id":2},{"id":3}]`)
	ref := rand.New(rand.NewPCG(1, 2))
	roll, float := randomInt(ref, 1, 6), ref.Float64()
	ok(m.wake(h, 2))
	ok(m.wake(h, 1))
	ok(m.wake(h, 3))
	logged(fmt.Sprintf(`"roll %d"`, roll), `"now 1234"`, string(appendJSON(nil, NewJsonNumber(float).Ref(m.apps[h-1].s.app.Bindings()))))
	// Commands that the app doesn't await are ignored.
	ok(m.wake(h, 1))
	ok(m.wake(h, 99))
	logged()

	// Subscriptions, as distinct intervals; the same again are nothing,
	// and those of the same model aren't asked for again.
	subscriptions(`{"every":[500,1000],"frame":false}`)
	asked := m.apps[h-1].subs.list.Value()
	subscriptions(``)
	if !eval.Same(asked, m.apps[h-1].subs.list.Value()) {
		t.Error("asked for the subscriptions of the same model again")
	}
	ok(m.tick(h, 1000))
	ok(m.tick(h, 500))
	ok(m.tick(h, 250))
	logged(`"a 1234"`, `"c 1234"`, `"b 1234"`)
	// A new model with the same subscriptions.
	subscriptions(``)

	// Ticks fire the subscriptions as last asked for.
	click()
	ok(m.tick(h, 500))
	logged(`"b 1234"`)
	subscriptions(`{"every":[1000],"frame":true}`)
	ok(m.tick(h, 500))
	ok(m.frame(h, 16.5))
	ok(m.tick(h, 1000))
	logged(`16.5`, `"d 1234"`)

	// A subscription whose continuation fails fails its tick, but the
	// others go on.
	click()
	subscriptions(`{"every":[1000],"frame":false}`)
	if err := m.tick(h, 1000); err == nil || !strings.Contains(err.Error(), "calls nested deeper") {
		t.Errorf("got %v, want the call depth error", err)
	}
	ok(m.frame(h, 16.5))
	logged(`"e 1234"`)

	click()
	subscriptions(`{"every":[],"frame":false}`)
	ok(m.tick(h, 1000))
	logged()

	m.stop(h)
	for _, err := range []error{m.wake(h, 1), m.tick(h, 1000), m.frame(h, 1)} {
		if err == nil {
			t.Error("passed a stopped app a message")
		}
	}
	if _, err := m.subscriptions(h); err == nil {
		t.Error("asked a stopped app for its subscriptions")
	}
}

func TestRandomInt(t *testing.T) {
	r := rand.New(rand.NewPCG(3, 4))
	for _, c := range []struct{ lo, hi int64 }{{1, 3}, {3, 1}, {-2, -2}, {math.MinInt64, math.MinInt64 + 1}, {math.MaxInt64 - 1, math.MaxInt64}} {
		lo, hi := min(c.lo, c.hi), max(c.lo, c.hi)
		seen := map[int64]bool{}
		for range 100 {
			n := randomInt(r, c.lo, c.hi)
			if n < lo || n > hi {
				t.Fatalf("randomInt(%d, %d) gave %d", c.lo, c.hi, n)
			}
			seen[n] = true
		}
		if uint64(len(seen)) != uint64(hi-lo)+1 {
			t.Errorf("randomInt(%d, %d) gave only %v", c.lo, c.hi, seen)
		}
	}
	// Every int64, which the span overflows.
	seen := map[bool]bool{}
	for range 100 {
		seen[randomInt(r, math.MinInt64, math.MaxInt64) < 0] = true
	}
	if len(seen) != 2 {
		t.Errorf("randomInt over every int64 gave only one sign")
	}
}
