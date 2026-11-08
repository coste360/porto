package formula

import (
	"sort"

	"github.com/coste360/porto/internal/qty"
)

// op is the kind of an AST node.
type op uint8

const (
	opNum op = iota + 1
	opStr
	opBool
	opIdent
	opNeg
	opNot
	opAnd
	opOr
	opLT
	opLE
	opGT
	opGE
	opEQ
	opNE
	opAdd
	opSub
	opMul
	opDiv
	opCall
)

// node is an immutable AST node. name is the identifier, the function name or the string literal.
type node struct {
	op   op
	pos  int
	num  qty.Milli
	b    bool
	name string
	kids []*node
}

// parser is a recursive-descent parser over the token list (grammar of m10-plan §3.2).
type parser struct {
	toks   []token
	i      int
	nodes  int
	depth  int
	idents map[string]bool
	err    error
}

func (p *parser) peek() token { return p.toks[p.i] }

func (p *parser) next() token {
	t := p.toks[p.i]
	if t.kind != tEOF {
		p.i++
	}
	return t
}

func (p *parser) fail(pos int, code string) *node {
	if p.err == nil {
		p.err = &SyntaxError{Pos: pos, Code: code}
	}
	return nil
}

func (p *parser) mk(o op, pos int, kids ...*node) *node {
	p.nodes++
	if p.nodes > MaxNodes {
		return p.fail(pos, CodeTooComplex)
	}
	return &node{op: o, pos: pos, kids: kids}
}

// enter bounds the recursion depth (MaxDepth); leave must follow.
func (p *parser) enter(pos int) bool {
	p.depth++
	if p.depth > MaxDepth {
		p.fail(pos, CodeTooComplex)
		return false
	}
	return true
}

func (p *parser) leave() { p.depth-- }

// Parse parses src (at most MaxSourceBytes, MaxNodes, MaxDepth, MaxIdents) into a program. Errors are
// *SyntaxError.
func Parse(src string) (*Program, error) {
	if len(src) > MaxSourceBytes {
		return nil, &SyntaxError{Pos: MaxSourceBytes, Code: CodeTooComplex}
	}
	toks, err := lex(src)
	if err != nil {
		return nil, err
	}
	p := &parser{toks: toks, idents: map[string]bool{}}
	root := p.parseOr()
	if p.err == nil && p.peek().kind != tEOF {
		p.fail(p.peek().pos, CodeSyntax)
	}
	if p.err != nil {
		return nil, p.err
	}
	return newProgram(src, root), nil
}

func newProgram(src string, root *node) *Program {
	vars, tables := map[string]bool{}, map[string]bool{}
	collect(root, vars, tables)
	return &Program{src: src, root: root, idents: sortedKeys(vars), tables: sortedKeys(tables)}
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// collect gathers the variables and the band tables referenced by the program.
func collect(n *node, vars, tables map[string]bool) {
	if n == nil {
		return
	}
	switch n.op {
	case opIdent:
		vars[n.name] = true
		return
	case opCall:
		if n.name == fnBand && len(n.kids) == 2 && n.kids[1].op == opIdent {
			collect(n.kids[0], vars, tables)
			tables[n.kids[1].name] = true
			return
		}
	}
	for _, k := range n.kids {
		collect(k, vars, tables)
	}
}

func (p *parser) parseOr() *node {
	if !p.enter(p.peek().pos) {
		return nil
	}
	defer p.leave()
	left := p.parseAnd()
	for p.err == nil && p.peek().kind == tOr {
		t := p.next()
		right := p.parseAnd()
		left = p.mk(opOr, t.pos, left, right)
	}
	return left
}

func (p *parser) parseAnd() *node {
	left := p.parseNot()
	for p.err == nil && p.peek().kind == tAnd {
		t := p.next()
		right := p.parseNot()
		left = p.mk(opAnd, t.pos, left, right)
	}
	return left
}

func (p *parser) parseNot() *node {
	if p.peek().kind == tNot {
		t := p.next()
		if !p.enter(t.pos) {
			return nil
		}
		defer p.leave()
		x := p.parseNot()
		if p.err != nil {
			return nil
		}
		return p.mk(opNot, t.pos, x)
	}
	return p.parseCmp()
}

var cmpOps = map[tokKind]op{tLT: opLT, tLE: opLE, tGT: opGT, tGE: opGE, tEQ: opEQ, tNE: opNE}

func (p *parser) parseCmp() *node {
	left := p.parseSum()
	if p.err != nil {
		return nil
	}
	if o, ok := cmpOps[p.peek().kind]; ok {
		t := p.next()
		right := p.parseSum()
		if p.err != nil {
			return nil
		}
		if _, again := cmpOps[p.peek().kind]; again {
			return p.fail(p.peek().pos, CodeSyntax) // comparisons do not chain
		}
		return p.mk(o, t.pos, left, right)
	}
	return left
}

func (p *parser) parseSum() *node {
	left := p.parseProd()
	for p.err == nil && (p.peek().kind == tPlus || p.peek().kind == tMinus) {
		t := p.next()
		o := opAdd
		if t.kind == tMinus {
			o = opSub
		}
		right := p.parseProd()
		left = p.mk(o, t.pos, left, right)
	}
	return left
}

func (p *parser) parseProd() *node {
	left := p.parseUnary()
	for p.err == nil && (p.peek().kind == tStar || p.peek().kind == tSlash) {
		t := p.next()
		o := opMul
		if t.kind == tSlash {
			o = opDiv
		}
		right := p.parseUnary()
		left = p.mk(o, t.pos, left, right)
	}
	return left
}

func (p *parser) parseUnary() *node {
	if p.peek().kind == tMinus {
		t := p.next()
		if !p.enter(t.pos) {
			return nil
		}
		defer p.leave()
		x := p.parseUnary()
		if p.err != nil {
			return nil
		}
		return p.mk(opNeg, t.pos, x)
	}
	return p.parseAtom()
}

func (p *parser) ident(t token) bool {
	if !p.idents[t.text] {
		p.idents[t.text] = true
		if len(p.idents) > MaxIdents {
			p.fail(t.pos, CodeTooComplex)
			return false
		}
	}
	return true
}

func (p *parser) parseAtom() *node {
	if p.err != nil {
		return nil
	}
	t := p.next()
	switch t.kind {
	case tNumber:
		n := p.mk(opNum, t.pos)
		if n != nil {
			n.num = t.num
		}
		return n
	case tString:
		n := p.mk(opStr, t.pos)
		if n != nil {
			n.name = t.text
		}
		return n
	case tTrue, tFalse:
		n := p.mk(opBool, t.pos)
		if n != nil {
			n.b = t.kind == tTrue
		}
		return n
	case tLParen:
		x := p.parseOr()
		if p.err != nil {
			return nil
		}
		if p.peek().kind != tRParen {
			return p.fail(p.peek().pos, CodeSyntax)
		}
		p.next()
		return x
	case tIdent:
		if p.peek().kind == tLParen {
			return p.parseCall(t)
		}
		if !p.ident(t) {
			return nil
		}
		n := p.mk(opIdent, t.pos)
		if n != nil {
			n.name = t.text
		}
		return n
	}
	return p.fail(t.pos, CodeSyntax)
}

func (p *parser) parseCall(name token) *node {
	p.next() // (
	n := p.mk(opCall, name.pos)
	if n == nil {
		return nil
	}
	n.name = name.text
	if p.peek().kind == tRParen {
		p.next()
		return n
	}
	for {
		arg := p.parseOr()
		if p.err != nil {
			return nil
		}
		n.kids = append(n.kids, arg)
		switch p.peek().kind {
		case tComma:
			p.next()
			continue
		case tRParen:
			p.next()
			return n
		}
		return p.fail(p.peek().pos, CodeSyntax)
	}
}
