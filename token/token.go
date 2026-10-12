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
	HASH // $scheme:hex, a published scrap's hash
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
	HASH:  "HASH",

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

// Binary operator precedences, loosest first (SPEC.md section 3). Function
// application binds tighter than any of them.
var precedences = [end_operators]int8{
	RPIPE: 1, LPIPE: 1,
	RCOMP: 2, LCOMP: 2,
	LT: 3, GT: 3,
	PREPEND: 4, APPEND: 4,
	CONCAT: 5,
	ADD:    6, SUB: 6,
	MUL: 7,
}

// Precedence returns the precedence of a binary operator, or 0 for other
// tokens.
func (op Token) Precedence() int {
	if op < end_operators {
		return int(precedences[op])
	}
	return 0
}

// RightAssoc returns true for the right-associative binary operators.
func (op Token) RightAssoc() bool {
	return op == PREPEND || op == LPIPE
}

// Comparison returns true for the comparison operators, which don't chain.
func (op Token) Comparison() bool {
	return op == LT || op == GT
}
