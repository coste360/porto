package formula

import (
	"slices"
	"strconv"

	"github.com/coste360/porto/internal/qty"
)

// Function names.
const (
	fnIf    = "if"
	fnMin   = "min"
	fnMax   = "max"
	fnRound = "round"
	fnCeil  = "ceil"
	fnFloor = "floor"
	fnBand  = "band"
)

// kindString is the type of a string literal while checking (only valid as an operand of == / != with an enum).
const kindString Kind = 100

// checker walks a program and collects issues; kind 0 means "unknown after an issue" and suppresses cascades.
type checker struct {
	decls   map[string]VarDecl
	tables  map[string]Table
	context map[string]bool
	issues  []Issue
}

func (c *checker) add(code string, pos int, ref string, params map[string]string) {
	c.issues = append(c.issues, Issue{Code: code, Pos: pos, Ref: ref, Params: params})
}

// Check type-checks the program against the declarations, the tables and the allowed context variables (qty,
// list_price inside price formulas). An empty result means the program is well typed.
func (p *Program) Check(decls map[string]VarDecl, tables map[string]Table, context []string) []Issue {
	_, issues := p.check(decls, tables, context)
	return issues
}

// ResultKind returns the type of the program's result (0 when the program does not check).
func (p *Program) ResultKind(decls map[string]VarDecl, tables map[string]Table, context []string) Kind {
	k, issues := p.check(decls, tables, context)
	if len(issues) > 0 || k == kindString {
		return 0
	}
	return k
}

func (p *Program) check(decls map[string]VarDecl, tables map[string]Table, context []string) (Kind, []Issue) {
	if p == nil || p.root == nil {
		return 0, []Issue{{Code: CodeSyntax}}
	}
	c := &checker{decls: decls, tables: tables, context: map[string]bool{}}
	for _, n := range context {
		c.context[n] = true
	}
	k := c.kind(p.root)
	if k == kindString {
		c.add(CodeTypeMismatch, p.root.pos, "", nil)
		k = 0
	}
	return k, c.issues
}

func kindName(k Kind) string {
	switch k {
	case KindNumber:
		return "number"
	case KindBool:
		return "bool"
	case KindEnum:
		return "enum"
	case kindString:
		return "string"
	}
	return "unknown"
}

// expect reports a type mismatch when got is known and differs from want.
func (c *checker) expect(n *node, got, want Kind) bool {
	if got == 0 {
		return false
	}
	if got != want {
		c.add(CodeTypeMismatch, n.pos, "", map[string]string{"want": kindName(want), "got": kindName(got)})
		return false
	}
	return true
}

func (c *checker) kind(n *node) Kind {
	switch n.op {
	case opNum:
		return KindNumber
	case opBool:
		return KindBool
	case opStr:
		return kindString
	case opIdent:
		if d, ok := c.decls[n.name]; ok {
			if d.Kind == 0 {
				return KindNumber
			}
			return d.Kind
		}
		if c.context[n.name] {
			return KindNumber
		}
		c.add(CodeUnknownVariable, n.pos, n.name, nil)
		return 0
	case opNeg:
		if c.expect(n.kids[0], c.kind(n.kids[0]), KindNumber) {
			return KindNumber
		}
		return 0
	case opNot:
		if c.expect(n.kids[0], c.kind(n.kids[0]), KindBool) {
			return KindBool
		}
		return 0
	case opAnd, opOr:
		a := c.expect(n.kids[0], c.kind(n.kids[0]), KindBool)
		b := c.expect(n.kids[1], c.kind(n.kids[1]), KindBool)
		if a && b {
			return KindBool
		}
		return 0
	case opAdd, opSub, opMul, opDiv:
		a := c.expect(n.kids[0], c.kind(n.kids[0]), KindNumber)
		b := c.expect(n.kids[1], c.kind(n.kids[1]), KindNumber)
		if a && b {
			return KindNumber
		}
		return 0
	case opLT, opLE, opGT, opGE:
		a := c.expect(n.kids[0], c.kind(n.kids[0]), KindNumber)
		b := c.expect(n.kids[1], c.kind(n.kids[1]), KindNumber)
		if a && b {
			return KindBool
		}
		return 0
	case opEQ, opNE:
		return c.equality(n)
	case opCall:
		return c.call(n)
	}
	c.add(CodeSyntax, n.pos, "", nil)
	return 0
}

// equality checks == and !=: numbers with numbers, booleans with booleans, enums with enums or string literals
// (the literal must be a value of the enum when the declaration lists them).
func (c *checker) equality(n *node) Kind {
	a, b := c.kind(n.kids[0]), c.kind(n.kids[1])
	if a == 0 || b == 0 {
		return 0
	}
	switch {
	case a == kindString && b == kindString:
		c.add(CodeTypeMismatch, n.pos, "", map[string]string{"want": "enum", "got": "string"})
		return 0
	case a == KindEnum && b == kindString:
		c.enumValue(n.kids[0], n.kids[1])
		return KindBool
	case a == kindString && b == KindEnum:
		c.enumValue(n.kids[1], n.kids[0])
		return KindBool
	case a == b:
		return KindBool
	}
	c.add(CodeTypeMismatch, n.pos, "", map[string]string{"want": kindName(a), "got": kindName(b)})
	return 0
}

func (c *checker) enumValue(enum, lit *node) {
	if enum.op != opIdent {
		return
	}
	d, ok := c.decls[enum.name]
	if !ok || len(d.Enum) == 0 {
		return
	}
	if !slices.Contains(d.Enum, lit.name) {
		c.add(CodeEnumValueUnknown, lit.pos, enum.name, map[string]string{"value": lit.name})
	}
}

func (c *checker) arity(n *node, minN, maxN int) bool {
	if len(n.kids) < minN || len(n.kids) > maxN {
		c.add(CodeArity, n.pos, n.name, map[string]string{"min": strconv.Itoa(minN), "max": strconv.Itoa(maxN)})
		return false
	}
	return true
}

func (c *checker) call(n *node) Kind {
	switch n.name {
	case fnIf:
		if !c.arity(n, 3, 3) {
			c.kids(n)
			return 0
		}
		cond := c.expect(n.kids[0], c.kind(n.kids[0]), KindBool)
		a, b := c.kind(n.kids[1]), c.kind(n.kids[2])
		if a == kindString || b == kindString {
			c.add(CodeTypeMismatch, n.pos, "", map[string]string{"got": "string"})
			return 0
		}
		if a == 0 || b == 0 || !cond {
			return 0
		}
		if a != b {
			c.add(CodeTypeMismatch, n.kids[2].pos, "", map[string]string{"want": kindName(a), "got": kindName(b)})
			return 0
		}
		return a
	case fnMin, fnMax:
		if !c.arity(n, MinArgs, MaxArgs) {
			c.kids(n)
			return 0
		}
		ok := true
		for _, k := range n.kids {
			if !c.expect(k, c.kind(k), KindNumber) {
				ok = false
			}
		}
		if ok {
			return KindNumber
		}
		return 0
	case fnRound:
		if !c.arity(n, 2, 2) {
			c.kids(n)
			return 0
		}
		ok := c.expect(n.kids[0], c.kind(n.kids[0]), KindNumber)
		d := n.kids[1]
		if d.op != opNum || d.num%qty.One != 0 || d.num < 0 || d.num > 3*qty.One {
			c.add(CodeTypeMismatch, d.pos, n.name, map[string]string{"want": "digits_0_3"})
			return 0
		}
		if ok {
			return KindNumber
		}
		return 0
	case fnCeil, fnFloor:
		if !c.arity(n, 1, 1) {
			c.kids(n)
			return 0
		}
		if c.expect(n.kids[0], c.kind(n.kids[0]), KindNumber) {
			return KindNumber
		}
		return 0
	case fnBand:
		if !c.arity(n, 2, 2) {
			c.kids(n)
			return 0
		}
		ok := c.expect(n.kids[0], c.kind(n.kids[0]), KindNumber)
		t := n.kids[1]
		if t.op != opIdent {
			c.add(CodeTypeMismatch, t.pos, n.name, map[string]string{"want": "table"})
			return 0
		}
		if _, found := c.tables[t.name]; !found {
			c.add(CodeUnknownTable, t.pos, t.name, nil)
			return 0
		}
		if ok {
			return KindNumber
		}
		return 0
	}
	c.add(CodeUnknownFunction, n.pos, n.name, nil)
	c.kids(n)
	return 0
}

// kids checks the arguments of a call that failed, so their own issues are reported too.
func (c *checker) kids(n *node) {
	for _, k := range n.kids {
		c.kind(k)
	}
}
