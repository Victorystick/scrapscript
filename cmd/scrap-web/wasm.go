//go:build wasip1

package main

import "unsafe"

// The module that the page uses, and the buffers it passes: the request
// that the page writes, and the response it reads, kept here so that the
// garbage collector doesn't free them while the page uses them.
var (
	mod           module
	input, output []byte
)

// exportAlloc returns a buffer of n bytes for the page to write a request into.
//
//go:wasmexport alloc
func exportAlloc(n int32) unsafe.Pointer {
	input = make([]byte, n)
	return unsafe.Pointer(unsafe.SliceData(input))
}

//go:wasmexport output
func exportOutput() unsafe.Pointer {
	return unsafe.Pointer(unsafe.SliceData(output))
}

// respond makes out the output, and returns its length, or minus the
// length of err's message if err isn't nil.
func respond(out []byte, err error) int32 {
	input = nil
	if err != nil {
		output = []byte(err.Error())
		return -int32(len(output))
	}
	output = out
	return int32(len(output))
}

// exportStart starts an app with the JSON request in the input, and returns the
// length of the JSON response (module.start).
//
//go:wasmexport start
func exportStart() int32 {
	return respond(mod.start(input), nil)
}

// exportRender returns the length of the patch to app h's view now, or minus the
// length of an error message (module.render).
//
//go:wasmexport render
func exportRender(h int32) int32 {
	return respond(mod.render(int(h)))
}

// exportEvent passes app h the event in the input, and returns 0, or minus the
// length of an error message (module.event).
//
//go:wasmexport event
func exportEvent(h int32) int32 {
	return respond(nil, mod.event(int(h), input))
}

// exportCommands returns the length of app h's commands as JSON, 0 if
// there are none, or minus the length of an error message
// (module.commands).
//
//go:wasmexport commands
func exportCommands(h int32) int32 {
	return respond(mod.commands(int(h)))
}

// exportRespond passes app h the response in the input, and returns 0, or
// minus the length of an error message (module.response).
//
//go:wasmexport respond
func exportRespond(h int32) int32 {
	return respond(nil, mod.response(int(h), input))
}

// exportWake runs app h's command with the id, which isn't a request,
// and returns 0, or minus the length of an error message (module.wake).
//
//go:wasmexport wake
func exportWake(h, id int32) int32 {
	return respond(nil, mod.wake(int(h), uint32(id)))
}

// exportSubscriptions returns the length of the timers to run for app h's
// subscriptions as JSON, 0 if they are as last returned, or minus the
// length of an error message (module.subscriptions).
//
//go:wasmexport subscriptions
func exportSubscriptions(h int32) int32 {
	return respond(mod.subscriptions(int(h)))
}

// exportTick fires app h's #every subscriptions of ms milliseconds, and
// returns 0, or minus the length of an error message (module.tick).
//
//go:wasmexport tick
func exportTick(h int32, ms float64) int32 {
	return respond(nil, mod.tick(int(h), int64(ms)))
}

// exportFrame fires app h's #animation-frame subscriptions with the
// milliseconds since the last frame, and returns 0, or minus the length
// of an error message (module.frame).
//
//go:wasmexport frame
func exportFrame(h int32, delta float64) int32 {
	return respond(nil, mod.frame(int(h), delta))
}

//go:wasmexport stop
func exportStop(h int32) {
	mod.stop(int(h))
}
