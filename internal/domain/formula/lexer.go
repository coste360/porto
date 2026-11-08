package formula

import (
	"github.com/coste360/porto/internal/qty"
)

// tokKind is the kind of a lexical token.
type tokKind uint8

const (
	tEOF tokKind = iota
	tNumber
	tString
	tIdent
	tLParen
	tRParen
	tComma
	tPlus
	tMinus
	tStar
	tSlash
	tLT
	tLE
	tGT
	tGE
	tEQ
	tNE
	tAnd
	tOr
	tNot
	tTrue
	tFalse
)

// token is one lexical token; pos is its byte offset in the source.
type token struct {
	kind tokKind
	pos  int
	text string    // identifier or string contents
	num  qty.Milli // number value
}

const (
	maxIdentLen  = 32
	maxStringLen = 32
)

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

func isIdentStart(c byte) bool { return (c >= 'a' && c <= 'z') || c == '_' }

func isIdentChar(c byte) bool { return isIdentStart(c) || isDigit(c) }

// isStringChar is the alphabet of string literals (enum values).
func isStringChar(c byte) bool { return (c >= 'a' && c <= 'z') || isDigit(c) || c == '_' }

// lex splits src into tokens; the last token is tEOF. Any unknown character, malformed number or string is a
// *SyntaxError at its offset.
func lex(src string) ([]token, error) {
	var out []token
	i := 0
	for i < len(src) {
		c := src[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			i++
			continue
		case isDigit(c):
			start := i
			for i < len(src) && isDigit(src[i]) {
				i++
			}
			if i < len(src) && src[i] == '.' {
				i++
				d := 0
				for i < len(src) && isDigit(src[i]) {
					i++
					d++
				}
				if d == 0 || d > 3 {
					return nil, &SyntaxError{Pos: start, Code: CodeSyntax}
				}
			}
			if i < len(src) && (isIdentChar(src[i]) || src[i] == '.') {
				return nil, &SyntaxError{Pos: start, Code: CodeSyntax}
			}
			v, err := qty.Parse(src[start:i])
			if err != nil {
				return nil, &SyntaxError{Pos: start, Code: CodeSyntax}
			}
			out = append(out, token{kind: tNumber, pos: start, num: v})
			continue
		case isIdentStart(c):
			start := i
			for i < len(src) && isIdentChar(src[i]) {
				i++
			}
			if i < len(src) && src[i] >= 'A' && src[i] <= 'Z' {
				return nil, &SyntaxError{Pos: start, Code: CodeSyntax}
			}
			word := src[start:i]
			if len(word) > maxIdentLen {
				return nil, &SyntaxError{Pos: start, Code: CodeSyntax}
			}
			k := tIdent
			switch word {
			case "and":
				k = tAnd
			case "or":
				k = tOr
			case "not":
				k = tNot
			case "true":
				k = tTrue
			case "false":
				k = tFalse
			}
			out = append(out, token{kind: k, pos: start, text: word})
			continue
		case c == '"':
			start := i
			i++
			for i < len(src) && src[i] != '"' {
				if !isStringChar(src[i]) {
					return nil, &SyntaxError{Pos: i, Code: CodeSyntax}
				}
				i++
			}
			if i >= len(src) || i-start-1 > maxStringLen {
				return nil, &SyntaxError{Pos: start, Code: CodeSyntax}
			}
			out = append(out, token{kind: tString, pos: start, text: src[start+1 : i]})
			i++
			continue
		}
		start := i
		two := ""
		if i+1 < len(src) {
			two = src[i : i+2]
		}
		var k tokKind
		n := 1
		switch {
		case two == "<=":
			k, n = tLE, 2
		case two == ">=":
			k, n = tGE, 2
		case two == "==":
			k, n = tEQ, 2
		case two == "!=":
			k, n = tNE, 2
		case c == '<':
			k = tLT
		case c == '>':
			k = tGT
		case c == '(':
			k = tLParen
		case c == ')':
			k = tRParen
		case c == ',':
			k = tComma
		case c == '+':
			k = tPlus
		case c == '-':
			k = tMinus
		case c == '*':
			k = tStar
		case c == '/':
			k = tSlash
		default:
			return nil, &SyntaxError{Pos: start, Code: CodeSyntax}
		}
		out = append(out, token{kind: k, pos: start})
		i += n
	}
	return append(out, token{kind: tEOF, pos: len(src)}), nil
}
