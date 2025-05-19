package parser

import (
	"fmt"
	"os"

	"github.com/Victorystick/scrapscript/ast"
	"github.com/Victorystick/scrapscript/scanner"
	"github.com/Victorystick/scrapscript/token"
)

type parser struct {
	source  *token.Source
	scanner scanner.Scanner

	tok  token.Token
	span token.Span

	errors scanner.Errors
}

var debug = true
var stack []string

func (p *parser) next() {
	p.tok, p.span = p.scanner.Scan()
}

func (p *parser) expect(tok token.Token) {
	if p.tok != tok {
		p.bail(fmt.Sprint("Expected ", tok, " got ", p.tok))
	}
}

func (p *parser) unexpected() {
	p.bail(fmt.Sprint("Unexpected ", p.tok))
}

func (p *parser) bail(msg string) {
	if debug {
		fmt.Fprintln(os.Stderr, stack)
	}
	panic(p.source.Error(p.span, msg))
}

func ParseExpr(source string) (ast.SourceExpr, error) {
	src := token.NewSource([]byte(source))
	return Parse(&src)
}

func Parse(source *token.Source) (se ast.SourceExpr, err error) {
	var p parser

	eh := func(e token.Error) {
		p.errors.Add(e)
	}

	defer func() {
		if pnc := recover(); pnc != nil {
			// resume same panic if it's not a token.Error.
			e, ok := pnc.(token.Error)
			if !ok {
				panic(e)
			} else if e.Msg != "" {
				p.errors.Add(e)
			}
		}
		err = p.errors.Err()
	}()

	p.source = source
	p.scanner.Init(p.source, eh)

	p.next()
	expr := p.parseExpr()
	if debug && p.tok != token.EOF {
		fmt.Fprintf(os.Stderr, "%#v\n", expr)
		// printer.Fprint(os.Stderr, p.source, expr)
	}
	p.expect(token.EOF)

	se = ast.SourceExpr{Source: *p.source, Expr: expr}
	return
}

func (p *parser) parseExpr() ast.Expr {
	if debug {
		stack = append(stack, "parseExpr")
		defer func() { stack = stack[:len(stack)-1] }()
	}
	expr := p.parsePlainExpr(token.BasePrec)
	i := 0

	for p.tok == token.WHERE {
		i += 1
		p.next()
		expr = p.parseWhereExpr(expr)
	}

	return expr
}

func (p *parser) parsePlainExpr(prec int) ast.Expr {
	if debug {
		stack = append(stack, "parsePlainExpr")
		defer func() { stack = stack[:len(stack)-1] }()
	}
	left := p.parseBinaryExpr(nil, prec)

	for {
		if p.tok == token.LPAREN {
			right := p.parseParenExpr()
			left = &ast.CallExpr{
				Fn:  left,
				Arg: right,
			}
		} else if p.tok.IsOperator() && p.tok.Precedence() > prec {
			op := p.tok
			left = p.parseBinaryExpr(left, op.Precedence())
		} else if p.tok != token.EOF && !p.tok.IsOperator() && token.CallPrec > prec {
			left = &ast.CallExpr{
				Fn:  left,
				Arg: p.parseBinaryExpr(nil, token.CallPrec-1),
			}
		} else {
			break
		}
	}

	return left
}

func (p *parser) parseParenExpr() ast.Expr {
	p.next()
	x := p.parseExpr()
	p.expect(token.RPAREN)
	p.next()
	return x
}

func (p *parser) ident() *ast.Ident {
	ident := &ast.Ident{
		Pos: p.span,
	}
	p.next()
	return ident
}

// Parses an identifier as a string.
func (p *parser) name() string {
	p.expect(token.IDENT)
	name := p.source.GetString(p.span)
	p.next()
	return name
}

func (p *parser) parseUnaryExpr() ast.Expr {
	if debug {
		stack = append(stack, "parseUnaryExpr")
		defer func() { stack = stack[:len(stack)-1] }()
	}
	switch p.tok {
	case token.IDENT:
		return p.ident()
	case token.INT, token.FLOAT,
		token.TEXT, token.BYTE, token.BYTES:
		lit := &ast.Literal{
			Pos:  p.span,
			Kind: p.tok,
		}
		p.next()
		return lit

	case token.LBRACE:
		return p.parseRecord()

	case token.LBRACK:
		return p.parseList()

	case token.LPAREN:
		return p.parseParenExpr()

	case token.PIPE:
		return p.parseMatchFuncExpr()
	}

	p.unexpected()
	return nil
}

func (p *parser) parseBinaryExpr(x ast.Expr, prec int) ast.Expr {
	if debug {
		stack = append(stack, "parseBinaryExpr")
		defer func() { stack = stack[:len(stack)-1] }()
	}
	if x == nil {
		x = p.parseUnaryExpr()
	}

	if p.tok.IsOperator() && p.tok.Precedence() < prec {
		return x
	}

	switch p.tok {
	case token.ADD, token.SUB, token.MUL,
		token.LT, token.GT,
		token.RPIPE, token.LPIPE,
		token.RCOMP, token.LCOMP,
		token.CONCAT, token.APPEND, token.PREPEND:
		op := p.tok
		p.next()
		return &ast.BinaryExpr{
			Left:  x,
			Op:    op,
			Right: p.parsePlainExpr(op.Precedence()),
		}

	case token.PICK:
		op := p.tok
		p.next()
		return &ast.BinaryExpr{
			Left:  x,
			Op:    op,
			Right: p.ident(),
		}

	case token.ARROW:
		p.next()
		return p.parseFuncExpr(x)
	}

	return x
}

func (p *parser) parseWhereExpr(x ast.Expr) ast.Expr {
	if debug {
		stack = append(stack, "parseWhereExpr")
		defer func() { stack = stack[:len(stack)-1] }()
	}
	p.expect(token.IDENT)
	id := ast.Ident{Pos: p.span}
	p.next()

	if p.tok == token.DEFINE {
		p.next()
		return &ast.WhereExpr{
			Expr: x,
			Id:   id,
			Val:  p.parseType(),
		}
	}

	p.expect(token.ASSIGN)
	p.next()

	if p.tok == token.PIPE {
		return &ast.WhereExpr{
			Expr: x,
			Id:   id,
			Val:  p.parseMatchFuncExpr(),
		}
	}

	return &ast.WhereExpr{
		Expr: x,
		Id:   id,
		Val:  p.parseBinaryExpr(nil, token.BasePrec),
	}
}

func (p *parser) parseRecord() *ast.RecordExpr {
	p.expect(token.LBRACE)
	start := p.span.Start
	p.next()

	entries := make(map[string]ast.Expr)
	for {
		if p.tok == token.RBRACE {
			break
		}
		name := p.name()

		p.expect(token.ASSIGN)
		p.next()

		x := p.parseExpr()

		entries[name] = x

		if p.tok != token.COMMA {
			break
		}
		p.next()
	}

	p.expect(token.RBRACE)
	end := p.span.End
	p.next()

	return &ast.RecordExpr{Pos: token.Span{Start: start, End: end}, Entries: entries}
}

func (p *parser) parseList() *ast.ListExpr {
	if debug {
		stack = append(stack, "parseList")
		defer func() { stack = stack[:len(stack)-1] }()
	}
	p.expect(token.LBRACK)
	start := p.span.Start
	p.next()

	es := make([]ast.Expr, 0)
	for {
		if p.tok == token.RBRACK {
			break
		}
		es = append(es, p.parseExpr())

		if p.tok != token.COMMA {
			break
		}
		p.next()
	}

	p.expect(token.RBRACK)
	end := p.span.End
	p.next()

	return &ast.ListExpr{Pos: token.Span{Start: start, End: end}, Elements: es}
}

func (p *parser) parseFuncExpr(x ast.Expr) *ast.FuncExpr {
	if debug {
		stack = append(stack, "parseFuncExpr")
		defer func() { stack = stack[:len(stack)-1] }()
	}
	return &ast.FuncExpr{
		Arg:  x,
		Body: p.parsePlainExpr(token.ARROW.Precedence()),
	}
}

func (p *parser) parseMatchFuncExpr() ast.Expr {
	if debug {
		stack = append(stack, "parseMatchFuncExpr")
		defer func() { stack = stack[:len(stack)-1] }()
	}
	// We guess there'll be about 2 branches.
	exprs := make(ast.MatchFuncExpr, 0, 2)

	for p.tok == token.PIPE {
		p.next()

		var arg ast.Expr
		if p.tok == token.OPTION {
			arg = p.parseVariant()
		} else {
			arg = p.parseBinaryExpr(nil, token.ARROW.Precedence()+1)
		}
		p.expect(token.ARROW)
		p.next()

		expr := p.parseFuncExpr(arg)
		exprs = append(exprs, expr)
	}

	return exprs
}

func (p *parser) parseType() ast.TypeExpr {
	if debug {
		stack = append(stack, "parseType")
		defer func() { stack = stack[:len(stack)-1] }()
	}
	// We guess there'll be about 2 branches.
	exprs := make(ast.TypeExpr, 0, 2)

	for p.tok == token.OPTION {
		variant := p.parseVariant()
		exprs = append(exprs, variant)
	}

	return exprs
}

func (p *parser) parseVariant() *ast.VariantExpr {
	if debug {
		stack = append(stack, "parseVariant")
		defer func() { stack = stack[:len(stack)-1] }()
	}
	// Eat option.
	p.next()

	p.expect(token.IDENT)
	id := ast.Ident{
		Pos: p.span,
	}
	p.next()

	if p.tok == token.ARROW {
		return &ast.VariantExpr{
			Tag: id,
		}
	}

	var val ast.Expr
	if p.tok != token.OPTION && p.tok != token.EOF {
		val = p.parseBinaryExpr(nil, token.ARROW.Precedence()+1)
	}

	return &ast.VariantExpr{
		Tag: id,
		Val: val,
	}
}
