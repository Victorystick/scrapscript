package yards

import (
	"crypto/sha256"
	"fmt"
	"testing"
	"testing/fstest"
)

func TestValidate(t *testing.T) {
	data := []byte{1, 2, 3}
	key := fmt.Sprintf("%x", sha256.Sum256(data))

	file := fstest.MapFile{Data: data}
	f := Validate(ByDirectory(fstest.MapFS{key: &file}))

	bs, err := f.FetchSha256(key)
	if err != nil {
		t.Error("unexpected read failure")
	}
	equalBytes(t, bs, data)

	// Corrupt the data
	file.Data = []byte{1, 2, 3, 4}
	bs, err = f.FetchSha256(key)
	if err != ErrWrongHash {
		t.Errorf("expected %s failure, got %s", ErrWrongHash, err)
	}
	if bs != nil {
		t.Error("unexpected read bytes")
	}
}
