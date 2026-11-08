package audit

import (
	"encoding/json"
	"testing"
	"time"
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

func TestWithOfflineQueuedAt_StampsEveryReplayRow(t *testing.T) {
	at := time.Date(2027, 6, 5, 9, 41, 0, 0, time.FixedZone("CEST", 2*3600))
	const want = "2027-06-05T07:41:00Z"
	if got := withOfflineQueuedAt(nil, at).(map[string]any)["offline_queued_at"]; got != want {
		t.Fatalf("nil diff: got %v", got)
	}
	m := withOfflineQueuedAt(map[string]any{"state": "active"}, at).(map[string]any)
	if m["offline_queued_at"] != want || m["state"] != "active" {
		t.Fatalf("map diff: got %v", m)
	}
	kept := withOfflineQueuedAt(map[string]any{"offline_queued_at": "domain"}, at).(map[string]any)
	if kept["offline_queued_at"] != "domain" {
		t.Fatalf("domain value overwritten: %v", kept)
	}
	w := withOfflineQueuedAt([]any{"x"}, at).(map[string]any)
	if w["offline_queued_at"] != want || w["diff"] == nil {
		t.Fatalf("wrapped diff: %v", w)
	}
}
