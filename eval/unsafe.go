package eval

import (
	"math"
	"unsafe"

	"github.com/Victorystick/scrapscript/label"
)

// The representation of values, and the only use of unsafe.
//
// A value is two words, n and p. A scalar, (), an int, a float or a byte,
// is n, with p pointing to its kind's sentinel. Any other value has its
// kind in the top byte of n, and p points to its data:
//
//	text, bytes  n: kind | length  p: the first byte
//	list         n: kind | length  p: the first element
//	variant      n: kind | tag     p: the payload, or nil if it has none
//	record       n: kind           p: the record
//	function     n: kind           p: the builtinFunc, partial or scriptFunc
//
// So a list's tail is its elements from the second on, without allocating.
// p never points past the end of what it points into, which the garbage
// collector requires, so empty text, bytes and lists have it nil.
//
// Values know their kinds, so that functions print the values they have
// captured, whose types may vary. The evaluator otherwise trusts the
// checker for kinds, and reads values without checking them.
type value struct {
	n uint64
	p unsafe.Pointer
}

const (
	kindShift = 56
	lenMask   = 1<<kindShift - 1
)

// The sentinels that scalars point to, by kind.
var sentinels [numKinds]byte

func scalar(k kind, n uint64) value {
	return value{n, unsafe.Pointer(&sentinels[k])}
}

func boxed(k kind, n uint64, p unsafe.Pointer) value {
	return value{uint64(k)<<kindShift | n, p}
}

// kind returns the kind of v, or kindNone for the zero value.
func (v value) kind() kind {
	if off := uintptr(v.p) - uintptr(unsafe.Pointer(&sentinels)); off < uintptr(numKinds) {
		return kind(off)
	}
	return kind(v.n >> kindShift)
}

// boxedKind returns the kind of v, which must not be a scalar, such as a
// function.
func (v value) boxedKind() kind {
	return kind(v.n >> kindShift)
}

// valid returns true if v is a value, rather than the zero value.
func (v value) valid() bool {
	return v != value{}
}

var hole = scalar(kindHole, 0)

func intVal(i int) value       { return scalar(kindInt, uint64(i)) }
func (v value) asInt() int     { return int(v.n) }
func floatVal(f float64) value { return scalar(kindFloat, math.Float64bits(f)) }
func (v value) asFloat() float64 {
	return math.Float64frombits(v.n)
}
func byteVal(b byte) value   { return scalar(kindByte, uint64(b)) }
func (v value) asByte() byte { return byte(v.n) }

func textVal(t string) value {
	if len(t) == 0 {
		return boxed(kindText, 0, nil)
	}
	return boxed(kindText, uint64(len(t)), unsafe.Pointer(unsafe.StringData(t)))
}

func (v value) asText() string {
	return unsafe.String((*byte)(v.p), v.n&lenMask)
}

// bytesVal returns the bytes b, which it takes ownership of.
func bytesVal(b []byte) value {
	if len(b) == 0 {
		return boxed(kindBytes, 0, nil)
	}
	return boxed(kindBytes, uint64(len(b)), unsafe.Pointer(unsafe.SliceData(b)))
}

// asBytes returns the bytes of v, which the caller must not modify. Their
// capacity is their length, so appending to them copies them.
func (v value) asBytes() []byte {
	return unsafe.Slice((*byte)(v.p), v.n&lenMask)
}

// listVal returns the list of elems, which it takes ownership of.
func listVal(elems []value) value {
	if len(elems) == 0 {
		return boxed(kindList, 0, nil)
	}
	return boxed(kindList, uint64(len(elems)), unsafe.Pointer(unsafe.SliceData(elems)))
}

// asList returns the elements of v, which the caller must not modify.
// Their capacity is their length, so appending to them copies them.
func (v value) asList() []value {
	return unsafe.Slice((*value)(v.p), v.n&lenMask)
}

func recordVal(r *record) value       { return boxed(kindRecord, 0, unsafe.Pointer(r)) }
func (v value) asRecord() *record     { return (*record)(v.p) }
func builtinVal(b *builtinFunc) value { return boxed(kindBuiltin, 0, unsafe.Pointer(b)) }
func partialVal(p *partial) value     { return boxed(kindPartial, 0, unsafe.Pointer(p)) }
func scriptVal(sf *scriptFunc) value  { return boxed(kindScript, 0, unsafe.Pointer(sf)) }
func (v value) asBuiltin() *builtinFunc {
	return (*builtinFunc)(v.p)
}
func (v value) asPartial() *partial   { return (*partial)(v.p) }
func (v value) asScript() *scriptFunc { return (*scriptFunc)(v.p) }

// script returns the script function that v, a function, is, if it is
// one.
func (v value) script() (*scriptFunc, bool) {
	if v.boxedKind() != kindScript {
		return nil, false
	}
	return v.asScript(), true
}

// builtin returns the builtin or host function that v, a function, is, if
// it is one.
func (v value) builtin() (*builtinFunc, bool) {
	if v.boxedKind() != kindBuiltin {
		return nil, false
	}
	return v.asBuiltin(), true
}

// variantVal returns the tag with payload, or without a payload if it is
// the zero value.
func variantVal(tag label.ID, payload value) value {
	if !payload.valid() {
		return boxed(kindVariant, uint64(tag), nil)
	}
	p := new(value)
	*p = payload
	return boxed(kindVariant, uint64(tag), unsafe.Pointer(p))
}

// tagVal returns the tag without a payload.
func tagVal(tag label.ID) value {
	return boxed(kindVariant, uint64(tag), nil)
}

func (v value) tag() label.ID {
	return label.ID(v.n & lenMask)
}

// hasTag returns true if v, a variant, has the tag.
func (v value) hasTag(tag label.ID) bool {
	return v.n == uint64(kindVariant)<<kindShift|uint64(tag)
}

// payload returns the payload of v, a variant, or the zero value if it has
// none.
func (v value) payload() value {
	if v.p == nil {
		return value{}
	}
	return *(*value)(v.p)
}

// toHost returns vs as Values, without copying them.
func toHost(vs []value) []Value {
	return unsafe.Slice((*Value)(unsafe.Pointer(unsafe.SliceData(vs))), len(vs))
}

// fromHost returns vs as values, without copying them.
func fromHost(vs []Value) []value {
	return unsafe.Slice((*value)(unsafe.Pointer(unsafe.SliceData(vs))), len(vs))
}
