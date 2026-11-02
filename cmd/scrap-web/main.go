// scrap-web hosts the web platform: it runs apps in the Elm architecture,
// whose interface is web.scrap. This far it is the session, which runs an
// app's init and update and collects its commands; the WebAssembly module
// that a page drives comes later (WEB_PLAN.md).
package main

//go:generate go run ../scrap-gen-go -main=false web.scrap

func main() {}
