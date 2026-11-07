package csvimport

import (
	"errors"
	"strings"
	"testing"
)

func TestParse_Scenario(t *testing.T) {
	tbl, err := Parse("\ufeffcode;millis\nA1;500\n\n A2 ; 500 \n", 0, true)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := tbl.Mapped(Mapping{"berth_code": 0, "millis": 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].Get("berth_code") != "A1" || rows[1].Get("millis") != "500" || rows[1].Line != 3 ||
		rows[0].Get("missing") != "" {
		t.Fatalf("rows %+v", rows)
	}
	if _, err := tbl.Mapped(Mapping{"x": 5}); !errors.Is(err, ErrCSVInvalid) {
		t.Fatalf("mapping outside: %v", err)
	}
	if tbl, err := Parse("a,b\n1,2\n", 0, false); err != nil || len(tbl.Rows) != 2 || tbl.Rows[1][1] != "2" {
		t.Fatalf("comma: %+v %v", tbl, err)
	}
	if _, err := Parse("a\xff", ';', false); !errors.Is(err, ErrCSVInvalid) {
		t.Fatalf("utf8: %v", err)
	}
	if _, err := Parse(strings.Repeat("x;1\n", MaxRows+1), ';', false); !errors.Is(err, ErrCSVInvalid) {
		t.Fatalf("too many rows: %v", err)
	}
	if _, err := Parse("\"unterminated\n", ';', false); !errors.Is(err, ErrCSVInvalid) {
		t.Fatalf("bad quote: %v", err)
	}
}

type fakeHandler struct{ EntityHandler }

func (fakeHandler) Entity() string { return EntityRights }

func TestRegistry(t *testing.T) {
	r := NewRegistry()
	if _, err := r.Handler(EntityRights); !errors.Is(err, ErrUnknownEntity) {
		t.Fatal(err)
	}
	r.Register(fakeHandler{})
	if h, err := r.Handler(EntityRights); err != nil || h.Entity() != EntityRights || len(r.Entities()) != 1 {
		t.Fatalf("%v %v", h, err)
	}
	var rep Report
	rep.Reject(2, "unknown_berth", "berth_code", "A9")
	rep.Duplicate(3, "fiscal_code", "")
	if len(rep.Rejected) != 1 || len(rep.Duplicates) != 1 {
		t.Fatal(rep)
	}
}
