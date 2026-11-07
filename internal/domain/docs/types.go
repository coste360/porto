package docs

import (
	"reflect"
	"time"

	"github.com/coste360/porto/internal/domain/docs/views"
)

// Kind is a print template kind (spec 15.1; m4-plan §6.2).
type Kind string

// Template kinds. Kinds with AvailableFrom set exist in the catalogue but cannot be published in M4.
const (
	KindQuote               Kind = "quote"
	KindContractAnnual      Kind = "contract_annual"
	KindContractSeasonal    Kind = "contract_seasonal"
	KindContractMonthly     Kind = "contract_monthly"
	KindContractPeriod      Kind = "contract_period"
	KindCharter             Kind = "charter"
	KindGeneralConditions   Kind = "general_conditions"
	KindHarbourRules        Kind = "harbour_rules"
	KindLetter              Kind = "letter"
	KindAddendum            Kind = "addendum"
	KindRenewalProposal     Kind = "renewal_proposal"
	KindPaymentRequest      Kind = "payment_request"
	KindReminder            Kind = "reminder"
	KindInstallmentNotice   Kind = "installment_notice"
	KindHolderChain         Kind = "holder_chain"
	KindMandateStatement    Kind = "mandate_statement"
	KindFeeNotice           Kind = "fee_notice"
	KindCommonChargesHolder Kind = "common_charges_holder"
	KindCommonChargesItems  Kind = "common_charges_items"
	KindTransfer            Kind = "transfer"
	KindCommunication       Kind = "communication"
	KindSignatureEvidence   Kind = "signature_evidence"
	KindMapPrint            Kind = "map_print"
	KindReceipt             Kind = "receipt"
	KindStatementOfAccount  Kind = "statement_of_account"
	// KindChargeExportSummary is the summary PDF of a "da fatturare" export (M5, m5-plan §3.6, §8.3).
	KindChargeExportSummary Kind = "charge_export_summary"
	KindYardWorkOrder       Kind = "yard_work_order"
	KindYardQuote           Kind = "yard_quote"
	KindRentalContract      Kind = "rental_contract"
	KindRentalHandover      Kind = "rental_handover"
)

// SubjectType is generated_documents.subject_type.
type SubjectType string

// Subject types.
const (
	SubjectQuote               SubjectType = "quote"
	SubjectContract            SubjectType = "contract"
	SubjectAddendum            SubjectType = "addendum"
	SubjectRenewalProposal     SubjectType = "renewal_proposal"
	SubjectRightTransfer       SubjectType = "right_transfer"
	SubjectHolderChain         SubjectType = "holder_chain"
	SubjectMandateStatement    SubjectType = "mandate_statement"
	SubjectFeeNotice           SubjectType = "fee_notice"
	SubjectCommonChargesHolder SubjectType = "common_charges_holder"
	SubjectCommonChargesItems  SubjectType = "common_charges_items"
	SubjectInstallmentNotice   SubjectType = "installment_notice"
	SubjectCommunication       SubjectType = "communication"
	SubjectMapPrint            SubjectType = "map_print"
	SubjectLetter              SubjectType = "letter"
)

// Document statuses (generated_documents.status).
const (
	StatusPending    = "pending"
	StatusRendering  = "rendering"
	StatusReady      = "ready"
	StatusFailed     = "failed"
	StatusSuperseded = "superseded"
)

// KindInfo describes a kind of the catalogue: its view struct, the milestone it becomes publishable in (empty
// = M4), whether marinas may override it, and the signature roles its documents carry.
type KindInfo struct {
	Kind           Kind
	View           reflect.Type
	AvailableFrom  string
	BuiltinOnly    bool
	SignatureRoles []string
}

// Catalogue returns every kind in display order (m4-plan §6.2). DOCUMENTS derives the field lists from View.
func Catalogue() []KindInfo {
	t := reflect.TypeOf
	contract := t(views.ContractView{})
	signers := []string{"customer", "marina"}
	return []KindInfo{
		{Kind: KindQuote, View: t(views.QuoteView{})},
		{Kind: KindContractAnnual, View: contract, SignatureRoles: signers},
		{Kind: KindContractSeasonal, View: contract, SignatureRoles: signers},
		{Kind: KindContractMonthly, View: contract, SignatureRoles: signers},
		{Kind: KindContractPeriod, View: contract, SignatureRoles: signers},
		{Kind: KindCharter, View: contract, SignatureRoles: signers},
		{Kind: KindGeneralConditions, View: t(views.MarinaView{})},
		{Kind: KindHarbourRules, View: t(views.MarinaView{})},
		{Kind: KindLetter, View: t(views.LetterView{})},
		{Kind: KindAddendum, View: t(views.AddendumView{}), SignatureRoles: signers},
		{Kind: KindRenewalProposal, View: t(views.RenewalView{})},
		{Kind: KindPaymentRequest, View: t(views.InstallmentView{})},
		{Kind: KindReminder, View: t(views.InstallmentView{})},
		{Kind: KindInstallmentNotice, View: t(views.InstallmentView{})},
		{Kind: KindHolderChain, View: t(views.HolderChainView{})},
		{Kind: KindMandateStatement, View: t(views.MandateStatementView{})},
		{Kind: KindFeeNotice, View: t(views.FeeNoticeView{})},
		{Kind: KindCommonChargesHolder, View: t(views.CommonChargesHolderView{})},
		{Kind: KindCommonChargesItems, View: t(views.CommonChargesItemsView{})},
		{Kind: KindTransfer, View: t(views.TransferView{}), SignatureRoles: []string{"outgoing_holder", "incoming_holder", "marina"}},
		{Kind: KindCommunication, View: t(views.CommunicationView{})},
		{Kind: KindSignatureEvidence, View: t(views.EvidenceView{}), BuiltinOnly: true},
		{Kind: KindMapPrint, View: t(views.MapPrintView{}), BuiltinOnly: true},
		// M5 kinds are rendered ephemerally (RenderEphemeral), never archived in generated_documents (m5-plan D20).
		{Kind: KindReceipt, View: t(views.ReceiptView{})},
		{Kind: KindStatementOfAccount, View: t(views.StatementView{})},
		{Kind: KindChargeExportSummary, View: t(views.ChargeExportSummaryView{})},
		{Kind: KindYardWorkOrder, AvailableFrom: "M10"},
		{Kind: KindYardQuote, AvailableFrom: "M10"},
		{Kind: KindRentalContract, AvailableFrom: "M11"},
		{Kind: KindRentalHandover, AvailableFrom: "M11"},
	}
}

// ContractKindOf returns the template kind of a contract by kind and term.
func ContractKindOf(contractKind, term string) Kind {
	if contractKind == "charter" {
		return KindCharter
	}
	switch term {
	case "seasonal":
		return KindContractSeasonal
	case "monthly":
		return KindContractMonthly
	case "period":
		return KindContractPeriod
	default:
		return KindContractAnnual
	}
}

// ComposeRequest asks for a document of a subject (m4-plan §6.4). Locale empty = the customer's, else the
// marina default, else it; TemplateID empty = the marina default template of the kind, else the built-in.
type ComposeRequest struct {
	Kind        Kind
	SubjectType SubjectType
	SubjectID   string
	Locale      string
	TemplateID  string
	PDFA        bool // documents to sign are finalised as PDF/A-2b
	CreatedBy   string
	At          time.Time
}
