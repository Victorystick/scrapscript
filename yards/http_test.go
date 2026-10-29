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

func TestPushByHttp(t *testing.T) {
	trans := transport{}
	client := http.Client{Transport: &trans}
	f := ByHttpWithClient("https://scraps.oseg.dev/", &client)

	// Happy case.
	trans.resp = &http.Response{
		StatusCode: 200,
		Body:       io.NopCloser(bytes.NewReader([]byte("abc"))),
	}
	key, err := f.PushScrap([]byte("1"))
	if err != nil || key != "abc" {
		t.Errorf("expected key abc, got %q, %v", key, err)
	}
	if trans.req.Method != "POST" || trans.req.Header.Get("Content-Type") != "application/scrap" {
		t.Errorf("unexpected request %s %s", trans.req.Method, trans.req.Header)
	}

	// A newly stored scrap.
	trans.resp = &http.Response{
		StatusCode: 201,
		Body:       io.NopCloser(bytes.NewReader([]byte("abc"))),
	}
	key, err = f.PushScrap([]byte("1"))
	if err != nil || key != "abc" {
		t.Errorf("expected key abc, got %q, %v", key, err)
	}

	// Error case: the body is the server's message, not a key.
	trans.resp = &http.Response{
		Status:     "422 Unprocessable Entity",
		StatusCode: 422,
		Body:       io.NopCloser(bytes.NewReader([]byte("type error\n"))),
	}
	key, err = f.PushScrap([]byte("1"))
	if err == nil || err.Error() != "http post failed with 422 Unprocessable Entity: type error" {
		t.Errorf("expected HTTP 422 error, got %v", err)
	}
	if key != "" {
		t.Errorf("unexpected key %q", key)
	}
}
