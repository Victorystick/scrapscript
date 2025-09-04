package eval

import (
	"crypto/sha256"
	"fmt"

	"github.com/Victorystick/scrapscript/ast"
	"github.com/Victorystick/scrapscript/parser"
	"github.com/Victorystick/scrapscript/token"
	"github.com/Victorystick/scrapscript/types"
	"github.com/Victorystick/scrapscript/yards"
)

type Scrap struct {
	expr  ast.SourceExpr
	typ   types.TypeRef
	value Value
}

func (s Scrap) Sha256() string {
	return fmt.Sprintf("%x", sha256.Sum256(s.expr.Source.Bytes()))
}

type Sha256Hash = [32]byte

type Environment struct {
	pusher  yards.Pusher
	fetcher yards.Fetcher
	reg     types.Registry
	// The TypeScope and Variables match each other's contents.
	// One is used for type inference, the other for evaluation.
	typeScope   types.TypeScope
	vars        Variables
	scraps      map[Sha256Hash]*Scrap
	evalImport  EvalImport
	inferImport types.InferImport
}

func NewEnvironment() *Environment {
	env := &Environment{}
	typeScope, vars := bindBuiltIns(&env.reg)
	env.typeScope = typeScope
	env.vars = vars
	env.scraps = make(map[Sha256Hash]*Scrap)
	env.evalImport = func(algo string, hash []byte) (Value, error) {
		scrap, err := env.fetch(algo, hash)
		if err != nil {
			return nil, err
		}
		return env.Eval(scrap)
	}
	env.inferImport = func(algo string, hash []byte) (types.TypeRef, error) {
		scrap, err := env.fetch(algo, hash)
		if err != nil {
			return types.NeverRef, err
		}
		return env.infer(scrap)
	}
	return env
}

func (e *Environment) UsePusher(pusher yards.Pusher) {
	e.pusher = pusher
}

func (e *Environment) UseFetcher(fetcher yards.Fetcher) {
	e.fetcher = fetcher
}

func (e *Environment) fetch(algo string, hash []byte) (*Scrap, error) {
	if algo != "sha256" {
		return nil, fmt.Errorf("only sha256 imports are supported")
	}

	if len(hash) != sha256.Size {
		return nil, fmt.Errorf("cannot import sha256 bytes of length %d, must be %d", len(hash), sha256.Size)
	}

	if scrap, ok := e.scraps[(Sha256Hash)(hash)]; ok {
		return scrap, nil
	}

	if e.fetcher == nil {
		return nil, fmt.Errorf("cannot import without a fetcher")
	}

	key := fmt.Sprintf("%x", hash)
	bytes, err := e.fetcher.FetchSha256(key)
	if err != nil {
		return nil, err
	}

	return e.Read(bytes)
}

func (e *Environment) Read(script []byte) (*Scrap, error) {
	src := token.NewSource(script)
	se, err := parser.Parse(&src)

	if err != nil {
		return nil, fmt.Errorf("parse error: %w", err)
	}

	scrap := &Scrap{expr: se}
	e.scraps[sha256.Sum256(script)] = scrap
	return scrap, nil
}

// Eval evaluates a Scrap.
func (e *Environment) Eval(scrap *Scrap) (Value, error) {
	if scrap.value == nil {
		value, err := Eval(scrap.expr, &e.reg, e.vars, e.evalImport)
		scrap.value = value
		return value, err
	}
	return scrap.value, nil
}

func (e *Environment) infer(scrap *Scrap) (types.TypeRef, error) {
	if scrap.typ == types.NeverRef {
		ref, err := types.Infer(&e.reg, e.typeScope, scrap.expr, e.inferImport)
		scrap.typ = ref
		return ref, err
	}
	return scrap.typ, nil
}

// Infer returns the string representation of the type of a Scrap.
func (e *Environment) Infer(scrap *Scrap) (string, error) {
	ref, err := e.infer(scrap)
	return e.reg.String(ref), err
}

// Scrap renders a Value as self-contained scrapscript program.
func (e *Environment) Scrap(value Value) string {
	if vr, ok := value.(Variant); ok {
		if vr.value == nil {
			return fmt.Sprintf("(%s)::%s", e.reg.String(vr.typ), vr.tag)
		}
		return fmt.Sprintf("(%s)::%s %s", e.reg.String(vr.typ), vr.tag, e.Scrap(vr.value))
	}
	return value.String()
}

func (e *Environment) Push(scrap *Scrap) (string, error) {
	if e.pusher == nil {
		return "", fmt.Errorf("cannot push without a pusher")
	}

	return e.pusher.PushScrap(scrap.expr.Source.Bytes())
}
