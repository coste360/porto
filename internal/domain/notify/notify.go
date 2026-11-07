// Package notify queues and delivers emails (spec 28). Messages are written to message_log in the caller's
// transaction (so a mail exists if and only if the change that caused it committed) and delivered by a job with
// lease, retries and backoff. Recipient and render inputs are encrypted at rest; logs carry only a recipient hash.
package notify

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/clock"
	"github.com/coste360/porto/internal/crypto"
	"github.com/coste360/porto/internal/store"
)

// Delivery policy.
const (
	deliveryLease = 2 * time.Minute
	maxAttempts   = 8
	retryBase     = time.Minute
	retryCeiling  = time.Hour
)

// Service queues and delivers emails.
type Service struct {
	db           *store.DB
	clock        clock.Clock
	keys         *crypto.KeyRing
	transport    Transport
	baseURL      string
	log          *slog.Logger
	recipientKey []byte
	wake         chan struct{}

	attachMu sync.RWMutex
	attach   AttachmentLoader
}

// NewService returns a Service. baseURL is PORTO_BASE_URL (links in mails).
func NewService(db *store.DB, c clock.Clock, keys *crypto.KeyRing, t Transport, baseURL string, log *slog.Logger) *Service {
	rk, err := keys.Derive("notify.recipient", 32)
	if err != nil {
		panic(fmt.Sprintf("notify: derive recipient key: %v", err)) // only with an empty key ring: programming error
	}
	return &Service{
		db: db, clock: c, keys: keys, transport: t, baseURL: strings.TrimRight(baseURL, "/"), log: log,
		recipientKey: rk, wake: make(chan struct{}, 1),
	}
}

// Wake is signalled after a commit that queued a message.
func (s *Service) Wake() <-chan struct{} { return s.wake }

func (s *Service) kick() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

// RecipientHash is the keyed hash used to find and log recipients without storing them in clear.
func (s *Service) RecipientHash(email string) string {
	h := hmac.New(sha256.New, s.recipientKey)
	h.Write([]byte(strings.ToLower(strings.TrimSpace(email))))
	return hex.EncodeToString(h.Sum(nil))
}

// PasswordResetMail asks for a password reset email.
type PasswordResetMail struct {
	UserID, Email, Name, Locale, Token string
	ExpiresAt                          time.Time
}

// InvitationMail asks for an invitation email.
type InvitationMail struct {
	UserID, Email, Name, Locale, Token, MarinaID, MarinaName, InviterName string
	ExpiresAt                                                             time.Time
}

// ResetLink is the link of a reset mail. The token travels in the fragment, so it never reaches server logs.
func (s *Service) ResetLink(token string) string {
	return s.baseURL + "/admin/reset-password#token=" + url.QueryEscape(token)
}

// InvitationLink is the link of an invitation mail (same page, different copy).
func (s *Service) InvitationLink(token string) string {
	return s.ResetLink(token) + "&invite=1"
}

// EnqueuePasswordReset queues a reset mail in tx.
func (s *Service) EnqueuePasswordReset(ctx context.Context, tx bun.IDB, m PasswordResetMail) error {
	return s.enqueue(ctx, tx, enqueueInput{
		template: TemplatePasswordReset, userID: m.UserID, email: m.Email, locale: m.Locale,
		dedupe: TemplatePasswordReset + ":" + crypto.SHA256Hex(m.Token),
		data: RenderData{
			Name: m.Name, Link: s.ResetLink(m.Token), ExpiresAt: formatInstant(m.ExpiresAt, m.Locale),
		},
	})
}

// EnqueueInvitation queues an invitation mail in tx.
func (s *Service) EnqueueInvitation(ctx context.Context, tx bun.IDB, m InvitationMail) error {
	return s.enqueue(ctx, tx, enqueueInput{
		template: TemplateInvitation, userID: m.UserID, marinaID: m.MarinaID, email: m.Email, locale: m.Locale,
		dedupe: TemplateInvitation + ":" + crypto.SHA256Hex(m.Token),
		data: RenderData{
			Name: m.Name, Link: s.InvitationLink(m.Token), ExpiresAt: formatInstant(m.ExpiresAt, m.Locale),
			MarinaName: m.MarinaName, InviterName: m.InviterName,
		},
	})
}

type enqueueInput struct {
	template, userID, marinaID, email, locale, dedupe string
	data                                              RenderData
}

func (s *Service) enqueue(ctx context.Context, tx bun.IDB, in enqueueInput) error {
	if in.email == "" {
		return errors.New("notify: recipient is required")
	}
	id := store.NewID()
	recipientEnc, err := s.keys.EncryptString(in.email, "notify.recipient:"+id)
	if err != nil {
		return fmt.Errorf("notify: encrypt recipient: %w", err)
	}
	payload, err := json.Marshal(in.data)
	if err != nil {
		return err
	}
	payloadEnc, err := s.keys.Encrypt(payload, []byte("notify.payload:"+id))
	if err != nil {
		return fmt.Errorf("notify: encrypt payload: %w", err)
	}
	now := store.UTC(s.clock.Now())
	row := &store.Message{
		ID: id, MarinaID: in.marinaID, UserID: in.userID, Channel: "email", Template: in.template,
		Locale: normalizeLocale(in.locale), RecipientHash: s.RecipientHash(in.email), RecipientEnc: recipientEnc,
		PayloadEnc: payloadEnc, DedupeKey: in.dedupe, Status: store.MessageQueued, NextAttemptAt: now,
		CreatedAt: now, UpdatedAt: now,
	}
	if _, err := store.InsertMessage(ctx, tx, row); err != nil {
		return fmt.Errorf("notify: queue %s: %w", in.template, err)
	}
	store.AfterCommit(ctx, s.kick)
	return nil
}

// DeliverDue sends up to limit due messages and returns how many were sent.
func (s *Service) DeliverDue(ctx context.Context, limit int) (int, error) {
	now := store.UTC(s.clock.Now())
	rows, err := store.ClaimMessages(ctx, s.db.DB, now, deliveryLease, limit)
	if err != nil {
		return 0, fmt.Errorf("notify: claim: %w", err)
	}
	sent := 0
	for i := range rows {
		row := &rows[i]
		msg, err := s.prepare(ctx, row)
		if err == nil {
			var providerID string
			providerID, err = s.transport.Send(ctx, msg)
			if err == nil {
				if err := store.MarkMessageSent(ctx, s.db.DB, row.ID, providerID, store.UTC(s.clock.Now())); err != nil {
					return sent, fmt.Errorf("notify: mark sent: %w", err)
				}
				sent++
				s.log.InfoContext(ctx, "email sent", "message_id", row.ID, "template", row.Template,
					"recipient_hash", row.RecipientHash)
				continue
			}
		}
		attempts := row.Attempts + 1
		var next time.Time
		if attempts < maxAttempts {
			next = store.UTC(s.clock.Now().Add(retryDelay(attempts)))
		}
		// The error text may quote the address (SMTP replies): it goes to message_log, not to the log.
		s.log.WarnContext(ctx, "email delivery failed", "message_id", row.ID, "template", row.Template,
			"recipient_hash", row.RecipientHash, "attempts", attempts, "gave_up", next.IsZero())
		if err := store.MarkMessageRetry(ctx, s.db.DB, row.ID, err.Error(), next, store.UTC(s.clock.Now())); err != nil {
			return sent, fmt.Errorf("notify: mark retry: %w", err)
		}
	}
	return sent, nil
}

// retryDelay is 1 min · 2^(n−1), capped at 1 h, for the n-th failed attempt.
func retryDelay(failed int) time.Duration {
	d := retryBase
	for i := 1; i < failed; i++ {
		d *= 2
		if d >= retryCeiling {
			return retryCeiling
		}
	}
	return d
}

func (s *Service) prepare(ctx context.Context, row *store.Message) (Message, error) {
	to, err := s.keys.DecryptString(row.RecipientEnc, "notify.recipient:"+row.ID)
	if err != nil {
		return Message{}, fmt.Errorf("notify: decrypt recipient: %w", err)
	}
	payload, err := s.keys.Decrypt(row.PayloadEnc, []byte("notify.payload:"+row.ID))
	if err != nil {
		return Message{}, fmt.Errorf("notify: decrypt payload: %w", err)
	}
	var data RenderData
	if err := json.Unmarshal(payload, &data); err != nil {
		return Message{}, fmt.Errorf("notify: payload: %w", err)
	}
	r, err := Render(row.Template, row.Locale, data)
	if err != nil {
		return Message{}, err
	}
	atts, err := s.loadAttachments(ctx, row.MarinaID, data.Attachments)
	if err != nil {
		return Message{}, err
	}
	return Message{
		ID: row.ID, Template: row.Template, Locale: row.Locale, To: to, RecipientHash: row.RecipientHash,
		Subject: r.Subject, Text: r.Text, HTML: r.HTML, Date: s.clock.Now().UTC(), Attachments: atts,
		Headers: data.Headers,
	}, nil
}

// formatInstant formats an expiry for a mail body (UTC: account mails have no marina time zone).
func formatInstant(t time.Time, locale string) string {
	if normalizeLocale(locale) == "en" {
		return t.UTC().Format("2006-01-02 15:04 UTC")
	}
	return t.UTC().Format("02/01/2006 15:04 UTC")
}
