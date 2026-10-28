package main

import (
	"encoding/json"
	stdhtml "html"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/Victorystick/scrapscript/host/wasmhost"
)

// playDrafts returns the drafts that the playground starts with, by name,
// in order.
func playDrafts(t *testing.T) ([]string, map[string]string) {
	t.Helper()
	page, err := os.ReadFile("play/index.html")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	drafts := map[string]string{}
	for _, m := range regexp.MustCompile(`<div class="draft" data-draft="([^"]+)"><textarea>([^<]*)</textarea>`).FindAllSubmatch(page, -1) {
		names = append(names, string(m[1]))
		drafts[string(m[1])] = stdhtml.UnescapeString(string(m[2]))
	}
	return names, drafts
}

// loadPlay returns a request that loads the playground's app with the
// drafts, from the base, with the messages.
func loadPlay(drafts map[string]string, base string, messages ...string) string {
	req, _ := json.Marshal(request{Op: "load", Source: `$"./main.scrap"`, State: base, Messages: messages, Scraps: wasmhost.Scraps{Drafts: drafts}})
	return string(req)
}

func TestPlay(t *testing.T) {
	names, start := playDrafts(t)
	if got := strings.Join(names, " "); got != "main.scrap state.scrap html.scrap style.scrap" {
		t.Fatalf("expected the app, its state and their drafts, got %s", got)
	}

	t.Run("its state is the one init leads to", func(t *testing.T) {
		p := newPage(t)
		p.run(loadPlay(start, ""))
		p.state(start["state.scrap"])
	})

	t.Run("runs as the page does", func(t *testing.T) {
		p := newPage(t)
		p.run(loadPlay(start, start["state.scrap"]))
		// Cross off the first todo, which patches its item's class and
		// the count of those left, and nothing else.
		res := p.send(p.event("span", 0, "click"))
		if got, want := steps(res), "1 #toggle 1"; got != want {
			t.Errorf("expected steps %s, got %s", want, got)
		}
		li, left := p.frame.find(t, "li", 0), p.frame.find(t, "p", 0)
		if got, want := string(res.Patch), `[5,`+strconv.Itoa(li)+`,"class","done",4,`+strconv.Itoa(p.frame.nodes[left].children[0].id)+`,"1 left"]`; got != want {
			t.Errorf("expected the patch %s, got %s", want, got)
		}
		// Type a todo, and add it.
		p.send(p.event("input", 0, "input", `Buy "milk"`))
		p.send(p.event("form", 0, "submit"))
		messages := []string{"#toggle 1", `#edit "Buy \"milk\""`, "#add"}
		p.state(`{ draft = "", next = 4, todos = [ ` +
			`{ done = #true, id = 1, text = "Write a scrap" }, ` +
			`{ done = #false, id = 2, text = "Click me to cross me off" }, ` +
			`{ done = #false, id = 3, text = "Buy \"milk\"" } ] }`)

		// A new version of the app replays the messages, from the state
		// that they started from, patching the view.
		drafts := map[string]string{}
		for k, v := range start {
			drafts[k] = v
		}
		drafts["main.scrap"] = strings.NewReplacer(`"Todos"`, `"Things"`,
			`{ ..t, done = not t.done }`, `{ ..t, text = "Done!" }`).Replace(start["main.scrap"])
		res = p.run(loadPlay(drafts, start["state.scrap"], messages...))
		if res.Replayed != 3 || res.Error != "" {
			t.Errorf("expected 3 messages replayed, got %+v", res)
		}
		if got, want := p.frame.html(), `<h1>Things</h1>`; !strings.Contains(got, want) {
			t.Errorf("expected %s in %s", want, got)
		}
		if got, want := p.frame.html(), `<ul><li class=""><span on:click>Done!</span><button on:click>×</button></li>`; !strings.Contains(got, want) {
			t.Errorf("expected %s in %s", want, got)
		}
	})

	t.Run("shows the interface that apps are checked against", func(t *testing.T) {
		page, err := os.ReadFile("play/index.html")
		if err != nil {
			t.Fatal(err)
		}
		iface, err := os.ReadFile("app.scrap")
		if err != nil {
			t.Fatal(err)
		}
		shown := regexp.MustCompile(`(?s)<details>.*<pre><code>(.*?)</code></pre>`).FindSubmatch(page)
		if got, want := stdhtml.UnescapeString(string(shown[1])), strings.TrimRight(string(iface), "\n"); got != want {
			t.Errorf("expected the page to show\n%s\ngot\n%s", want, got)
		}
	})
}
