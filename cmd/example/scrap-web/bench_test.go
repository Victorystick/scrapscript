package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/Victorystick/scrapscript/host/wasmhost"
)

// todos is a todo list of n items, which #toggle 0 crosses the first of off.
func todos(n int) map[string]string {
	var items []string
	for i := range n {
		items = append(items, fmt.Sprintf(`{ id = %d, text = "Item %d", done = #false }`, i, i))
	}
	return map[string]string{"main.scrap": `{ init, update, view }
; init = [` + strings.Join(items, ", ") + `]
; update = | #toggle id -> list/map (t -> int/cmp id t.id |> | #eq -> { ..t, done = not t.done } | _ -> t)
; not = | #true -> #false | #false -> #true
; el = tag -> attrs -> children -> #el { tag, attrs, children }
; view = ts -> el "ul" [] (list/map (t -> el "li" [#attr { name = "key", value = int/to-text t.id }, #attr { name = "class", value = t.done |> | #true -> "done" | #false -> "" }]
  [ el "span" [#on { event = "click", msg = #toggle t.id }] [#text t.text]
  , el "button" [] [#text "×"]
  ]) ts)`}
}

// BenchmarkSend measures an event that changes one of n items, and its
// patch.
func BenchmarkSend(b *testing.B) {
	for _, n := range []int{10, 1000} {
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			current = nil
			resetView()
			req, _ := json.Marshal(request{Op: "load", Source: `$"./main.scrap"`, Scraps: wasmhost.Scraps{Drafts: todos(n)}})
			res := handle(req)
			b.ReportMetric(float64(len(res)), "load-bytes")
			var load response
			json.Unmarshal(res, &load)
			f := newFrame()
			f.apply(b, load.Patch)
			span := f.find(b, "span", 0)
			var size int
			b.ResetTimer()
			for i := range b.N {
				res = handle([]byte(fmt.Sprintf(`{"op":"send","events":[{"generation":%d,"node":%d,"event":"click"}]}`, i+1, span)))
				size = len(res)
			}
			b.ReportMetric(float64(size), "send-bytes")
		})
	}
}

// BenchmarkReplay measures loading an app of n items with n messages, as a
// page does when the app changes.
func BenchmarkReplay(b *testing.B) {
	for _, n := range []int{10, 1000} {
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			messages := make([]string, n)
			for i := range messages {
				messages[i] = fmt.Sprintf("#toggle %d", i%10)
			}
			req, _ := json.Marshal(request{Op: "load", Source: `$"./main.scrap"`, Messages: messages, Scraps: wasmhost.Scraps{Drafts: todos(n)}})
			for b.Loop() {
				current = nil
				resetView()
				handle(req)
			}
		})
	}
}
