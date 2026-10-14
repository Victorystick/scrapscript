package rich

import (
	"bytes"
	"slices"
	"testing"

	"github.com/Victorystick/scrapscript/eval"
)

const app = `{ init = 5
, handle = c -> url ->
    #fetch { url = url, then = body -> #respond { status = c, body = bytes/from-utf8-text body } }
, tags = ["a", "b"]
, data = ~~aGk=
, ratio = 1.5
, flag = ~FF
, unit = ()
, tree = #node { left = #leaf, value = 2, right = #node { left = #leaf, value = 3, right = #leaf } }
, helper = add -> add 20 22
}`

func load(t *testing.T, source string) (Api, error) {
	t.Helper()
	env := eval.NewEnvironment()
	env.UseFetcher(Interface.Fetcher())
	s, err := env.Read([]byte(source))
	if err != nil {
		t.Fatal(err)
	}
	return Load(env, s)
}

func TestBindings(t *testing.T) {
	api, err := load(t, app)
	if err != nil {
		t.Fatal(err)
	}

	if !slices.Equal(api.Tags, []string{"a", "b"}) || string(api.Data) != "hi" ||
		api.Ratio != 1.5 || api.Flag != 0xFF {
		t.Errorf("unexpected fields: %+v", api)
	}

	// A recursive enum, with records as payloads.
	node := api.Tree.Node()
	if node.Left.Tag() != TreeLeaf || node.Value != 2 || node.Right.Node().Value != 3 {
		t.Errorf("unexpected tree: %+v", node)
	}

	// The opaque init goes back into handle, whose reply continues with then.
	reply, err := api.Handle(api.Init, "/x")
	if err != nil {
		t.Fatal(err)
	}
	fetch := reply.Fetch()
	if fetch.Url != "/x" {
		t.Errorf("expected to fetch /x, got %q", fetch.Url)
	}
	reply, err = fetch.Then("body")
	if err != nil {
		t.Fatal(err)
	}
	if r := reply.Respond(); r.Status != 5 || !bytes.Equal(r.Body, []byte("body")) {
		t.Errorf("unexpected response: %+v", r)
	}

	// A Go function, passed to the script.
	sum, err := api.Helper(func(a, b int64) (int64, error) { return a + b, nil })
	if err != nil || sum != 42 {
		t.Errorf("expected 42, got %d, %v", sum, err)
	}
}

// TestToValue converts Go values to values, and back.
func TestToValue(t *testing.T) {
	tree := NewTreeNode(TreeNodePayload{Left: NewTreeLeaf(), Value: 7, Right: NewTreeLeaf()})
	if got := tree.toValue().String(); got != `#node { left = #leaf, right = #leaf, value = 7 }` {
		t.Errorf("unexpected value: %s", got)
	}
	if back := treeFromValue(tree.toValue()); back.Node().Value != 7 {
		t.Errorf("unexpected tree: %+v", back)
	}
	r := NewReplyRange("x").toValue().String()
	if r != `#range "x"` {
		t.Errorf("unexpected value: %s", r)
	}
}

func TestWrongTag(t *testing.T) {
	defer func() {
		if r := recover(); r != "Tree: #node of #leaf" {
			t.Errorf("expected a panic for the wrong tag, got %v", r)
		}
	}()
	NewTreeLeaf().Node()
}

func TestLoadRejects(t *testing.T) {
	if _, err := load(t, `{ init = 5 }`); err == nil {
		t.Errorf("expected Load to reject a scrap that lacks fields")
	}
}
