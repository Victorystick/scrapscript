package main

import (
	"encoding/json"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/Victorystick/scrapscript/host/wasmhost"
)

// The app in README.md runs.
func TestReadme(t *testing.T) {
	readme, err := os.ReadFile("README.md")
	if err != nil {
		t.Fatal(err)
	}
	app := regexp.MustCompile("(?s)```scrap\n(\\{ init = 0.*?)```").FindSubmatch(readme)
	req, _ := json.Marshal(request{Op: "load", Source: `$"./main.scrap"`, Scraps: wasmhost.Scraps{Drafts: map[string]string{"main.scrap": string(app[1])}}})
	p := newPage(t)
	// The patch starts as README.md's example does.
	example := regexp.MustCompile("```json\n\\s*(\\[0, 1, .*?), …\\]").FindSubmatch(readme)
	if patch := p.run(string(req)).Patch; !strings.HasPrefix(string(patch), strings.ReplaceAll(string(example[1]), ", ", ",")) {
		t.Errorf("expected the patch to start with %s, got %s", example[1], patch)
	}
	res := p.send(p.event("button", 1, "click"), p.event("button", 1, "click"), p.event("button", 0, "click"))
	if got, want := steps(res), "1 #inc, 2 #inc, 3 #dec"; got != want {
		t.Errorf("expected steps %s, got %s", want, got)
	}
	p.shows(`<p><button on:click>-</button>1<button on:click>+</button></p>`)
}
