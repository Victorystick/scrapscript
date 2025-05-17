package yards

import (
	"bytes"
	"testing"
	"testing/fstest"
)

func TestByDirectory(t *testing.T) {
	dir := fstest.MapFS{
		"key": {Data: []byte("value")},
	}

	f := ByDirectory(dir)

	bs, err := f.FetchSha256("key")
	if err != nil {
		t.Error("unexpected read failure")
	}
	equalBytes(t, bs, []byte("value"))

	bs, err = f.FetchSha256("missing")
	if err == nil {
		t.Error("expected read failure")
	}
	if bs != nil {
		t.Error("unexpected read bytes")
	}
}

func TestInOrder(t *testing.T) {
	f := InOrder(
		ByDirectory(fstest.MapFS{
			"key1": {Data: []byte("first")},
		}),
		ByDirectory(fstest.MapFS{
			"key1": {Data: []byte("second")},
			"key2": {Data: []byte("another")},
		}),
	)

	bs, err := f.FetchSha256("key1")
	if err != nil {
		t.Error("unexpected read failure")
	}
	equalBytes(t, bs, []byte("first"))

	bs, err = f.FetchSha256("key2")
	if err != nil {
		t.Error("unexpected read failure")
	}
	equalBytes(t, bs, []byte("another"))
}

func equalBytes(t *testing.T, actual, expected []byte) {
	if !bytes.Equal(actual, expected) {
		t.Errorf("read bytes were wrong %v != %v", actual, expected)
	}
}
