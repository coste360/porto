// Package audit writes the insert-only audit log. Domain services call Writer.Write inside the same transaction
// as the change they record, together with the outbox event.
package audit

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/clock"
	"github.com/coste360/porto/internal/reqctx"
	"github.com/coste360/porto/internal/store"
)

// Actor types.
const (
	ActorUser   = "user"
	ActorAPIKey = "api_key"
	ActorDevice = "device"
	ActorSystem = "system"
)

// Actor is who performed an action.
type Actor struct {
	Type string // user | api_key | device | system
	ID   string
}

// System is the actor of background jobs and CLI bootstrap.
var System = Actor{Type: ActorSystem}

// Entry is one audit record. Diff is any JSON-marshallable value; it is redacted before storage.
type Entry struct {
	OrganizationID string
	MarinaID       string
	Actor          Actor
	Action         string
	EntityType     string
	EntityID       string
	Diff           any
}

// Writer writes audit entries.
type Writer struct {
	clock clock.Clock
}

// NewWriter returns a Writer using c for timestamps.
func NewWriter(c clock.Clock) *Writer { return &Writer{clock: c} }

// Write stores e in tx. Request id and client IP come from the request context.
func (w *Writer) Write(ctx context.Context, tx bun.IDB, e Entry) error {
	if e.Action == "" || e.EntityType == "" {
		return fmt.Errorf("audit: action and entity type are required")
	}
	actorType := e.Actor.Type
	if actorType == "" {
		actorType = ActorSystem
	}
	info := reqctx.From(ctx)
	row := &store.AuditEntry{
		ID:             store.NewID(),
		OrganizationID: e.OrganizationID,
		MarinaID:       e.MarinaID,
		ActorType:      actorType,
		ActorID:        e.Actor.ID,
		Action:         e.Action,
		EntityType:     e.EntityType,
		EntityID:       e.EntityID,
		RequestID:      info.RequestID,
		CreatedAt:      store.UTC(w.clock.Now()),
	}
	if info.ClientIP.IsValid() {
		row.IP = info.ClientIP.String()
	}
	if e.Diff != nil {
		b, err := json.Marshal(Redact(e.Diff))
		if err != nil {
			return fmt.Errorf("audit: marshal diff: %w", err)
		}
		row.Diff = b
	}
	if err := store.InsertAudit(ctx, tx, row); err != nil {
		return fmt.Errorf("audit: write: %w", err)
	}
	return nil
}

// Filter narrows a listing.
type Filter = store.AuditFilter

// List returns the audit entries of a marina, newest first.
func (w *Writer) List(ctx context.Context, db bun.IDB, s store.MarinaScope, f Filter, cursor string, limit int) (store.Page[store.AuditEntry], error) {
	return store.ListAudit(ctx, db, s, f, cursor, limit)
}

// Redact returns a copy of v (as generic JSON) without the keys that may hold personal data or secrets:
// password*, *token*, *secret*, email, phone, totp*, code (case-insensitive, at any depth).
func Redact(v any) any {
	b, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	var generic any
	if err := json.Unmarshal(b, &generic); err != nil {
		return nil
	}
	return redact(generic)
}

func redact(v any) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, val := range t {
			if sensitiveKey(k) {
				continue
			}
			out[k] = redact(val)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, val := range t {
			out[i] = redact(val)
		}
		return out
	default:
		return v
	}
}

func sensitiveKey(k string) bool {
	k = strings.ToLower(k)
	switch {
	case strings.HasPrefix(k, "password"), strings.Contains(k, "token"), strings.Contains(k, "secret"),
		strings.HasPrefix(k, "totp"), k == "email", k == "phone", k == "code":
		return true
	}
	return false
}
