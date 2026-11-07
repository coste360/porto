package tmpl

import (
	"fmt"
	"strconv"
	"strings"
)

// Issue codes (details of 422 template_invalid).
const (
	CodeSyntax           = "syntax"
	CodeUnclosedBlock    = "unclosed_block"
	CodeUnexpectedClose  = "unexpected_close"
	CodeUnknownPath      = "unknown_path"
	CodeUnknownFilter    = "filter_unknown"
	CodeFilterType       = "filter_type"
	CodeFilterArg        = "filter_arg"
	CodeEachNotList      = "each_not_list"
	CodeNestingTooDeep   = "nesting_too_deep"
	CodeNotScalar        = "not_scalar"
	CodeCompareType      = "compare_type"
	CodeDirectiveContext = "directive_context"
	CodeUnknownRole      = "unknown_role"
	CodeThisOutsideEach  = "this_outside_each"
)

// MaxEachDepth is the deepest nesting of {{#each}} blocks.
const MaxEachDepth = 4

// Issue is one error of a template (1-based line and column of the tag).
type Issue struct {
	Line int    `json:"line"`
	Col  int    `json:"col"`
	Code string `json:"code"`
	Path string `json:"path,omitempty"`
}

// Error is a template that does not parse or validate.
type Error struct {
	Issues []Issue
}

func (e *Error) Error() string {
	if len(e.Issues) == 0 {
		return "tmpl: invalid template"
	}
	i := e.Issues[0]
	return fmt.Sprintf("tmpl: %d issue(s), first %s at %d:%d", len(e.Issues), i.Code, i.Line, i.Col)
}

type pos struct{ line, col int }

type nodeKind int

const (
	nText nodeKind = iota
	nVar
	nEach
	nIf
	nDirective
)

// filter is one "| name arg" of a variable.
type filter struct {
	name string
	arg  string
	has  bool
}

// cond is the expression of {{#if}}.
type cond struct {
	path   string
	negate bool
	op     string // "", "==", "!=", ">", "<"
	str    string
	num    int64
}

type node struct {
	kind    nodeKind
	pos     pos
	text    string // nText
	path    string // nVar, nEach
	filters []filter
	cond    cond
	body    []*node
	els     []*node
	name    string // nDirective: signature, pagebreak, logo, page, pages
	arg     string
}

// Template is a parsed template.
type Template struct {
	nodes []*node
}

type tag struct {
	pos  pos
	body string
}

// Parse parses src. Syntax errors are reported all at once as *Error.
func Parse(src string) (*Template, error) {
	p := &parser{src: src, line: 1, col: 1}
	nodes := p.parseNodes(0, "")
	if len(p.issues) > 0 {
		return nil, &Error{Issues: p.issues}
	}
	return &Template{nodes: nodes}, nil
}

type parser struct {
	src       string
	i         int
	line, col int
	issues    []Issue
	closer    string // the closing tag that ended the last parseNodes ("/each", "/if", "else")
	closerPos pos
}

func (p *parser) issue(at pos, code, path string) {
	p.issues = append(p.issues, Issue{Line: at.line, Col: at.col, Code: code, Path: path})
}

func (p *parser) advance(n int) {
	for range n {
		if p.i >= len(p.src) {
			return
		}
		if p.src[p.i] == '\n' {
			p.line++
			p.col = 1
		} else if p.src[p.i]&0xC0 != 0x80 {
			p.col++
		}
		p.i++
	}
}

// next returns the next text run (with \{ kept as written: it is a Markdown escape) or the next tag.
func (p *parser) next() (text string, t *tag, eof bool) {
	if p.i >= len(p.src) {
		return "", nil, true
	}
	start := p.i
	for p.i < len(p.src) {
		if p.src[p.i] == '\\' && p.i+1 < len(p.src) {
			p.advance(2)
			continue
		}
		if strings.HasPrefix(p.src[p.i:], "{{") {
			break
		}
		p.advance(1)
	}
	if p.i > start {
		return p.src[start:p.i], nil, false
	}
	at := pos{p.line, p.col}
	end := strings.Index(p.src[p.i+2:], "}}")
	if end < 0 {
		p.issue(at, CodeSyntax, "")
		p.advance(len(p.src) - p.i)
		return "", nil, true
	}
	body := p.src[p.i+2 : p.i+2+end]
	p.advance(end + 4)
	return "", &tag{pos: at, body: strings.TrimSpace(body)}, false
}

// parseNodes reads nodes until EOF or a closing tag of depth > 0.
func (p *parser) parseNodes(depth int, open string) []*node {
	var out []*node
	for {
		text, t, eof := p.next()
		if eof {
			if depth > 0 {
				p.closer = ""
			}
			return out
		}
		if t == nil {
			out = append(out, &node{kind: nText, text: text})
			continue
		}
		b := t.body
		switch {
		case strings.HasPrefix(b, "#each"):
			path := strings.TrimSpace(strings.TrimPrefix(b, "#each"))
			if !validPath(path) {
				p.issue(t.pos, CodeSyntax, path)
			}
			n := &node{kind: nEach, pos: t.pos, path: path}
			n.body = p.parseNodes(depth+1, "each")
			if p.closer != "/each" {
				p.issue(t.pos, CodeUnclosedBlock, path)
				if p.closer == "" {
					return append(out, n)
				}
			}
			out = append(out, n)
		case strings.HasPrefix(b, "#if"):
			c, ok := parseCond(strings.TrimSpace(strings.TrimPrefix(b, "#if")))
			if !ok {
				p.issue(t.pos, CodeSyntax, "")
			}
			n := &node{kind: nIf, pos: t.pos, cond: c}
			n.body = p.parseNodes(depth+1, "if")
			if p.closer == "else" {
				n.els = p.parseNodes(depth+1, "else")
			}
			if p.closer != "/if" {
				p.issue(t.pos, CodeUnclosedBlock, c.path)
				if p.closer == "" {
					return append(out, n)
				}
			}
			out = append(out, n)
		case b == "/each" || b == "/if" || b == "else":
			valid := (b == "/each" && open == "each") || (b == "/if" && (open == "if" || open == "else")) ||
				(b == "else" && open == "if")
			if !valid {
				p.issue(t.pos, CodeUnexpectedClose, b)
				continue
			}
			p.closer, p.closerPos = b, t.pos
			return out
		default:
			if n := p.parseTag(t); n != nil {
				out = append(out, n)
			}
		}
	}
}

func (p *parser) parseTag(t *tag) *node {
	b := t.body
	word, rest, _ := strings.Cut(b, " ")
	switch word {
	case "pagebreak", "logo", "page", "pages":
		if strings.TrimSpace(rest) != "" {
			p.issue(t.pos, CodeSyntax, word)
			return nil
		}
		return &node{kind: nDirective, pos: t.pos, name: word}
	case "signature":
		role, ok := unquote(strings.TrimSpace(rest))
		if !ok || !validIdent(role) {
			p.issue(t.pos, CodeSyntax, "signature")
			return nil
		}
		return &node{kind: nDirective, pos: t.pos, name: word, arg: role}
	}
	parts := splitPipes(b)
	path := strings.TrimSpace(parts[0])
	if !validPath(path) {
		p.issue(t.pos, CodeSyntax, path)
		return nil
	}
	n := &node{kind: nVar, pos: t.pos, path: path}
	for _, f := range parts[1:] {
		f = strings.TrimSpace(f)
		name, arg, has := strings.Cut(f, " ")
		fl := filter{name: name}
		if has {
			a := strings.TrimSpace(arg)
			if s, ok := unquote(a); ok {
				fl.arg = s
			} else if validIdent(a) {
				fl.arg = a
			} else {
				p.issue(t.pos, CodeFilterArg, name)
				continue
			}
			fl.has = true
		}
		if !validIdent(name) {
			p.issue(t.pos, CodeSyntax, f)
			continue
		}
		n.filters = append(n.filters, fl)
	}
	return n
}

// splitPipes splits a tag on "|" outside quotes.
func splitPipes(s string) []string {
	var out []string
	inQ, start := false, 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '\\':
			i++
		case '"':
			inQ = !inQ
		case '|':
			if !inQ {
				out = append(out, s[start:i])
				start = i + 1
			}
		}
	}
	return append(out, s[start:])
}

func unquote(s string) (string, bool) {
	if len(s) < 2 || s[0] != '"' || s[len(s)-1] != '"' {
		return "", false
	}
	v, err := strconv.Unquote(s)
	if err != nil {
		return "", false
	}
	return v, true
}

func parseCond(s string) (cond, bool) {
	if s == "" {
		return cond{}, false
	}
	for _, op := range []string{"==", "!=", ">", "<"} {
		l, r, ok := strings.Cut(s, op)
		if !ok {
			continue
		}
		c := cond{path: strings.TrimSpace(l), op: op}
		r = strings.TrimSpace(r)
		if op == "==" || op == "!=" {
			v, ok := unquote(r)
			if !ok {
				return c, false
			}
			c.str = v
		} else {
			n, err := strconv.ParseInt(r, 10, 64)
			if err != nil {
				return c, false
			}
			c.num = n
		}
		return c, validPath(c.path)
	}
	c := cond{path: s}
	if strings.HasPrefix(s, "!") {
		c.negate, c.path = true, strings.TrimSpace(s[1:])
	}
	return c, validPath(c.path)
}

func validIdent(s string) bool {
	if s == "" {
		return false
	}
	for i, c := range s {
		ok := c == '_' || (c >= 'a' && c <= 'z') || (i > 0 && c >= '0' && c <= '9')
		if !ok {
			return false
		}
	}
	return true
}

// validPath accepts a.b.c, this, this.a.b, @index, @first, @last.
func validPath(s string) bool {
	switch s {
	case "@index", "@first", "@last":
		return true
	}
	for _, seg := range strings.Split(s, ".") {
		if !validIdent(seg) {
			return false
		}
	}
	return true
}
