package main

import (
	"encoding/binary"
	"math"
	"strconv"
	"unicode/utf8"
)

// A patch is a batch of changes to the page, which a small interpreter in
// JavaScript applies. It is little-endian 32-bit words, followed by bytes:
//
//	nWords nStrings          the header
//	op operand…              nWords words of ops
//	byteLength…              nStrings words, each string's length in bytes
//	bytes…                   the strings' UTF-8 bytes, one after another
//
// An op's operands are DOM ids, which name nodes, and indices into the
// patch's strings, which hold each distinct string once. Id 0 is the
// element that the app is mounted in. The ops, with their operands:
//
//	CREATE_ELEMENT id tag     a new element, not yet in the page
//	CREATE_TEXT id text       a new text node, not yet in the page
//	SET_TEXT id text          a text node's text
//	SET_ATTR id name value    an attribute
//	REMOVE_ATTR id name
//	SET_PROP id name json     a property, to the value of JSON text
//	REMOVE_PROP id name       resets a property: to "" if it holds a
//	                          string, and to null otherwise
//	SET_STYLE id name value   a style property, as style.setProperty
//	REMOVE_STYLE id name
//	LISTEN id event           the node has handlers for an event, which
//	                          the page reports with the node's id
//	UNLISTEN id event
//	APPEND parent child       makes child, which isn't in the page, the
//	                          last child of parent
//	REPLACE old new           puts new, which isn't in the page, in old's
//	                          place, and drops old
//	REMOVE id                 drops a node
//	INSERT parent child before
//	                          puts child before before, a child of
//	                          parent, or last if before is 0; child is
//	                          new, or a child of parent, which moves
//
// A dropped node's id, and the ids of the nodes in it, may name new nodes
// later in the same patch: a CREATE forgets whatever its id named before,
// with its listeners.
type patch struct {
	words []uint32
	lens  []uint32
	bytes []byte
	index map[string]uint32
	out   []byte
}

// The ops, as numbered in patches.
const (
	opCreateElement uint32 = iota + 1
	opCreateText
	opSetText
	opSetAttr
	opRemoveAttr
	opSetProp
	opRemoveProp
	opSetStyle
	opRemoveStyle
	opListen
	opUnlisten
	opAppend
	opReplace
	opRemove
	opInsert
)

// reset empties the patch for a new batch.
func (p *patch) reset() {
	p.words = p.words[:0]
	p.lens = p.lens[:0]
	p.bytes = p.bytes[:0]
	if p.index == nil {
		p.index = map[string]uint32{}
	}
	clear(p.index)
}

func (p *patch) op(words ...uint32) {
	p.words = append(p.words, words...)
}

// str returns the index of s among the patch's strings, adding it if it
// isn't there yet.
func (p *patch) str(s string) uint32 {
	if i, ok := p.index[s]; ok {
		return i
	}
	i := uint32(len(p.lens))
	p.index[s] = i
	p.lens = append(p.lens, uint32(len(s)))
	p.bytes = append(p.bytes, s...)
	return i
}

// json returns the index of j as JSON text among the patch's strings.
func (p *patch) json(j JsonRef) uint32 {
	start := len(p.bytes)
	p.bytes = appendJSON(p.bytes, j)
	s := string(p.bytes[start:])
	if i, ok := p.index[s]; ok {
		p.bytes = p.bytes[:start]
		return i
	}
	i := uint32(len(p.lens))
	p.index[s] = i
	p.lens = append(p.lens, uint32(len(s)))
	return i
}

// empty reports whether the patch has no ops.
func (p *patch) empty() bool { return len(p.words) == 0 }

// encode returns the patch in its binary form, which is valid until the
// next encode.
func (p *patch) encode() []byte {
	out := p.out[:0]
	out = binary.LittleEndian.AppendUint32(out, uint32(len(p.words)))
	out = binary.LittleEndian.AppendUint32(out, uint32(len(p.lens)))
	for _, w := range p.words {
		out = binary.LittleEndian.AppendUint32(out, w)
	}
	for _, n := range p.lens {
		out = binary.LittleEndian.AppendUint32(out, n)
	}
	out = append(out, p.bytes...)
	p.out = out
	return out
}

// appendJSON appends j as JSON text. Numbers that JSON can't hold, NaN and
// the infinities, become null, as with JSON.stringify.
func appendJSON(b []byte, j JsonRef) []byte {
	switch j.Tag() {
	case JsonNull:
		return append(b, "null"...)
	case JsonBool:
		if j.Bool().Tag() == BoolTrue {
			return append(b, "true"...)
		}
		return append(b, "false"...)
	case JsonNumber:
		f := j.Number()
		if math.IsNaN(f) || math.IsInf(f, 0) {
			return append(b, "null"...)
		}
		return strconv.AppendFloat(b, f, 'g', -1, 64)
	case JsonText:
		return appendJSONString(b, j.Text())
	case JsonList:
		b = append(b, '[')
		for i, e := range j.List().All() {
			if i > 0 {
				b = append(b, ',')
			}
			b = appendJSON(b, e)
		}
		return append(b, ']')
	case JsonObject:
		b = append(b, '{')
		for i, m := range j.Object().All() {
			if i > 0 {
				b = append(b, ',')
			}
			b = appendJSONString(b, m.Key())
			b = append(b, ':')
			b = appendJSON(b, m.Value())
		}
		return append(b, '}')
	}
	panic("appendJSON: unknown tag")
}

// appendJSONString appends s as a JSON string.
func appendJSONString(b []byte, s string) []byte {
	const hex = "0123456789abcdef"
	b = append(b, '"')
	for i := 0; i < len(s); {
		c := s[i]
		switch {
		case c == '"' || c == '\\':
			b = append(b, '\\', c)
		case c < 0x20:
			b = append(b, '\\', 'u', '0', '0', hex[c>>4], hex[c&0xF])
		case c < utf8.RuneSelf:
			b = append(b, c)
		default:
			r, n := utf8.DecodeRuneInString(s[i:])
			if r == utf8.RuneError && n == 1 {
				b = append(b, `�`...)
			} else {
				b = append(b, s[i:i+n]...)
			}
			i += n
			continue
		}
		i++
	}
	return append(b, '"')
}
