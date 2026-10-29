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
, count = { value = 7 }
, names = { value = ["x", "y"] }
, helper = add -> add 20 22
}`

func load(t testing.TB, source string) (ApiRef, error) {
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
	ref, err := load(t, app)
	if err != nil {
		t.Fatal(err)
	}
	// The structs, converted all at once.
	api := ref.Load()

	if !slices.Equal(api.Tags, []string{"a", "b"}) || string(api.Data) != "hi" ||
		api.Ratio != 1.5 || api.Flag != 0xFF {
		t.Errorf("unexpected fields: %+v", api)
	}

	// An alias used at two types binds to a type for each.
	if api.Count.Value != 7 || !slices.Equal(api.Names.Value, []string{"x", "y"}) {
		t.Errorf("unexpected boxes: %+v, %+v", api.Count, api.Names)
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

// TestRefs reads the same values through Refs, which convert only what is
// read.
func TestRefs(t *testing.T) {
	api, err := load(t, app)
	if err != nil {
		t.Fatal(err)
	}

	if !slices.Equal(api.Tags().Load(), []string{"a", "b"}) || api.Tags().Len() != 2 ||
		api.Tags().At(1) != "b" || string(api.Data()) != "hi" || api.Ratio() != 1.5 || api.Flag() != 0xFF {
		t.Errorf("unexpected fields: %+v", api.Load())
	}
	for i, tag := range api.Tags().All() {
		if tag != []string{"a", "b"}[i] {
			t.Errorf("unexpected tag %d: %q", i, tag)
		}
	}

	if api.Count().Value() != 7 || api.Names().Value().At(1) != "y" {
		t.Errorf("unexpected boxes: %+v, %+v", api.Count().Load(), api.Names().Load())
	}

	node := api.Tree().Node()
	if node.Left().Tag() != TreeLeaf || node.Value() != 2 || node.Right().Node().Value() != 3 {
		t.Errorf("unexpected tree: %+v", node.Load())
	}

	// Function fields are methods, which take and return Refs.
	reply, err := api.Handle(api.Init(), "/x")
	if err != nil {
		t.Fatal(err)
	}
	fetch := reply.Fetch()
	if fetch.Url() != "/x" {
		t.Errorf("expected to fetch /x, got %q", fetch.Url())
	}
	reply, err = fetch.Then("body")
	if err != nil {
		t.Fatal(err)
	}
	if r := reply.Respond(); r.Status() != 5 || !bytes.Equal(r.Body(), []byte("body")) {
		t.Errorf("unexpected response: %+v", r.Load())
	}

	sum, err := api.Helper(func(a, b int64) (int64, error) { return a + b, nil })
	if err != nil || sum != 42 {
		t.Errorf("expected 42, got %d, %v", sum, err)
	}

	// A struct goes back to the script through its Ref method.
	rebuilt := api.Tree().Load().Ref(api.Bindings())
	if got := rebuilt.Node().Right().Node().Value(); got != 3 {
		t.Errorf("expected the same tree, got %d", got)
	}
}

// TestToValue converts Go values to values, and back.
func TestToValue(t *testing.T) {
	env := eval.NewEnvironment()
	l := newBindings(env)
	tree := NewTreeNode(TreeNodePayload{Left: NewTreeLeaf(), Value: 7, Right: NewTreeLeaf()})
	if got := env.Print(tree.toValue(l)); got != `#node { left = #leaf, right = #leaf, value = 7 }` {
		t.Errorf("unexpected value: %s", got)
	}
	if back := treeFromValue(l, tree.toValue(l)); back.Node().Value != 7 {
		t.Errorf("unexpected tree: %+v", back)
	}
	r := env.Print(NewReplyRange("x").toValue(l))
	if r != `#range "x"` {
		t.Errorf("unexpected value: %s", r)
	}

	// The structs' Ref methods make the same values.
	ref := tree.Ref(l)
	if got := env.Print(ref.v); got != `#node { left = #leaf, right = #leaf, value = 7 }` {
		t.Errorf("unexpected value: %s", got)
	}
	if got := ref.Node().Value(); got != 7 {
		t.Errorf("expected 7, got %d", got)
	}
	if got := env.Print(NewReplyRange("x").Ref(l).v); got != `#range "x"` {
		t.Errorf("unexpected value: %s", got)
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

func TestWrongTagRef(t *testing.T) {
	defer func() {
		if r := recover(); r != "TreeRef: #node of #leaf" {
			t.Errorf("expected a panic for the wrong tag, got %v", r)
		}
	}()
	NewTreeLeaf().Ref(newBindings(eval.NewEnvironment())).Node()
}

func TestLoadRejects(t *testing.T) {
	if _, err := load(t, `{ init = 5 }`); err == nil {
		t.Errorf("expected Load to reject a scrap that lacks fields")
	}
}

// A tree of 2^16 - 1 nodes, which a script makes, for the host to read.
const bigTree = `{ init = 0
, handle = c -> url -> #range url
, tags = []
, data = ~~
, ratio = 0.0
, flag = ~00
, unit = ()
, tree = make 16
, helper = add -> 0
}
; make : int -> t = | 0 -> #leaf | n -> #node { left = make (n - 1), value = n, right = make (n - 1) }
; t : type = [#leaf, #node { left : t, value : int, right : t }]`

func sumRef(t TreeRef) int64 {
	if t.Tag() == TreeLeaf {
		return 0
	}
	n := t.Node()
	return sumRef(n.Left()) + n.Value() + sumRef(n.Right())
}

func sum(t Tree) int64 {
	if t.Tag() == TreeLeaf {
		return 0
	}
	n := t.Node()
	return sum(n.Left) + n.Value + sum(n.Right)
}

// BenchmarkTree reads a large tree: one field of it, or all of it, through
// Refs or by converting it to structs first.
func BenchmarkTree(b *testing.B) {
	api, err := load(b, bigTree)
	if err != nil {
		b.Fatal(err)
	}
	const want = 131054 // the sum of n * 2^(16-n) for n from 1 to 16
	b.Run("root/ref", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if api.Tree().Node().Value() != 16 {
				b.Fatal("wrong root")
			}
		}
	})
	b.Run("root/load", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if api.Tree().Load().Node().Value != 16 {
				b.Fatal("wrong root")
			}
		}
	})
	b.Run("all/ref", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if sumRef(api.Tree()) != want {
				b.Fatal("wrong sum")
			}
		}
	})
	b.Run("all/load", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if sum(api.Tree().Load()) != want {
				b.Fatal("wrong sum")
			}
		}
	})
}
