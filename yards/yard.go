package yards

import (
	"errors"
	"io/fs"
)

var ErrNotFound = errors.New("no scrap found")

// Fetcher is the interface for retrieving scraps by their SHA hashes.
type Fetcher interface {
	FetchSha256(key string) ([]byte, error)
}

// Pusher is the interface for storing scraps, returning their SHA hashes.
type Pusher interface {
	PushScrap(data []byte) (key string, err error)
}

// A FetchPusher is both a Fetcher and a Pusher.
type FetchPusher interface {
	Fetcher
	Pusher
}

// ByDirectory returns a Fetcher that looks in the given directory.
func ByDirectory(fs fs.FS) Fetcher {
	return &directoryFetcher{fs}
}

// A directoryFetcher looks for scraps in a file system directory.
type directoryFetcher struct{ fs.FS }

func (d *directoryFetcher) FetchSha256(key string) ([]byte, error) {
	return fs.ReadFile(d, key)
}

type sequenceFetcher []Fetcher

// InOrder returns a Fetcher that looks for scraps using each fetcher in order.
func InOrder(options ...Fetcher) Fetcher {
	return sequenceFetcher(options)
}

func (s sequenceFetcher) FetchSha256(key string) ([]byte, error) {
	for _, f := range s {
		if bs, err := f.FetchSha256(key); err == nil {
			return bs, nil
		}
	}
	return nil, ErrNotFound
}
