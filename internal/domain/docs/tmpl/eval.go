package tmpl

import (
	"errors"
	"strconv"
	"strings"
	"unicode"
)

// Limits of an evaluation (422 template_limits).
const (
	MaxOutputBytes = 2 << 20
	MaxIterations  = 10_000
)

// ErrLimits means the output or the loop iterations exceed the limits.
var ErrLimits = errors.New("tmpl: limits exceeded")

// Mode is where a template is used: the body or a page header/footer ({{page}} and {{pages}} only there).
type Mode int

// Modes.
const (
	ModeBody Mode = iota
	ModeHeaderFooter
)

// Options are the validation options.
type Options struct {
	Mode  Mode
	Roles []string // allowed {{signature "role"}} roles; nil = none allowed
}

// pathToday is the pseudo-path of the evaluation date.
const pathToday = "today"

var filterIn = map[string][]Type{
	"meters":  {TCm},
	"cm":      {TCm},
	"money":   {TCents},
	"date":    {TDate, TDateTime},
	"upper":   {TText, TNotes},
	"lower":   {TText, TNotes},
	"default": nil, // any scalar
	"bp":      {TBP},
	"millis":  {TMillis},
	"count":   {TList},
}

// Validate checks every path, filter and block against schema. Issues are returned as *Error.
func (t *Template) Validate(schema Schema, o Options) error {
	v := &validator{schema: schema, o: o}
	v.nodes(t.nodes, nil)
	if len(v.issues) > 0 {
		return &Error{Issues: v.issues}
	}
	return nil
}

type validator struct {
	schema Schema
	o      Options
	issues []Issue
}

func (v *validator) issue(n *node, code, path string) {
	v.issues = append(v.issues, Issue{Line: n.pos.line, Col: n.pos.col, Code: code, Path: path})
}

// resolve returns the schema type of path in the scope of the enclosing each lists (innermost last).
func (v *validator) resolve(n *node, path string, lists []string) (Type, bool) {
	switch path {
	case "@index":
		if len(lists) == 0 {
			v.issue(n, CodeThisOutsideEach, path)
			return "", false
		}
		return TInt, true
	case "@first", "@last":
		if len(lists) == 0 {
			v.issue(n, CodeThisOutsideEach, path)
			return "", false
		}
		return TBool, true
	case pathToday:
		return TDate, true
	}
	full := path
	if path == "this" || strings.HasPrefix(path, "this.") {
		if len(lists) == 0 {
			v.issue(n, CodeThisOutsideEach, path)
			return "", false
		}
		full = lists[len(lists)-1] + strings.TrimPrefix(path, "this")
		if path == "this" {
			return TObject, true
		}
	}
	f, ok := v.schema[full]
	if !ok {
		v.issue(n, CodeUnknownPath, path)
		return "", false
	}
	return f.Type, true
}

func (v *validator) fullPath(path string, lists []string) string {
	if path == "this" || strings.HasPrefix(path, "this.") {
		return lists[len(lists)-1] + strings.TrimPrefix(path, "this")
	}
	return path
}

func (v *validator) nodes(ns []*node, lists []string) {
	for _, n := range ns {
		switch n.kind {
		case nVar:
			typ, ok := v.resolve(n, n.path, lists)
			if !ok {
				continue
			}
			for _, f := range n.filters {
				in, known := filterIn[f.name]
				if !known {
					v.issue(n, CodeUnknownFilter, n.path)
					break
				}
				if in != nil && !contains(in, typ) {
					v.issue(n, CodeFilterType, n.path)
					break
				}
				if f.name == "default" && (typ == TList || typ == TObject) {
					v.issue(n, CodeFilterType, n.path)
					break
				}
				if (f.name == "default") != f.has && f.name != "date" {
					v.issue(n, CodeFilterArg, n.path)
					break
				}
				if f.name == "date" && f.has && f.arg != "long" && f.arg != "short" {
					v.issue(n, CodeFilterArg, n.path)
					break
				}
				switch f.name {
				case "count":
					typ = TInt
				case "default", "upper", "lower":
				default:
					typ = TText
				}
			}
			if typ == TList || typ == TObject {
				v.issue(n, CodeNotScalar, n.path)
			}
		case nEach:
			typ, ok := v.resolve(n, n.path, lists)
			if !ok {
				v.nodes(n.body, append(lists, "?"))
				continue
			}
			if typ != TList {
				v.issue(n, CodeEachNotList, n.path)
				continue
			}
			if len(lists) >= MaxEachDepth {
				v.issue(n, CodeNestingTooDeep, n.path)
				continue
			}
			v.nodes(n.body, append(lists[:len(lists):len(lists)], v.fullPath(n.path, lists)))
		case nIf:
			typ, ok := v.resolve(n, n.cond.path, lists)
			if ok {
				switch n.cond.op {
				case "==", "!=":
					if !typ.stringy() {
						v.issue(n, CodeCompareType, n.cond.path)
					}
				case ">", "<":
					if !typ.numeric() {
						v.issue(n, CodeCompareType, n.cond.path)
					}
				}
			}
			v.nodes(n.body, lists)
			v.nodes(n.els, lists)
		case nDirective:
			switch n.name {
			case "page", "pages":
				if v.o.Mode != ModeHeaderFooter {
					v.issue(n, CodeDirectiveContext, n.name)
				}
			case "signature", "pagebreak":
				if v.o.Mode != ModeBody {
					v.issue(n, CodeDirectiveContext, n.name)
				} else if n.name == "signature" && !contains(v.o.Roles, n.arg) {
					v.issue(n, CodeUnknownRole, n.arg)
				}
			}
		}
	}
}

func contains[T comparable](xs []T, x T) bool {
	for _, y := range xs {
		if y == x {
			return true
		}
	}
	return false
}

// Env is the evaluation environment: locale of the formatting and the evaluation date (ISO YYYY-MM-DD) of
// {{today}}. Evaluation is pure: the caller passes the date.
type Env struct {
	Locale string
	Today  string
}

type frame struct {
	item  Value
	index int
	last  bool
}

type evaluator struct {
	root   Value
	env    Env
	out    strings.Builder
	iters  int
	frames []frame
	err    error
}

// Eval evaluates the template on data and returns Markdown where every inserted value is escaped, so values never
// create Markdown structure. Directives become the block markers of docs/markdown ({{pagebreak}}, {{logo}},
// {{signature "role"}} on their own lines; {{page}}, {{pages}} and {{br}} inline).
func (t *Template) Eval(data Value, env Env) (string, error) {
	e := &evaluator{root: data, env: env}
	e.nodes(t.nodes)
	if e.err != nil {
		return "", e.err
	}
	return e.out.String(), nil
}

func (e *evaluator) write(s string) {
	if e.err != nil {
		return
	}
	if e.out.Len()+len(s) > MaxOutputBytes {
		e.err = ErrLimits
		return
	}
	e.out.WriteString(s)
}

func (e *evaluator) lookup(path string) Value {
	switch path {
	case "@index":
		if len(e.frames) > 0 {
			return Int(TInt, int64(e.frames[len(e.frames)-1].index+1))
		}
	case "@first":
		if len(e.frames) > 0 {
			return Value{Type: TBool, B: e.frames[len(e.frames)-1].index == 0}
		}
	case "@last":
		if len(e.frames) > 0 {
			return Value{Type: TBool, B: e.frames[len(e.frames)-1].last}
		}
	case pathToday:
		return Value{Type: TDate, S: e.env.Today}
	}
	cur := e.root
	segs := strings.Split(path, ".")
	if segs[0] == "this" {
		if len(e.frames) == 0 {
			return Value{Null: true}
		}
		cur = e.frames[len(e.frames)-1].item
		segs = segs[1:]
	}
	for _, s := range segs {
		c, ok := cur.get(s)
		if !ok {
			return Value{Null: true}
		}
		cur = c
	}
	return cur
}

func (e *evaluator) nodes(ns []*node) {
	for _, n := range ns {
		if e.err != nil {
			return
		}
		switch n.kind {
		case nText:
			e.write(n.text)
		case nVar:
			e.write(e.variable(n))
		case nEach:
			list := e.lookup(n.path)
			for i, item := range list.L {
				e.iters++
				if e.iters > MaxIterations {
					e.err = ErrLimits
					return
				}
				e.frames = append(e.frames, frame{item: item, index: i, last: i == len(list.L)-1})
				e.nodes(n.body)
				e.frames = e.frames[:len(e.frames)-1]
			}
		case nIf:
			if e.test(n.cond) {
				e.nodes(n.body)
			} else {
				e.nodes(n.els)
			}
		case nDirective:
			switch n.name {
			case "signature":
				e.write("\n\n{{signature " + strconv.Quote(n.arg) + "}}\n\n")
			case "pagebreak", "logo":
				e.write("\n\n{{" + n.name + "}}\n\n")
			default:
				e.write("{{" + n.name + "}}")
			}
		}
	}
}

func (e *evaluator) test(c cond) bool {
	v := e.lookup(c.path)
	switch c.op {
	case "==", "!=":
		s := v.S
		if v.Type == TBool {
			s = strconv.FormatBool(v.B)
		}
		return (s == c.str) == (c.op == "==")
	case ">":
		return !v.Null && v.N > c.num
	case "<":
		return !v.Null && v.N < c.num
	}
	return v.truthy() != c.negate
}

func (e *evaluator) variable(n *node) string {
	v := e.lookup(n.path)
	text, formatted := "", false
	notes := v.Type == TNotes
	loc := e.env.Locale
	for _, f := range n.filters {
		switch f.name {
		case "default":
			if !formatted && !v.truthy() && v.Type != TInt && !v.Type.numeric() {
				text, formatted = f.arg, true
				notes = false
			}
			continue
		case "upper", "lower":
			if !formatted {
				text, formatted = plain(v, loc), true
			}
			if f.name == "upper" {
				text = strings.ToUpper(text)
			} else {
				text = strings.ToLower(text)
			}
			continue
		}
		if formatted {
			continue
		}
		formatted = true
		switch f.name {
		case "meters":
			text = formatMeters(v.N, loc)
		case "cm":
			text = strconv.FormatInt(v.N, 10) + " cm"
		case "money":
			text = formatMoney(v.N, loc)
		case "date":
			long := f.arg == "long"
			if v.Type == TDateTime {
				text = formatDateTime(v.T, long, loc)
			} else if v.S != "" {
				text = formatDate(v.S, long, loc)
			}
		case "bp":
			text = formatBP(v.N, loc)
		case "millis":
			text = formatMillisShare(v.N, loc)
		case "count":
			text = strconv.Itoa(len(v.L))
		}
	}
	if !formatted {
		text = plain(v, loc)
	}
	return Escape(text, notes)
}

// Escape makes a value safe to insert into Markdown: every ASCII punctuation character is backslash-escaped,
// control characters are dropped, and newlines become spaces (or {{br}} hard breaks for notes, runs of newlines
// collapsed into one), so a value can never start a block, a table cell or an inline marker.
func Escape(s string, notes bool) string {
	var b strings.Builder
	b.Grow(len(s) + len(s)/4)
	newline := false
	for _, r := range strings.ReplaceAll(s, "\r\n", "\n") {
		if r == '\n' || r == '\r' {
			newline = true
			continue
		}
		if newline {
			if notes {
				b.WriteString("{{br}}")
			} else {
				b.WriteByte(' ')
			}
			newline = false
		}
		switch {
		case r == '\t':
			b.WriteByte(' ')
		case unicode.IsControl(r) || r == '\u2028' || r == '\u2029' || r == '\ufeff':
		case r < 128 && unicode.IsPunct(r) || r < 128 && unicode.IsSymbol(r):
			b.WriteByte('\\')
			b.WriteRune(r)
		default:
			b.WriteRune(r)
		}
	}
	if newline && !notes {
		b.WriteByte(' ')
	}
	return b.String()
}
