// Command scrap-check is the type checker as a WebAssembly module, for hosts
// such as a scrapyard that check scraps before storing them. Build it with:
//
//	GOOS=wasip1 GOARCH=wasm go build -buildmode=c-shared -o scrap-check.wasm ./cmd/scrap-check
//
// The host initializes the module once (_initialize) and then checks any
// number of scraps with it. Each check is a JSON request,
//
//	{"source": "...", "sources": {"<sha256>": "...", ...}}
//
// with the sources of the scraps it refers to by hash, and has a JSON
// response: {"type": "..."} if it checks, {"error": "..."} if it doesn't, or
// {"missing": ["<sha256>", ...]} if the sources lack scraps that it refers
// to, directly or not. The host fetches those and asks again, so that the
// module never waits on the host.
package main

import (
	"encoding/json"
	"errors"
	"sort"

	"github.com/Victorystick/scrapscript/eval"
	"github.com/Victorystick/scrapscript/token"
)

func main() {}

type request struct {
	Source  string            `json:"source"`
	Sources map[string]string `json:"sources"`
}

type response struct {
	Type    string   `json:"type,omitempty"`
	Error   string   `json:"error,omitempty"`
	Missing []string `json:"missing,omitempty"`
}

// handle checks the JSON request in input and returns its JSON response.
func handle(input []byte) []byte {
	var req request
	var res response
	if err := json.Unmarshal(input, &req); err != nil {
		res.Error = "bad request: " + err.Error()
	} else {
		res = check(req)
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

func (s sources) FetchSha256(key string) ([]byte, error) {
	if src, ok := s[key]; ok {
		return []byte(src), nil
	}
	return nil, errNotFound
}

func check(req request) response {
	env := eval.NewEnvironment()
	env.UseFetcher(sources(req.Sources))
	s, err := env.Read([]byte(req.Source))
	if err != nil {
		return response{Error: token.Render(err, false)}
	}
	if missing := missing(env, s, req.Sources); len(missing) > 0 {
		return response{Missing: missing}
	}
	t, err := env.Infer(s)
	if err != nil {
		return response{Error: token.Render(err, false)}
	}
	return response{Type: t}
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
