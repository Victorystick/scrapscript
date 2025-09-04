package yards

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
)

type httpFetcher struct {
	client   *http.Client
	hostname string
}

func ByHttp(hostname string) FetchPusher {
	return ByHttpWithClient(hostname, http.DefaultClient)
}

func ByHttpWithClient(hostname string, client *http.Client) FetchPusher {
	return httpFetcher{client, hostname}
}

func (h httpFetcher) FetchSha256(key string) ([]byte, error) {
	req, err := http.NewRequest("GET", string(h.hostname)+key, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Add("Accept", "application/scrap")

	resp, err := h.client.Do(req)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("http get failed with %s", resp.Status)
	}

	return io.ReadAll(resp.Body)
}

func (h httpFetcher) PushScrap(data []byte) (key string, err error) {
	req, err := http.NewRequest("POST", string(h.hostname), bytes.NewReader(data))
	if err != nil {
		return
	}
	req.Header.Add("Content-Type", "application/scrap")

	resp, err := h.client.Do(req)
	if err != nil {
		return
	}

	bytes, err := io.ReadAll(resp.Body)
	key = string(bytes)
	return
}
