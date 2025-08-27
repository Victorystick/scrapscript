package ast

import (
	"github.com/Victorystick/scrapscript/token"
)

// A SourceExpr combines an expression with its source,
// which is necessary to extract identifiers and literals
// from it, as well as for error reporting.
type SourceExpr struct {
	Source token.Source
	Expr   Expr
}

type Node interface {
	Span() token.Span
}

type Expr interface {
	Node
	expr()
}

type Ident struct {
	Pos token.Span
	// Name string
}

type Literal struct {
	Pos  token.Span
	Kind token.Token
	// Value string
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
	Pos     token.Span
	Entries map[string]Expr
	Rest    Expr // May be nil
}

type AccessExpr struct {
	Pos token.Span
	Rec Expr
	Key Ident
}

type ListExpr struct {
	Pos      token.Span
	Elements []Expr
}

type WhereExpr struct {
	Expr Expr
	Id   Ident
	Typ  Expr // Optional type annotation.
	Val  Expr
}

type ImportExpr struct {
	Pos token.Span
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

func span(start, end Expr) token.Span {
	return token.Span{
		Start: start.Span().Start,
		End:   end.Span().End,
	}
}

func (i *Ident) Span() token.Span        { return i.Pos }
func (i *Literal) Span() token.Span      { return i.Pos }
func (b *BinaryExpr) Span() token.Span   { return span(b.Left, b.Right) }
func (b *FuncExpr) Span() token.Span     { return span(b.Arg, b.Body) }
func (b MatchFuncExpr) Span() token.Span { return span(b[0].Arg, b[len(b)-1].Body) }
func (b *CallExpr) Span() token.Span     { return span(b.Fn, b.Arg) }
func (b *VariantExpr) Span() token.Span {
	// Skip 1 char back for #.
	end := b.Tag.Span().End
	if b.Typ != nil {
		end = b.Typ.Span().End
	}
	return token.Span{Start: b.Tag.Span().Start - 1, End: end}
}
func (b EnumExpr) Span() token.Span   { return span(b[0], b[len(b)-1]) }
func (b RecordExpr) Span() token.Span { return b.Pos }
func (b AccessExpr) Span() token.Span { return b.Pos }
func (b ListExpr) Span() token.Span   { return b.Pos }
func (b *WhereExpr) Span() token.Span { return span(b.Expr, b.Val) }
func (b ImportExpr) Span() token.Span { return b.Pos }
