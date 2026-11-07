package contract

import (
	"context"
	"encoding/json"
	"errors"
	"io"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/docs"
	"github.com/coste360/porto/internal/domain/document"
	"github.com/coste360/porto/internal/domain/magiclink"
	"github.com/coste360/porto/internal/domain/schedule"
	"github.com/coste360/porto/internal/money"
	"github.com/coste360/porto/internal/store"
)

// Customer magic-link pages of quotes and renewal proposals (m4-plan §3.2, §7.3.6, INV-75): a token resolves
// only its marina, purpose, subject and customer; every failure is magiclink.ErrInvalid (404 link_invalid). The
// views carry only this customer's data. Signing and unsubscribe are DOCUMENTS' (signature, comms).

// PublicMarina is the marina of a link page.
type PublicMarina struct {
	Name          string
	DefaultLocale string
	Locales       []string
}

// PublicOption is a quote option shown to the customer.
type PublicOption struct {
	ID, Label         string
	Seq               int
	StartsOn, EndsOn  calendar.Date
	Total, Deposit    int64
	EarlyPaymentTotal *int64
	ResourceCodes     []string
}

// PublicQuoteView is the quote page.
type PublicQuoteView struct {
	Code           string
	Status         string
	ValidUntil     calendar.Date
	CustomerName   string
	VesselName     string
	Options        []PublicOption
	RequiredFields []string
	MissingFields  []string
}

// PublicRenewalView is the renewal proposal page.
type PublicRenewalView struct {
	Status            string
	DeadlineOn        calendar.Date
	NewStartsOn       calendar.Date
	NewEndsOn         calendar.Date
	OldTotal          int64
	NewTotal          int64
	EarlyPaymentTotal *int64
	CustomerName      string
	VesselName        string
	ResourceCodes     []string
}

// PublicLinkView is a resolved link with the page of its purpose (quote and renewal; nil for the others).
type PublicLinkView struct {
	Link    *magiclink.Link
	Marina  PublicMarina
	Quote   *PublicQuoteView
	Renewal *PublicRenewalView
}

// PublicDeposit is the deposit instructions after an acceptance.
type PublicDeposit struct {
	Amount int64
	DueOn  calendar.Date
	Bank   *BankDetails
	Ref    string // the contract code, to quote in the transfer
}

// PublicAcceptance is the outcome of a public acceptance.
type PublicAcceptance struct {
	ContractCode   string
	ProposalStatus string
	Deposit        *PublicDeposit
	SignatureReqID string
}

func (s *Service) resolve(ctx context.Context, slug, token, purpose string) (*magiclink.Link, store.MarinaScope, error) {
	if s.links == nil {
		return nil, store.MarinaScope{}, magiclink.ErrInvalid
	}
	l, err := s.links.Resolve(ctx, slug, token)
	if err != nil {
		return nil, store.MarinaScope{}, magiclink.ErrInvalid
	}
	if purpose != "" && l.Purpose != purpose {
		return nil, store.MarinaScope{}, magiclink.ErrInvalid
	}
	scope, err := store.SystemMarinaScope(ctx, s.d.DB.DB, l.MarinaID)
	if err != nil {
		return nil, store.MarinaScope{}, magiclink.ErrInvalid
	}
	return l, scope, nil
}

// ResolvePublicLink resolves a link and builds the quote or renewal page; other purposes return the link only
// (the handler asks the signature and comms services).
func (s *Service) ResolvePublicLink(ctx context.Context, slug, token string) (*PublicLinkView, error) {
	l, scope, err := s.resolve(ctx, slug, token, "")
	if err != nil {
		return nil, err
	}
	db := s.d.DB.DB
	m, err := store.GetMarina(ctx, db, scope)
	if err != nil {
		return nil, magiclink.ErrInvalid
	}
	out := &PublicLinkView{Link: l, Marina: PublicMarina{Name: m.Name, DefaultLocale: m.DefaultLocale, Locales: m.Locales}}
	switch l.Purpose {
	case LinkQuote:
		out.Quote, err = s.publicQuote(ctx, db, scope, l)
	case LinkRenewal:
		out.Renewal, err = s.publicRenewal(ctx, db, scope, l)
	}
	if errors.Is(err, store.ErrNotFound) || errors.Is(err, auth.ErrNotFound) {
		return nil, magiclink.ErrInvalid
	}
	return out, err
}

func customerName(ctx context.Context, db bun.IDB, scope store.MarinaScope, id string) string {
	ps, err := parties(ctx, db, scope, []string{id}, true)
	if err != nil {
		return ""
	}
	if p := partyOf(ps, id); p != nil {
		return p.Name
	}
	return ""
}

func (s *Service) publicQuote(ctx context.Context, db bun.IDB, scope store.MarinaScope, l *magiclink.Link) (*PublicQuoteView, error) {
	q, err := store.GetQuote(ctx, db, scope, l.SubjectID)
	if err != nil || q.CustomerID != l.CustomerID {
		return nil, store.ErrNotFound
	}
	opts, err := store.OptionsOfQuotes(ctx, db, scope, []string{q.ID})
	if err != nil {
		return nil, err
	}
	var optIDs []string
	for _, o := range opts {
		optIDs = append(optIDs, o.ID)
	}
	pls, err := store.PlacementsOfOptions(ctx, db, scope, optIDs)
	if err != nil {
		return nil, err
	}
	var rids []string
	for _, p := range pls {
		rids = append(rids, p.ResourceID)
	}
	refs, err := store.ResourceRefs(ctx, db, scope, rids)
	if err != nil {
		return nil, err
	}
	cs, err := s.settingsTx(ctx, db, scope)
	if err != nil {
		return nil, err
	}
	out := &PublicQuoteView{
		Code: ContractCode(SeriesQuote, q.Year, q.Number), Status: q.Status, ValidUntil: q.ValidUntil,
		CustomerName: customerName(ctx, db, scope, q.CustomerID), RequiredFields: []string{}, MissingFields: []string{},
	}
	if v, err := store.GetVessel(ctx, db, scope, q.VesselID); err == nil {
		out.VesselName = v.Name
	}
	_ = json.Unmarshal(cs.RequiredFields, &out.RequiredFields)
	if out.MissingFields, err = missingFields(ctx, db, scope, out.RequiredFields, q.CustomerID, q.VesselID, q.StartsOn); err != nil {
		return nil, err
	}
	pol, _ := DecodePolicy(q.Policy)
	pay, _, _ := effective(pol, cs)
	today := s.today(ctx, db, scope)
	for _, o := range opts {
		po := PublicOption{ID: o.ID, Label: o.Label, Seq: o.Seq, StartsOn: q.StartsOn, EndsOn: q.EndsOn, Total: o.TotalCents, Deposit: o.DepositCents}
		for _, p := range pls {
			if p.OptionID == o.ID {
				if r, ok := refs[p.ResourceID]; ok {
					po.ResourceCodes = append(po.ResourceCodes, r.Code)
				}
			}
		}
		if q.Kind == KindMooring {
			in := schedule.Input{Total: money.Cents(o.TotalCents), StartsOn: q.StartsOn, EndsOn: q.EndsOn, Today: today, Payment: pay.Planner()}
			if items, err := schedule.Plan(in); err == nil && len(items) > 0 {
				po.Deposit = int64(items[0].Amount)
			}
			if alt, err := schedule.EarlyPaymentPlan(in); err == nil && alt != nil {
				t := int64(alt.Total)
				po.EarlyPaymentTotal = &t
			}
		}
		out.Options = append(out.Options, po)
	}
	return out, nil
}

func (s *Service) publicRenewal(ctx context.Context, db bun.IDB, scope store.MarinaScope, l *magiclink.Link) (*PublicRenewalView, error) {
	pr, err := store.GetRenewalProposal(ctx, db, scope, l.SubjectID)
	if err != nil {
		return nil, err
	}
	src, err := store.GetContract(ctx, db, scope, pr.SourceContractID)
	if err != nil || src.CustomerID != l.CustomerID {
		return nil, store.ErrNotFound
	}
	out := &PublicRenewalView{
		Status: pr.Status, DeadlineOn: pr.DeadlineOn, OldTotal: pr.OldTotalCents, CustomerName: customerName(ctx, db, scope, src.CustomerID),
	}
	if pr.NewTotalCents != nil {
		out.NewTotal = *pr.NewTotalCents
	}
	if v, err := store.GetVessel(ctx, db, scope, src.VesselID); err == nil {
		out.VesselName = v.Name
	}
	if pr.NewContractID != "" {
		succ, err := store.GetContract(ctx, db, scope, pr.NewContractID)
		if err != nil {
			return nil, err
		}
		out.NewStartsOn, out.NewEndsOn = succ.StartsOn, succ.EndsOn
		lines, err := store.LinesOfContracts(ctx, db, scope, []string{succ.ID})
		if err != nil {
			return nil, err
		}
		var rids []string
		for _, ln := range lines {
			if ln.ResourceID != "" {
				rids = append(rids, ln.ResourceID)
			}
		}
		if refs, err := store.ResourceRefs(ctx, db, scope, rids); err == nil {
			for _, id := range rids {
				if r, ok := refs[id]; ok {
					out.ResourceCodes = append(out.ResourceCodes, r.Code)
				}
			}
		}
		cs, err := s.settingsTx(ctx, db, scope)
		if err != nil {
			return nil, err
		}
		pol, _ := DecodePolicy(succ.Policy)
		pay, _, _ := effective(pol, cs)
		in := schedule.Input{Total: money.Cents(succ.TotalCents), StartsOn: succ.StartsOn, EndsOn: succ.EndsOn, Today: s.today(ctx, db, scope), Payment: pay.Planner()}
		if alt, err := schedule.EarlyPaymentPlan(in); err == nil && alt != nil {
			t := int64(alt.Total)
			out.EarlyPaymentTotal = &t
		}
	}
	return out, nil
}

// PublicLinkDocument returns the PDF of a quote or renewal link (docs.ErrNotReady while it renders).
func (s *Service) PublicLinkDocument(ctx context.Context, slug, token string) ([]byte, error) {
	l, scope, err := s.resolve(ctx, slug, token, "")
	if err != nil {
		return nil, err
	}
	var kind docs.Kind
	switch l.Purpose {
	case LinkQuote:
		kind = docs.KindQuote
	case LinkRenewal:
		kind = docs.KindRenewalProposal
	default:
		return nil, magiclink.ErrInvalid
	}
	if s.d.Docs == nil {
		return nil, docs.ErrNotReady
	}
	row, err := store.LatestDocumentOf(ctx, s.d.DB.DB, scope, l.SubjectType, l.SubjectID, string(kind), false)
	if err != nil || row == nil {
		return nil, docs.ErrNotReady
	}
	b, _, err := s.d.Docs.DocumentPDF(ctx, s.d.DB.DB, scope, row.ID)
	return b, err
}

// linkActor is the actor of a customer's action through a link (audited as the system: no user).
var linkActor = system

// AcceptPublicQuote accepts an option through a quote link (m4-plan §3.2): the same transaction as the panel
// acceptance (acceptTx, via link), so the contract is created with its plan and, with a signature driver, awaits
// the customer's signature.
func (s *Service) AcceptPublicQuote(ctx context.Context, slug, token, optionID, plan string, data PublicData) (*PublicAcceptance, error) {
	l, scope, err := s.resolve(ctx, slug, token, LinkQuote)
	if err != nil {
		return nil, err
	}
	if err := s.requireAllocator(); err != nil {
		return nil, err
	}
	if _, err := s.quoteInsurance(ctx, scope, l.SubjectID); err != nil {
		return nil, err
	}
	var res *AcceptResult
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		q, err := lockQuote(ctx, tx, scope, l.SubjectID, 0)
		if err != nil {
			return magiclink.ErrInvalid
		}
		if q.CustomerID != l.CustomerID || q.Kind != KindMooring {
			return magiclink.ErrInvalid
		}
		cs, err := s.settingsTx(ctx, tx, scope)
		if err != nil {
			return err
		}
		var required []string
		_ = json.Unmarshal(cs.RequiredFields, &required)
		if err := s.savePublicDataTx(ctx, tx, scope, required, q.CustomerID, q.VesselID, q.StartsOn, data); err != nil {
			return err
		}
		res, err = s.acceptTx(ctx, tx, scope, linkActor, q, AcceptInput{OptionID: optionID, Plan: plan}, ViaLink)
		return err
	})
	if err != nil {
		return nil, err
	}
	c, err := store.GetContract(ctx, s.d.DB.DB, scope, res.Contract.Row.ID)
	if err != nil {
		return nil, err
	}
	out := &PublicAcceptance{ContractCode: Code(c), SignatureReqID: res.SignatureRequestID}
	out.Deposit, err = s.depositOf(ctx, scope, c)
	return out, err
}

// depositOf returns the deposit instructions of a contract: its first live instalment with the bank details.
func (s *Service) depositOf(ctx context.Context, scope store.MarinaScope, c *store.Contract) (*PublicDeposit, error) {
	live, err := store.InstallmentsOfContracts(ctx, s.d.DB.DB, scope, []string{c.ID}, true)
	if err != nil || len(live) == 0 {
		return nil, err
	}
	cs, err := s.settingsTx(ctx, s.d.DB.DB, scope)
	if err != nil {
		return nil, err
	}
	return &PublicDeposit{Amount: live[0].AmountCents, DueOn: live[0].DueOn, Bank: s.bankDetails(cs), Ref: Code(c)}, nil
}

// AcceptPublicRenewal accepts a proposal through its link.
func (s *Service) AcceptPublicRenewal(ctx context.Context, slug, token, plan string) (*PublicAcceptance, error) {
	return s.onPublicProposal(ctx, slug, token, func(ctx context.Context, tx bun.IDB, scope store.MarinaScope, pr *store.RenewalProposal) (string, error) {
		return s.acceptProposalTx(ctx, tx, scope, linkActor, pr, plan, ViaLink)
	})
}

// DeclinePublicRenewal declines a proposal through its link (the hold is released at once).
func (s *Service) DeclinePublicRenewal(ctx context.Context, slug, token, reason string) (*PublicAcceptance, error) {
	reason, ok := cleanText(reason, 500)
	if !ok {
		v := &auth.ValidationError{}
		v.Add("reason", "too_long")
		return nil, v
	}
	return s.onPublicProposal(ctx, slug, token, func(ctx context.Context, tx bun.IDB, scope store.MarinaScope, pr *store.RenewalProposal) (string, error) {
		return "", s.declineProposalTx(ctx, tx, scope, linkActor, pr, reason, ViaLink)
	})
}

func (s *Service) onPublicProposal(ctx context.Context, slug, token string,
	fn func(ctx context.Context, tx bun.IDB, scope store.MarinaScope, pr *store.RenewalProposal) (string, error),
) (*PublicAcceptance, error) {
	l, scope, err := s.resolve(ctx, slug, token, LinkRenewal)
	if err != nil {
		return nil, err
	}
	if err := s.requireAllocator(); err != nil {
		return nil, err
	}
	out := &PublicAcceptance{}
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		pr, err := store.LockRenewalProposal(ctx, tx, scope, l.SubjectID)
		if err != nil {
			return magiclink.ErrInvalid
		}
		src, err := store.GetContract(ctx, tx, scope, pr.SourceContractID)
		if err != nil || src.CustomerID != l.CustomerID {
			return magiclink.ErrInvalid
		}
		reqID, err := fn(ctx, tx, scope, pr)
		if err != nil {
			return err
		}
		out.SignatureReqID, out.ProposalStatus = reqID, pr.Status
		return nil
	})
	if err != nil {
		return nil, err
	}
	pr, err := store.GetRenewalProposal(ctx, s.d.DB.DB, scope, l.SubjectID)
	if err != nil {
		return nil, err
	}
	out.ProposalStatus = pr.Status
	if pr.NewContractID != "" && pr.Status != string(ProposalDeclined) {
		if c, err := store.GetContract(ctx, s.d.DB.DB, scope, pr.NewContractID); err == nil {
			out.ContractCode = Code(c)
			out.Deposit, err = s.depositOf(ctx, scope, c)
			if err != nil {
				return nil, err
			}
		}
	}
	return out, nil
}

// Owners of an uploaded document.
const (
	ownerCustomer = "customer"
	ownerVessel   = "vessel"
)

// UploadPublicDocument stores a document the customer uploads through a quote or renewal link (insurance
// policy on the vessel, identity documents on the customer; M1 documents, encrypted). It runs with a principal
// limited to customers.write on the link's marina; the audit records it as an API action of the link.
func (s *Service) UploadPublicDocument(ctx context.Context, slug, token, docType string, expiresOn calendar.Date, contentType, fileName string, body io.Reader) (string, error) {
	if s.d.Documents == nil {
		return "", magiclink.ErrInvalid
	}
	l, scope, err := s.resolve(ctx, slug, token, "")
	if err != nil {
		return "", err
	}
	db := s.d.DB.DB
	var customerID, vesselID string
	switch l.Purpose {
	case LinkQuote:
		q, err := store.GetQuote(ctx, db, scope, l.SubjectID)
		if err != nil || q.CustomerID != l.CustomerID {
			return "", magiclink.ErrInvalid
		}
		customerID, vesselID = q.CustomerID, q.VesselID
	case LinkRenewal:
		pr, err := store.GetRenewalProposal(ctx, db, scope, l.SubjectID)
		if err != nil {
			return "", magiclink.ErrInvalid
		}
		c, err := store.GetContract(ctx, db, scope, pr.SourceContractID)
		if err != nil || c.CustomerID != l.CustomerID {
			return "", magiclink.ErrInvalid
		}
		customerID, vesselID = c.CustomerID, c.VesselID
	default:
		return "", magiclink.ErrInvalid
	}
	in := document.Input{OwnerType: ownerCustomer, CustomerID: customerID, Type: docType, ExpiresOn: expiresOn}
	if docType == "insurance_policy" && vesselID != "" {
		in = document.Input{OwnerType: ownerVessel, VesselID: vesselID, Type: docType, ExpiresOn: expiresOn}
	}
	p := &auth.Principal{
		Kind: auth.KindAPIKey, Grants: []auth.Grant{{OrganizationID: scope.OrgID(), MarinaID: scope.ID()}},
		Scopes: []auth.Permission{auth.PermCustomersWrite},
	}
	d, err := s.d.Documents.Create(ctx, p, scope.ID(), in)
	if err != nil {
		return "", err
	}
	if _, err := s.d.Documents.UploadFile(ctx, p, scope.ID(), d.Row.ID, contentType, fileName, body); err != nil {
		return "", err
	}
	return d.Row.ID, nil
}

// DepositOfSigner returns the deposit instructions of the contract a customer just signed through a signature
// link (nil when the link's request is not a contract's, or the contract has no live instalment). The public
// signing page calls it after a successful verification with the link resolved before it (a completed request
// revokes its links).
func (s *Service) DepositOfSigner(ctx context.Context, l *magiclink.Link) (*PublicDeposit, error) {
	if l == nil || l.Purpose != magiclink.PurposeSignature {
		return nil, nil
	}
	db := s.d.DB.DB
	scope, err := store.SystemMarinaScope(ctx, db, l.MarinaID)
	if err != nil {
		return nil, err
	}
	sg, err := store.GetSignatureSigner(ctx, db, scope, l.SubjectID)
	if err != nil {
		return nil, nil //nolint:nilerr // not a signer of this marina: no deposit to show
	}
	req, err := store.GetSignatureRequest(ctx, db, scope, sg.RequestID)
	if err != nil || req.SubjectType != SubjectContract {
		return nil, nil //nolint:nilerr // not a contract signature: no deposit
	}
	c, err := store.GetContract(ctx, db, scope, req.SubjectID)
	if err != nil || c.CustomerID != l.CustomerID {
		return nil, nil //nolint:nilerr // only the contract's own customer sees its deposit
	}
	return s.depositOf(ctx, scope, c)
}
