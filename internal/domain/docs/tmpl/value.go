package tmpl

import "time"

// Type is the type of a catalogue field and of a value (m4-plan §6.1; the API FieldType enum).
type Type string

// Field types.
const (
	TText     Type = "text"
	TNotes    Type = "notes" // newlines kept as hard line breaks
	TInt      Type = "int"
	TCents    Type = "cents"
	TCm       Type = "cm"
	TBP       Type = "bp"
	TMillis   Type = "millis"
	TDate     Type = "date"
	TDateTime Type = "datetime"
	TBool     Type = "bool"
	TList     Type = "list"
	TObject   Type = "object"
)

// numeric reports whether values of t compare with < and >.
func (t Type) numeric() bool {
	switch t {
	case TInt, TCents, TCm, TBP, TMillis:
		return true
	}
	return false
}

// stringy reports whether values of t compare with == and != against a string literal.
func (t Type) stringy() bool {
	switch t {
	case TText, TNotes, TDate, TBool:
		return true
	}
	return false
}

// Field is one path of the catalogue of a kind. Element fields of a list are listed under the list path
// ("options.label" for the label of each option).
type Field struct {
	Path           string `json:"path"`
	Type           Type   `json:"type"`
	List           bool   `json:"list"`
	DescriptionKey string `json:"description_key"`
}

// Schema maps a path to its field.
type Schema map[string]Field

// Value is a node of the data tree a template is evaluated on. Scalars use S (text, notes, date as YYYY-MM-DD),
// N (numbers), B (bool) or T (datetime); lists use L, objects O. Null marks a missing object or date.
type Value struct {
	Type Type
	S    string
	N    int64
	B    bool
	T    time.Time
	L    []Value
	O    map[string]Value
	Null bool
}

// Text returns a text value.
func Text(s string) Value { return Value{Type: TText, S: s} }

// Int returns a numeric value of type t.
func Int(t Type, n int64) Value { return Value{Type: t, N: n} }

// truthy is the condition of {{#if path}}.
func (v Value) truthy() bool {
	if v.Null {
		return false
	}
	switch v.Type {
	case TText, TNotes, TDate:
		return v.S != ""
	case TDateTime:
		return !v.T.IsZero()
	case TBool:
		return v.B
	case TList:
		return len(v.L) > 0
	case TObject:
		return true
	default:
		return v.N != 0
	}
}

// get returns the child of an object value ("" path segment returns v).
func (v Value) get(seg string) (Value, bool) {
	if v.Type != TObject || v.Null {
		return Value{Null: true}, false
	}
	c, ok := v.O[seg]
	return c, ok
}
