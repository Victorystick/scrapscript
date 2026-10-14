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

// ByMap returns a Fetcher of the scraps in sources, by their sha256 hash in
// hex, such as the scraps that a program embeds.
func ByMap(sources map[string]string) Fetcher {
	return mapFetcher(sources)
}

type mapFetcher map[string]string

func (m mapFetcher) FetchSha256(key string) ([]byte, error) {
	if s, ok := m[key]; ok {
		return []byte(s), nil
	}
	return nil, ErrNotFound
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
