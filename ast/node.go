package ast

import (
	"strings"

	"github.com/Victorystick/scrapscript/scanner"
	"github.com/Victorystick/scrapscript/token"
)

// Tok is the index of a token in a SourceExpr's Tokens.
type Tok uint32

// Node is the index of a node in a SourceExpr's node arrays.
type Node uint32

// None is the placeholder node 0, which stands in for a missing optional
// child.
const None Node = 0

// Kind is the kind of a node. Each kind's comment says how it uses its main
// token and its Lhs and Rhs data; a range Lhs..Rhs is a slice of Extra.
type Kind uint8

const (
	Invalid   Kind = iota // only None
	Ident                 // Main: the name.
	Literal               // Main: the literal, whose token kind is its kind.
	Binary                // Main: the operator. Lhs, Rhs: the operands.
	Func                  // Main: '->'. Lhs: the parameter. Rhs: the body.
	MatchFunc             // Lhs..Rhs: the arms, which are Func nodes.
	Call                  // Lhs: the function. Rhs: the argument.
	Variant               // Main: the tag after '#'. Lhs: the payload, or None.
	Record                // Main: '{'. Lhs: the spread record, or None. Rhs: see Record.
	Access                // Main: the key. Lhs: the record.
	List                  // Main: '['. Lhs..Rhs: the elements, then the ']' token.
	Where                 // Main: the bound name. Lhs: the body. Rhs: see Where.
	Import                // Main: '$', followed by the algorithm and hash tokens, or a path's TEXT token.

	// Types. A type also uses Ident and Import for names, Literal for (),
	// Call for type application and Func for function types.

	Forall     // Main: the variable before '=>'. Lhs: the type it is bound in.
	RecordType // Main: '{'. Lhs: the row tail, an Ident, or None. Rhs: see Record.
	Enum       // Main: '['. Lhs: the row tail, an Ident, or None. Rhs: see Enum.
)

var kindStrings = [...]string{
	Invalid:    "Invalid",
	Ident:      "Ident",
	Literal:    "Literal",
	Binary:     "Binary",
	Func:       "Func",
	MatchFunc:  "MatchFunc",
	Call:       "Call",
	Variant:    "Variant",
	Record:     "Record",
	Access:     "Access",
	List:       "List",
	Where:      "Where",
	Import:     "Import",
	Forall:     "Forall",
	RecordType: "RecordType",
	Enum:       "Enum",
}

func (k Kind) String() string {
	if int(k) < len(kindStrings) {
		return kindStrings[k]
	}
	return "Kind(?)"
}

// Data holds a node's two children or indices; see Kind for their meaning.
type Data struct {
	Lhs, Rhs uint32
}

// A SourceExpr is a parsed source: its text, tokens and expression tree.
//
// The tree is stored as parallel arrays indexed by Node. Nodes with more than
// two children keep them in Extra, which holds node indices and, where a
// Kind says so, token indices.
type SourceExpr struct {
	Source token.Source
	Tokens scanner.Tokens

	Kinds []Kind
	Mains []Tok
	Data  []Data
	Extra []Node

	// Expr is the root node.
	Expr Node
}

func (se *SourceExpr) Kind(n Node) Kind { return se.Kinds[n] }
func (se *SourceExpr) Main(n Node) Tok  { return se.Mains[n] }

func (se *SourceExpr) extra(d Data) []Node { return se.Extra[d.Lhs:d.Rhs] }

// LiteralKind returns the token kind of a Literal.
func (se *SourceExpr) LiteralKind(n Node) token.Token {
	return se.Tokens.Kinds[se.Mains[n]]
}

// Binary returns a Binary node's operator and operands.
func (se *SourceExpr) Binary(n Node) (op token.Token, left, right Node) {
	d := se.Data[n]
	return se.Tokens.Kinds[se.Mains[n]], Node(d.Lhs), Node(d.Rhs)
}

// Func returns a Func node's parameter and body.
func (se *SourceExpr) Func(n Node) (arg, body Node) {
	d := se.Data[n]
	return Node(d.Lhs), Node(d.Rhs)
}

// Call returns a Call node's function and argument.
func (se *SourceExpr) Call(n Node) (fn, arg Node) {
	d := se.Data[n]
	return Node(d.Lhs), Node(d.Rhs)
}

// Arms returns a MatchFunc node's arms, which are Func nodes.
func (se *SourceExpr) Arms(n Node) []Node { return se.extra(se.Data[n]) }

// Enum returns an Enum node's row tail, which may be None, and its Variant
// nodes. Its Rhs indexes Extra at: the ']' token, the variant count n, then
// the n variants.
func (se *SourceExpr) Enum(n Node) (tail Node, variants []Node) {
	d := se.Data[n]
	count := uint32(se.Extra[d.Rhs+1])
	start := d.Rhs + 2
	return Node(d.Lhs), se.Extra[start : start+count]
}

// Forall returns a Forall node's variable and the type it is bound in.
func (se *SourceExpr) Forall(n Node) (param Tok, typ Node) {
	return se.Mains[n], Node(se.Data[n].Lhs)
}

// Variant returns a Variant node's tag and payload, which may be None.
func (se *SourceExpr) Variant(n Node) (tag Tok, typ Node) {
	return se.Mains[n], Node(se.Data[n].Lhs)
}

// Fields are a record's fields: alternating key tokens and value nodes.
type Fields []Node

func (f Fields) Len() int         { return len(f) / 2 }
func (f Fields) Key(i int) Tok    { return Tok(f[2*i]) }
func (f Fields) Value(i int) Node { return f[2*i+1] }

// Record returns a Record node's spread record, which may be None, and its
// fields. Its Rhs indexes Extra at: the '}' token, the field count n, then
// the n fields. For a RecordType, it returns the row tail and the field types.
func (se *SourceExpr) Record(n Node) (rest Node, fields Fields) {
	d := se.Data[n]
	count := uint32(se.Extra[d.Rhs+1])
	start := d.Rhs + 2
	return Node(d.Lhs), Fields(se.Extra[start : start+2*count])
}

// Access returns an Access node's record and key.
func (se *SourceExpr) Access(n Node) (rec Node, key Tok) {
	return Node(se.Data[n].Lhs), se.Mains[n]
}

// List returns a List node's elements.
func (se *SourceExpr) List(n Node) []Node {
	d := se.Data[n]
	return se.Extra[d.Lhs : d.Rhs-1]
}

// Where returns a Where node's body, bound name, its annotation, which may be
// None, and its value. Its Rhs indexes Extra at: the annotation, the value,
// then 1 for a sort binding, whose value is a type and annotation a kind.
func (se *SourceExpr) Where(n Node) (body Node, id Tok, typ, val Node) {
	d := se.Data[n]
	return Node(d.Lhs), se.Mains[n], se.Extra[d.Rhs], se.Extra[d.Rhs+1]
}

// Sort returns true if a Where node is a sort binding, which binds a type.
func (se *SourceExpr) Sort(n Node) bool {
	return se.Extra[se.Data[n].Rhs+2] != 0
}

// Hash returns the scheme and hex digest of an Import node that refers to a
// published scrap by hash, $scheme:hex.
func (se *SourceExpr) Hash(n Node) (scheme, digest string) {
	scheme, digest, _ = strings.Cut(se.TokenText(se.Mains[n])[1:], ":")
	return scheme, digest
}

// Path returns the path of an Import node that refers to a file, $"path",
// and whether it does.
func (se *SourceExpr) Path(n Node) (string, bool) {
	if se.Tokens.Kinds[se.Mains[n]] != token.IMPORT {
		return "", false
	}
	return scanner.Unquote(se.TokenText(se.Mains[n] + 1)), true
}

// Imports returns the Import nodes of the tree.
func (se *SourceExpr) Imports() (imports []Node) {
	for n, k := range se.Kinds {
		if k == Import {
			imports = append(imports, Node(n))
		}
	}
	return imports
}

// First returns the index of n's first token.
func (se *SourceExpr) First(n Node) Tok {
	switch se.Kinds[n] {
	case Binary, Func, Call, Where, Access:
		return se.First(Node(se.Data[n].Lhs))
	case MatchFunc:
		arg, _ := se.Func(se.Arms(n)[0])
		return se.First(arg)
	case Variant:
		// The token before the tag is its '#'.
		return se.Mains[n] - 1
	}
	return se.Mains[n]
}

// Last returns the index of n's last token.
func (se *SourceExpr) Last(n Node) Tok {
	switch se.Kinds[n] {
	case Binary, Func, Call:
		return se.Last(Node(se.Data[n].Rhs))
	case MatchFunc:
		arms := se.Arms(n)
		_, body := se.Func(arms[len(arms)-1])
		return se.Last(body)
	case Forall:
		return se.Last(Node(se.Data[n].Lhs))
	case Variant:
		if _, typ := se.Variant(n); typ != None {
			return se.Last(typ)
		}
	case Record, RecordType, Enum:
		return Tok(se.Extra[se.Data[n].Rhs])
	case List:
		return Tok(se.Extra[se.Data[n].Rhs-1])
	case Where:
		_, _, _, val := se.Where(n)
		return se.Last(val)
	case Import:
		if _, ok := se.Path(n); ok {
			return se.Mains[n] + 1
		}
	}
	return se.Mains[n]
}

// TokenSpan returns the span of token t.
func (se *SourceExpr) TokenSpan(t Tok) token.Span {
	return se.Tokens.Span(se.Source.Bytes(), int(t))
}

// TokenText returns the source text of token t.
func (se *SourceExpr) TokenText(t Tok) string {
	return se.Source.GetString(se.TokenSpan(t))
}

// Span returns the span from the start of n's first token to the end of its
// last token.
func (se *SourceExpr) Span(n Node) token.Span {
	return token.Span{
		Start: int(se.Tokens.Starts[se.First(n)]),
		End:   se.Tokens.End(se.Source.Bytes(), int(se.Last(n))),
	}
}

// NodeText returns the source text of n.
func (se *SourceExpr) NodeText(n Node) string {
	return se.Source.GetString(se.Span(n))
}

// Error returns an error located at n.
func (se *SourceExpr) Error(n Node, msg string) token.Error {
	return se.Source.Error(se.Span(n), msg)
}

// TokenError returns an error located at token t.
func (se *SourceExpr) TokenError(t Tok, msg string) token.Error {
	return se.Source.Error(se.TokenSpan(t), msg)
}
