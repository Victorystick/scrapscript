package token

import "strconv"

// Token is the kind of a token. It fits in a byte, so a scanned token costs
// five bytes: its kind and its start offset.
type Token uint8

const (
	BAD Token = iota
	EOF

	begin_literals
	IDENT
	INT
	FLOAT
	TEXT
	BYTE
	BYTES
	end_literals

	begin_operators
	HOLE // ()

	// Import
	IMPORT

	// Where clauses.

	ASSIGN // =
	WHERE  // ;
	COMMA  // ,

	// Relating to types.

	DEFINE // :
	OPTION // #
	ACCESS // .
	SPREAD // ..

	// Mathematic operators.

	ADD // +
	SUB // -
	MUL // *

	// List operations.

	CONCAT  // ++
	APPEND  // +<
	PREPEND // >+

	// Function definition.

	ARROW  // ->
	PIPE   // |
	FORALL // =>

	// Function application.

	LPIPE // <|
	RPIPE // |>

	// Function composition.

	RCOMP // >>
	LCOMP // <<

	LT // <
	GT // >

	LPAREN // (
	LBRACK // [
	LBRACE // {

	RPAREN // )
	RBRACK // ]
	RBRACE // }
	end_operators
)

var tokens = [...]string{
	BAD: "BAD",
	EOF: "EOF",

	HOLE:  "HOLE",
	IDENT: "IDENT",
	INT:   "INT",
	FLOAT: "FLOAT",
	TEXT:  "TEXT",
	BYTE:  "BYTE",
	BYTES: "BYTES",

	ASSIGN: "ASSIGN",
	WHERE:  "WHERE",
	COMMA:  "COMMA",

	IMPORT: "IMPORT",

	DEFINE: "DEFINE",
	OPTION: "OPTION",
	ACCESS: "ACCESS",
	SPREAD: "SPREAD",

	ADD: "ADD",
	SUB: "SUB",
	MUL: "MUL",

	CONCAT:  "CONCAT",
	APPEND:  "APPEND",
	PREPEND: "PREPEND",

	RCOMP: "RCOMP",
	LCOMP: "LCOMP",

	LT: "LT",
	GT: "GT",

	ARROW:  "ARROW",
	PIPE:   "PIPE",
	FORALL: "FORALL",
	LPIPE:  "LPIPE",
	RPIPE:  "RPIPE",

	LPAREN: "LPAREN",
	LBRACK: "LBRACK",
	LBRACE: "LBRACE",

	RPAREN: "RPAREN",
	RBRACK: "RBRACK",
	RBRACE: "RBRACE",
}

var operators = [...]string{
	HOLE:   "()",
	ASSIGN: "=",
	WHERE:  ";",
	COMMA:  ",",

	IMPORT: "$",

	DEFINE: ":",
	OPTION: "#",
	ACCESS: ".",
	SPREAD: "..",

	ADD: "+",
	SUB: "-",
	MUL: "*",

	CONCAT:  "++",
	APPEND:  "+<",
	PREPEND: ">+",

	ARROW:  "->",
	PIPE:   "|",
	FORALL: "=>",
	LPIPE:  "<|",
	RPIPE:  "|>",

	RCOMP: ">>",
	LCOMP: "<<",

	LT: "<",
	GT: ">",

	LPAREN: "(",
	LBRACK: "[",
	LBRACE: "{",

	RPAREN: ")",
	RBRACK: "]",
	RBRACE: "}",
}

func (tok Token) IsLiteral() bool {
	return begin_literals < tok && tok < end_literals
}

func (tok Token) IsOperator() bool {
	return begin_operators < tok && tok < end_operators
}

func (tok Token) Op() string {
	if tok.IsOperator() {
		return operators[tok]
	}
	return ""
}

func (tok Token) String() string {
	s := ""
	if int(tok) < len(tokens) {
		s = tokens[tok]
	}
	if s == "" {
		s = "token(" + strconv.Itoa(int(tok)) + ")"
	}
	return s
}

const (
	WherePrec = 0
	BasePrec  = 1
	CallPrec  = 7
)

var precedences = func() (p [end_operators]int8) {
	for i := range p {
		p[i] = BasePrec
	}
	p[WHERE] = WherePrec
	p[PIPE] = 1
	p[LPIPE], p[RPIPE] = 2, 2
	p[ARROW] = 3
	p[LT], p[GT] = 4, 4
	p[ADD], p[SUB], p[CONCAT], p[APPEND], p[PREPEND] = 5, 5, 5, 5, 5
	p[MUL] = 6
	p[ACCESS], p[SPREAD] = 8, 8
	return
}()

func (op Token) Precedence() int {
	if op < end_operators {
		return int(precedences[op])
	}
	return BasePrec
}
