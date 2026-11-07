package notify

import (
	"bytes"
	"embed"
	"fmt"
	htmltemplate "html/template"
	"io/fs"
	"strings"
	texttemplate "text/template"
)

// Template names.
const (
	TemplatePasswordReset = "password_reset"
	TemplateInvitation    = "invitation"
)

// M4 customer and office templates (m4-plan §6.7), it and en, each with subject, text and html. They read the
// display values of RenderData.Values with the v function ({{v "contract_code"}}); see the Value* keys.
const (
	TemplateQuote                     = "quote"
	TemplateSignatureRequest          = "signature_request"
	TemplateSignatureOTP              = "signature_otp"
	TemplateSignatureReminder         = "signature_reminder"
	TemplateContractActivated         = "contract_activated"
	TemplateRenewalProposal           = "renewal_proposal"
	TemplateRenewalTacitNotice        = "renewal_tacit_notice"
	TemplateInstallmentPaymentRequest = "installment_payment_request"
	TemplateInstallmentReminder       = "installment_reminder"
	TemplateDeadlineNoticeCustomer    = "deadline_notice_customer"
	TemplateDeadlineNoticeOffice      = "deadline_notice_office"
	TemplateMandateStatement          = "mandate_statement"
	TemplateCommunication             = "communication"
)

// M5 template names (m5-plan §14.1). The files under templates/{it,en}/ are written by CASH-DRIVERS; until a
// file set exists the name is not loaded (Render returns an unknown template error).
const (
	TemplatePaymentRequest      = "payment_request"
	TemplatePaymentReceipt      = "payment_receipt"
	TemplateReminderPreDue      = "reminder_pre_due"
	TemplateReminderDue         = "reminder_due"
	TemplateReminderCourteous   = "reminder_courteous"
	TemplateReminderFormal      = "reminder_formal"
	TemplateReminderFinalNotice = "reminder_final_notice"
	TemplateSEPAMandateRequest  = "sepa_mandate_request"
	TemplateLatePaymentOffice   = "late_payment_office"
)

// M5Templates lists the M5 templates.
func M5Templates() []string {
	return []string{
		TemplatePaymentRequest, TemplatePaymentReceipt, TemplateReminderPreDue, TemplateReminderDue,
		TemplateReminderCourteous, TemplateReminderFormal, TemplateReminderFinalNotice, TemplateSEPAMandateRequest,
		TemplateLatePaymentOffice,
	}
}

// M4Templates lists the M4 templates.
func M4Templates() []string {
	return []string{
		TemplateQuote, TemplateSignatureRequest, TemplateSignatureOTP, TemplateSignatureReminder,
		TemplateContractActivated, TemplateRenewalProposal, TemplateRenewalTacitNotice,
		TemplateInstallmentPaymentRequest, TemplateInstallmentReminder, TemplateDeadlineNoticeCustomer,
		TemplateDeadlineNoticeOffice, TemplateMandateStatement, TemplateCommunication,
	}
}

// Keys of RenderData.Values read by the M4 templates. Values are display strings prepared by the caller
// (formatted amounts and dates); any key may be missing and the templates leave its line out.
const (
	ValueMarinaName    = "marina_name"
	ValueCustomerName  = "customer_name"
	ValueLink          = "link"           // magic link (quote, renewal, signature, unsubscribe)
	ValueContractCode  = "contract_code"  // also the payment reference
	ValueQuoteCode     = "quote_code"     //
	ValueStartsOn      = "starts_on"      //
	ValueEndsOn        = "ends_on"        //
	ValueAmount        = "amount"         // formatted amount (instalment, total)
	ValueDueOn         = "due_on"         //
	ValueDeadlineOn    = "deadline_on"    // proposal or signature deadline
	ValueValidUntil    = "valid_until"    //
	ValueOldTotal      = "old_total"      //
	ValueNewTotal      = "new_total"      //
	ValueIBAN          = "iban"           // bank details for the transfer
	ValueBankHolder    = "bank_holder"    //
	ValueBank          = "bank"           //
	ValueCode          = "code"           // the OTP code
	ValueExpiresAt     = "expires_at"     //
	ValueDocumentTitle = "document_title" //
	ValueDeadlineKind  = "deadline_kind"  // localised kind of a deadline
	ValueSubject       = "subject"        // free subject (communications)
	ValuePeriod        = "period"         // mandate statement period
	ValueMessage       = "message"        // free text from the office (renewal campaign message)
)

// Locales with templates; the first is the fallback (Italian is the source language).
var locales = []string{"it", "en"}

//go:embed templates/*/*.tmpl
var templateFS embed.FS

// RenderData is what templates see. Values are plain text; the HTML template escapes them. BodyHTML and
// BodyText are the pre-rendered body of a communication (escaped HTML from docs/markdown).
type RenderData struct {
	Name        string            `json:"name"`
	Link        string            `json:"link"`
	ExpiresAt   string            `json:"expires_at"`
	MarinaName  string            `json:"marina_name,omitempty"`
	InviterName string            `json:"inviter_name,omitempty"`
	Values      map[string]string `json:"values,omitempty"`
	BodyHTML    htmltemplate.HTML `json:"body_html,omitempty"`
	BodyText    string            `json:"body_text,omitempty"`
	Attachments []AttachmentRef   `json:"attachments,omitempty"`
	Headers     map[string]string `json:"headers,omitempty"`
}

func (d RenderData) value(k string) string { return d.Values[k] }

// Rendered is a rendered email.
type Rendered struct {
	Subject, Text, HTML string
}

type templateSet struct {
	subject, text *texttemplate.Template
	html          *htmltemplate.Template
}

var templates = func() map[string]templateSet {
	out := map[string]templateSet{}
	names := append([]string{TemplatePasswordReset, TemplateInvitation}, M4Templates()...)
	optional := map[string]bool{}
	for _, name := range M5Templates() {
		names, optional[name] = append(names, name), true
	}
	for _, loc := range locales {
		for _, name := range names {
			base := "templates/" + loc + "/" + name
			if _, err := fs.Stat(templateFS, base+".subject.tmpl"); err != nil && optional[name] {
				continue // M5 template not written yet
			}
			parse := func(suffix string) *texttemplate.Template {
				return texttemplate.Must(texttemplate.New(name+suffix).Funcs(textFuncs).ParseFS(templateFS, base+suffix))
			}
			html := htmltemplate.Must(htmltemplate.New(name+".html.tmpl").Funcs(htmlFuncs).ParseFS(templateFS, base+".html.tmpl"))
			out[loc+"/"+name] = templateSet{subject: parse(".subject.tmpl"), text: parse(".txt.tmpl"), html: html}
		}
	}
	return out
}()

// The v function reads a display value: {{v . "contract_code"}}.
var (
	textFuncs = texttemplate.FuncMap{"v": func(d RenderData, k string) string { return d.value(k) }}
	htmlFuncs = htmltemplate.FuncMap{"v": func(d RenderData, k string) string { return d.value(k) }}
)

// normalizeLocale returns a supported locale ("it" when unknown).
func normalizeLocale(l string) string {
	l = strings.ToLower(strings.TrimSpace(l))
	if i := strings.IndexAny(l, "-_"); i > 0 {
		l = l[:i]
	}
	for _, s := range locales {
		if s == l {
			return l
		}
	}
	return locales[0]
}

// Render renders a template in a locale.
func Render(name, locale string, d RenderData) (Rendered, error) {
	set, ok := templates[normalizeLocale(locale)+"/"+name]
	if !ok {
		return Rendered{}, fmt.Errorf("notify: unknown template %q", name)
	}
	var subj, txt, html bytes.Buffer
	if err := set.subject.Execute(&subj, d); err != nil {
		return Rendered{}, fmt.Errorf("notify: render %s subject: %w", name, err)
	}
	if err := set.text.Execute(&txt, d); err != nil {
		return Rendered{}, fmt.Errorf("notify: render %s text: %w", name, err)
	}
	if err := set.html.Execute(&html, d); err != nil {
		return Rendered{}, fmt.Errorf("notify: render %s html: %w", name, err)
	}
	return Rendered{
		Subject: strings.TrimSpace(strings.ReplaceAll(subj.String(), "\n", " ")),
		Text:    txt.String(),
		HTML:    html.String(),
	}, nil
}
