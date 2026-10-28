// play serves a playground of scrap-web, to work on it with: a page of
// drafts of a little website, an app of the web platform, which runs in
// scrap-web.wasm. It builds the module anew from the source each time the
// page loads it, and serves the page's files from this directory as they
// are, so that a reload of the page runs what was edited, of either. It
// fetches the scraps that the drafts refer to by hash from a scrapyard.
// Run it from anywhere in the repository with:
//
//	go run ./cmd/example/scrap-web/play
package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"

	"github.com/Victorystick/scrapscript/yards"
)

func main() {
	addr := flag.String("addr", "localhost:8080", "the address to serve the playground on")
	server := flag.String("server", "https://scraps.oseg.dev/", "the scrapyard to fetch scraps from")
	flag.Parse()

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		log.Fatal("cannot find the playground's files")
	}
	h := handler(filepath.Dir(file), yards.Validate(yards.ByHttp(*server)))
	log.Printf("serving the playground on http://%s", *addr)
	log.Fatal(http.ListenAndServe(*addr, h))
}

// A hash names a scrap that the page fetches, as it does from a yard.
var hash = regexp.MustCompile(`^/[0-9a-f]{64}$`)

// handler serves the page's files in dir, scrap-web.wasm, built from the
// package that dir is in, and the scraps that the yard has.
func handler(dir string, yard yards.Fetcher) http.Handler {
	files := http.FileServer(http.Dir(dir))
	var building sync.Mutex
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		switch p := r.URL.Path; {
		case p == "/scrap-web.wasm":
			building.Lock()
			defer building.Unlock()
			wasm, err := build(filepath.Dir(dir))
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/wasm")
			w.Write(wasm)
		case hash.MatchString(p):
			src, err := yard.FetchSha256(p[1:])
			if err != nil {
				http.Error(w, err.Error(), http.StatusNotFound)
				return
			}
			w.Write(src)
		case strings.HasSuffix(p, ".go"):
			http.NotFound(w, r)
		default:
			files.ServeHTTP(w, r)
		}
	})
}

// build builds the package in dir for WebAssembly, as a page runs it, or
// fails with what go build printed.
func build(dir string) ([]byte, error) {
	out, err := os.CreateTemp("", "scrap-web-*.wasm")
	if err != nil {
		return nil, err
	}
	out.Close()
	defer os.Remove(out.Name())
	cmd := exec.Command("go", "build", "-buildmode=c-shared", "-o", out.Name(), ".")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOOS=wasip1", "GOARCH=wasm")
	if msg, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("building scrap-web failed: %v\n%s", err, msg)
	}
	return os.ReadFile(out.Name())
}
