package eval

import "unsafe"

// The only use of unsafe: a Value holds just a value, so a slice of either
// is a slice of the other.

// toHost returns vs as Values, without copying them.
func toHost(vs []value) []Value {
	return unsafe.Slice((*Value)(unsafe.Pointer(unsafe.SliceData(vs))), len(vs))
}

// fromHost returns vs as values, without copying them.
func fromHost(vs []Value) []value {
	return unsafe.Slice((*value)(unsafe.Pointer(unsafe.SliceData(vs))), len(vs))
}
