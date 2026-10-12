package eval

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"path"

	"github.com/Victorystick/scrapscript/ast"
	"github.com/Victorystick/scrapscript/parser"
	"github.com/Victorystick/scrapscript/token"
	"github.com/Victorystick/scrapscript/types"
)

// A Fetcher fetches the source of a published scrap by its sha256 hash, in
// hex. The yards package has implementations.
type Fetcher interface {
	FetchSha256(key string) ([]byte, error)
}

// A Pusher stores the source of a published scrap, returning its hash.
type Pusher interface {
	PushScrap(data []byte) (key string, err error)
}

// A Scrap is a parsed scrap: a draft, or a published scrap fetched by hash.
type Scrap struct {
	expr      ast.SourceExpr
	published bool
	// The path of a draft read from a file, which its own paths are relative
	// to. A draft read otherwise, as from standard input, is an unnamed file
	// at the root.
	name string

	// The scraps that its Import nodes refer to, once it is loaded.
	deps            map[ast.Node]*Scrap
	loading, loaded bool
	inferred        bool
	result          types.Result
	value           Value
}

func (s Scrap) Sha256() string {
	return fmt.Sprintf("%x", sha256.Sum256(s.expr.Source.Bytes()))
}

// A hashKey identifies a published scrap by its hashing scheme and digest.
type hashKey struct {
	algo   string
	digest string
}

// An Environment loads, checks and evaluates scraps. It reaches outside
// itself only through the Fetcher, Pusher and drafts it is given, so it runs
// anywhere, including where there is no filesystem or network.
type Environment struct {
	pusher  Pusher
	fetcher Fetcher
	// The files that drafts' path references refer to, or nil if they
	// aren't allowed.
	files fs.FS
	reg   types.Registry
	// The TypeScope and Variables match each other's contents.
	// One is used for type inference, the other for evaluation.
	typeScope types.TypeScope
	vars      Variables
	scraps    map[hashKey]*Scrap
	drafts    map[string]*Scrap // by path
}

func NewEnvironment() *Environment {
	env := &Environment{}
	typeScope, vars := bindBuiltIns(&env.reg)
	env.typeScope = typeScope
	env.vars = vars
	env.scraps = make(map[hashKey]*Scrap)
	env.drafts = make(map[string]*Scrap)
	return env
}

func (e *Environment) UsePusher(pusher Pusher) {
	e.pusher = pusher
}

func (e *Environment) UseFetcher(fetcher Fetcher) {
	e.fetcher = fetcher
}

// UseDrafts lets drafts refer to the files in files by path, such as
// os.DirFS(".") for the current directory, or an in-memory fs.FS.
func (e *Environment) UseDrafts(files fs.FS) {
	e.files = files
}

// Read parses a draft.
func (e *Environment) Read(script []byte) (*Scrap, error) {
	src := token.NewSource(script)
	se, err := parser.Parse(&src)
	if err != nil {
		return nil, fmt.Errorf("parse error: %w", err)
	}
	return &Scrap{expr: se}, nil
}

// load loads the closure of a scrap: the scraps that it refers to, by hash
// or path, and theirs in turn. Only paths can form cycles.
func (e *Environment) load(s *Scrap) error {
	if s.loaded {
		return nil
	}
	s.loading = true
	defer func() { s.loading = false }()

	s.deps = make(map[ast.Node]*Scrap)
	for _, n := range s.expr.Imports() {
		dep, err := e.resolve(s, n)
		if err != nil {
			return err
		}
		if dep.loading {
			return s.expr.Error(n, fmt.Sprintf("%s refers back to this scrap", s.expr.NodeText(n)))
		}
		if err := e.load(dep); err != nil {
			return err
		}
		s.deps[n] = dep
	}
	s.loaded = true
	return nil
}

// resolve returns the scrap that the Import node n of s refers to.
func (e *Environment) resolve(s *Scrap, n ast.Node) (*Scrap, error) {
	if path, ok := s.expr.Path(n); ok {
		if s.published {
			return nil, s.expr.Error(n, "a published scrap cannot refer to a path")
		}
		dep, err := e.draft(s, path)
		if err != nil {
			return nil, s.expr.Error(n, err.Error())
		}
		return dep, nil
	}

	algo, hash := s.expr.Import(n)
	digest := s.expr.TokenText(hash)[2:]
	dep, err := e.fetch(s.expr.TokenText(algo), digest)
	if err != nil {
		return nil, s.expr.Error(n, err.Error())
	}
	return dep, nil
}

// draft returns the draft at the path p, relative to the draft from that
// refers to it. It must be within the root of the environment's files.
func (e *Environment) draft(from *Scrap, p string) (*Scrap, error) {
	if e.files == nil {
		return nil, fmt.Errorf("cannot refer to a path here")
	}
	if path.IsAbs(p) {
		return nil, fmt.Errorf("cannot refer to an absolute path")
	}
	name := path.Join(path.Dir(from.name), p)
	if !fs.ValidPath(name) {
		return nil, fmt.Errorf("cannot refer to a path outside the current directory")
	}

	if s, ok := e.drafts[name]; ok {
		return s, nil
	}
	bytes, err := fs.ReadFile(e.files, name)
	if err != nil {
		return nil, err
	}
	s, err := e.Read(bytes)
	if err != nil {
		return nil, err
	}
	s.name = name
	e.drafts[name] = s
	return s, nil
}

// fetch returns the published scrap with the given hash.
func (e *Environment) fetch(algo, digest string) (*Scrap, error) {
	if algo != "sha256" {
		return nil, fmt.Errorf("only sha256 imports are supported")
	}
	hash, err := hex.DecodeString(digest)
	if err != nil {
		return nil, fmt.Errorf("bad hash %s", digest)
	}
	if len(hash) != sha256.Size {
		return nil, fmt.Errorf("cannot import sha256 bytes of length %d, must be %d", len(hash), sha256.Size)
	}

	key := hashKey{algo, hex.EncodeToString(hash)}
	if s, ok := e.scraps[key]; ok {
		return s, nil
	}
	if e.fetcher == nil {
		return nil, fmt.Errorf("cannot import without a fetcher")
	}
	bytes, err := e.fetcher.FetchSha256(key.digest)
	if err != nil {
		return nil, err
	}
	s, err := e.Read(bytes)
	if err != nil {
		return nil, err
	}
	s.published = true
	e.scraps[key] = s
	return s, nil
}

// infer loads and checks a scrap's closure, the scraps it refers to first.
func (e *Environment) infer(s *Scrap) (types.Result, error) {
	if err := e.load(s); err != nil {
		return types.Result{}, err
	}
	return e.check(s)
}

func (e *Environment) check(s *Scrap) (types.Result, error) {
	if s.inferred {
		return s.result, nil
	}
	imports := make(types.Imports, len(s.deps))
	for n, dep := range s.deps {
		res, err := e.check(dep)
		if err != nil {
			return types.Result{}, err
		}
		imports[n] = res
	}
	res, err := types.Infer(&e.reg, e.typeScope, s.expr, imports)
	if err != nil {
		return types.Result{}, err
	}
	s.result, s.inferred = res, true
	return res, nil
}

// Eval type-checks and evaluates a Scrap. The scraps it refers to are
// evaluated when first used.
func (e *Environment) Eval(s *Scrap) (Value, error) {
	res, err := e.infer(s)
	if err != nil {
		return nil, err
	}
	return e.eval(s, res)
}

func (e *Environment) eval(s *Scrap, res types.Result) (Value, error) {
	if res.Sort {
		return nil, fmt.Errorf("cannot evaluate a type")
	}
	if s.value == nil {
		value, err := Eval(s.expr, e.vars, func(n ast.Node) (Value, error) {
			dep := s.deps[n]
			v, err := e.eval(dep, dep.result)
			// A published function prints as the reference to it.
			if sf, ok := v.(ScriptFunc); ok && dep.published {
				sf.ref = s.expr.NodeText(n)
				return sf, err
			}
			return v, err
		})
		if err != nil {
			return nil, err
		}
		s.value = value
	}
	return s.value, nil
}

// Infer returns the string representation of the type of a Scrap.
func (e *Environment) Infer(s *Scrap) (string, error) {
	res, err := e.infer(s)
	if err != nil {
		return "", err
	}
	return e.reg.Describe(res), nil
}

// Scrap renders a Value as self-contained scrapscript program.
func (e *Environment) Scrap(value Value) string {
	return value.String()
}

func (e *Environment) Push(s *Scrap) (string, error) {
	if e.pusher == nil {
		return "", fmt.Errorf("cannot push without a pusher")
	}
	for _, n := range s.expr.Imports() {
		if _, ok := s.expr.Path(n); ok {
			return "", s.expr.Error(n, "cannot push a draft that refers to a path")
		}
	}
	return e.pusher.PushScrap(s.expr.Source.Bytes())
}
