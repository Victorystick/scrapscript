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

//go:wasmexport stop
func exportStop(h int32) {
	mod.stop(int(h))
}
