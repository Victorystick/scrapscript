package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Victorystick/scrapscript/eval"
)

// web names the interface as scripts do, by its hash.
var web = "; web : type -> type -> type = " + Interface.Ref()

// counter counts clicks on its button, and seconds.
var counter = `app
; app : web int msg =
    { init = _ -> { model = 0, cmds = [] }
    , update = | #inc -> n -> { model = n + 1, cmds = [] }
               | #tick _ -> n -> { model = n + 1, cmds = [] }
    , view = n -> #el { tag = "button", attrs = [#on-click #inc], children = [#text (int/to-text n)] }
    , subscriptions = _ -> [#every { ms = 1000, msg = t -> #tick t }]
    }
; msg : type = [#inc, #tick int]
` + web

// clock shows its flags, then the time, and then rolls a die; #deep
// recurses until it fails.
var clock = `app
; app : web text msg =
    { init = | #text t -> { model = t, cmds = [#now (t -> #time t)] }
             | _       -> { model = "?", cmds = [] }
    , update = | #time t -> _ -> { model = int/to-text t, cmds = [#random-int { min = 1, max = 6, msg = n -> #roll n }] }
               | #roll n -> _ -> { model = "rolled " ++ int/to-text n, cmds = [] }
               | #deep   -> m -> { model = int/to-text (deep 0), cmds = [#random-float (_ -> #deep)] }
    , view = m -> #el { tag = "p", attrs = [#attr { name = "id", value = "t" }, #on-click #deep], children = [#text m] }
    , subscriptions = _ -> []
    }
; msg : type = [#time int, #roll int, #deep]
; deep = n -> 1 + deep n
` + web

func start(t *testing.T, env *eval.Environment, source string, flags Json) *Session {
	t.Helper()
	env.UseFetcher(Interface.Fetcher())
	script, err := env.Read([]byte(source))
	if err != nil {
		t.Fatal(err)
	}
	s, err := Start(env, script, flags)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// render prints a view as HTML, with its #attrs.
func render(t *testing.T, s *Session) string {
	t.Helper()
	n, err := s.View()
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	var print func(NodeRef)
	print = func(n NodeRef) {
		switch n.Tag() {
		case NodeText:
			b.WriteString(n.Text())
		case NodeEl:
			el := n.El()
			b.WriteString("<" + el.Tag())
			for _, a := range el.Attrs().All() {
				if a.Tag() == AttrAttr {
					b.WriteString(" " + a.Attr().Name() + "=" + a.Attr().Value())
				}
			}
			b.WriteString(">")
			for _, c := range el.Children().All() {
				print(c)
			}
			b.WriteString("</" + el.Tag() + ">")
		default:
			t.Fatalf("unexpected node %v", n.Tag())
		}
	}
	print(n)
	return b.String()
}

// click returns the message of the first #on-click in the view.
func click(t *testing.T, s *Session) Msg {
	t.Helper()
	n, err := s.View()
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range n.El().Attrs().All() {
		if a.Tag() == AttrOnClick {
			return a.OnClick()
		}
	}
	t.Fatal("no #on-click")
	return Msg{}
}

func update(t *testing.T, s *Session, msg Msg, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Update(msg); err != nil {
		t.Fatal(err)
	}
}

func expect(t *testing.T, s *Session, want string) {
	t.Helper()
	if got := render(t, s); got != want {
		t.Errorf("got view %s, want %s", got, want)
	}
}

func TestCounter(t *testing.T) {
	s := start(t, eval.NewEnvironment(), counter, NewJsonNull())
	expect(t, s, "<button>0</button>")
	if cmds := s.Commands(); len(cmds) != 0 {
		t.Errorf("got %d commands from init, want none", len(cmds))
	}

	// A handler's message goes to update.
	update(t, s, click(t, s), nil)
	expect(t, s, "<button>1</button>")

	// So does a subscription's.
	subs, err := s.Subscriptions()
	if err != nil {
		t.Fatal(err)
	}
	if subs.Len() != 1 || subs.At(0).Tag() != SubEvery || subs.At(0).Every().Ms() != 1000 {
		t.Fatalf("got subscriptions %v, want one #every 1000", subs.Value())
	}
	msg, err := subs.At(0).Every().Msg(5000)
	update(t, s, msg, err)
	expect(t, s, "<button>2</button>")
}

func TestCommands(t *testing.T) {
	s := start(t, eval.NewEnvironment(), clock, NewJsonText("hi"))
	expect(t, s, "<p id=t>hi</p>")

	// Init's command, whose continuation gives a message.
	cmds := s.Commands()
	if len(cmds) != 1 || cmds[0].Tag() != CmdNow {
		t.Fatalf("got commands %v, want one #now", cmds)
	}
	if len(s.Commands()) != 0 {
		t.Errorf("taking the commands didn't empty the queue")
	}
	msg, err := cmds[0].Now()(42)
	update(t, s, msg, err)
	expect(t, s, "<p id=t>42</p>")

	// Update's command.
	cmds = s.Commands()
	if len(cmds) != 1 || cmds[0].Tag() != CmdRandomInt {
		t.Fatalf("got commands %v, want one #random-int", cmds)
	}
	r := cmds[0].RandomInt()
	if r.Min() != 1 || r.Max() != 6 {
		t.Errorf("got #random-int from %d to %d, want 1 to 6", r.Min(), r.Max())
	}
	msg, err = r.Msg(4)
	update(t, s, msg, err)
	expect(t, s, "<p id=t>rolled 4</p>")

	// Flags that init doesn't expect.
	s = start(t, eval.NewEnvironment(), clock, NewJsonNumber(1))
	expect(t, s, "<p id=t>?</p>")
}

// A step that fails changes neither the model nor the queue.
func TestFailedUpdate(t *testing.T) {
	env := eval.NewEnvironment()
	s := start(t, env, clock, NewJsonText("hi"))
	deep := click(t, s)

	if err := s.Update(deep); err == nil || !strings.Contains(err.Error(), "calls nested deeper") {
		t.Errorf("got %v, want the call depth error", err)
	}
	expect(t, s, "<p id=t>hi</p>")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	env.UseContext(ctx)
	if err := s.Update(deep); !errors.Is(err, context.Canceled) {
		t.Errorf("got %v, want context.Canceled", err)
	}
	env.UseContext(nil)
	expect(t, s, "<p id=t>hi</p>")

	if cmds := s.Commands(); len(cmds) != 1 || cmds[0].Tag() != CmdNow {
		t.Errorf("got commands %v, want init's #now alone", cmds)
	}
}

func TestNotAnApp(t *testing.T) {
	for _, c := range []struct{ source, error string }{
		// update has no arm for #tick.
		{strings.Replace(counter, "| #tick _ -> n -> { model = n + 1, cmds = [] }", "", 1),
			"cannot unify '[ #inc ]' with 'msg'"},
		// msg is left open, with a catch-all arm.
		{`{ init = _ -> { model = 0, cmds = [] }
, update = | #inc -> n -> { model = n + 1, cmds = [] }
           | _ -> n -> { model = n, cmds = [] }
, view = n -> #el { tag = "button", attrs = [#on-click #inc], children = [] }
, subscriptions = _ -> []
}`, "'[ #inc, ..a ]' is open"},
	} {
		env := eval.NewEnvironment()
		env.UseFetcher(Interface.Fetcher())
		script, err := env.Read([]byte(c.source))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := Start(env, script, NewJsonNull()); err == nil || !strings.Contains(err.Error(), c.error) {
			t.Errorf("got %v, want an error containing %q", err, c.error)
		}
	}
}
