package deadline

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/notify"
	"github.com/coste360/porto/internal/store"
)

// Lead-day notifications (m4-plan §6.8): for each open deadline and each lead day L of its kind with
// today ≥ due_on − L not yet notified, one email per channel (customer_email to the customer, office_email to the
// contract settings office address; panel sends nothing), deduplicated by deadline:<id>:<L>:<channel>; the
// notified lead days are recorded in the same transaction as the enqueue. When several lead days are due at once
// (a deadline created late) one notification covers them.

//nolint:misspell // Italian labels of the notifications
var kindLabels = map[string]map[string]string{
	"it": {
		KindIdentityDocument: "Documento d'identità", KindBoatingLicence: "Patente nautica", KindNavigationLicence: "Licenza di navigazione",
		KindRegistrationCertificate: "Certificato d'iscrizione", KindInsurance: "Assicurazione", KindSafetyCertificate: "Certificato di sicurezza",
		KindPowerDeclaration: "Dichiarazione di potenza", KindOtherDocument: "Documento", KindContractEnd: "Fine contratto",
		KindInstallmentDue: "Scadenza rata", KindHolderInstallmentDue: "Scadenza rata titolare", KindRightEnd: "Fine diritto d'uso",
		KindMandateEnd: "Fine mandato di gestione", KindConcessionExpiry: "Scadenza concessione", KindConcessionObligation: "Adempimento concessione",
		KindRenewalDeadline: "Termine proposta di rinnovo", KindTacitRenewalBlocked: "Rinnovo tacito bloccato", KindWaitlistOffer: "Posto per lista d'attesa",
		KindDocumentFailed: "Documento non generato", KindSignatureExpiring: "Firma in scadenza",
		KindMaintenanceDue: "Manutenzione in scadenza",
	},
	"en": {
		KindIdentityDocument: "Identity document", KindBoatingLicence: "Boating licence", KindNavigationLicence: "Navigation licence",
		KindRegistrationCertificate: "Registration certificate", KindInsurance: "Insurance", KindSafetyCertificate: "Safety certificate",
		KindPowerDeclaration: "Power declaration", KindOtherDocument: "Document", KindContractEnd: "Contract end",
		KindInstallmentDue: "Instalment due", KindHolderInstallmentDue: "Holder instalment due", KindRightEnd: "Berth right end",
		KindMandateEnd: "Management mandate end", KindConcessionExpiry: "Concession expiry", KindConcessionObligation: "Concession obligation",
		KindRenewalDeadline: "Renewal proposal deadline", KindTacitRenewalBlocked: "Tacit renewal blocked", KindWaitlistOffer: "Berth for the waiting list",
		KindDocumentFailed: "Document not generated", KindSignatureExpiring: "Signature expiring",
		KindMaintenanceDue: "Maintenance due",
	},
}

// KindLabel is the localised label of a kind.
func KindLabel(kind, locale string) string {
	if locale != "en" {
		locale = "it"
	}
	if l := kindLabels[locale][kind]; l != "" {
		return l
	}
	return kind
}

var errRace = errors.New("deadline: notified by another run")

func formatDate(d calendar.Date, locale string) string {
	if locale == "en" {
		return d.String()
	}
	return fmt.Sprintf("%02d/%02d/%04d", d.Day(), int(d.Month()), d.Year())
}

// NotifyMarina sends the due lead-day notifications of a marina and returns how many deadlines were notified.
func (s *Service) NotifyMarina(ctx context.Context, scope store.MarinaScope) (int, error) {
	db := s.d.DB.DB
	pol, err := loadPolicies(ctx, db, scope)
	if err != nil {
		return 0, err
	}
	loc := s.location(ctx, db, scope)
	today := calendar.In(s.d.Clock.Now(), loc)
	maxLead := 0
	for _, l := range pol.leads {
		maxLead = max(maxLead, maxOf(l))
	}
	rows, err := store.OpenDeadlinesDue(ctx, db, scope, today.AddDays(maxLead))
	if err != nil {
		return 0, err
	}
	m, err := store.GetMarina(ctx, db, scope)
	if err != nil {
		return 0, err
	}
	office := ""
	if cs, err := store.GetContractSettings(ctx, db, scope, s.now()); err == nil && cs.OfficeEmailEnc != "" && s.d.Keys != nil {
		office, _ = s.d.Keys.DecryptString(cs.OfficeEmailEnc, "office-email:"+scope.ID())
	}
	sent := 0
	for i := range rows {
		d := &rows[i]
		var notified []int
		_ = json.Unmarshal(d.NotifiedAt, &notified)
		var due []int
		for _, l := range pol.leads[d.Kind] {
			if !today.Before(d.DueOn.AddDays(-l)) && !slices.Contains(notified, l) {
				due = append(due, l)
			}
		}
		if len(due) == 0 {
			continue
		}
		lead := slices.Min(due)
		err := s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
			for _, ch := range pol.channels[d.Kind] {
				if err := s.notifyChannel(ctx, tx, scope, m, d, ch, lead, office); err != nil {
					return err
				}
			}
			ok, err := store.SetDeadlineNotified(ctx, tx, scope, d.ID, d.Version, store.MustJSON(append(notified, due...)), s.now())
			if err != nil {
				return err
			}
			if !ok {
				return errRace
			}
			return nil
		})
		if errors.Is(err, errRace) {
			continue
		}
		if err != nil {
			return sent, err
		}
		sent++
	}
	return sent, nil
}

func (s *Service) notifyChannel(ctx context.Context, tx bun.IDB, scope store.MarinaScope, m *store.Marina, d *store.Deadline, channel string, lead int, office string) error {
	if s.d.Notify == nil || channel == ChannelPanel {
		return nil
	}
	values := map[string]string{notify.ValueMarinaName: m.Name}
	mail := notify.Mail{
		MarinaID: scope.ID(), DedupeKey: "deadline:" + d.ID + ":" + strconv.Itoa(lead) + ":" + channel,
		SourceType: "deadline", SourceID: d.ID, Values: values,
	}
	switch channel {
	case ChannelCustomerEmail:
		if d.CustomerID == "" {
			return nil
		}
		c, err := store.GetCustomer(ctx, tx, scope, d.CustomerID)
		if err != nil {
			return nil
		}
		to := firstEmail(c.Emails)
		if to == "" {
			return nil
		}
		mail.Template, mail.To, mail.Locale, mail.CustomerID, mail.Category = notify.TemplateDeadlineNoticeCustomer, to, c.Locale, c.ID, notify.CategoryService
		values[notify.ValueCustomerName] = displayName(c)
	case ChannelOfficeEmail:
		if office == "" {
			return nil
		}
		mail.Template, mail.To, mail.Locale = notify.TemplateDeadlineNoticeOffice, office, m.DefaultLocale
	default:
		return nil
	}
	loc := mail.Locale
	values[notify.ValueDeadlineKind] = KindLabel(d.Kind, loc)
	values[notify.ValueDueOn] = formatDate(d.DueOn, loc)
	_, _, err := s.d.Notify.QueueMailTx(ctx, tx, mail)
	return err
}

func firstEmail(raw []byte) string {
	var list []struct {
		Value string `json:"value"`
	}
	if json.Unmarshal(raw, &list) != nil {
		return ""
	}
	for _, e := range list {
		if e.Value != "" {
			return e.Value
		}
	}
	return ""
}

func displayName(c *store.Customer) string {
	if c.Kind == "company" && c.CompanyName != "" {
		return c.CompanyName
	}
	n := c.FirstName
	if c.LastName != "" {
		if n != "" {
			n += " "
		}
		n += c.LastName
	}
	return n
}

// RunNotify sends the lead-day notifications of every marina (job deadlines.notify).
func (s *Service) RunNotify(ctx context.Context) error {
	ids, err := store.ActiveMarinaIDs(ctx, s.d.DB.DB)
	if err != nil {
		return err
	}
	var first error
	for _, id := range ids {
		scope, err := store.SystemMarinaScope(ctx, s.d.DB.DB, id)
		if err != nil {
			continue
		}
		if _, err := s.NotifyMarina(ctx, scope); err != nil && first == nil {
			first = err
		}
	}
	return first
}

// EmailEnabled reports whether the service can send the email channels (Deps.Notify wired).
func (s *Service) EmailEnabled() bool { return s.d.Notify != nil }
