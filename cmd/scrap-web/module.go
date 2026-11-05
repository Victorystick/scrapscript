package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"time"

	"github.com/Victorystick/scrapscript/eval"
	"github.com/Victorystick/scrapscript/host"
	"github.com/Victorystick/scrapscript/token"
	"github.com/Victorystick/scrapscript/yards"
)

// A module is what the WebAssembly module serves a page, which wasm.go
// exports: the apps it runs, by handle. A page starts an app with its
// source, then renders it and passes it events, without ever waiting on
// the module: the module asks for the scraps that an app refers to, and
// the page fetches them and starts it again.
type module struct {
	apps []*app // by handle - 1, nil once stopped
}

// An app is a session, with its environment, how long each step may
// take, and what its requests expect, by id, until the page responds.
type app struct {
	s       *Session
	env     *eval.Environment
	timeout time.Duration
	pending map[uint32]ExpectRef
	next    uint32 // the last request's id
}

// A startRequest starts an app.
type startRequest struct {
	// The app's scrap.
	Source string `json:"source"`
	// The sources of the scraps it refers to by hash, by hash; the
	// interface's are built in.
	Sources map[string]string `json:"sources"`
	// The flags that init gets, as JSON; null if absent.
	Flags json.RawMessage `json:"flags"`
	// How long each step of the app may run, in milliseconds: starting
	// it, an update, or a view. No limit if 0.
	Timeout int `json:"timeout"`
}

// A startResponse has the app's handle, or why it didn't start, or the
// hashes of the scraps that the page should fetch before trying again.
type startResponse struct {
	Handle  int      `json:"handle,omitempty"`
	Error   string   `json:"error,omitempty"`
	Missing []string `json:"missing,omitempty"`
}

// start starts an app with the JSON request in input, and returns the
// JSON response.
func (m *module) start(input []byte) []byte {
	var req startRequest
	var res startResponse
	if err := json.Unmarshal(input, &req); err != nil {
		res.Error = "bad request: " + err.Error()
	} else {
		res = m.respond(req)
	}
	out, err := json.Marshal(res)
	if err != nil {
		panic(err)
	}
	return out
}

func (m *module) respond(req startRequest) startResponse {
	flags := NewJsonNull()
	if len(req.Flags) > 0 {
		var err error
		if flags, err = parseJSON(req.Flags); err != nil {
			return startResponse{Error: "bad flags: " + err.Error()}
		}
	}
	sources := maps.Clone(req.Sources)
	if sources == nil {
		sources = map[string]string{}
	}
	maps.Copy(sources, Interface.Sources)

	a := &app{env: eval.NewEnvironment(), timeout: time.Duration(req.Timeout) * time.Millisecond}
	a.env.UseFetcher(yards.Validate(yards.ByMap(sources)))
	script, err := a.env.Read([]byte(req.Source))
	if err != nil {
		return startResponse{Error: token.Render(err, nil)}
	}
	if missing := host.Missing(a.env, script, sources); len(missing) > 0 {
		return startResponse{Missing: missing}
	}
	// Check the script before the deadline starts, which bounds evaluating
	// only: Load checks it again, against the interface, but inference is
	// done by then.
	if _, err := a.env.Infer(script); err != nil {
		return startResponse{Error: token.Render(err, nil)}
	}
	a.step()
	if a.s, err = Start(a.env, script, flags); err != nil {
		return startResponse{Error: token.Render(err, nil)}
	}
	m.apps = append(m.apps, a)
	return startResponse{Handle: len(m.apps)}
}

// app returns the app with handle h.
func (m *module) app(h int) (*app, error) {
	if h < 1 || h > len(m.apps) || m.apps[h-1] == nil {
		return nil, fmt.Errorf("no app %d", h)
	}
	return m.apps[h-1], nil
}

// step gives the app's next step its deadline.
func (a *app) step() {
	if a.timeout > 0 {
		a.env.UseContext(host.Deadline(time.Now().Add(a.timeout)))
	}
}

// render returns the patch from the app's last view to its view now, as
// Session.Render does.
func (m *module) render(h int) ([]byte, error) {
	a, err := m.app(h)
	if err != nil {
		return nil, err
	}
	a.step()
	p, err := a.s.Render()
	if err != nil {
		return nil, errors.New(token.Render(err, nil))
	}
	return p, nil
}

// event passes the app an event that the page reports, as little-endian
// words, then bytes:
//
//	node flags nameLength name value
//
// where flags is 1 if the element is checked, and the value, the element's
// value, takes the rest.
func (m *module) event(h int, input []byte) error {
	a, err := m.app(h)
	if err != nil {
		return err
	}
	if len(input) < 12 {
		return errors.New("bad event: too short")
	}
	node := binary.LittleEndian.Uint32(input)
	flags := binary.LittleEndian.Uint32(input[4:])
	n := binary.LittleEndian.Uint32(input[8:])
	if uint64(n) > uint64(len(input)-12) {
		return errors.New("bad event: its name is cut short")
	}
	e := Event{
		Node:    node,
		Name:    string(input[12 : 12+n]),
		Value:   string(input[12+n:]),
		Checked: flags&1 != 0,
	}
	a.step()
	if err := a.s.Event(e); err != nil {
		return errors.New(token.Render(err, nil))
	}
	return nil
}

// stop forgets the app with handle h.
func (m *module) stop(h int) {
	if h >= 1 && h <= len(m.apps) {
		m.apps[h-1] = nil
	}
}

// parseJSON reads JSON text as a json value, keeping the order of objects'
// members.
func parseJSON(data []byte) (Json, error) {
	d := json.NewDecoder(bytes.NewReader(data))
	j, err := readJSON(d)
	if err != nil {
		return j, err
	}
	if _, err := d.Token(); err != io.EOF {
		return j, errors.New("more after the value")
	}
	return j, nil
}

func readJSON(d *json.Decoder) (Json, error) {
	t, err := d.Token()
	if err != nil {
		return Json{}, err
	}
	switch v := t.(type) {
	case nil:
		return NewJsonNull(), nil
	case bool:
		if v {
			return NewJsonBool(NewBoolTrue()), nil
		}
		return NewJsonBool(NewBoolFalse()), nil
	case float64:
		return NewJsonNumber(v), nil
	case string:
		return NewJsonText(v), nil
	case json.Delim:
		if v == '[' {
			var xs []Json
			for d.More() {
				x, err := readJSON(d)
				if err != nil {
					return Json{}, err
				}
				xs = append(xs, x)
			}
			_, err := d.Token()
			return NewJsonList(xs), err
		}
		var ms []Member
		for d.More() {
			k, err := d.Token()
			if err != nil {
				return Json{}, err
			}
			x, err := readJSON(d)
			if err != nil {
				return Json{}, err
			}
			ms = append(ms, Member{Key: k.(string), Value: x})
		}
		_, err := d.Token()
		return NewJsonObject(ms), err
	}
	return Json{}, fmt.Errorf("unexpected %v", t)
}
