package audit

import (
	"encoding/json"
	"testing"
)

func TestRedact_DropsSensitiveKeys(t *testing.T) {
	in := map[string]any{
		"name":          "Marina",
		"password_hash": "x",
		"Password":      "x",
		"api_token":     "x",
		"webhookSecret": "x",
		"email":         "a@b.c",
		"Phone":         "+39",
		"totp_secret":   "x",
		"code":          "ABCD-EFGH",
		"role":          "office",
		"nested": map[string]any{
			"email": "x", "keep": 1,
			"list": []any{map[string]any{"token": "x", "id": "1"}},
		},
	}
	b, _ := json.Marshal(Redact(in))
	want := `{"name":"Marina","nested":{"keep":1,"list":[{"id":"1"}]},"role":"office"}`
	if string(b) != want {
		t.Fatalf("got %s\nwant %s", b, want)
	}
}

func TestRedact_Structs(t *testing.T) {
	type s struct {
		Email string `json:"email"`
		Role  string `json:"role"`
	}
	b, _ := json.Marshal(Redact(s{Email: "x@y.z", Role: "dock"}))
	if string(b) != `{"role":"dock"}` {
		t.Fatalf("got %s", b)
	}
	if Redact(func() {}) != nil {
		t.Fatal("unmarshalable value must redact to nil")
	}
}
