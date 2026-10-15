package eval

import (
	"encoding/binary"
	"slices"
	"sync"

	"github.com/Victorystick/scrapscript/label"
)

// A shape is the labels of a record, in order by name, which is the order
// that the record holds its values in. Shapes are interned per environment,
// so records with the same labels share one, and never change.
type shape struct {
	labels []label.ID
}

// index returns the position of the label l in s, or -1 if s lacks it.
func (s *shape) index(l label.ID) int {
	return slices.Index(s.labels, l)
}

// A shapeTable interns the shapes of an environment's records, whose
// labels are in its label table. It only grows, and is safe for concurrent
// use, as label.Table is.
type shapeTable struct {
	labels *label.Table

	mu    sync.RWMutex
	byKey map[string]*shape // by the bytes of their labels
	// The shape of a record of one shape without the labels of another, by
	// the pair, for rest patterns.
	rests map[[2]*shape]*shape
}

func newShapeTable(labels *label.Table) *shapeTable {
	return &shapeTable{
		labels: labels,
		byKey:  make(map[string]*shape),
		rests:  make(map[[2]*shape]*shape),
	}
}

// sort sorts labels by name, the order of a shape.
func (t *shapeTable) sort(labels []label.ID) {
	slices.SortFunc(labels, t.labels.Compare)
}

// of returns the shape of labels, which must be distinct and sorted by
// name. It keeps labels only if it makes a new shape.
func (t *shapeTable) of(labels []label.ID) *shape {
	var buf [64]byte
	key := buf[:0]
	for _, l := range labels {
		key = binary.LittleEndian.AppendUint32(key, uint32(l))
	}
	t.mu.RLock()
	s, ok := t.byKey[string(key)]
	t.mu.RUnlock()
	if ok {
		return s
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if s, ok := t.byKey[string(key)]; ok {
		return s
	}
	s = &shape{labels}
	t.byKey[string(key)] = s
	return s
}

// without returns the shape of a record of shape s without drop's labels,
// which s must have.
func (t *shapeTable) without(s, drop *shape) *shape {
	pair := [2]*shape{s, drop}
	t.mu.RLock()
	r, ok := t.rests[pair]
	t.mu.RUnlock()
	if ok {
		return r
	}
	labels := make([]label.ID, 0, len(s.labels)-len(drop.labels))
	for _, l := range s.labels {
		if drop.index(l) < 0 {
			labels = append(labels, l)
		}
	}
	r = t.of(labels)
	t.mu.Lock()
	t.rests[pair] = r
	t.mu.Unlock()
	return r
}
