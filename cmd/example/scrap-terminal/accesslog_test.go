package main

import (
	"cmp"
	"fmt"
	"io"
	"math/rand/v2"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/Victorystick/scrapscript/eval"
)

// accessLog returns n lines of a generated access log over the given number
// of paths, about 1% of them malformed.
func accessLog(n, paths int) string {
	r := rand.New(rand.NewPCG(1, 2))
	methods := []string{"GET", "GET", "GET", "POST", "PUT", "DELETE"}
	statuses := []int{200, 200, 200, 200, 201, 204, 301, 404, 500, 503}
	var sb strings.Builder
	for range n {
		switch r.IntN(100) {
		case 0:
			sb.WriteString("garbage\n")
			continue
		case 1:
			fmt.Fprintf(&sb, "GET /api/v1/items/%d 200 slow\n", r.IntN(paths))
			continue
		}
		// Skewed, so that some paths are busier than others.
		p := r.IntN(1 + r.IntN(paths))
		fmt.Fprintf(&sb, "%s /api/v1/items/%d %d %d\n",
			methods[r.IntN(len(methods))], p, statuses[r.IntN(len(statuses))], 1+r.IntN(1+r.IntN(2000)))
	}
	return sb.String()
}

// accessReport is what access-log.scrap prints for the log, computed in Go.
func accessReport(log string) string {
	type stat struct {
		path                         string
		requests, errors, total, max int
	}
	stats := map[string]*stat{}
	bad := 0
	for line := range strings.Lines(log) {
		f := strings.Split(strings.TrimSuffix(line, "\n"), " ")
		var status, ms int64
		var err1, err2 error
		if len(f) == 4 {
			status, err1 = strconv.ParseInt(f[2], 10, 64)
			ms, err2 = strconv.ParseInt(f[3], 10, 64)
		}
		if len(f) != 4 || err1 != nil || err2 != nil {
			bad++
			continue
		}
		s := stats[f[1]]
		if s == nil {
			s = &stat{path: f[1]}
			stats[f[1]] = s
		}
		s.requests++
		if status > 499 {
			s.errors++
		}
		s.total += int(ms)
		s.max = max(s.max, int(ms))
	}
	sorted := slices.SortedFunc(func(yield func(*stat) bool) {
		for _, s := range stats {
			if !yield(s) {
				return
			}
		}
	}, func(a, b *stat) int {
		return cmp.Or(cmp.Compare(b.requests, a.requests), strings.Compare(a.path, b.path))
	})
	var sb strings.Builder
	sb.WriteString("  reqs   5xx  mean   max path\n")
	for _, s := range sorted {
		fmt.Fprintf(&sb, "%5d %5d %5d %5d %s\n", s.requests, s.errors, s.total/s.requests, s.max, s.path)
	}
	if bad > 0 {
		fmt.Fprintf(&sb, "%d malformed\n", bad)
	}
	return sb.String()
}

func loadAccessLog(tb testing.TB) ProgramRef {
	tb.Helper()
	source, err := os.ReadFile("testdata/access-log.scrap")
	if err != nil {
		tb.Fatal(err)
	}
	env := eval.NewEnvironment()
	app, err := env.Read(source)
	if err != nil {
		tb.Fatal(err)
	}
	p, err := Load(env, app)
	if err != nil {
		tb.Fatal(err)
	}
	return p
}

// loadLibraryAccessLog loads the version of the program in lib/, which
// imports the parser and avl libraries.
func loadLibraryAccessLog(tb testing.TB) ProgramRef {
	tb.Helper()
	source, err := os.ReadFile("../../../lib/access-log.scrap")
	if err != nil {
		tb.Fatal(err)
	}
	env := eval.NewEnvironment()
	env.UseDrafts(os.DirFS("../../../lib"))
	app, err := env.ReadDraft("access-log.scrap", source)
	if err != nil {
		tb.Fatal(err)
	}
	p, err := Load(env, app)
	if err != nil {
		tb.Fatal(err)
	}
	return p
}

func TestAccessLog(t *testing.T) {
	t.Run("testdata", func(t *testing.T) { testAccessLog(t, loadAccessLog(t)) })
	t.Run("lib", func(t *testing.T) { testAccessLog(t, loadLibraryAccessLog(t)) })
}

func testAccessLog(t *testing.T, p ProgramRef) {
	for _, size := range []struct{ lines, paths int }{{0, 1}, {1, 1}, {100, 3}, {5000, 300}} {
		log := accessLog(size.lines, size.paths)
		var out strings.Builder
		status, err := Run(p, strings.NewReader(log), &out)
		if err != nil {
			t.Fatal(err)
		}
		if want := accessReport(log); out.String() != want || status != 0 {
			t.Errorf("%d lines over %d paths: got status %d and\n%s\nwant\n%s", size.lines, size.paths, status, out.String(), want)
		}
	}
}

// BenchmarkAccessLog runs a realistic program on the host: parsing, a tree
// of records, sorting and formatting, over 10000 lines and 200 paths.
func BenchmarkAccessLog(b *testing.B) {
	benchmarkAccessLog(b, loadAccessLog(b))
}

// BenchmarkAccessLogLib runs the version in lib/, which parses each line
// with parser combinators, one character at a time.
func BenchmarkAccessLogLib(b *testing.B) {
	benchmarkAccessLog(b, loadLibraryAccessLog(b))
}

func benchmarkAccessLog(b *testing.B, p ProgramRef) {
	log := accessLog(10000, 200)
	b.SetBytes(int64(len(log)))
	b.ReportAllocs()
	for b.Loop() {
		if _, err := Run(p, strings.NewReader(log), io.Discard); err != nil {
			b.Fatal(err)
		}
	}
}
