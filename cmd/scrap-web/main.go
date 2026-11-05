// scrap-web hosts the web platform: it runs apps in the Elm architecture,
// whose interface is web.scrap, for a page, as a WebAssembly module that
// js/mount.ts drives (module.go and wasm.go). A Session runs an app's init
// and update, collects its commands, and renders its views as patches to
// the page (patch.go). Carrying out commands comes later (WEB_PLAN.md).
// Build it with:
//
//	GOOS=wasip1 GOARCH=wasm go build -buildmode=c-shared -o scrap-web.wasm ./cmd/scrap-web
package main

//go:generate go run ../scrap-gen-go -main=false web.scrap

func main() {}
