package yards

import (
	"crypto/sha256"
	"errors"
	"fmt"
)

var ErrWrongHash = errors.New("fetched bytes had wrong hash")

type valid struct{ Fetcher }

func (v valid) FetchSha256(key string) ([]byte, error) {
	bytes, err := v.Fetcher.FetchSha256(key)
	if err != nil {
		return nil, err
	}

	hash := sha256.Sum256(bytes)
	if fmt.Sprintf("%x", hash) != key {
		return nil, ErrWrongHash
	}

	return bytes, nil
}

// Validate wraps a Fetcher and checks that any returned bytes actually have
// the sha256 hash that was requested.
func Validate(fetcher Fetcher) Fetcher {
	return valid{fetcher}
}
