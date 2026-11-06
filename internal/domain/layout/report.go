package layout

// Ref identifies an element inside one map document: the UUID of a saved element or the client key of a new one.
type Ref string

// Severity of a validation issue.
type Severity string

// Severities.
const (
	SeverityError   Severity = "error"
	SeverityWarning Severity = "warning"
)

// Issue is one finding of ValidateMap (m1-plan §2.3). Params carries numbers only (cm, counts), never names or
// free text, so issues can be returned and logged without personal data.
type Issue struct {
	Code     string
	Severity Severity
	Kind     string // element kind of Refs[0]
	Refs     []Ref
	Field    string
	Params   map[string]int64
}

// Report is the result of ValidateMap: blocking errors and warnings, in deterministic order (by code, then by
// first ref).
type Report struct{ Errors, Warnings []Issue }

// OK reports whether the report has no blocking errors.
func (r Report) OK() bool { return len(r.Errors) == 0 }
