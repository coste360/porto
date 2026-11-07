package docs

import (
	"context"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/store"
)

// DataSource fills the view of a subject for the kinds it owns (m4-plan §6.2, §13.3). It is registered at wiring
// (internal/app) by contract, rights, comms and deadline, so docs never imports them. View runs inside the
// compose transaction and returns the typed struct of docs/views for the kind (QuoteView, ContractView, ...).
type DataSource interface {
	Kinds() []Kind
	View(ctx context.Context, tx bun.IDB, scope store.MarinaScope, kind Kind, subjectType SubjectType, subjectID, locale string) (any, error)
	// DemoView returns the demo data of a kind for template previews (no database access).
	DemoView(kind Kind, locale string) (any, error)
}
