package yards

import (
	"io/fs"
	"os"
	"testing"
	"testing/fstest"
)

func TestCache(t *testing.T) {
	root := t.TempDir()
	fsys := os.DirFS(root)

	// Cache directory should be empty.
	_, err := fsys.Open("key1")
	if err == nil {
		t.Error("expected not to read key1")
	}

	f, err := NewCacheFetcher(root, ByDirectory(fstest.MapFS{
		"key1": {Data: []byte("first")},
	}))
	if err != nil {
		t.Error("could not create cache directory")
	}

	bs, err := f.FetchSha256("key1")
	if err != nil {
		t.Error("unexpected read failure")
	}
	equalBytes(t, bs, []byte("first"))

	// Cache directory should contain fetched file.
	bs, err = fs.ReadFile(fsys, "key1")
	if err != nil {
		t.Error("unexpected read failure")
	}
	equalBytes(t, bs, []byte("first"))
}
