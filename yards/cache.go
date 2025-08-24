package yards

import (
	"os"
	"path/filepath"
)

type cachingFetcher struct {
	path     string // The path to the cache directory.
	main     Fetcher
	fallback Fetcher
}

func (c *cachingFetcher) FetchSha256(key string) ([]byte, error) {
	bs, err := c.main.FetchSha256(key)
	if err == nil {
		return bs, nil
	}

	bs, err = c.fallback.FetchSha256(key)
	if err != nil {
		return nil, err
	}

	// TODO: Is this the correct mode perm?
	return bs, os.WriteFile(filepath.Join(c.path, key), bs, 0644)
}

func NewCacheFetcher(pathname string, fetcher Fetcher) (Fetcher, error) {
	// Create the cache directory if it doesn't exist.
	if _, err := os.Stat(pathname); os.IsNotExist(err) {
		err = os.MkdirAll(pathname, 0700)
		if err != nil {
			return nil, err
		}
	}
	return &cachingFetcher{
		path:     pathname,
		main:     ByDirectory(os.DirFS(pathname)),
		fallback: fetcher,
	}, nil
}

func NewDefaultCacheFetcher(fetcher Fetcher) (Fetcher, error) {
	dir, err := os.UserCacheDir()
	if err != nil {
		return nil, err
	}

	return NewCacheFetcher(filepath.Join(dir, "scrapscript/sha256"), fetcher)
}
