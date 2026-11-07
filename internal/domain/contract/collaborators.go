package contract

import (
	"context"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/domain/docs"
	"github.com/coste360/porto/internal/domain/magiclink"
	"github.com/coste360/porto/internal/domain/notify"
	"github.com/coste360/porto/internal/domain/signature"
	"github.com/coste360/porto/internal/store"
)

// The M4 collaborators of the service (m4-plan §2, §13.3): documents, signature requests, customer links and
// emails. They are narrow interfaces satisfied by the docs, signature and magiclink services (wired from Deps)
// so the unit and integration tests of this package can substitute fakes. A nil collaborator disables the step
// that needs it (no document, no signature request, no link, no email) and the flows keep working.

// documentComposer composes generated documents in the caller's transaction and renders them after commit.
type documentComposer interface {
	ComposeTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, req docs.ComposeRequest) (string, error)
	RenderPending(ctx context.Context, documentID string) error
}

// signatureOpener opens and cancels the signature request of a subject in the caller's transaction.
type signatureOpener interface {
	OpenTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, req signature.OpenRequest) (string, error)
	CancelTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, subjectType, subjectID string) error
}

// linkIssuer creates and resolves the customer magic links.
type linkIssuer interface {
	CreateTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, purpose, subjectType, subjectID, customerID string, expiresAt time.Time) (token, id string, err error)
	Resolve(ctx context.Context, slug, token string) (*magiclink.Link, error)
	URL(slug, purpose, token string) string
}

// Customer email templates queued by the contract service (m4-plan §6.7; the templates are notify's).
const (
	MailQuote                     = "quote"
	MailContractActivated         = "contract_activated"
	MailRenewalProposal           = "renewal_proposal"
	MailRenewalTacitNotice        = "renewal_tacit_notice"
	MailInstallmentPaymentRequest = "installment_payment_request"
	MailInstallmentReminder       = "installment_reminder"
)

// Mail is one customer email. Values are display values for the template (codes, amounts, dates, the link);
// they are encrypted in the message payload by the mailer and never logged.
type Mail struct {
	Template    string
	CustomerID  string
	To          string // the customer's first email (resolved by the contract service)
	Locale      string
	DedupeKey   string // unique per logical email (message_log dedupe)
	Values      map[string]string
	DocumentIDs []string // generated_documents attached (rendered at send time)
	SourceType  string   // contract, quote, installment, renewal_proposal
	SourceID    string
}

// Mailer queues a customer email inside the caller's transaction (notify, with the customer message registry);
// a duplicate dedupe key is not an error and returns the existing message id.
type Mailer interface {
	QueueCustomerMailTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, m Mail) (messageID string, err error)
}

// notifyMailer queues the contract emails through notify (QueueMailTx, m4-plan §6.7).
type notifyMailer struct{ n *notify.Service }

func (m notifyMailer) QueueCustomerMailTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, mail Mail) (string, error) {
	refs := make([]notify.AttachmentRef, 0, len(mail.DocumentIDs))
	for _, id := range mail.DocumentIDs {
		refs = append(refs, notify.AttachmentRef{DocumentID: id})
	}
	id, _, err := m.n.QueueMailTx(ctx, tx, notify.Mail{
		Template: mail.Template, MarinaID: scope.ID(), CustomerID: mail.CustomerID, To: mail.To, Locale: mail.Locale,
		DedupeKey: mail.DedupeKey, SourceType: mail.SourceType, SourceID: mail.SourceID, Values: mail.Values,
		Attachments: refs,
	})
	return id, err
}
