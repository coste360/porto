package formula

import (
	"github.com/coste360/porto/internal/qty"
)

// LanguageVersion is the version of the formula semantics stored in every snapshot (m10-plan §3.2).
const LanguageVersion = 1

// Limits of a formula (m10-plan §3.2); exceeding one is CodeTooComplex.
const (
	MaxSourceBytes = 500
	MaxNodes       = 256
	MaxDepth       = 32
	MaxIdents      = 32
	MaxTableRows   = 50
	MinArgs        = 2 // min, max
	MaxArgs        = 8 // min, max
)

// Kind is the type of a value.
type Kind uint8

// Value kinds.
const (
	KindNumber Kind = iota + 1
	KindBool
	KindEnum
)

// Variable sources (m10-plan §3.3).
const (
	SourceVessel  = "vessel"
	SourceDerived = "derived"
	SourceJob     = "job"
	SourceContext = "context"
)

// Context variable names, valid only inside price formulas.
const (
	VarQty       = "qty"
	VarListPrice = "list_price"
)

// Issue and error codes (m10-plan §3.4); also the codes of the problem formula_invalid.
const (
	CodeSyntax           = "syntax"
	CodeUnknownVariable  = "unknown_variable"
	CodeUnknownFunction  = "unknown_function"
	CodeUnknownTable     = "unknown_table"
	CodeArity            = "arity"
	CodeTypeMismatch     = "type_mismatch"
	CodeEnumValueUnknown = "enum_value_unknown"
	CodeTooComplex       = "formula_too_complex"
	CodeDerivedCycle     = "derived_cycle"
	CodeDivByZero        = "div_by_zero"
	CodeMissingVariable  = "missing_variable"
	CodeOverflow         = "overflow"
	CodeBandNoRow        = "band_no_row"
	CodeNegativeQuantity = "negative_quantity"
	CodeNegativePrice    = "negative_price"
	CodeOutOfRange       = "out_of_range"
)

// Value is a formula value: a number in milli, a boolean or an enum string.
type Value struct {
	Kind Kind
	Num  qty.Milli
	Bool bool
	Enum string
}

// Number returns a number value.
func Number(m qty.Milli) Value { return Value{Kind: KindNumber, Num: m} }

// Bool returns a boolean value.
func Bool(b bool) Value { return Value{Kind: KindBool, Bool: b} }

// Enum returns an enum value.
func Enum(s string) Value { return Value{Kind: KindEnum, Enum: s} }

// VarDecl declares a variable known to the checker (vessel, derived, job or context).
type VarDecl struct {
	Name    string
	Kind    Kind
	Unit    string
	Source  string
	Min     *qty.Milli
	Max     *qty.Milli
	Default *qty.Milli
	Enum    []string
}

// Row is one row of a band table: the value applies when x <= UpTo; UpTo nil is the open last row.
type Row struct {
	UpTo  *qty.Milli
	Value qty.Milli
}

// Table is a band table used by band(x, name); rows ascending, at most MaxTableRows.
type Table struct {
	Name string
	Rows []Row
}

// Derived is a derived variable definition (an expression over vessel and other derived variables).
type Derived struct {
	Name string
	Src  string
}

// Env is the evaluation environment: variable values and band tables.
type Env struct {
	Vars   map[string]Value
	Tables map[string]Table
}

// Issue is a check problem of a formula (Pos is the byte offset in the source; Ref names the variable, table or
// function concerned).
type Issue struct {
	Code   string
	Ref    string
	Pos    int
	Params map[string]string
}

// SyntaxError is returned by Parse.
type SyntaxError struct {
	Pos  int
	Code string // CodeSyntax or CodeTooComplex
}

func (e *SyntaxError) Error() string { return "formula: " + e.Code }

// EvalError is returned by Eval and by the result conversions.
type EvalError struct {
	Code string
	Pos  int
	Ref  string
}

func (e *EvalError) Error() string { return "formula: " + e.Code }

// Program is a parsed, immutable formula.
type Program struct {
	src    string
	root   *node
	idents []string
	tables []string
}

// Source returns the formula text.
func (p *Program) Source() string { return p.src }
