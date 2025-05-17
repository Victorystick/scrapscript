package yards

import (
	"fmt"
	"io"
	"net/http"
)

type httpFetcher struct {
	client   *http.Client
	hostname string
}

func ByHttp(hostname string) Fetcher {
	return ByHttpWithClient(hostname, http.DefaultClient)
}

func ByHttpWithClient(hostname string, client *http.Client) Fetcher {
	return httpFetcher{client, hostname}
}

func (h httpFetcher) FetchSha256(key string) ([]byte, error) {
	req, err := http.NewRequest("GET", string(h.hostname)+key, nil)
	if err != nil {
		return nil, err
	}

	resp, err := h.client.Do(req)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("http get failed with %s", resp.Status)
	}

	return io.ReadAll(resp.Body)
}
