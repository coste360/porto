package formula

import (
	"errors"

	"github.com/coste360/porto/internal/qty"
)

// Eval evaluates the program. Errors are *EvalError (div_by_zero, missing_variable, overflow, band_no_row, ...).
func (p *Program) Eval(env Env) (Value, error) {
	if p == nil || p.root == nil {
		return Value{}, &EvalError{Code: CodeSyntax}
	}
	return eval(p.root, env)
}

func evalErr(code string, n *node, ref string) error {
	return &EvalError{Code: code, Pos: n.pos, Ref: ref}
}

// arith maps a qty error to the formula code.
func arith(n *node, v qty.Milli, err error) (Value, error) {
	switch {
	case err == nil:
		return Number(v), nil
	case errors.Is(err, qty.ErrDivByZero):
		return Value{}, evalErr(CodeDivByZero, n, "")
	default:
		return Value{}, evalErr(CodeOverflow, n, "")
	}
}

func evalKind(n *node, env Env, want Kind) (Value, error) {
	v, err := eval(n, env)
	if err != nil {
		return v, err
	}
	if v.Kind != want {
		return Value{}, evalErr(CodeTypeMismatch, n, "")
	}
	return v, nil
}

func eval(n *node, env Env) (Value, error) {
	switch n.op {
	case opNum:
		return Number(n.num), nil
	case opBool:
		return Bool(n.b), nil
	case opStr:
		return Enum(n.name), nil
	case opIdent:
		v, ok := env.Vars[n.name]
		if !ok {
			return Value{}, evalErr(CodeMissingVariable, n, n.name)
		}
		return v, nil
	case opNeg:
		x, err := evalKind(n.kids[0], env, KindNumber)
		if err != nil {
			return x, err
		}
		v, err := qty.Milli(0).Sub(x.Num)
		return arith(n, v, err)
	case opNot:
		x, err := evalKind(n.kids[0], env, KindBool)
		if err != nil {
			return x, err
		}
		return Bool(!x.Bool), nil
	case opAnd, opOr:
		a, err := evalKind(n.kids[0], env, KindBool)
		if err != nil {
			return a, err
		}
		if (n.op == opAnd && !a.Bool) || (n.op == opOr && a.Bool) {
			return a, nil
		}
		return evalKind(n.kids[1], env, KindBool)
	case opAdd, opSub, opMul, opDiv:
		a, err := evalKind(n.kids[0], env, KindNumber)
		if err != nil {
			return a, err
		}
		b, err := evalKind(n.kids[1], env, KindNumber)
		if err != nil {
			return b, err
		}
		var v qty.Milli
		switch n.op {
		case opAdd:
			v, err = a.Num.Add(b.Num)
		case opSub:
			v, err = a.Num.Sub(b.Num)
		case opMul:
			v, err = a.Num.Mul(b.Num)
		default:
			v, err = a.Num.Div(b.Num)
		}
		return arith(n, v, err)
	case opLT, opLE, opGT, opGE:
		a, err := evalKind(n.kids[0], env, KindNumber)
		if err != nil {
			return a, err
		}
		b, err := evalKind(n.kids[1], env, KindNumber)
		if err != nil {
			return b, err
		}
		c := a.Num.Cmp(b.Num)
		switch n.op {
		case opLT:
			return Bool(c < 0), nil
		case opLE:
			return Bool(c <= 0), nil
		case opGT:
			return Bool(c > 0), nil
		}
		return Bool(c >= 0), nil
	case opEQ, opNE:
		a, err := eval(n.kids[0], env)
		if err != nil {
			return a, err
		}
		b, err := eval(n.kids[1], env)
		if err != nil {
			return b, err
		}
		if a.Kind != b.Kind {
			return Value{}, evalErr(CodeTypeMismatch, n, "")
		}
		eq := false
		switch a.Kind {
		case KindNumber:
			eq = a.Num == b.Num
		case KindBool:
			eq = a.Bool == b.Bool
		case KindEnum:
			eq = a.Enum == b.Enum
		}
		if n.op == opNE {
			eq = !eq
		}
		return Bool(eq), nil
	case opCall:
		return evalCall(n, env)
	}
	return Value{}, evalErr(CodeSyntax, n, "")
}

func evalCall(n *node, env Env) (Value, error) {
	switch n.name {
	case fnIf:
		if len(n.kids) != 3 {
			return Value{}, evalErr(CodeArity, n, n.name)
		}
		c, err := evalKind(n.kids[0], env, KindBool)
		if err != nil {
			return c, err
		}
		if c.Bool {
			return eval(n.kids[1], env)
		}
		return eval(n.kids[2], env)
	case fnMin, fnMax:
		if len(n.kids) < MinArgs || len(n.kids) > MaxArgs {
			return Value{}, evalErr(CodeArity, n, n.name)
		}
		var best qty.Milli
		for i, k := range n.kids {
			v, err := evalKind(k, env, KindNumber)
			if err != nil {
				return v, err
			}
			if i == 0 || (n.name == fnMin && v.Num < best) || (n.name == fnMax && v.Num > best) {
				best = v.Num
			}
		}
		return Number(best), nil
	case fnRound:
		if len(n.kids) != 2 || n.kids[1].op != opNum {
			return Value{}, evalErr(CodeArity, n, n.name)
		}
		d := n.kids[1].num
		if d%qty.One != 0 || d < 0 || d > 3*qty.One {
			return Value{}, evalErr(CodeTypeMismatch, n.kids[1], n.name)
		}
		x, err := evalKind(n.kids[0], env, KindNumber)
		if err != nil {
			return x, err
		}
		return Number(x.Num.Round(int(d / qty.One))), nil
	case fnCeil, fnFloor:
		if len(n.kids) != 1 {
			return Value{}, evalErr(CodeArity, n, n.name)
		}
		x, err := evalKind(n.kids[0], env, KindNumber)
		if err != nil {
			return x, err
		}
		v, err := toInteger(x.Num, n.name == fnCeil)
		return arith(n, v, err)
	case fnBand:
		if len(n.kids) != 2 || n.kids[1].op != opIdent {
			return Value{}, evalErr(CodeArity, n, n.name)
		}
		x, err := evalKind(n.kids[0], env, KindNumber)
		if err != nil {
			return x, err
		}
		name := n.kids[1].name
		t, ok := env.Tables[name]
		if !ok {
			return Value{}, evalErr(CodeUnknownTable, n.kids[1], name)
		}
		v, ok := BandValue(t, x.Num)
		if !ok {
			return Value{}, evalErr(CodeBandNoRow, n, name)
		}
		return Number(v), nil
	}
	return Value{}, evalErr(CodeUnknownFunction, n, n.name)
}

// toInteger rounds m up (ceil) or down (floor) to a whole number.
func toInteger(m qty.Milli, up bool) (qty.Milli, error) {
	r := m % qty.One
	if r == 0 {
		return m, nil
	}
	base := m - r // truncated towards zero
	switch {
	case up && m > 0:
		return base.Add(qty.One)
	case !up && m < 0:
		return base.Sub(qty.One)
	}
	return base, nil
}

// BandValue returns the value of the first row with x <= UpTo, else the open row; false when no row applies.
func BandValue(t Table, x qty.Milli) (qty.Milli, bool) {
	for _, r := range t.Rows {
		if r.UpTo == nil || x <= *r.UpTo {
			return r.Value, true
		}
	}
	return 0, false
}
