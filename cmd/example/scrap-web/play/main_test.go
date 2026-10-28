package main

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Victorystick/scrapscript/yards"
)

func TestServe(t *testing.T) {
	inc := "x -> x + 1"
	key := fmt.Sprintf("%x", sha256.Sum256([]byte(inc)))
	h := handler(".", yards.ByMap(map[string]string{key: inc}))
	get := func(path string) (int, string, []byte) {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		body, _ := io.ReadAll(w.Result().Body)
		return w.Code, w.Header().Get("Content-Type"), body
	}

	if code, _, body := get("/"); code != http.StatusOK || !bytes.Contains(body, []byte(`<script type="module" src="./play.js">`)) {
		t.Errorf("expected the page, got %d %.100s", code, body)
	}
	if code, _, body := get("/" + key); code != http.StatusOK || string(body) != inc {
		t.Errorf("expected the scrap, got %d %s", code, body)
	}
	if code, _, _ := get("/" + strings.Repeat("0", 64)); code != http.StatusNotFound {
		t.Errorf("expected a scrap that the yard lacks not to be found, got %d", code)
	}
	if code, _, _ := get("/main.go"); code != http.StatusNotFound {
		t.Errorf("expected the server's source not to be served, got %d", code)
	}
	if testing.Short() {
		t.Skip("building scrap-web takes a while")
	}
	code, typ, body := get("/scrap-web.wasm")
	if code != http.StatusOK || typ != "application/wasm" || !bytes.HasPrefix(body, []byte("\x00asm")) {
		t.Errorf("expected a module, got %d %s %.200s", code, typ, body)
	}
}
