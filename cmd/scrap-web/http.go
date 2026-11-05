package main

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"strconv"
	"strings"

	"github.com/Victorystick/scrapscript/token"
)

// A command is what the page carries out for an app, which commands
// encode as JSON: an HTTP request, for now. Its id names it when the
// page responds.
type command struct {
	ID   uint32       `json:"id"`
	HTTP *httpRequest `json:"http,omitempty"`
}

// An httpRequest is a request for fetch, with a text body, or bytes as
// base64, or neither.
type httpRequest struct {
	Method  string      `json:"method"`
	URL     string      `json:"url"`
	Headers [][2]string `json:"headers"`
	Text    *string     `json:"text,omitempty"`
	Bytes   *[]byte     `json:"bytes,omitempty"`
}

// commands takes app h's queued commands, gives each request an id, and
// returns them as a JSON list of commands, or nothing if there are none.
// The other commands are dropped for now (WEB_PLAN.md).
func (m *module) commands(h int) ([]byte, error) {
	a, err := m.app(h)
	if err != nil {
		return nil, err
	}
	var cmds []command
	for _, c := range a.s.Commands() {
		if c.Tag() != CmdHttp {
			continue
		}
		r := c.Http()
		a.next++
		if a.pending == nil {
			a.pending = map[uint32]ExpectRef{}
		}
		a.pending[a.next] = r.Expect()
		cmds = append(cmds, command{ID: a.next, HTTP: newHTTPRequest(r)})
	}
	if len(cmds) == 0 {
		return nil, nil
	}
	return json.Marshal(cmds)
}

// newHTTPRequest makes r a request for fetch. A JSON body is encoded as
// text, with the content type application/json unless r gives one.
func newHTTPRequest(r RequestRef) *httpRequest {
	req := &httpRequest{Method: r.Method(), URL: r.Url(), Headers: [][2]string{}}
	typed := false
	for _, h := range r.Headers().All() {
		req.Headers = append(req.Headers, [2]string{h.Name(), h.Value()})
		typed = typed || strings.EqualFold(h.Name(), "content-type")
	}
	switch b := r.Body(); b.Tag() {
	case BodyText:
		t := b.Text()
		req.Text = &t
	case BodyBytes:
		bs := b.Bytes()
		req.Bytes = &bs
	case BodyJson:
		t := string(appendJSON(nil, b.Json()))
		req.Text = &t
		if !typed {
			req.Headers = append(req.Headers, [2]string{"content-type", "application/json"})
		}
	}
	return req
}

// response passes app h the response to a request that the page reports,
// as little-endian words, then bytes:
//
//	id status headerCount (nameLength valueLength)… names and values… body
//
// where the status is 0 if the request failed, and the body is then why.
// As in Elm, a response whose status isn't 2xx is an error too, as is a
// body that the request expects to be JSON and isn't. A response to a
// request that the app no longer awaits is ignored.
func (m *module) response(h int, input []byte) error {
	a, err := m.app(h)
	if err != nil {
		return err
	}
	id, status, headers, body, err := decodeResponse(input)
	if err != nil {
		return err
	}
	expect, ok := a.pending[id]
	if !ok {
		return nil
	}
	delete(a.pending, id)

	failure := ""
	switch {
	case status == 0:
		failure = string(body)
	case status < 200 || status > 299:
		failure = "bad status: " + strconv.Itoa(int(status))
	}
	b := a.s.app.Bindings()
	a.step()
	var msg Msg
	switch expect.Tag() {
	case ExpectText:
		r := NewResultResponseTextError(failure)
		if failure == "" {
			text := strings.ToValidUTF8(string(body), "�")
			r = NewResultResponseTextOk(ResponseText{Status: int64(status), Headers: headers, Body: text})
		}
		msg, err = expect.Text()(r.Ref(b))
	case ExpectBytes:
		r := NewResultResponseBytesError(failure)
		if failure == "" {
			r = NewResultResponseBytesOk(ResponseBytes{Status: int64(status), Headers: headers, Body: body})
		}
		msg, err = expect.Bytes()(r.Ref(b))
	case ExpectJson:
		r := NewResultResponseJsonError(failure)
		if failure == "" {
			if j, err := parseJSON(body); err != nil {
				r = NewResultResponseJsonError("bad body: " + err.Error())
			} else {
				r = NewResultResponseJsonOk(ResponseJson{Status: int64(status), Headers: headers, Body: j})
			}
		}
		msg, err = expect.Json()(r.Ref(b))
	}
	if err == nil {
		err = a.s.Update(msg)
	}
	if err != nil {
		return errors.New(token.Render(err, nil))
	}
	return nil
}

// decodeResponse decodes a response in the format of module.response.
func decodeResponse(input []byte) (id, status uint32, headers []Header, body []byte, err error) {
	word := func() uint32 {
		w := binary.LittleEndian.Uint32(input)
		input = input[4:]
		return w
	}
	if len(input) < 12 {
		return 0, 0, nil, nil, errors.New("bad response: too short")
	}
	id, status = word(), word()
	n := word()
	if uint64(n) > uint64(len(input))/8 {
		return 0, 0, nil, nil, errors.New("bad response: its headers are cut short")
	}
	lengths := make([]uint32, 2*n)
	for i := range lengths {
		lengths[i] = word()
	}
	text := func(n uint32) (string, bool) {
		if uint64(n) > uint64(len(input)) {
			return "", false
		}
		s := string(input[:n])
		input = input[n:]
		return s, true
	}
	headers = make([]Header, n)
	for i := range headers {
		name, ok1 := text(lengths[2*i])
		value, ok2 := text(lengths[2*i+1])
		if !ok1 || !ok2 {
			return 0, 0, nil, nil, errors.New("bad response: its headers are cut short")
		}
		headers[i] = Header{Name: name, Value: value}
	}
	return id, status, headers, input, nil
}
