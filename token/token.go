package token

import "strconv"

type Token int

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
	PICK   // ::
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

	ARROW // ->
	PIPE  // |

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
	PICK:   "PICK",
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

	ARROW: "ARROW",
	PIPE:  "PIPE",
	LPIPE: "LPIPE",
	RPIPE: "RPIPE",

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
	PICK:   "::",
	OPTION: "#",
	ACCESS: ".",
	SPREAD: "..",

	ADD: "+",
	SUB: "-",
	MUL: "*",

	CONCAT:  "++",
	APPEND:  "+<",
	PREPEND: ">+",

	ARROW: "->",
	PIPE:  "|",
	LPIPE: "<|",
	RPIPE: "|>",

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
	if 0 <= tok && tok < Token(len(tokens)) {
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

func (op Token) Precedence() int {
	switch op {
	case WHERE:
		return WherePrec
	case PIPE:
		return 1
	case LPIPE, RPIPE:
		return 2
	case ARROW:
		return 3
	case LT, GT:
		return 4
	case ADD, SUB, CONCAT, APPEND, PREPEND:
		return 5
	case MUL:
		return 6
	case PICK, ACCESS, SPREAD:
		return 8
	}
	return BasePrec
}
