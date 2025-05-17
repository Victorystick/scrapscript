package yards

import (
	"bytes"
	"io"
	"net/http"
	"net/url"
	"testing"
)

type transport struct {
	req  *http.Request
	resp *http.Response
	err  error
}

func (t *transport) RoundTrip(req *http.Request) (resp *http.Response, err error) {
	t.req = req
	return t.resp, t.err
}

func TestByHttp(t *testing.T) {
	u, err := url.Parse("https://scraps.oseg.dev/key")
	if err != nil {
		t.Fatalf("could not parse url: %v", err)
	}

	trans := transport{}
	client := http.Client{Transport: &trans}
	f := ByHttpWithClient("https://scraps.oseg.dev/", &client)

	// Happy case.
	trans.resp = &http.Response{
		StatusCode: 200,
		Body:       io.NopCloser(bytes.NewReader([]byte{1, 2, 3})),
	}
	bs, err := f.FetchSha256("key")
	if err != nil {
		t.Errorf("unexpected error %v", err)
	}
	equalBytes(t, bs, []byte{1, 2, 3})
	if trans.req.URL.String() != u.String() {
		t.Errorf("unexpectedly URL %s != %s", trans.req.URL, u)
	}

	// Error case.
	trans.resp = &http.Response{
		Status:     "Bad Req. 400",
		StatusCode: 400,
		Body:       io.NopCloser(bytes.NewReader([]byte("Bad!"))),
	}
	bs, err = f.FetchSha256("key")
	if err.Error() != "http get failed with Bad Req. 400" {
		t.Error("expected HTTP 400 error")
	}
	if bs != nil {
		t.Error("unexpected read bytes")
	}
}
