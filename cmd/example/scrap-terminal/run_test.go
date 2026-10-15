package main

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/Victorystick/scrapscript/eval"
)

// simulate is the reference semantics of the terminal platform, which Run
// must agree with.
const simulate = `simulate
; simulate : s => $"./program.scrap" s -> list text -> result
  = prog -> input -> run prog.step input "" prog.init
; result : type = { out : text, status : int }
; run = step -> input -> acc -> t -> dispatch step input (text/join "" [acc, t.out]) t.state t.next
; dispatch = step -> input -> acc -> state ->
  | #exit status -> { out = acc, status = status }
  | #read-line ->
      (| []           -> run step [] acc (step #eof state)
       | line >+ rest -> run step rest acc (step (#line line) state)) input`

var programs = map[string]string{
	"echo": `{ init = { state = (), out = "", next = #read-line }
, step = | #line text -> state -> { state = state, out = text ++ "\n", next = #read-line }
         | #eof       -> state -> { state = state, out = "",           next = #exit 0 }
}`,
	// Exits with the number of lines.
	"count": `{ init = { state = 0, out = "", next = #read-line }
, step = | #line _ -> n -> { state = n + 1, out = "", next = #read-line }
         | #eof    -> n -> { state = n, out = "lines\n", next = #exit n }
}`,
	// Stops at the line "quit", before the end of the input.
	"quit": `{ init = { state = (), out = "> ", next = #read-line }
, step = | #line "quit" -> s -> { state = s, out = "bye\n", next = #exit 3 }
         | #line text   -> s -> { state = s, out = text ++ "\n> ", next = #read-line }
         | #eof         -> s -> { state = s, out = "", next = #exit 1 }
}`,
	// Reads past the end of the input, which keeps giving #eof.
	"eofs": `{ init = { state = 0, out = "", next = #read-line }
, step = | #eof    -> n -> (| 2 -> { state = 2, out = "done\n", next = #exit 0 }
                             | _ -> { state = n + 1, out = "eof\n", next = #read-line }) n
         | #line t -> n -> { state = n, out = t, next = #read-line }
}`,
}

var inputs = []string{"", "a\nb\n", "a\nb", "\n\n", "quit\nmore\n"}

// lines splits input into lines as Run reads them.
func lines(input string) []eval.Value {
	ls := strings.SplitAfter(input, "\n")
	if ls[len(ls)-1] == "" {
		ls = ls[:len(ls)-1]
	}
	vs := make([]eval.Value, len(ls))
	for i, l := range ls {
		vs[i] = eval.Text(strings.TrimSuffix(l, "\n"))
	}
	return vs
}

func TestRunAgreesWithSimulate(t *testing.T) {
	env := eval.NewEnvironment()
	env.UseDrafts(fstest.MapFS{
		"program.scrap":  {Data: []byte(Interface.Source())},
		"simulate.scrap": {Data: []byte(simulate)},
	})
	sim, err := env.Read([]byte(`$"./simulate.scrap"`))
	if err != nil {
		t.Fatal(err)
	}
	simv, err := env.Eval(sim)
	if err != nil {
		t.Fatal(err)
	}

	for name, source := range programs {
		app, err := env.Read([]byte(source))
		if err != nil {
			t.Fatal(err)
		}
		p, err := Load(env, app)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		appv, _ := env.Eval(app)

		for _, input := range inputs {
			var out strings.Builder
			status, err := Run(p, strings.NewReader(input), &out)
			if err != nil {
				t.Fatalf("%s on %q: %v", name, input, err)
			}

			res, err := eval.Call(simv, appv, eval.NewList(lines(input)...))
			if err != nil {
				t.Fatalf("%s on %q: simulate: %v", name, input, err)
			}
			r := res.(eval.Record)
			want, wantStatus := string(r.Get(env.Label("out")).(eval.Text)), int(r.Get(env.Label("status")).(eval.Int))
			if out.String() != want || status != wantStatus {
				t.Errorf("%s on %q: Run gave %q, %d; simulate gave %q, %d",
					name, input, out.String(), status, want, wantStatus)
			}
		}
	}
}

func TestLoadRejectsOtherScraps(t *testing.T) {
	sources := []string{
		`{ init = 1, step = 2 }`,
		// Has no arm for #eof.
		`{ init = { state = (), out = "", next = #read-line }
		 , step = | #line t -> s -> { state = s, out = t, next = #read-line } }`,
	}
	for _, source := range sources {
		env := eval.NewEnvironment()
		app, err := env.Read([]byte(source))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := Load(env, app); err == nil || !strings.Contains(err.Error(), "does not implement") {
			t.Errorf("%s: expected Load to fail, got %v", source, err)
		}
	}
}

func TestRunFails(t *testing.T) {
	env := eval.NewEnvironment()
	app, err := env.Read([]byte(`{ init = { state = (), out = "", next = #read-line }
, step = | #line t -> s -> { state = s, out = t, next = #read-line }
         | #eof    -> s -> { state = s, out = text/repeat -1 "x", next = #exit 0 } }`))
	if err != nil {
		t.Fatal(err)
	}
	p, err := Load(env, app)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Run(p, strings.NewReader(""), &strings.Builder{}); err == nil || !strings.Contains(err.Error(), "negative count") {
		t.Errorf("expected text/repeat to fail, got %v", err)
	}
}
