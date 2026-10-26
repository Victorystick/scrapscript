package main

import (
	"encoding/json"
	"os"
	"regexp"
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
	got := run(t, string(req), `{"op":"send","generation":1,"events":[{"handler":1},{"handler":1},{"handler":0}]}`)
	want := `{"view":{"tag":"p","children":[{"tag":"button","on":{"click":{"handler":0}},"children":["-"]},"1",` +
		`{"tag":"button","on":{"click":{"handler":1}},"children":["+"]}]},"generation":2,` +
		`"steps":[{"step":1,"message":"#inc"},{"step":2,"message":"#inc"},{"step":3,"message":"#dec"}]}`
	if got[1] != want {
		t.Errorf("expected\n%s\ngot\n%s", want, got[1])
	}
}
