package eval

import (
	"testing"
	"unsafe"

	"github.com/Victorystick/scrapscript/label"
)

// TestVariantPayloads checks that a variant gives back each kind of
// payload it holds, whether it holds it in itself or in a box.
func TestVariantPayloads(t *testing.T) {
	elems := []value{intVal(1), intVal(2)}
	small := &shape{id: 3, labels: []label.ID{1, 2}}
	wide := &shape{id: 1 << (tagShift - infoLenBits), labels: []label.ID{1, 2}}
	f := &scriptFunc{}
	payloads := []struct {
		name  string
		v     value
		boxed bool
	}{
		{"zero", intVal(0), false},
		{"negative", intVal(-1), false},
		{"largest small int", intVal(1<<(tagShift-1) - 1), false},
		{"smallest small int", intVal(-1 << (tagShift - 1)), false},
		{"large int", intVal(1 << (tagShift - 1)), true},
		{"small negative int", intVal(-1<<(tagShift-1) - 1), true},
		{"float", floatVal(1.5), true},
		{"byte", byteVal(255), false},
		{"()", hole, false},
		{"tag", tagVal(maxTag), false},
		{"variant", variantVal(7, intVal(1<<40)), true},
		{"text", textVal("hi"), false},
		{"empty text", textVal(""), false},
		{"bytes", bytesVal([]byte{1}), false},
		{"empty bytes", bytesVal(nil), false},
		{"list", listVal(elems), false},
		{"empty list", listVal(nil), false},
		{"record", recordVal(small, elems), false},
		{"empty record", recordVal(&shape{id: 5}, nil), false},
		{"record of a late shape", recordVal(wide, elems), true},
		{"function", scriptVal(f), false},
	}
	for _, p := range payloads {
		for _, tag := range []label.ID{1, maxTag} {
			v := variantVal(tag, p.v)
			if !v.hasTag(tag) || v.hasTag(tag-1) || v.tag() != tag || v.kind() != kindVariant {
				t.Errorf("%s: tag %d reads as %d", p.name, tag, v.tag())
			}
			if got := v.payload(); got != p.v {
				t.Errorf("%s: payload %#v, want %#v", p.name, got, p.v)
			}
			if boxed := v.n>>innerShift&0xf == 0; boxed != p.boxed {
				t.Errorf("%s: boxed is %v, want %v", p.name, boxed, p.boxed)
			}
		}
	}
	if v := tagVal(4); v.payload().valid() || v.p != nil {
		t.Errorf("a tag without a payload has one: %#v", v)
	}
	if v := variantVal(1, scriptVal(f)); v.payload().asScript() != f || unsafe.Pointer(f) != v.p {
		t.Errorf("a function payload isn't held: %#v", v)
	}
}
