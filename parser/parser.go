package parser

import (
	"fmt"
	"os"

	"github.com/Victorystick/scrapscript/ast"
	"github.com/Victorystick/scrapscript/scanner"
	"github.com/Victorystick/scrapscript/token"
)

type parser struct {
	source *token.Source
	src    []byte
	tokens scanner.Tokens

	i   int         // index of the current token
	tok token.Token // kind of the current token

	// The tree being built; see ast.SourceExpr.
	kinds []ast.Kind
	mains []ast.Tok
	data  []ast.Data
	extra []ast.Node

	// scratch collects the children of the lists being parsed, innermost
	// last, until each list is complete and moved to extra.
	scratch []ast.Node

	errors scanner.Errors
}

var debug = true
var stack []string

func (p *parser) next() {
	// The last token is EOF; stay there.
	if p.i < len(p.tokens.Kinds)-1 {
		p.i++
	}
	p.tok = p.tokens.Kinds[p.i]
}

// peek returns the kind of the token after the current one.
func (p *parser) peek() token.Token {
	return p.tokens.Kinds[min(p.i+1, len(p.tokens.Kinds)-1)]
}

// span returns the current token's span. Token ends are not stored, so only
// call this where the end is needed.
func (p *parser) span() token.Span {
	return p.tokens.Span(p.src, p.i)
}

// at returns the index of the current token.
func (p *parser) at() ast.Tok {
	return ast.Tok(p.i)
}

func (p *parser) add(kind ast.Kind, main ast.Tok, lhs, rhs uint32) ast.Node {
	p.kinds = append(p.kinds, kind)
	p.mains = append(p.mains, main)
	p.data = append(p.data, ast.Data{Lhs: lhs, Rhs: rhs})
	return ast.Node(len(p.kinds) - 1)
}

// addExtra appends ns to extra and returns the index of the first.
func (p *parser) addExtra(ns ...ast.Node) uint32 {
	i := len(p.extra)
	p.extra = append(p.extra, ns...)
	return uint32(i)
}

// flush moves the scratch children from start onwards to extra, and returns
// their range there.
func (p *parser) flush(start int) (lhs, rhs uint32) {
	lhs = p.addExtra(p.scratch[start:]...)
	p.scratch = p.scratch[:start]
	return lhs, uint32(len(p.extra))
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
	panic(p.source.Error(p.span(), msg))
}

func ParseExpr(source string) (ast.SourceExpr, error) {
	src := token.NewSource([]byte(source))
	return Parse(&src)
}

func Parse(source *token.Source) (se ast.SourceExpr, err error) {
	var p parser

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
	p.src = source.Bytes()
	var errs []scanner.Error
	p.tokens, errs = scanner.Scan(p.src)
	for _, e := range errs {
		p.errors.Add(source.Error(e.Span(), e.Code.String()))
	}
	p.tok = p.tokens.Kinds[0]

	// Reserve for about 3/4 node and 1/3 extra entry per token, so that the
	// arrays rarely regrow.
	n := len(p.tokens.Kinds)*3/4 + 1
	p.kinds = make([]ast.Kind, 0, n)
	p.mains = make([]ast.Tok, 0, n)
	p.data = make([]ast.Data, 0, n)
	p.extra = make([]ast.Node, 0, len(p.tokens.Kinds)/3)
	p.add(ast.Invalid, 0, 0, 0) // ast.None

	expr := p.parseExpr()
	if debug && p.tok != token.EOF {
		fmt.Fprintf(os.Stderr, "%#v\n", expr)
		// printer.Fprint(os.Stderr, p.source, expr)
	}
	p.expect(token.EOF)

	se = ast.SourceExpr{
		Source: *p.source,
		Tokens: p.tokens,
		Kinds:  p.kinds,
		Mains:  p.mains,
		Data:   p.data,
		Extra:  p.extra,
		Expr:   expr,
	}
	return
}

func (p *parser) parseExpr() ast.Node {
	if debug {
		stack = append(stack, "parseExpr")
		defer func() { stack = stack[:len(stack)-1] }()
	}
	expr := p.parsePlainExpr(token.BasePrec)

	for p.tok == token.WHERE {
		p.next()
		expr = p.parseWhereExpr(expr)
	}

	return expr
}

func (p *parser) parsePlainExpr(prec int) ast.Node {
	if debug {
		stack = append(stack, "parsePlainExpr")
		defer func() { stack = stack[:len(stack)-1] }()
	}
	left := p.parseBinaryExpr(ast.None, prec)

	for {
		if p.tok == token.LPAREN {
			right := p.parseParenExpr()
			left = p.add(ast.Call, 0, uint32(left), uint32(right))
		} else if p.tok.IsOperator() && p.tok.Precedence() > prec {
			op := p.tok
			val := p.parseBinaryExpr(left, op.Precedence())
			if left == val {
				// Break if parse binary can't parse more.
				break
			}
			left = val
		} else if p.tok == token.OPTION && token.CallPrec > prec {
			// A tag in argument position takes no payload.
			right := p.parseTag()
			left = p.add(ast.Call, 0, uint32(left), uint32(right))
		} else if p.tok != token.EOF && (!p.tok.IsOperator() || startsSimpleValue(p.tok)) && token.CallPrec > prec {
			right := p.parseBinaryExpr(ast.None, token.CallPrec-1)
			left = p.add(ast.Call, 0, uint32(left), uint32(right))
		} else {
			break
		}
	}

	return left
}

func (p *parser) parseParenExpr() ast.Node {
	p.next()
	x := p.parseExpr()
	p.expect(token.RPAREN)
	p.next()
	return x
}

// identTok consumes an identifier and returns its token.
func (p *parser) identTok() ast.Tok {
	p.expect(token.IDENT)
	t := p.at()
	p.next()
	return t
}

func (p *parser) ident() ast.Node {
	return p.add(ast.Ident, p.identTok(), 0, 0)
}

// Returns true if `tok` is the start of a simple value.
func startsSimpleValue(tok token.Token) bool {
	switch tok {
	case token.IDENT,
		token.INT, token.FLOAT, token.HOLE,
		token.TEXT, token.BYTE, token.BYTES, token.IMPORT,
		// Or a record, list or parens expression.
		token.LBRACE, token.LBRACK, token.LPAREN:
		return true
	}
	return false
}

func (p *parser) parseUnaryExpr() ast.Node {
	if debug {
		stack = append(stack, "parseUnaryExpr")
		defer func() { stack = stack[:len(stack)-1] }()
	}
	switch p.tok {
	case token.IDENT:
		return p.ident()
	case token.INT, token.FLOAT, token.HOLE,
		token.TEXT, token.BYTE, token.BYTES:
		lit := p.add(ast.Literal, p.at(), 0, 0)
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

	case token.OPTION:
		return p.parseVariantExpr()

	case token.IMPORT:
		return p.parseImport()
	}

	p.unexpected()
	return ast.None
}

func (p *parser) parseBinaryExpr(x ast.Node, prec int) ast.Node {
	if debug {
		stack = append(stack, "parseBinaryExpr")
		defer func() { stack = stack[:len(stack)-1] }()
	}
	if x == ast.None {
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
		op, at := p.tok, p.at()
		p.next()
		right := p.parsePlainExpr(op.Precedence())
		return p.add(ast.Binary, at, uint32(x), uint32(right))

	case token.ACCESS:
		p.next()
		return p.add(ast.Access, p.identTok(), uint32(x), 0)

	case token.ARROW:
		at := p.at()
		p.next()
		return p.parseFuncExpr(x, at)
	}

	return x
}

func (p *parser) parseWhereExpr(x ast.Node) ast.Node {
	if debug {
		stack = append(stack, "parseWhereExpr")
		defer func() { stack = stack[:len(stack)-1] }()
	}

	id := p.identTok()
	typ := ast.None
	if p.tok == token.DEFINE {
		p.next()
		typ = p.parseType()
	}

	p.expect(token.ASSIGN)
	p.next()

	// A sort binding binds a type. Its annotation is a kind, or it has none
	// and its type starts with a type variable.
	sort := p.isKind(typ) || p.tok == token.IDENT && p.peek() == token.FORALL
	if sort {
		return p.add(ast.Where, id, uint32(x), p.addExtra(typ, p.parseType(), 1))
	}
	val := p.parsePlainExpr(token.BasePrec)
	return p.add(ast.Where, id, uint32(x), p.addExtra(typ, val, 0))
}

// isKind returns true if the annotation t is a kind: a type whose final
// result is `type`, such as `type` or `type -> type`.
func (p *parser) isKind(t ast.Node) bool {
	for p.kinds[t] == ast.Func {
		t = ast.Node(p.data[t].Rhs)
	}
	return p.kinds[t] == ast.Ident && p.source.GetString(p.tokens.Span(p.src, int(p.mains[t]))) == "type"
}

// parseType parses a type, or a kind:
//
//	type  = NAME "=>" type | arrow
//	arrow = tapp [ "->" arrow ]
func (p *parser) parseType() ast.Node {
	if debug {
		stack = append(stack, "parseType")
		defer func() { stack = stack[:len(stack)-1] }()
	}
	if p.tok == token.IDENT && p.peek() == token.FORALL {
		param := p.identTok()
		p.next()
		return p.add(ast.Forall, param, uint32(p.parseType()), 0)
	}
	return p.parseArrowType()
}

func (p *parser) parseArrowType() ast.Node {
	t := p.parseTypeApp()
	if p.tok != token.ARROW {
		return t
	}
	arrow := p.at()
	p.next()
	return p.add(ast.Func, arrow, uint32(t), uint32(p.parseArrowType()))
}

// parseTypeApp parses a type application, which is left-associative:
//
//	tapp = tatom { tatom }
func (p *parser) parseTypeApp() ast.Node {
	t := p.parseTypeAtom()
	for startsTypeAtom(p.tok) {
		t = p.add(ast.Call, 0, uint32(t), uint32(p.parseTypeAtom()))
	}
	return t
}

// Returns true if tok is the start of a type atom.
func startsTypeAtom(tok token.Token) bool {
	switch tok {
	case token.IDENT, token.HOLE, token.LPAREN, token.LBRACE, token.LBRACK, token.IMPORT:
		return true
	}
	return false
}

// parseTypeAtom parses a type name, (), a parenthesised type, a record or
// enum type, or a hash reference to a type scrap.
func (p *parser) parseTypeAtom() ast.Node {
	switch p.tok {
	case token.IDENT:
		return p.ident()
	case token.HOLE:
		lit := p.add(ast.Literal, p.at(), 0, 0)
		p.next()
		return lit
	case token.LPAREN:
		p.next()
		t := p.parseType()
		p.expect(token.RPAREN)
		p.next()
		return t
	case token.LBRACE:
		start := p.at()
		tail, info := p.parseRow(token.RBRACE, 2, func() {
			key := p.identTok()
			p.expect(token.DEFINE)
			p.next()
			p.scratch = append(p.scratch, ast.Node(key), p.parseType())
		})
		return p.add(ast.RecordType, start, uint32(tail), info)
	case token.LBRACK:
		start := p.at()
		tail, info := p.parseRow(token.RBRACK, 1, func() {
			tag := p.tagTok()
			payload := ast.None
			if startsTypeAtom(p.tok) {
				payload = p.parseTypeAtom()
			}
			p.scratch = append(p.scratch, p.add(ast.Variant, tag, uint32(payload), 0))
		})
		return p.add(ast.Enum, start, uint32(tail), info)
	case token.IMPORT:
		return p.parseImport()
	}

	p.unexpected()
	return ast.None
}

// parseRow parses the comma-separated labels of a record or enum type, from
// its opening bracket to close, and an optional `..NAME` tail last. Each
// label adds width entries to scratch. It returns the tail and the index in
// Extra of: the closing token, the label count, then the labels' entries.
func (p *parser) parseRow(close token.Token, width int, label func()) (tail ast.Node, info uint32) {
	p.next()
	top := len(p.scratch)
	for p.tok != close {
		if p.tok == token.SPREAD {
			p.next()
			tail = p.ident()
			break
		}
		label()
		if p.tok != token.COMMA {
			break
		}
		p.next()
		if p.tok == close {
			p.bail("A type cannot end in a trailing comma.")
		}
	}

	p.expect(close)
	end := p.at()
	p.next()

	count := (len(p.scratch) - top) / width
	info = p.addExtra(ast.Node(end), ast.Node(count))
	p.flush(top)
	return tail, info
}

func (p *parser) parseRecord() ast.Node {
	p.expect(token.LBRACE)
	start := p.at()
	p.next()

	// Do we start with a spread?
	rest := ast.None
	if p.tok == token.SPREAD {
		p.next()
		rest = p.parseExpr()
		p.expect(token.COMMA)
		p.next()
	}

	// Fields are collected as key token, value node pairs.
	top := len(p.scratch)
	for {
		if p.tok == token.RBRACE {
			break
		}

		// Is this a misplaced spread? Let's tell the user.
		if p.tok == token.SPREAD {
			p.bail("A spread must be first in a record.")
		}

		key := p.identTok()

		p.expect(token.ASSIGN)
		p.next()

		x := p.parseExpr()

		p.scratch = append(p.scratch, ast.Node(key), x)

		if p.tok != token.COMMA {
			break
		}
		p.next()
	}

	p.expect(token.RBRACE)
	end := p.at()
	p.next()

	count := (len(p.scratch) - top) / 2
	info := p.addExtra(ast.Node(end), ast.Node(count))
	p.flush(top)
	return p.add(ast.Record, start, uint32(rest), info)
}

func (p *parser) parseList() ast.Node {
	if debug {
		stack = append(stack, "parseList")
		defer func() { stack = stack[:len(stack)-1] }()
	}
	p.expect(token.LBRACK)
	start := p.at()
	p.next()

	top := len(p.scratch)
	for {
		if p.tok == token.RBRACK {
			break
		}
		p.scratch = append(p.scratch, p.parseExpr())

		if p.tok != token.COMMA {
			break
		}
		p.next()
	}

	p.expect(token.RBRACK)
	p.scratch = append(p.scratch, ast.Node(p.at()))
	p.next()

	lhs, rhs := p.flush(top)
	return p.add(ast.List, start, lhs, rhs)
}

// parseFuncExpr parses the body of a function whose parameter x and arrow
// have been parsed.
func (p *parser) parseFuncExpr(x ast.Node, arrow ast.Tok) ast.Node {
	if debug {
		stack = append(stack, "parseFuncExpr")
		defer func() { stack = stack[:len(stack)-1] }()
	}
	body := p.parsePlainExpr(token.ARROW.Precedence())
	return p.add(ast.Func, arrow, uint32(x), uint32(body))
}

func (p *parser) parseMatchFuncExpr() ast.Node {
	if debug {
		stack = append(stack, "parseMatchFuncExpr")
		defer func() { stack = stack[:len(stack)-1] }()
	}
	top := len(p.scratch)

	for p.tok == token.PIPE {
		p.next()

		arg := p.parseBinaryExpr(ast.None, token.ARROW.Precedence()+1)
		p.expect(token.ARROW)
		arrow := p.at()
		p.next()

		p.scratch = append(p.scratch, p.parseFuncExpr(arg, arrow))
	}

	lhs, rhs := p.flush(top)
	return p.add(ast.MatchFunc, 0, lhs, rhs)
}

// tagTok consumes a '#' and the tag after it, and returns the tag's token.
func (p *parser) tagTok() ast.Tok {
	p.expect(token.OPTION)
	p.next()

	if p.tok != token.INT {
		p.expect(token.IDENT)
	}
	tag := p.at()
	p.next()
	return tag
}

// parseTag parses a tag without a payload.
func (p *parser) parseTag() ast.Node {
	return p.add(ast.Variant, p.tagTok(), 0, 0)
}

// parseVariantExpr parses a tag value, which takes the next atom as its
// payload, if there is one. A tag payload is itself nullary.
func (p *parser) parseVariantExpr() ast.Node {
	if debug {
		stack = append(stack, "parseVariantExpr")
		defer func() { stack = stack[:len(stack)-1] }()
	}
	tag := p.tagTok()
	payload := ast.None

	if p.tok == token.OPTION {
		payload = p.parseTag()
	} else if startsSimpleValue(p.tok) {
		payload = p.parseBinaryExpr(ast.None, token.CallPrec-1)
	}

	return p.add(ast.Variant, tag, uint32(payload), 0)
}

func (p *parser) parseImport() ast.Node {
	start := p.at()

	// Eat $.
	p.next()

	// A path.
	if p.tok == token.TEXT {
		p.next()
		return p.add(ast.Import, start, 0, 0)
	}

	// Hash algo.
	p.expect(token.IDENT)
	p.next()

	p.expect(token.BYTES)
	p.next()

	return p.add(ast.Import, start, 0, 0)
}
