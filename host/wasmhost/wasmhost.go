// Package wasmhost is what hosts that are WebAssembly modules share, for
// WASI preview 1 (GOOS=wasip1), as cmd/scrap-wasm and the web platform's
// host cmd/example/scrap-web are. Such a host has no files or network of
// its own: its host, such as a web page, gives it the scraps to run with
// each request, and it answers requests through its exports. Build one
// with:
//
//	GOOS=wasip1 GOARCH=wasm go build -buildmode=c-shared -o host.wasm ./cmd/...
//
// The module's host initializes it once (_initialize), and then runs any
// number of requests with it: it writes a request into a buffer of n
// bytes from alloc(n), calls run(), which returns the length of the
// response, and reads the response from output(). A module that keeps
// state across requests keeps it until it traps, after which its host
// starts a new one.
package wasmhost

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"sort"

	"github.com/Victorystick/scrapscript/eval"
)

// handler answers requests; Serve sets it.
var handler func(request []byte) []byte

// Serve makes the module answer each request with handle, which returns the
// response. A module calls it once, as from init.
func Serve(handle func(request []byte) []byte) {
	handler = handle
}

// Scraps are the scraps that a request gives, for the scrap that it runs to
// refer to, as a page has them. A request embeds them, as JSON:
//
//	{"sources": {"<sha256>": "...", ...}, "drafts": {"lib.scrap": "...", ...}}
//
// Sources are published scraps, by hash, which a module checks. Drafts are
// by path, as an editor has unsaved files, and scraps refer to them by
// path, $"./lib.scrap", from an unnamed draft at the root, as the scrap CLI
// reads standard input. Without drafts, a path is an error, as in a
// published scrap.
type Scraps struct {
	Sources map[string]string `json:"sources"`
	Drafts  map[string]string `json:"drafts"`
}

// Use makes env find the scraps.
func (s Scraps) Use(env *eval.Environment) {
	env.UseFetcher(sources(s.Sources))
	if s.Drafts != nil {
		// Missing reads the drafts that scraps refer to; any other path is to
		// a file that doesn't exist.
		env.UseDrafts(noFiles{})
	}
}

// noFiles has no files, as Missing reads the drafts that scraps refer to.
type noFiles struct{}

func (noFiles) Open(name string) (fs.File, error) {
	return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
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

// Missing returns the hashes of the published scraps that the scraps refer
// to, directly or through the sources or drafts, that aren't in the
// sources, sorted. A module answers with them, so that its host fetches
// them and asks again, as it can't wait on its host. The scraps are
// unnamed drafts in env, which Use made find these scraps; Missing reads
// the drafts that they refer to as it goes, and fails if one doesn't parse.
// References that can't be resolved for other reasons are left for
// inference to report.
func (s Scraps) Missing(env *eval.Environment, scraps ...*eval.Scrap) ([]string, error) {
	seen := make(map[string]bool)
	// The paths of the drafts read so far, by scrap; the scraps are unnamed.
	names := make(map[*eval.Scrap]string)
	var missing []string
	var visit func(s *eval.Scrap) error
	visit = func(scrap *eval.Scrap) error {
		expr := scrap.Expr()
		for _, n := range expr.Imports() {
			if p, ok := expr.Path(n); ok {
				from, ok := names[scrap]
				if !ok {
					continue // A published scrap can't refer to a path.
				}
				name := path.Join(path.Dir(from), p)
				src, ok := s.Drafts[name]
				if !ok || seen[name] || !fs.ValidPath(name) {
					continue
				}
				seen[name] = true
				draft, err := env.ReadDraft(name, []byte(src))
				if err != nil {
					return fmt.Errorf("%s: %w", name, err)
				}
				names[draft] = name
				if err := visit(draft); err != nil {
					return err
				}
				continue
			}
			scheme, digest := expr.Hash(n)
			if scheme != "source" || seen[digest] {
				continue
			}
			seen[digest] = true
			if _, ok := s.Sources[digest]; !ok {
				missing = append(missing, digest)
				continue
			}
			if dep, err := env.Fetch(scheme, digest); err == nil {
				if err := visit(dep); err != nil {
					return err
				}
			}
		}
		return nil
	}
	for _, scrap := range scraps {
		names[scrap] = ""
		if err := visit(scrap); err != nil {
			return nil, err
		}
	}
	sort.Strings(missing)
	return missing, nil
}
