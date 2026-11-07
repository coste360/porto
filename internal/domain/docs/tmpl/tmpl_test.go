package tmpl

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/coste360/porto/internal/domain/docs/markdown"
)

func testSchema() Schema {
	return Schema{
		"customer":              {Path: "customer", Type: TObject},
		"customer.display_name": {Path: "customer.display_name", Type: TText},
		"customer.kind":         {Path: "customer.kind", Type: TText},
		"contract":              {Path: "contract", Type: TObject},
		"contract.notes":        {Path: "contract.notes", Type: TNotes},
		"contract.starts_on":    {Path: "contract.starts_on", Type: TDate},
		"contract.signed":       {Path: "contract.signed", Type: TBool},
		"vessel":                {Path: "vessel", Type: TObject},
		"vessel.loa":            {Path: "vessel.loa", Type: TCm},
		"price":                 {Path: "price", Type: TObject},
		"price.total":           {Path: "price.total", Type: TCents},
		"policy":                {Path: "policy", Type: TObject},
		"policy.deposit_bp":     {Path: "policy.deposit_bp", Type: TBP},
		"policy.count":          {Path: "policy.count", Type: TInt},
		"share":                 {Path: "share", Type: TMillis},
		"at":                    {Path: "at", Type: TDateTime},
		"lines":                 {Path: "lines", Type: TList, List: true},
		"lines.description":     {Path: "lines.description", Type: TText},
		"lines.amount":          {Path: "lines.amount", Type: TCents},
		"lines.parts":           {Path: "lines.parts", Type: TList, List: true},
		"lines.parts.code":      {Path: "lines.parts.code", Type: TText},
	}
}

func testData() Value {
	obj := func(kv ...any) Value {
		o := map[string]Value{}
		for i := 0; i < len(kv); i += 2 {
			o[kv[i].(string)] = kv[i+1].(Value)
		}
		return Value{Type: TObject, O: o}
	}
	part := func(c string) Value { return obj("code", Text(c)) }
	return obj(
		"customer", obj("display_name", Text("Mario *Rossi*"), "kind", Text("person")),
		"contract", obj("notes", Value{Type: TNotes, S: "line one\n\nline two"},
			"starts_on", Value{Type: TDate, S: "2026-01-01"}, "signed", Value{Type: TBool, B: true}),
		"vessel", obj("loa", Int(TCm, 1134)),
		"price", obj("total", Int(TCents, 123456)),
		"policy", obj("deposit_bp", Int(TBP, 3000), "count", Int(TInt, 3)),
		"share", Int(TMillis, 333),
		"at", Value{Type: TDateTime, T: time.Date(2026, 11, 7, 14, 5, 0, 0, time.UTC)},
		"lines", Value{Type: TList, L: []Value{
			obj("description", Text("Ormeggio"), "amount", Int(TCents, 100000),
				"parts", Value{Type: TList, L: []Value{part("A1"), part("A2")}}),
			obj("description", Text("Acqua"), "amount", Int(TCents, 2345),
				"parts", Value{Type: TList}),
		}},
	)
}

func eval(t *testing.T, src string, o Options, env Env) (string, error) {
	t.Helper()
	tp, err := Parse(src)
	if err != nil {
		return "", err
	}
	if err := tp.Validate(testSchema(), o); err != nil {
		return "", err
	}
	return tp.Eval(testData(), env)
}

func TestTemplate_Scenario(t *testing.T) {
	it := Env{Locale: "it", Today: "2026-11-07"}
	en := Env{Locale: "en", Today: "2026-11-07"}
	body := Options{Roles: []string{"customer", "marina"}}
	hf := Options{Mode: ModeHeaderFooter}
	ok := []struct {
		name, src string
		o         Options
		env       Env
		want      string
	}{
		{"text escaped", "{{customer.display_name}}", body, it, `Mario \*Rossi\*`},
		{"upper", "{{customer.display_name | upper}}", body, it, `MARIO \*ROSSI\*`},
		{"lower", "{{customer.kind | lower}}", body, it, "person"},
		{"meters it", "{{vessel.loa | meters}}", body, it, `11\,34 m`},
		{"meters en", "{{vessel.loa | meters}}", body, en, `11\.34 m`},
		{"cm", "{{vessel.loa | cm}}", body, it, "1134 cm"},
		{"money it", "{{price.total | money}}", body, it, `1\.234\,56 €`},
		{"money en", "{{price.total | money}}", body, en, `€1\,234\.56`},
		{"money default", "{{price.total}}", body, it, `1\.234\,56 €`},
		{"date short", "{{contract.starts_on | date \"short\"}}", body, it, `01\/01\/2026`},
		{"date long it", "{{contract.starts_on | date \"long\"}}", body, it, "1 gennaio 2026"},
		{"date long en", "{{contract.starts_on | date long}}", body, en, "1 January 2026"},
		{"datetime", "{{at | date}}", body, it, `07\/11\/2026 14\:05 UTC`},
		{"bp", "{{policy.deposit_bp | bp}}", body, it, `30 \%`},
		{"millis share", "{{share | millis}}", body, it, "333 ‰"},
		{"millis plain", "{{share}}", body, it, `0\,333`},
		{"count", "{{lines | count}}", body, it, "2"},
		{"default used", "{{contract.missing_safe | default \"x\"}}", body, it, ""},
		{"bool", "{{contract.signed}}", body, it, "sì"},
		{"notes breaks", "{{contract.notes}}", body, it, "line one{{br}}line two"},
		{"today", "{{today | date \"long\"}}", body, it, "7 novembre 2026"},
		{"each", "{{#each lines}}{{@index}}={{this.description}};{{/each}}", body, it, "1=Ormeggio;2=Acqua;"},
		{"each first last", "{{#each lines}}{{#if @first}}F{{/if}}{{#if @last}}L{{/if}}{{/each}}", body, it, "FL"},
		{"nested each", "{{#each lines}}{{#each this.parts}}{{this.code}}{{/each}}|{{/each}}", body, it, "A1A2||"},
		{"if else", "{{#if contract.signed}}Y{{else}}N{{/if}}", body, it, "Y"},
		{"if not", "{{#if !contract.signed}}Y{{else}}N{{/if}}", body, it, "N"},
		{"if eq", `{{#if customer.kind == "person"}}P{{/if}}`, body, it, "P"},
		{"if ne", `{{#if customer.kind != "person"}}P{{else}}C{{/if}}`, body, it, "C"},
		{"if gt", "{{#if policy.count > 2}}G{{/if}}", body, it, "G"},
		{"if lt", "{{#if policy.count < 2}}G{{else}}S{{/if}}", body, it, "S"},
		{"signature", `{{signature "customer"}}`, body, it, "\n\n{{signature \"customer\"}}\n\n"},
		{"pagebreak", "{{pagebreak}}", body, it, "\n\n{{pagebreak}}\n\n"},
		{"logo", "{{logo}}", body, it, "\n\n{{logo}}\n\n"},
		{"page pages", "{{page}}/{{pages}}", hf, it, "{{page}}/{{pages}}"},
		{"escaped braces kept", `\{\{x\}\}`, body, it, `\{\{x\}\}`},
	}
	for _, c := range ok {
		t.Run(c.name, func(t *testing.T) {
			if c.name == "default used" {
				return // covered below with a declared empty field
			}
			got, err := eval(t, c.src, c.o, c.env)
			if err != nil {
				t.Fatalf("eval: %v", err)
			}
			if got != c.want {
				t.Fatalf("got %q want %q", got, c.want)
			}
		})
	}

	// default on an empty value.
	tp, err := Parse(`{{customer.display_name | default "n/d"}}`)
	if err != nil {
		t.Fatal(err)
	}
	empty := testData()
	empty.O["customer"].O["display_name"] = Text("")
	if got, _ := tp.Eval(empty, it); got != `n\/d` {
		t.Fatalf("default: %q", got)
	}

	bad := []struct {
		name, src string
		o         Options
		code      string
		line, col int
	}{
		{"unknown path", "ok\n  {{customer.nope}}", body, CodeUnknownPath, 2, 3},
		{"unknown filter", "{{price.total | euro}}", body, CodeUnknownFilter, 1, 1},
		{"filter type", "{{customer.display_name | money}}", body, CodeFilterType, 1, 1},
		{"default needs arg", "{{customer.display_name | default}}", body, CodeFilterArg, 1, 1},
		{"date bad arg", `{{contract.starts_on | date "medium"}}`, body, CodeFilterArg, 1, 1},
		{"each not list", "{{#each customer}}x{{/each}}", body, CodeEachNotList, 1, 1},
		{"not scalar", "{{lines}}", body, CodeNotScalar, 1, 1},
		{"unclosed each", "a {{#each lines}}x", body, CodeUnclosedBlock, 1, 3},
		{"unclosed if", "{{#if contract.signed}}x", body, CodeUnclosedBlock, 1, 1},
		{"unexpected close", "x{{/each}}", body, CodeUnexpectedClose, 1, 2},
		{"syntax unterminated", "x {{customer", body, CodeSyntax, 1, 3},
		{"syntax path", "{{Customer.Name}}", body, CodeSyntax, 1, 1},
		{"compare type", `{{#if price.total == "1"}}x{{/if}}`, body, CodeCompareType, 1, 1},
		{"compare num type", "{{#if customer.kind > 1}}x{{/if}}", body, CodeCompareType, 1, 1},
		{"page in body", "{{page}}", body, CodeDirectiveContext, 1, 1},
		{"signature in footer", `{{signature "customer"}}`, hf, CodeDirectiveContext, 1, 1},
		{"unknown role", `{{signature "notary"}}`, body, CodeUnknownRole, 1, 1},
		{"this outside", "{{this.x}}", body, CodeThisOutsideEach, 1, 1},
		{"index outside", "{{@index}}", body, CodeThisOutsideEach, 1, 1},
		{"too deep", "{{#each lines}}{{#each this.parts}}{{#each lines}}{{#each lines}}{{#each lines}}x{{/each}}{{/each}}{{/each}}{{/each}}{{/each}}", body, CodeNestingTooDeep, 1, 0},
		{"unicode col", "àè {{nope}}", body, CodeUnknownPath, 1, 4},
	}
	for _, c := range bad {
		t.Run("error/"+c.name, func(t *testing.T) {
			_, err := eval(t, c.src, c.o, it)
			var te *Error
			if !errors.As(err, &te) {
				t.Fatalf("want *Error, got %v", err)
			}
			found := false
			for _, is := range te.Issues {
				if is.Code == c.code && is.Line == c.line && (c.col == 0 || is.Col == c.col) {
					found = true
				}
			}
			if !found {
				t.Fatalf("want %s at %d:%d, got %+v", c.code, c.line, c.col, te.Issues)
			}
		})
	}
}

func TestTemplate_Limits(t *testing.T) {
	big := make([]Value, MaxIterations+1)
	for i := range big {
		big[i] = Value{Type: TObject, O: map[string]Value{"description": Text("x")}}
	}
	data := Value{Type: TObject, O: map[string]Value{"lines": {Type: TList, L: big}}}
	tp, _ := Parse("{{#each lines}}{{this.description}}{{/each}}")
	if _, err := tp.Eval(data, Env{}); !errors.Is(err, ErrLimits) {
		t.Fatalf("iterations: %v", err)
	}
	long := strings.Repeat("x", MaxOutputBytes+1)
	tp, _ = Parse(long)
	if _, err := tp.Eval(data, Env{}); !errors.Is(err, ErrLimits) {
		t.Fatalf("output: %v", err)
	}
}

func FuzzTemplate_Parse(f *testing.F) {
	for _, s := range []string{
		"{{customer.display_name}}", "{{#each lines}}{{this.amount | money}}{{/each}}", "{{#if a}}x{{else}}y{{/if}}",
		"{{", "}}", "{{#each}}", "{{/if}}", `{{signature "customer"}}`, "{{x | default \"a|b\"}}", "\\{{",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, src string) {
		tp, err := Parse(src)
		if err != nil {
			var te *Error
			if !errors.As(err, &te) || len(te.Issues) == 0 {
				t.Fatalf("parse error without issues: %v", err)
			}
			return
		}
		_ = tp.Validate(testSchema(), Options{Roles: []string{"customer"}})
		out, err := tp.Eval(testData(), Env{Locale: "it", Today: "2026-01-01"})
		if err == nil && len(out) > MaxOutputBytes {
			t.Fatalf("output %d bytes", len(out))
		}
	})
}

// structure is the block shape of Markdown text (kinds, levels, list and table sizes), ignoring the text.
func structure(md string) string {
	var b strings.Builder
	for _, bl := range markdown.Parse(md) {
		b.WriteString(reflect.ValueOf(bl.Kind).String())
		switch bl.Kind {
		case markdown.List:
			b.WriteString(":" + strings.Repeat("i", len(bl.Items)))
		case markdown.Table:
			b.WriteString(":" + strings.Repeat("c", len(bl.Header)) + strings.Repeat("r", len(bl.Rows)))
			for _, r := range bl.Rows {
				for _, c := range r {
					for _, s := range c {
						if s.Kind == markdown.SpanPage || s.Kind == markdown.SpanPages || s.Bold || s.Italic {
							b.WriteString("!")
						}
					}
				}
			}
		}
		for _, in := range [][]markdown.Span{bl.Content} {
			for _, s := range in {
				if s.Kind == markdown.SpanPage || s.Kind == markdown.SpanPages || s.Bold || s.Italic {
					b.WriteString("!")
				}
			}
		}
		b.WriteString(";")
	}
	return b.String()
}

func FuzzTemplate_ValueEscaping(f *testing.F) {
	for _, s := range []string{
		"x", "# title", "- item", "1. one", "| a | b |\n|---|---|", "---", "**bold**", "*it*", "{{pagebreak}}",
		"{{signature \"customer\"}}", "a\n\nb", "\\", "{{br}}", "{{page}}", "\r\n# x", "\u2028- x",
	} {
		f.Add(s)
	}
	src := "# Title\n\n{{customer.display_name}}\n\n- {{customer.display_name}}\n- b\n\n| A | B |\n|---|---|\n| {{customer.display_name}} | {{contract.notes}} |\n\nend {{contract.notes}}\n"
	tp, err := Parse(src)
	if err != nil {
		f.Fatal(err)
	}
	data := func(v string) Value {
		d := testData()
		d.O["customer"].O["display_name"] = Text(v)
		d.O["contract"].O["notes"] = Value{Type: TNotes, S: v}
		return d
	}
	ref, err := tp.Eval(data("x"), Env{Locale: "it"})
	if err != nil {
		f.Fatal(err)
	}
	want := structure(ref)
	f.Fuzz(func(t *testing.T, v string) {
		if strings.TrimSpace(strings.ReplaceAll(Escape(v, true), "{{br}}", "")) == "" {
			return // an empty value removes its own paragraph; it cannot add structure
		}
		out, err := tp.Eval(data(v), Env{Locale: "it"})
		if err != nil {
			return
		}
		if got := structure(out); got != want {
			t.Fatalf("value %q changed the structure:\n got %s\nwant %s", v, got, want)
		}
	})
}
