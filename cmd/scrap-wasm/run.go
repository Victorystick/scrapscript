// Command scrap-wasm is the scrap tool as a WebAssembly module, for hosts
// such as a scrapyard that check scraps before storing them, or a web page
// that runs them. Build it with:
//
//	GOOS=wasip1 GOARCH=wasm go build -buildmode=c-shared -o scrap-wasm.wasm ./cmd/scrap-wasm
//
// The host initializes the module once (_initialize) and then runs any
// number of requests with it. Each is JSON,
//
//	{"source": "...", "sources": {"<sha256>": "...", ...}}
//
// with the sources of the scraps it refers to by hash, and has a JSON
// response: {"type": "..."} if it checks, {"error": "..."} if it doesn't, or
// {"missing": ["<sha256>", ...]} if the sources lack scraps that it refers
// to, directly or not. The host fetches those and asks again, so that the
// module never waits on the host. A source whose sha256 isn't its hash is
// an error.
//
// A request's "command" says what to do with a scrap that checks, as the
// commands of the scrap CLI do:
//
//   - "type", the default, only checks it.
//   - "eval" evaluates it too. The response has its value as `scrap eval`
//     prints it, "value", next to its type.
//   - "test" runs the test cases it holds, as `scrap test` does. The
//     response has a result for each, "results", next to its type: its
//     "path", and whether it "failed" and why, "message".
//
// If evaluating fails, the response has the error next to the type.
//
// "highlight" neither checks nor runs the scrap, which needn't parse. The
// response has its ranges for syntax highlighting, "highlights", for the
// CSS Custom Highlight API: per class, named as the highlight package names
// them, its ranges as start and end pairs, in UTF-16 code units as
// JavaScript indexes strings.
package main

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/Victorystick/scrapscript/eval"
	"github.com/Victorystick/scrapscript/highlight"
	"github.com/Victorystick/scrapscript/token"
)

func main() {}

type request struct {
	Source  string            `json:"source"`
	Sources map[string]string `json:"sources"`
	Command string            `json:"command"`
}

type response struct {
	Highlights map[string][]int `json:"highlights,omitempty"`
	Type       string           `json:"type,omitempty"`
	Value      string           `json:"value,omitempty"`
	Results    []result         `json:"results,omitzero"`
	Error      string           `json:"error,omitempty"`
	Missing    []string         `json:"missing,omitempty"`
}

type result struct {
	Path    string `json:"path"`
	Failed  bool   `json:"failed,omitempty"`
	Message string `json:"message,omitempty"`
}

// handle checks the JSON request in input and returns its JSON response.
func handle(input []byte) []byte {
	var req request
	var res response
	if err := json.Unmarshal(input, &req); err != nil {
		res.Error = "bad request: " + err.Error()
	} else {
		res = respond(req)
	}
	out, err := json.Marshal(res)
	if err != nil {
		panic(err)
	}
	return out
}

// sources is a Fetcher of the scraps in a request, by hash.
type sources map[string]string

var errNotFound = errors.New("no scrap found")

// FetchSha256 returns the source with the sha256 key, which it verifies, as
// the scrap CLI does with what it fetches.
func (s sources) FetchSha256(key string) ([]byte, error) {
	src, ok := s[key]
	if !ok {
		return nil, errNotFound
	}
	if fmt.Sprintf("%x", sha256.Sum256([]byte(src))) != key {
		return nil, fmt.Errorf("the source of $source:%s has a different hash", key)
	}
	return []byte(src), nil
}

func respond(req request) response {
	switch req.Command {
	case "", "type", "eval", "test":
	case "highlight":
		return response{Highlights: highlights(req.Source)}
	default:
		return response{Error: "bad request: unknown command " + strconv.Quote(req.Command)}
	}
	env := eval.NewEnvironment()
	env.UseFetcher(sources(req.Sources))
	s, err := env.Read([]byte(req.Source))
	if err != nil {
		return response{Error: token.Render(err, nil)}
	}
	if missing := missing(env, s, req.Sources); len(missing) > 0 {
		return response{Missing: missing}
	}
	t, err := env.Infer(s)
	if err != nil {
		return response{Error: token.Render(err, nil)}
	}
	switch req.Command {
	case "eval":
		v, err := env.PrintScrap(s)
		if err != nil {
			return response{Type: t, Error: token.Render(err, nil)}
		}
		return response{Type: t, Value: v}
	case "test":
		results, err := env.Test(s)
		if err != nil {
			return response{Type: t, Error: token.Render(err, nil)}
		}
		res := response{Type: t, Results: make([]result, len(results))}
		for i, r := range results {
			res.Results[i] = result{Path: r.Path, Failed: r.Failed, Message: r.Message}
		}
		return res
	}
	return response{Type: t}
}

// highlights returns the ranges of src's highlight classes, in UTF-16 code
// units.
func highlights(src string) map[string][]int {
	s := highlight.Highlight([]byte(src))
	res := map[string][]int{}
	// The offsets only grow, so one pass over src converts them all.
	at, units := 0, 0
	unitsAt := func(offset int) int {
		for at < offset {
			r, w := utf8.DecodeRuneInString(src[at:])
			at += w
			units += utf16.RuneLen(r)
		}
		return units
	}
	for i, c := range s.Classes {
		name := c.String()
		start := unitsAt(int(s.Starts[i]))
		res[name] = append(res[name], start, unitsAt(int(s.Ends[i])))
	}
	return res
}

// missing returns the hashes of the scraps that s refers to, directly or
// through the scraps in srcs, that aren't in srcs, sorted. References that
// can't be fetched for other reasons are left for inference to report.
func missing(env *eval.Environment, s *eval.Scrap, srcs map[string]string) []string {
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
