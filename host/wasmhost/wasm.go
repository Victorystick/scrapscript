//go:build wasip1

package wasmhost

import "unsafe"

// The request that the host writes, and the response it reads, kept here
// so that the garbage collector doesn't free them while the host uses them.
var input, output []byte

// alloc returns a buffer of n bytes for the host to write a request into.
//
//go:wasmexport alloc
func alloc(n int32) unsafe.Pointer {
	input = make([]byte, n)
	return unsafe.Pointer(unsafe.SliceData(input))
}

// run answers the request in the buffer from alloc, and returns the length
// of its response, which output points to.
//
//go:wasmexport run
func run() int32 {
	output = handler(input)
	input = nil
	return int32(len(output))
}

//go:wasmexport output
func outputPtr() unsafe.Pointer {
	return unsafe.Pointer(unsafe.SliceData(output))
}
