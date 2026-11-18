package eval

import (
	"runtime"
	"sync/atomic"
)

// Frames are recycled. A frame's slots are dead once the body that runs in
// them returns or calls on in tail position, since closures copy what they
// capture and no value points into a frame. So trampoline frees them, and
// calls take slots from the free frames of their size, last freed first,
// as from a stack.
//
// Freed slots are not cleared: clearing takes a bulk write barrier while
// the garbage collector marks, which cost more than allocating. Instead,
// as with a sync.Pool, the free frames are dropped after each collection,
// so that what their stale slots refer to outlives being garbage by at most
// one cycle. The pool is the environment's, so an environment's values must
// not be called from several goroutines at once.

// The largest frames that are recycled, and how many slots of free frames
// are kept of each size.
const (
	maxPooled  = 32
	slotsKept  = 1 << 12
	framesKept = slotsKept / maxPooled
)

type framePool struct {
	free  [maxPooled + 1][][]value
	epoch uint32 // gcEpoch when free was last dropped
}

// gcEpoch counts garbage collections, as a finalizer that rearms itself
// notices them.
var gcEpoch atomic.Uint32

func init() { watchGC() }

func watchGC() {
	// A pointer keeps it out of the tiny allocator, whose objects may not
	// be finalized.
	s := &struct{ _ *byte }{}
	runtime.SetFinalizer(s, func(any) {
		gcEpoch.Add(1)
		watchGC()
	})
}

// slots returns size slots for a frame, which hold stale values unless
// they are new.
func (l *limit) slots(size int) []value {
	if size <= maxPooled {
		free := &l.frames.free[size]
		if n := len(*free); n > 0 {
			s := (*free)[n-1]
			*free = (*free)[:n-1]
			return s
		}
	}
	return make([]value, size)
}

// free keeps the slots s of a frame that is done with, for another.
func (l *limit) free(s []value) {
	if len(s) == 0 || len(s) > maxPooled {
		return
	}
	if e := gcEpoch.Load(); e != l.frames.epoch {
		l.frames = framePool{epoch: e}
	}
	free := &l.frames.free[len(s)]
	if len(*free)*len(s) < slotsKept {
		*free = append(*free, s)
	}
}
