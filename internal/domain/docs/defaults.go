package docs

import (
	"embed"
	"strings"

	"github.com/coste360/porto/internal/crypto"
)

// Built-in default templates (m4-plan §6.2): one Markdown file per kind and locale, plus a shared header and
// footer per locale. A marina template overrides them; documents made from a built-in record builtin_ref
// "<locale>/<kind>@<sha256 of body, header and footer>".

//go:embed defaults/*/*.md
var defaultsFS embed.FS

// Locales of the built-in templates; the first is the fallback (Italian is the source language).
var builtinLocales = []string{"it", "en"}

// Source is the text of a template version: body, header, footer and page settings.
type Source struct {
	Body, Header, Footer string
	MarginMM, FontSizePt int64
}

// SHA256 is the hash recorded for a published version or a built-in (body, header and footer).
func (s Source) SHA256() string {
	return crypto.SHA256Hex(s.Body + "\x00" + s.Header + "\x00" + s.Footer)
}

func normLocale(l string) string {
	l = strings.ToLower(strings.TrimSpace(l))
	if i := strings.IndexAny(l, "-_"); i > 0 {
		l = l[:i]
	}
	return l
}

// Builtin returns the built-in template of a kind in a locale (falling back to it).
func Builtin(kind Kind, locale string) (Source, string, bool) {
	loc := normLocale(locale)
	if !contains(builtinLocales, loc) {
		loc = builtinLocales[0]
	}
	body, err := defaultsFS.ReadFile("defaults/" + loc + "/" + string(kind) + ".md")
	if err != nil {
		return Source{}, "", false
	}
	header, _ := defaultsFS.ReadFile("defaults/" + loc + "/_header.md")
	footer, _ := defaultsFS.ReadFile("defaults/" + loc + "/_footer.md")
	src := Source{Body: string(body), Header: string(header), Footer: string(footer)}
	if kind == KindMapPrint {
		src.Footer = ""
	}
	return src, loc + "/" + string(kind) + "@" + src.SHA256(), true
}

// builtinByRef returns the built-in of a builtin_ref (the hash must match the embedded text).
func builtinByRef(ref string) (Source, Kind, string, bool) {
	head, sha, ok := strings.Cut(ref, "@")
	if !ok {
		return Source{}, "", "", false
	}
	loc, kind, ok := strings.Cut(head, "/")
	if !ok {
		return Source{}, "", "", false
	}
	src, got, ok := Builtin(Kind(kind), loc)
	if !ok || !strings.HasSuffix(got, "@"+sha) {
		return Source{}, "", "", false
	}
	return src, Kind(kind), loc, true
}

func contains[T comparable](xs []T, x T) bool {
	for _, y := range xs {
		if y == x {
			return true
		}
	}
	return false
}

// Labels of signature frames and document titles per locale.
//
//nolint:misspell // Italian labels of the documents
var (
	roleLabels = map[string]map[string]string{
		"it": {
			"customer": "Il cliente", "co_holder": "Il cointestatario", "outgoing_holder": "Il cedente",
			"incoming_holder": "Il cessionario", "legal_representative": "Il legale rappresentante", "marina": "Il porto",
		},
		"en": {
			"customer": "The customer", "co_holder": "The co-holder", "outgoing_holder": "The outgoing holder",
			"incoming_holder": "The incoming holder", "legal_representative": "The legal representative",
			"marina": "The marina",
		},
	}
	nameDateLabels = map[string][2]string{"it": {"Nome e cognome", "Data e firma"}, "en": {"Name", "Date and signature"}}
	kindTitles     = map[string]map[Kind]string{
		"it": {
			KindQuote: "Preventivo", KindContractAnnual: "Contratto di ormeggio annuale",
			KindContractSeasonal: "Contratto di ormeggio stagionale", KindContractMonthly: "Contratto di ormeggio mensile",
			KindContractPeriod: "Contratto di ormeggio", KindCharter: "Contratto charter",
			KindGeneralConditions: "Condizioni generali", KindHarbourRules: "Regolamento del porto", KindLetter: "Lettera",
			KindAddendum: "Addendum", KindRenewalProposal: "Proposta di rinnovo", KindPaymentRequest: "Richiesta di pagamento",
			KindReminder: "Promemoria di pagamento", KindInstallmentNotice: "Avviso di scadenza",
			KindHolderChain: "Catena dei titolari", KindMandateStatement: "Rendiconto del mandato",
			KindFeeNotice: "Oneri di gestione", KindCommonChargesHolder: "Spese comuni",
			KindCommonChargesItems: "Ripartizione spese comuni", KindTransfer: "Cessione del diritto d'uso",
			KindCommunication: "Comunicazione", KindSignatureEvidence: "Evidenze di firma", KindMapPrint: "Mappa",
		},
		"en": {
			KindQuote: "Quote", KindContractAnnual: "Annual mooring contract", KindContractSeasonal: "Seasonal mooring contract",
			KindContractMonthly: "Monthly mooring contract", KindContractPeriod: "Mooring contract",
			KindCharter: "Charter contract", KindGeneralConditions: "General conditions", KindHarbourRules: "Harbour rules",
			KindLetter: "Letter", KindAddendum: "Addendum", KindRenewalProposal: "Renewal proposal",
			KindPaymentRequest: "Payment request", KindReminder: "Payment reminder", KindInstallmentNotice: "Due notice",
			KindHolderChain: "Holder chain", KindMandateStatement: "Mandate statement", KindFeeNotice: "Management fees",
			KindCommonChargesHolder: "Common charges", KindCommonChargesItems: "Common charges allocation",
			KindTransfer: "Berth right transfer", KindCommunication: "Communication",
			KindSignatureEvidence: "Signature evidence", KindMapPrint: "Map",
		},
	}
)

// Title is the localised title of a kind (the PDF title and the email subject of a document).
func Title(kind Kind, locale string) string {
	loc := normLocale(locale)
	if t, ok := kindTitles[loc][kind]; ok {
		return t
	}
	return kindTitles["it"][kind]
}
