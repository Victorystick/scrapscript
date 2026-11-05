package host

import (
	"context"
	"sort"
	"time"

	"github.com/Victorystick/scrapscript/eval"
)

// Deadline returns a context that is done at a time, which it tells by
// the clock alone, for Environment.UseContext. context.WithDeadline would
// start a timer, which the Go runtime waits on through WASI's poll_oneoff,
// which hosts of WebAssembly modules such as ours don't implement; the
// evaluator compares the deadline with the clock itself.
func Deadline(at time.Time) context.Context {
	return deadline{context.Background(), at}
}

type deadline struct {
	context.Context
	at time.Time
}

func (d deadline) Deadline() (time.Time, bool) { return d.at, true }

func (d deadline) Err() error {
	if time.Now().Before(d.at) {
		return nil
	}
	return context.DeadlineExceeded
}

// Missing returns the hashes of the scraps that s refers to, directly or
// through the scraps in srcs, that aren't in srcs, sorted, so that a host
// on one thread can fetch them all and try again, rather than wait on a
// fetch mid-evaluation. The environment must fetch the scraps in srcs.
// References that can't be fetched for other reasons are left for
// inference to report.
func Missing(env *eval.Environment, s *eval.Scrap, srcs map[string]string) []string {
	seen := make(map[string]bool)
	var missing []string
	var visit func(s *eval.Scrap)
	visit = func(s *eval.Scrap) {
		expr := s.Expr()
		for _, n := range expr.Imports() {
			if _, ok := expr.Path(n); ok {
				continue
			}
			scheme, digest := expr.Hash(n)
			if scheme != "source" || seen[digest] {
				continue
			}
			seen[digest] = true
			if _, ok := srcs[digest]; !ok {
				missing = append(missing, digest)
				continue
			}
			if dep, err := env.Fetch(scheme, digest); err == nil {
				visit(dep)
			}
		}
	}
	visit(s)
	sort.Strings(missing)
	return missing
}
