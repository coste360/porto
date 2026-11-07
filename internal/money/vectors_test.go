package money

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestParseCents_SharedVectors: the Go parser agrees with the web MoneyInput parser on testdata/money/parse.json
// (m5-plan §9.2). Vectors above the JavaScript safe integer range are invalid on the web only.
func TestParseCents_SharedVectors(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "testdata", "money", "parse.json"))
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Vectors []struct {
			Input string `json:"input"`
			Cents *int64 `json:"cents"`
		} `json:"vectors"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	const maxSafe = 1<<53 - 1
	for _, v := range doc.Vectors {
		got, err := ParseCents(v.Input)
		switch {
		case v.Cents == nil && err == nil && int64(got) <= maxSafe && int64(got) >= -maxSafe:
			t.Errorf("%q: got %d, want invalid", v.Input, got)
		case v.Cents != nil && (err != nil || int64(got) != *v.Cents):
			t.Errorf("%q: got %d %v, want %d", v.Input, got, err, *v.Cents)
		}
	}
}
