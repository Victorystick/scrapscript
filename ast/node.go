package ast

import (
	"github.com/Victorystick/scrapscript/scanner"
	"github.com/Victorystick/scrapscript/token"
)

// A SourceExpr combines an expression with its source and tokens,
// which are necessary to extract identifiers and literals from it,
// as well as for error reporting.
type SourceExpr struct {
	Source token.Source
	Tokens scanner.Tokens
	Expr   Expr
}

// Tok is the index of a token in a SourceExpr's Tokens.
type Tok uint32

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
		Start: int(se.Tokens.Starts[n.First()]),
		End:   se.Tokens.End(se.Source.Bytes(), int(n.Last())),
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

// A Node knows its first and last token; its span is derived from them.
type Node interface {
	First() Tok
	Last() Tok
}

type Expr interface {
	Node
	expr()
}

type Ident struct {
	Tok Tok
}

type Literal struct {
	Tok  Tok
	Kind token.Token
}

type BinaryExpr struct {
	Left  Expr
	Op    token.Token
	Right Expr
}

type FuncExpr struct {
	Arg  Expr
	Body Expr
}

// A pattern-matched FuncExpr
type MatchFuncExpr []*FuncExpr

type CallExpr struct {
	Fn  Expr
	Arg Expr
}

type VariantExpr struct {
	Tag Ident
	Typ Expr
}

// A name-matched VariantExpr
type EnumExpr []*VariantExpr

type RecordExpr struct {
	LBrace  Tok
	RBrace  Tok
	Entries map[string]Expr
	Rest    Expr // May be nil
}

type AccessExpr struct {
	Rec Expr
	Key Ident
}

type ListExpr struct {
	LBrack   Tok
	RBrack   Tok
	Elements []Expr
}

type WhereExpr struct {
	Expr Expr
	Id   Ident
	Typ  Expr // Optional type annotation.
	Val  Expr
}

type ImportExpr struct {
	Dollar Tok
	// Typically "sha256".
	HashAlgo string
	// Any literal, typically a byte-string.
	Value Literal
}

func (b Ident) expr()         {}
func (b Literal) expr()       {}
func (b BinaryExpr) expr()    {}
func (b FuncExpr) expr()      {}
func (b MatchFuncExpr) expr() {}
func (b CallExpr) expr()      {}
func (b VariantExpr) expr()   {}
func (b EnumExpr) expr()      {}
func (b RecordExpr) expr()    {}
func (b AccessExpr) expr()    {}
func (b ListExpr) expr()      {}
func (b WhereExpr) expr()     {}
func (b ImportExpr) expr()    {}

func (i *Ident) First() Tok        { return i.Tok }
func (i *Ident) Last() Tok         { return i.Tok }
func (i *Literal) First() Tok      { return i.Tok }
func (i *Literal) Last() Tok       { return i.Tok }
func (b *BinaryExpr) First() Tok   { return b.Left.First() }
func (b *BinaryExpr) Last() Tok    { return b.Right.Last() }
func (b *FuncExpr) First() Tok     { return b.Arg.First() }
func (b *FuncExpr) Last() Tok      { return b.Body.Last() }
func (b MatchFuncExpr) First() Tok { return b[0].Arg.First() }
func (b MatchFuncExpr) Last() Tok  { return b[len(b)-1].Body.Last() }
func (b *CallExpr) First() Tok     { return b.Fn.First() }
func (b *CallExpr) Last() Tok      { return b.Arg.Last() }

// The token before the tag is its '#'.
func (b *VariantExpr) First() Tok { return b.Tag.Tok - 1 }
func (b *VariantExpr) Last() Tok {
	if b.Typ != nil {
		return b.Typ.Last()
	}
	return b.Tag.Tok
}
func (b EnumExpr) First() Tok   { return b[0].First() }
func (b EnumExpr) Last() Tok    { return b[len(b)-1].Last() }
func (b RecordExpr) First() Tok { return b.LBrace }
func (b RecordExpr) Last() Tok  { return b.RBrace }
func (b AccessExpr) First() Tok { return b.Rec.First() }
func (b AccessExpr) Last() Tok  { return b.Key.Tok }
func (b ListExpr) First() Tok   { return b.LBrack }
func (b ListExpr) Last() Tok    { return b.RBrack }
func (b *WhereExpr) First() Tok { return b.Expr.First() }
func (b *WhereExpr) Last() Tok {
	switch {
	case b.Val != nil:
		return b.Val.Last()
	case b.Typ != nil:
		return b.Typ.Last()
	}
	return b.Id.Tok
}
func (b ImportExpr) First() Tok { return b.Dollar }
func (b ImportExpr) Last() Tok  { return b.Value.Tok }
