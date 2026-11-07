// Package views holds the typed merge data of every print template kind (spec 15.1; m4-plan §6.2). The field
// catalogue a template is validated against (docs.Fields) is derived by reflection from these structs: the doc
// tag is "name,description_key" (the key is under docsEnums.fields in the web i18n). Data sources (contract,
// rights, comms, deadline) fill them inside the compose transaction; the JSON snapshot is stored encrypted.
//
// The structs are frozen for M4 (CONTRACT): a builder that needs a field asks the orchestrator. Names never carry
// secrets; personal data appear only because the document is addressed to that customer.
//
// Field types for the catalogue: string → text, Notes → notes (newlines kept as hard breaks), int → int,
// money.Cents → cents, Cm → cm, BP → bp, Millis → millis, calendar.Date → date, time.Time → datetime, bool →
// bool, slices → list, structs → object.
package views

import (
	"time"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/money"
)

// Cm is a length in centimetres (filter meters prints "11,34 m").
type Cm int64

// BP is a percentage in basis points (filter bp prints "30 %").
type BP int64

// Millis is a share in thousandths (filter millis prints "333,334 ‰").
type Millis int64

// Notes is free text whose newlines become hard line breaks (other values have them collapsed to spaces).
type Notes string

// Marina is the issuing marina (header data).
type Marina struct {
	Name                string        `json:"name" doc:"name,fields.marina.name"`
	LegalName           string        `json:"legal_name" doc:"legal_name,fields.marina.legal_name"`
	Address             string        `json:"address" doc:"address,fields.marina.address"`
	VATNumber           string        `json:"vat_number" doc:"vat_number,fields.marina.vat_number"`
	FiscalCode          string        `json:"fiscal_code" doc:"fiscal_code,fields.marina.fiscal_code"`
	Email               string        `json:"email" doc:"email,fields.marina.email"`
	Phone               string        `json:"phone" doc:"phone,fields.marina.phone"`
	Website             string        `json:"website" doc:"website,fields.marina.website"`
	ConcessionExpiresOn calendar.Date `json:"concession_expires_on" doc:"concession_expires_on,fields.marina.concession_expires_on"`
	Bank                Bank          `json:"bank" doc:"bank,fields.marina.bank"`
}

// Bank are the bank details for transfers (contract_settings.bank_details).
type Bank struct {
	IBAN   string `json:"iban" doc:"iban,fields.bank.iban"`
	Holder string `json:"holder" doc:"holder,fields.bank.holder"`
	Bank   string `json:"bank" doc:"bank,fields.bank.bank"`
	BIC    string `json:"bic" doc:"bic,fields.bank.bic"`
}

// Party is a customer (person or company) as printed on a document.
type Party struct {
	Code        string `json:"code" doc:"code,fields.party.code"`
	Kind        string `json:"kind" doc:"kind,fields.party.kind"` // person, company
	DisplayName string `json:"display_name" doc:"display_name,fields.party.display_name"`
	FirstName   string `json:"first_name" doc:"first_name,fields.party.first_name"`
	LastName    string `json:"last_name" doc:"last_name,fields.party.last_name"`
	CompanyName string `json:"company_name" doc:"company_name,fields.party.company_name"`
	FiscalCode  string `json:"fiscal_code" doc:"fiscal_code,fields.party.fiscal_code"`
	VATNumber   string `json:"vat_number" doc:"vat_number,fields.party.vat_number"`
	Address     string `json:"address" doc:"address,fields.party.address"`
	Email       string `json:"email" doc:"email,fields.party.email"`
	Phone       string `json:"phone" doc:"phone,fields.party.phone"`
}

// Vessel is a vessel as printed on a document.
type Vessel struct {
	Name         string `json:"name" doc:"name,fields.vessel.name"`
	Registration string `json:"registration" doc:"registration,fields.vessel.registration"`
	Flag         string `json:"flag" doc:"flag,fields.vessel.flag"`
	HullType     string `json:"hull_type" doc:"hull_type,fields.vessel.hull_type"`
	LOA          Cm     `json:"loa_cm" doc:"loa,fields.vessel.loa"`
	Beam         Cm     `json:"beam_cm" doc:"beam,fields.vessel.beam"`
	Draft        Cm     `json:"draft_cm" doc:"draft,fields.vessel.draft"`
}

// Place is a berth, a dynamic area extent or a land resource of a contract.
type Place struct {
	Code       string        `json:"code" doc:"code,fields.place.code"`
	Type       string        `json:"type" doc:"type,fields.place.type"` // berth, dynamic_area, land_resource
	Structure  string        `json:"structure" doc:"structure,fields.place.structure"`
	Map        string        `json:"map" doc:"map,fields.place.map"`
	Length     Cm            `json:"length_cm" doc:"length,fields.place.length"`
	Width      Cm            `json:"width_cm" doc:"width,fields.place.width"`
	FirstNight calendar.Date `json:"first_night" doc:"first_night,fields.place.first_night"`
	LastNight  calendar.Date `json:"last_night" doc:"last_night,fields.place.last_night"`
}

// PriceLine is a line of a price breakdown.
type PriceLine struct {
	Description string      `json:"description" doc:"description,fields.price_line.description"`
	Qty         Millis      `json:"qty_milli" doc:"qty,fields.price_line.qty"`
	Unit        string      `json:"unit" doc:"unit,fields.price_line.unit"`
	UnitPrice   money.Cents `json:"unit_price_cents" doc:"unit_price,fields.price_line.unit_price"`
	Amount      money.Cents `json:"amount_cents" doc:"amount,fields.price_line.amount"`
	VATRate     BP          `json:"vat_rate_bp" doc:"vat_rate,fields.price_line.vat_rate"`
}

// Adjustment is a snapshot adjustment (addendum, early payment, manual).
type Adjustment struct {
	Kind   string      `json:"kind" doc:"kind,fields.adjustment.kind"`
	Reason string      `json:"reason" doc:"reason,fields.adjustment.reason"`
	Amount money.Cents `json:"amount_cents" doc:"amount,fields.adjustment.amount"`
}

// Price is a price with its breakdown.
type Price struct {
	Total       money.Cents  `json:"total_cents" doc:"total,fields.price.total"`
	Net         money.Cents  `json:"net_cents" doc:"net,fields.price.net"`
	VAT         money.Cents  `json:"vat_cents" doc:"vat,fields.price.vat"`
	Deposit     money.Cents  `json:"deposit_cents" doc:"deposit,fields.price.deposit"`
	Lines       []PriceLine  `json:"lines" doc:"lines,fields.price.lines"`
	Adjustments []Adjustment `json:"adjustments" doc:"adjustments,fields.price.adjustments"`
}

// Installment is a planned or issued instalment.
type Installment struct {
	Seq     int           `json:"seq" doc:"seq,fields.installment.seq"`
	Kind    string        `json:"kind" doc:"kind,fields.installment.kind"` // deposit, installment, single
	IssueOn calendar.Date `json:"issue_on" doc:"issue_on,fields.installment.issue_on"`
	DueOn   calendar.Date `json:"due_on" doc:"due_on,fields.installment.due_on"`
	Amount  money.Cents   `json:"amount_cents" doc:"amount,fields.installment.amount"`
	Status  string        `json:"status" doc:"status,fields.installment.status"`
}

// Policy are the printable conditions of a contract (policy v2).
type Policy struct {
	DepositBP              BP          `json:"deposit_bp" doc:"deposit_bp,fields.policy.deposit_bp"`
	Deposit                money.Cents `json:"deposit_cents" doc:"deposit,fields.policy.deposit"`
	DepositDueDays         int         `json:"deposit_due_days" doc:"deposit_due_days,fields.policy.deposit_due_days"`
	InstallmentsCount      int         `json:"installments_count" doc:"installments_count,fields.policy.installments_count"`
	EarlyPaymentDiscountBP BP          `json:"early_payment_discount_bp" doc:"early_payment_discount_bp,fields.policy.early_payment_discount_bp"`
	SEPA                   bool        `json:"sepa" doc:"sepa,fields.policy.sepa"`
	TacitRenewal           bool        `json:"tacit_renewal" doc:"tacit_renewal,fields.policy.tacit_renewal"`
	RenewalNoticeDays      int         `json:"renewal_notice_days" doc:"renewal_notice_days,fields.policy.renewal_notice_days"`
	CancellationNoticeDays int         `json:"cancellation_notice_days" doc:"cancellation_notice_days,fields.policy.cancellation_notice_days"`
	Refund                 string      `json:"refund" doc:"refund,fields.policy.refund"` // pro_rata, none
	PenaltyBP              BP          `json:"penalty_bp" doc:"penalty_bp,fields.policy.penalty_bp"`
}

// ContractInfo are the header data of a contract.
type ContractInfo struct {
	Code     string        `json:"code" doc:"code,fields.contract.code"`
	Kind     string        `json:"kind" doc:"kind,fields.contract.kind"` // mooring, charter
	Term     string        `json:"term" doc:"term,fields.contract.term"`
	StartsOn calendar.Date `json:"starts_on" doc:"starts_on,fields.contract.starts_on"`
	EndsOn   calendar.Date `json:"ends_on" doc:"ends_on,fields.contract.ends_on"` // exclusive end date
	IssuedOn calendar.Date `json:"issued_on" doc:"issued_on,fields.contract.issued_on"`
	SignedOn calendar.Date `json:"signed_on" doc:"signed_on,fields.contract.signed_on"`
	Notes    Notes         `json:"notes" doc:"notes,fields.contract.notes"`
}

// Fleet is the charter fleet of a charter contract.
type Fleet struct {
	Name              string   `json:"name" doc:"name,fields.fleet.name"`
	Vessels           []Vessel `json:"vessels" doc:"vessels,fields.fleet.vessels"`
	ChangeoverWeekday int      `json:"changeover_weekday" doc:"changeover_weekday,fields.fleet.changeover_weekday"`
	PricingMode       string   `json:"pricing_mode" doc:"pricing_mode,fields.fleet.pricing_mode"`
}

// QuoteInfo are the header data of a quote.
type QuoteInfo struct {
	Code     string        `json:"code" doc:"code,fields.quote.code"`
	IssuedOn calendar.Date `json:"issued_on" doc:"issued_on,fields.quote.issued_on"`
	Kind     string        `json:"kind" doc:"kind,fields.quote.kind"`
	Term     string        `json:"term" doc:"term,fields.quote.term"`
	StartsOn calendar.Date `json:"starts_on" doc:"starts_on,fields.quote.starts_on"`
	EndsOn   calendar.Date `json:"ends_on" doc:"ends_on,fields.quote.ends_on"`
	Notes    Notes         `json:"notes" doc:"notes,fields.quote.notes"`
}

// QuoteOption is one priced alternative of a quote.
type QuoteOption struct {
	Seq        int     `json:"seq" doc:"seq,fields.quote_option.seq"`
	Label      string  `json:"label" doc:"label,fields.quote_option.label"`
	Placements []Place `json:"placements" doc:"placements,fields.quote_option.placements"`
	Price      Price   `json:"price" doc:"price,fields.quote_option.price"`
}

// QuoteView is the data of kind quote.
type QuoteView struct {
	Marina     Marina        `json:"marina" doc:"marina,fields.groups.marina"`
	Customer   Party         `json:"customer" doc:"customer,fields.customer"`
	Vessel     Vessel        `json:"vessel" doc:"vessel,fields.groups.vessel"`
	Quote      QuoteInfo     `json:"quote" doc:"quote,fields.groups.quote"`
	Options    []QuoteOption `json:"options" doc:"options,fields.quote.options"`
	ValidUntil calendar.Date `json:"valid_until" doc:"valid_until,fields.quote.valid_until"`
	Link       string        `json:"link" doc:"link,fields.quote.link"`
}

// ContractView is the data of kinds contract_annual, contract_seasonal, contract_monthly, contract_period and
// charter (Fleet set only for charter).
type ContractView struct {
	Marina          Marina        `json:"marina" doc:"marina,fields.groups.marina"`
	Customer        Party         `json:"customer" doc:"customer,fields.customer"`
	BillingCustomer Party         `json:"billing_customer" doc:"billing_customer,fields.billing_customer"`
	Vessel          Vessel        `json:"vessel" doc:"vessel,fields.groups.vessel"`
	Contract        ContractInfo  `json:"contract" doc:"contract,fields.groups.contract"`
	Berths          []Place       `json:"berths" doc:"berths,fields.contract.berths"`
	Lines           []PriceLine   `json:"lines" doc:"lines,fields.contract.lines"`
	Price           Price         `json:"price" doc:"price,fields.contract.price"`
	Installments    []Installment `json:"installments" doc:"installments,fields.contract.installments"`
	Policy          Policy        `json:"policy" doc:"policy,fields.groups.policy"`
	Fleet           *Fleet        `json:"fleet,omitempty" doc:"fleet,fields.groups.fleet"`
}

// MarinaView is the data of kinds general_conditions and harbour_rules.
type MarinaView struct {
	Marina Marina `json:"marina" doc:"marina,fields.groups.marina"`
}

// LetterView is the data of kind letter.
type LetterView struct {
	Marina   Marina `json:"marina" doc:"marina,fields.groups.marina"`
	Customer Party  `json:"customer" doc:"customer,fields.customer"`
}

// AddendumInfo are the data of an addendum.
type AddendumInfo struct {
	Seq         int           `json:"seq" doc:"seq,fields.addendum.seq"`
	Kind        string        `json:"kind" doc:"kind,fields.addendum.kind"`
	EffectiveOn calendar.Date `json:"effective_on" doc:"effective_on,fields.addendum.effective_on"`
	Description Notes         `json:"description" doc:"description,fields.addendum.description"`
	OldTotal    money.Cents   `json:"old_total_cents" doc:"old_total,fields.addendum.old_total"`
	NewTotal    money.Cents   `json:"new_total_cents" doc:"new_total,fields.addendum.new_total"`
	Delta       money.Cents   `json:"delta_cents" doc:"delta,fields.addendum.delta"`
	NewVessel   *Vessel       `json:"new_vessel,omitempty" doc:"new_vessel,fields.addendum.new_vessel"`
}

// AddendumView is the data of kind addendum.
type AddendumView struct {
	Marina       Marina        `json:"marina" doc:"marina,fields.groups.marina"`
	Customer     Party         `json:"customer" doc:"customer,fields.customer"`
	Vessel       Vessel        `json:"vessel" doc:"vessel,fields.groups.vessel"`
	Contract     ContractInfo  `json:"contract" doc:"contract,fields.groups.contract"`
	Addendum     AddendumInfo  `json:"addendum" doc:"addendum,fields.groups.addendum"`
	Lines        []PriceLine   `json:"lines" doc:"lines,fields.addendum.lines"`
	Installments []Installment `json:"installments" doc:"installments,fields.addendum.installments"`
}

// RenewalView is the data of kind renewal_proposal.
type RenewalView struct {
	Marina            Marina        `json:"marina" doc:"marina,fields.groups.marina"`
	Customer          Party         `json:"customer" doc:"customer,fields.customer"`
	Vessel            Vessel        `json:"vessel" doc:"vessel,fields.groups.vessel"`
	Contract          ContractInfo  `json:"contract" doc:"contract,fields.renewal.contract"`
	NewContract       ContractInfo  `json:"new_contract" doc:"new_contract,fields.renewal.new_contract"`
	Berths            []Place       `json:"berths" doc:"berths,fields.renewal.berths"`
	OldPrice          Price         `json:"old_price" doc:"old_price,fields.renewal.old_price"`
	NewPrice          Price         `json:"new_price" doc:"new_price,fields.renewal.new_price"`
	EarlyPaymentTotal money.Cents   `json:"early_payment_total_cents" doc:"early_payment_total,fields.renewal.early_payment_total"`
	Installments      []Installment `json:"installments" doc:"installments,fields.renewal.installments"`
	Deadline          calendar.Date `json:"deadline" doc:"deadline,fields.renewal.deadline"`
	Link              string        `json:"link" doc:"link,fields.renewal.link"`
	Message           Notes         `json:"message" doc:"message,fields.renewal.message"`
}

// InstallmentView is the data of kinds payment_request, reminder and installment_notice (contract instalments or
// holder instalments: Contract or Right is set).
type InstallmentView struct {
	Marina           Marina        `json:"marina" doc:"marina,fields.groups.marina"`
	Customer         Party         `json:"customer" doc:"customer,fields.customer"`
	Contract         *ContractInfo `json:"contract,omitempty" doc:"contract,fields.groups.contract"`
	Right            *RightInfo    `json:"right,omitempty" doc:"right,fields.groups.right"`
	Installment      Installment   `json:"installment" doc:"installment,fields.groups.installment"`
	Overdue          bool          `json:"overdue" doc:"overdue,fields.installment.overdue"`
	PaymentReference string        `json:"payment_reference" doc:"payment_reference,fields.installment.payment_reference"`
	Open             []Installment `json:"open" doc:"open,fields.installment.open"`
}

// RightInfo are the data of a berth right.
type RightInfo struct {
	Kind        string        `json:"kind" doc:"kind,fields.right.kind"`
	Berth       Place         `json:"berth" doc:"berth,fields.right.berth"`
	FromOn      calendar.Date `json:"from_on" doc:"from_on,fields.right.from_on"`
	ToOn        calendar.Date `json:"to_on" doc:"to_on,fields.right.to_on"` // inclusive last night
	TitleDeed   string        `json:"title_deed" doc:"title_deed,fields.right.title_deed"`
	TitleNumber string        `json:"title_number" doc:"title_number,fields.right.title_number"`
	TitleNotary string        `json:"title_notary" doc:"title_notary,fields.right.title_notary"`
	TitleDate   calendar.Date `json:"title_date" doc:"title_date,fields.right.title_date"`
}

// Holder is a holder of a right with its share.
type Holder struct {
	Party  Party       `json:"party" doc:"party,fields.holder.party"`
	Share  Millis      `json:"share_millis" doc:"share,fields.holder.share"`
	Amount money.Cents `json:"amount_cents" doc:"amount,fields.holder.amount"`
}

// HolderChainEntry is one right in the history of a berth.
type HolderChainEntry struct {
	Right   RightInfo `json:"right" doc:"right,fields.holder_chain.right"`
	Status  string    `json:"status" doc:"status,fields.holder_chain.status"`
	Holders []Holder  `json:"holders" doc:"holders,fields.holder_chain.holders"`
}

// HolderChainView is the data of kind holder_chain.
type HolderChainView struct {
	Marina  Marina             `json:"marina" doc:"marina,fields.groups.marina"`
	Berth   Place              `json:"berth" doc:"berth,fields.holder_chain.berth"`
	Entries []HolderChainEntry `json:"entries" doc:"entries,fields.holder_chain.entries"`
}

// TransferView is the data of kind transfer.
type TransferView struct {
	Marina        Marina        `json:"marina" doc:"marina,fields.groups.marina"`
	Right         RightInfo     `json:"right" doc:"right,fields.groups.right"`
	EffectiveOn   calendar.Date `json:"effective_on" doc:"effective_on,fields.transfer.effective_on"`
	From          []Holder      `json:"from" doc:"from,fields.transfer.from"`
	To            []Holder      `json:"to" doc:"to,fields.transfer.to"`
	DeclaredPrice money.Cents   `json:"declared_price_cents" doc:"declared_price,fields.transfer.declared_price"`
	TakeoverFee   money.Cents   `json:"takeover_fee_cents" doc:"takeover_fee,fields.transfer.takeover_fee"`
}

// MandateLine is one sold allocation inside a mandate.
type MandateLine struct {
	ContractCode string        `json:"contract_code" doc:"contract_code,fields.mandate_line.contract_code"`
	FirstNight   calendar.Date `json:"first_night" doc:"first_night,fields.mandate_line.first_night"`
	LastNight    calendar.Date `json:"last_night" doc:"last_night,fields.mandate_line.last_night"`
	Nights       int           `json:"nights" doc:"nights,fields.mandate_line.nights"`
	Revenue      money.Cents   `json:"revenue_cents" doc:"revenue,fields.mandate_line.revenue"`
	OwnerShare   money.Cents   `json:"owner_share_cents" doc:"owner_share,fields.mandate_line.owner_share"`
}

// MandateStatementView is the data of kind mandate_statement.
type MandateStatementView struct {
	Marina        Marina        `json:"marina" doc:"marina,fields.groups.marina"`
	Holder        Party         `json:"holder" doc:"holder,fields.mandate.holder"`
	Right         RightInfo     `json:"right" doc:"right,fields.groups.right"`
	From          calendar.Date `json:"from" doc:"from,fields.mandate.from"`
	To            calendar.Date `json:"to" doc:"to,fields.mandate.to"`
	OwnerShareBP  BP            `json:"owner_share_bp" doc:"owner_share_bp,fields.mandate.owner_share_bp"`
	MinGuaranteed money.Cents   `json:"min_guaranteed_cents" doc:"min_guaranteed,fields.mandate.min_guaranteed"`
	Lines         []MandateLine `json:"lines" doc:"lines,fields.mandate.lines"`
	Revenue       money.Cents   `json:"revenue_cents" doc:"revenue,fields.mandate.revenue"`
	OwnerShare    money.Cents   `json:"owner_share_cents" doc:"owner_share,fields.mandate.owner_share"`
	TopUp         money.Cents   `json:"top_up_cents" doc:"top_up,fields.mandate.top_up"`
}

// FeeNoticeView is the data of kind fee_notice: the yearly management fee of a holder.
type FeeNoticeView struct {
	Marina       Marina        `json:"marina" doc:"marina,fields.groups.marina"`
	Holder       Party         `json:"holder" doc:"holder,fields.fee.holder"`
	Right        RightInfo     `json:"right" doc:"right,fields.groups.right"`
	Year         int           `json:"year" doc:"year,fields.fee.year"`
	Base         money.Cents   `json:"base_cents" doc:"base,fields.fee.base"`
	IstatBP      BP            `json:"istat_bp" doc:"istat,fields.fee.istat"`
	Updated      money.Cents   `json:"updated_cents" doc:"updated,fields.fee.updated"`
	Days         int           `json:"days" doc:"days,fields.fee.days"`
	Amount       money.Cents   `json:"amount_cents" doc:"amount,fields.fee.amount"`
	Share        Millis        `json:"share_millis" doc:"share,fields.fee.share"`
	HolderAmount money.Cents   `json:"holder_amount_cents" doc:"holder_amount,fields.fee.holder_amount"`
	Installments []Installment `json:"installments" doc:"installments,fields.fee.installments"`
}

// CommonChargeLine is one item of a holder's common charges statement.
type CommonChargeLine struct {
	Item         string      `json:"item" doc:"item,fields.common_line.item"`
	Table        string      `json:"table" doc:"table,fields.common_line.table"`
	Berth        string      `json:"berth" doc:"berth,fields.common_line.berth"`
	Millis       Millis      `json:"millis" doc:"millis,fields.common_line.millis"`
	ItemAmount   money.Cents `json:"item_amount_cents" doc:"item_amount,fields.common_line.item_amount"`
	BerthShare   money.Cents `json:"berth_share_cents" doc:"berth_share,fields.common_line.berth_share"`
	HolderAmount money.Cents `json:"holder_amount_cents" doc:"holder_amount,fields.common_line.holder_amount"`
}

// CommonChargesHolderView is the data of kind common_charges_holder.
type CommonChargesHolderView struct {
	Marina       Marina             `json:"marina" doc:"marina,fields.groups.marina"`
	Holder       Party              `json:"holder" doc:"holder,fields.common.holder"`
	Year         int                `json:"year" doc:"year,fields.common.year"`
	Kind         string             `json:"kind" doc:"kind,fields.common.kind"` // advance, balance
	Lines        []CommonChargeLine `json:"lines" doc:"lines,fields.common.lines"`
	Total        money.Cents        `json:"total_cents" doc:"total,fields.common.total"`
	Installments []Installment      `json:"installments" doc:"installments,fields.common.installments"`
}

// CommonChargeItemTotal is one item of the per-item common charges statement.
type CommonChargeItemTotal struct {
	Item        string      `json:"item" doc:"item,fields.common_item.item"`
	Table       string      `json:"table" doc:"table,fields.common_item.table"`
	Amount      money.Cents `json:"amount_cents" doc:"amount,fields.common_item.amount"`
	Allocated   money.Cents `json:"allocated_cents" doc:"allocated,fields.common_item.allocated"`
	MarinaShare money.Cents `json:"marina_share_cents" doc:"marina_share,fields.common_item.marina_share"`
}

// CommonChargesItemsView is the data of kind common_charges_items.
type CommonChargesItemsView struct {
	Marina Marina                  `json:"marina" doc:"marina,fields.groups.marina"`
	Year   int                     `json:"year" doc:"year,fields.common.year"`
	Kind   string                  `json:"kind" doc:"kind,fields.common.kind"`
	Items  []CommonChargeItemTotal `json:"items" doc:"items,fields.common.items"`
	Total  money.Cents             `json:"total_cents" doc:"total,fields.common.total"`
}

// CommunicationView is the data of kind communication (rendered once per recipient).
type CommunicationView struct {
	Marina    Marina         `json:"marina" doc:"marina,fields.groups.marina"`
	Customer  Party          `json:"customer" doc:"customer,fields.customer"`
	Vessels   []Vessel       `json:"vessels" doc:"vessels,fields.communication.vessels"`
	Contracts []ContractInfo `json:"contracts" doc:"contracts,fields.communication.contracts"`
}

// EvidenceSigner is one signer on the evidence page of an OTP signature ("firma elettronica semplice").
type EvidenceSigner struct {
	Role             string    `json:"role" doc:"role,fields.evidence.role"`
	Name             string    `json:"name" doc:"name,fields.evidence.name"`
	DeclaredIdentity string    `json:"declared_identity" doc:"declared_identity,fields.evidence.declared_identity"`
	EmailHash        string    `json:"email_hash" doc:"email_hash,fields.evidence.email_hash"`
	IP               string    `json:"ip" doc:"ip,fields.evidence.ip"`
	UserAgentSHA256  string    `json:"user_agent_sha256" doc:"user_agent_sha256,fields.evidence.user_agent_sha256"`
	OpenedAt         time.Time `json:"opened_at" doc:"opened_at,fields.evidence.opened_at"`
	OTPSentAt        time.Time `json:"otp_sent_at" doc:"otp_sent_at,fields.evidence.otp_sent_at"`
	SignedAt         time.Time `json:"signed_at" doc:"signed_at,fields.evidence.signed_at"`
}

// EvidenceView is the data of kind signature_evidence (built-in template only).
type EvidenceView struct {
	Marina         Marina           `json:"marina" doc:"marina,fields.groups.marina"`
	DocumentTitle  string           `json:"document_title" doc:"document_title,fields.evidence.document_title"`
	DocumentSHA256 string           `json:"document_sha256" doc:"document_sha256,fields.evidence.document_sha256"`
	RequestID      string           `json:"request_id" doc:"request_id,fields.evidence.request_id"`
	Signers        []EvidenceSigner `json:"signers" doc:"signers,fields.evidence.signers"`
}

// MapPrintView is the header data of kind map_print (the map itself is drawn by docs, not by the template).
type MapPrintView struct {
	Marina  Marina        `json:"marina" doc:"marina,fields.groups.marina"`
	MapName string        `json:"map_name" doc:"map_name,fields.map_print.map_name"`
	Date    calendar.Date `json:"date" doc:"date,fields.map_print.date"`
	Lens    string        `json:"lens" doc:"lens,fields.map_print.lens"`
}
