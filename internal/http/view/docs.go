package view

import (
	"sort"

	"github.com/coste360/porto/internal/csvimport"
	"github.com/coste360/porto/internal/domain/comms"
	"github.com/coste360/porto/internal/domain/docs"
	"github.com/coste360/porto/internal/domain/signature"
	"github.com/coste360/porto/internal/http/api"
)

// Converters of the M4 documents area (m4-plan §7.3.3–§7.3.6): templates, generated documents, signatures,
// communications, imports.

// TemplateKinds converts the catalogue.
func TemplateKinds(ks []docs.KindDetail) api.TemplateKindList {
	out := api.TemplateKindList{Items: make([]api.TemplateKindInfo, 0, len(ks))}
	for _, k := range ks {
		info := api.TemplateKindInfo{Kind: api.TemplateKind(k.Info.Kind), Publishable: k.Info.Publishable(), Fields: []api.FieldDef{}}
		if k.Info.AvailableFrom != "" {
			info.AvailableFrom = String(k.Info.AvailableFrom)
		}
		if len(k.Info.SignatureRoles) > 0 {
			roles := make([]api.SignerRole, 0, len(k.Info.SignatureRoles))
			for _, r := range k.Info.SignatureRoles {
				roles = append(roles, api.SignerRole(r))
			}
			info.SignatureRoles = &roles
		}
		for _, f := range k.Fields {
			info.Fields = append(info.Fields, api.FieldDef{Path: f.Path, Type: api.FieldType(f.Type), List: f.List, DescriptionKey: f.DescriptionKey})
		}
		out.Items = append(out.Items, info)
	}
	return out
}

// Template converts a template with its published locales.
func Template(t *docs.Template) api.Template {
	out := api.Template{
		Id: t.ID, Kind: api.TemplateKind(t.Kind), Code: t.Code, Name: t.Name, IsDefault: t.IsDefault,
		ArchivedAt: Time(t.ArchivedAt), CreatedAt: t.CreatedAt.UTC(), Version: t.Version,
	}
	out.Locales = []struct {
		Locale  api.Locale `json:"locale"`
		Version int        `json:"version"`
	}{}
	locs := make([]string, 0, len(t.Locales))
	for l := range t.Locales {
		locs = append(locs, l)
	}
	sort.Strings(locs)
	for _, l := range locs {
		out.Locales = append(out.Locales, struct {
			Locale  api.Locale `json:"locale"`
			Version int        `json:"version"`
		}{Locale: api.Locale(l), Version: t.Locales[l]})
	}
	return out
}

// Templates converts templates.
func Templates(ts []docs.Template) api.TemplateList {
	out := api.TemplateList{Items: make([]api.Template, 0, len(ts))}
	for i := range ts {
		out.Items = append(out.Items, Template(&ts[i]))
	}
	return out
}

// TemplateVersion converts a version.
func TemplateVersion(v *docs.Version) api.TemplateVersion {
	m, f := docs.PageOf(v)
	out := api.TemplateVersion{
		Id: v.ID, TemplateId: v.TemplateID, Version: v.Version, Locale: api.Locale(v.Locale), BodyMd: v.BodyMD,
		HeaderMd: v.HeaderMD, FooterMd: v.FooterMD, Status: api.TemplateVersionStatus(v.Status),
		Sha256: String(v.SHA256), PublishedAt: Time(v.PublishedAt), CreatedAt: v.CreatedAt.UTC(),
	}
	if m > 0 {
		out.Page.MarginsMm = Int(int(m))
	}
	if f > 0 {
		out.Page.FontSizePt = Int(int(f))
	}
	return out
}

// TemplateVersions converts versions.
func TemplateVersions(vs []docs.Version) api.TemplateVersionList {
	out := api.TemplateVersionList{Items: make([]api.TemplateVersion, 0, len(vs))}
	for i := range vs {
		out.Items = append(out.Items, TemplateVersion(&vs[i]))
	}
	return out
}

// GeneratedDocument converts a generated document.
func GeneratedDocument(d *docs.Document) api.GeneratedDocument {
	out := api.GeneratedDocument{
		Id: d.ID, Kind: api.TemplateKind(d.Kind), Locale: api.Locale(d.Locale), SubjectType: api.DocumentSubjectType(d.SubjectType),
		SubjectId: d.SubjectID, Status: api.GeneratedDocumentStatus(d.Status), Sha256: String(d.SHA256), Pages: d.Pages,
		Pdfa: d.PDFA, Signed: d.Signed, SignedFromId: String(d.SignedFromID), ErrorCode: String(d.ErrorCode),
		RenderedAt: Time(d.RenderedAt), CreatedAt: d.CreatedAt.UTC(),
	}
	tv := struct {
		BuiltinRef *string `json:"builtin_ref,omitempty"`
		TemplateId *string `json:"template_id,omitempty"` //nolint:revive // must match the generated anonymous struct
		Version    *int    `json:"version,omitempty"`
	}{BuiltinRef: String(d.BuiltinRef), TemplateId: String(d.TemplateID)}
	if d.TemplateVersion > 0 {
		tv.Version = Int(d.TemplateVersion)
	}
	out.TemplateVersion = &tv
	return out
}

// GeneratedDocuments converts a page of documents.
func GeneratedDocuments(ds []docs.Document, next string) api.GeneratedDocumentPage {
	out := api.GeneratedDocumentPage{Items: make([]api.GeneratedDocument, 0, len(ds)), NextCursor: String(next)}
	for i := range ds {
		out.Items = append(out.Items, GeneratedDocument(&ds[i]))
	}
	return out
}

// SignatureRequest converts a request (names and emails are set only when the service decrypted them).
func SignatureRequest(r *signature.Request) api.SignatureRequest {
	out := api.SignatureRequest{
		Id: r.ID, SubjectType: api.SignatureSubjectType(r.SubjectType), SubjectId: r.SubjectID, DocumentId: r.DocumentID,
		Driver: api.SignatureRequestDriver(r.Driver), ProviderCode: String(r.ProviderCode), OrderMode: api.SignatureOrderMode(r.OrderMode),
		Status: api.SignatureRequestStatus(r.Status), ExpiresAt: r.ExpiresAt.UTC(), LastReminderAt: Time(r.LastReminderAt),
		SignedDocumentId: String(r.SignedDocumentID), CompletedAt: Time(r.CompletedAt), CreatedAt: r.CreatedAt.UTC(),
		Version: r.Version, Signers: make([]api.Signer, 0, len(r.Signers)),
	}
	for _, s := range r.Signers {
		scan := s.PaperScanFileID != ""
		out.Signers = append(out.Signers, api.Signer{
			Id: s.ID, Seq: s.Seq, Role: api.SignerRole(s.Role), CustomerId: String(s.CustomerID), UserId: String(s.UserID),
			DisplayName: String(s.DisplayName), Email: String(s.Email), Status: api.SignerStatus(s.Status),
			InvitedAt: Time(s.InvitedAt), OpenedAt: Time(s.OpenedAt), SignedAt: Time(s.SignedAt), DeclinedAt: Time(s.DeclinedAt),
			HasPaperScan: &scan,
		})
	}
	return out
}

// SignatureRequests converts a page of requests.
func SignatureRequests(rs []signature.Request, next string) api.SignatureRequestPage {
	out := api.SignatureRequestPage{Items: make([]api.SignatureRequest, 0, len(rs)), NextCursor: String(next)}
	for i := range rs {
		out.Items = append(out.Items, SignatureRequest(&rs[i]))
	}
	return out
}

// SignatureSettings converts the provider settings (never the secrets).
func SignatureSettings(s *signature.Settings) api.SignatureSettings {
	providers := s.AvailableProviders
	if providers == nil {
		providers = []string{}
	}
	return api.SignatureSettings{
		ProviderCode: String(s.ProviderCode), HasCredentials: s.HasCredentials, HasWebhookSecret: s.HasWebhookSecret,
		AvailableProviders: providers, WebhookUrl: String(s.WebhookURL), Version: s.Version,
	}
}

func strs(p *[]string) []string {
	if p == nil {
		return nil
	}
	return *p
}

// AudienceIn converts an API audience.
func AudienceIn(a api.CommAudience) comms.Audience {
	out := comms.Audience{
		Groups: strs(a.Groups), Structures: strs(a.Structures), CustomerIDs: strs(a.CustomerIds), ResourceIDs: strs(a.ResourceIds),
		MapID: deref(a.MapId), PresentFrom: deref(a.PresentFrom), PresentTo: deref(a.PresentTo),
	}
	if a.ContractKinds != nil {
		for _, k := range *a.ContractKinds {
			out.ContractKinds = append(out.ContractKinds, string(k))
		}
	}
	if a.Terms != nil {
		for _, k := range *a.Terms {
			out.Terms = append(out.Terms, string(k))
		}
	}
	if a.Holders != nil {
		out.Holders = *a.Holders
	}
	if a.WithDeadlinesWithinDays != nil {
		out.WithDeadlinesWithinDays = *a.WithDeadlinesWithinDays
	}
	return out
}

func listPtr(xs []string) *[]string {
	if len(xs) == 0 {
		return nil
	}
	return &xs
}

// Audience converts an audience.
func Audience(a comms.Audience) api.CommAudience {
	out := api.CommAudience{
		Groups: listPtr(a.Groups), Structures: listPtr(a.Structures), CustomerIds: listPtr(a.CustomerIDs),
		ResourceIds: listPtr(a.ResourceIDs), MapId: String(a.MapID), PresentFrom: String(a.PresentFrom), PresentTo: String(a.PresentTo),
	}
	if len(a.ContractKinds) > 0 {
		ks := make([]api.ContractKind, 0, len(a.ContractKinds))
		for _, k := range a.ContractKinds {
			ks = append(ks, api.ContractKind(k))
		}
		out.ContractKinds = &ks
	}
	if len(a.Terms) > 0 {
		ts := make([]api.ContractTerm, 0, len(a.Terms))
		for _, k := range a.Terms {
			ts = append(ts, api.ContractTerm(k))
		}
		out.Terms = &ts
	}
	if a.Holders {
		out.Holders = &a.Holders
	}
	if a.WithDeadlinesWithinDays > 0 {
		out.WithDeadlinesWithinDays = Int(a.WithDeadlinesWithinDays)
	}
	return out
}

// AttachmentsIn converts API attachments.
func AttachmentsIn(p *[]api.CommAttachment) []comms.Attachment {
	if p == nil {
		return nil
	}
	out := make([]comms.Attachment, 0, len(*p))
	for _, a := range *p {
		out = append(out, comms.Attachment{DocumentID: deref(a.DocumentId), FileID: deref(a.FileId)})
	}
	return out
}

// Communication converts a communication.
func Communication(c *comms.Communication) api.Communication {
	out := api.Communication{
		Id: c.ID, Name: c.Name, Category: api.CommCategory(c.Category), Audience: Audience(c.AudienceOf),
		Channels: nonNil(c.Channels), TemplateId: String(c.TemplateID), Subject: c.Subject, BodyMd: c.BodyMD,
		Attachments: make([]api.CommAttachment, 0, len(c.AttachmentsOf)), ScheduledAt: Time(c.ScheduledAt),
		Status: api.CommStatus(c.Status), Counts: c.CountsOf, SentAt: Time(c.SentAt), CreatedAt: c.CreatedAt.UTC(),
		Version: c.Version,
	}
	for _, a := range c.AttachmentsOf {
		out.Attachments = append(out.Attachments, api.CommAttachment{DocumentId: String(a.DocumentID), FileId: String(a.FileID)})
	}
	if out.Counts == nil {
		out.Counts = map[string]int{}
	}
	return out
}

// Communications converts a page of communications.
func Communications(cs []comms.Communication, next string) api.CommunicationPage {
	out := api.CommunicationPage{Items: make([]api.Communication, 0, len(cs)), NextCursor: String(next)}
	for i := range cs {
		out.Items = append(out.Items, Communication(&cs[i]))
	}
	return out
}

// CommRecipients converts a page of recipients.
func CommRecipients(rs []comms.Recipient, next string) api.CommRecipientPage {
	out := api.CommRecipientPage{Items: make([]api.CommRecipient, 0, len(rs)), NextCursor: String(next)}
	for _, r := range rs {
		item := api.CommRecipient{
			Customer: api.CustomerBrief{Id: r.CustomerID, DisplayName: String(r.DisplayName)},
			Status:   api.CommRecipientStatus(r.Status), SentAt: Time(r.SentAt),
		}
		if r.Delivery != "" {
			d := api.CommRecipientDeliveryStatus(r.Delivery)
			item.DeliveryStatus = &d
		}
		out.Items = append(out.Items, item)
	}
	return out
}

// CustomerMessages converts a page of the customer registry.
func CustomerMessages(ms []comms.Message, next string) api.CustomerMessagePage {
	out := api.CustomerMessagePage{Items: make([]api.CustomerMessage, 0, len(ms)), NextCursor: String(next)}
	for _, m := range ms {
		out.Items = append(out.Items, api.CustomerMessage{
			Id: m.ID, Category: api.CustomerMessageCategory(m.Category), Template: m.Template, SourceType: String(m.SourceType),
			SourceId: String(m.SourceID), Status: api.CustomerMessageStatus(m.Status), SentAt: Time(m.SentAt),
			CreatedAt: m.CreatedAt.UTC(),
		})
	}
	return out
}

func issues(is []csvimport.Issue) []api.ImportRowIssue {
	out := make([]api.ImportRowIssue, 0, len(is))
	for _, i := range is {
		out = append(out, api.ImportRowIssue{Row: i.Row, Code: i.Code, Field: String(i.Field), Ref: String(i.Ref)})
	}
	return out
}

// ImportReport converts a report.
func ImportReport(r *csvimport.Report) api.ImportReport {
	out := api.ImportReport{
		RunId: String(r.RunID), Entity: api.ImportEntity(r.Entity), DryRun: r.DryRun, Rows: r.Rows, Valid: r.Valid,
		Rejected: issues(r.Rejected), Duplicates: issues(r.Duplicates),
	}
	if len(r.Created) > 0 {
		c := r.Created
		out.Created = &c
	}
	return out
}

// ImportRun converts a run.
func ImportRun(r *csvimport.RunView) api.ImportRun {
	m := map[string]int(r.MapOf)
	if m == nil {
		m = map[string]int{}
	}
	rep := r.Decoded
	if rep.Entity == "" {
		rep.Entity = r.Entity
	}
	return api.ImportRun{
		Id: r.ID, Entity: api.ImportEntity(r.Entity), Status: api.ImportStatus(r.Status), RowCount: r.RowCount,
		Mapping: &m, Report: ImportReport(&rep), Undoable: &r.Undoable, AppliedAt: Time(r.AppliedAt),
		UndoneAt: Time(r.UndoneAt), CreatedAt: r.CreatedAt.UTC(), Version: r.Version,
	}
}

// ImportRuns converts a page of runs.
func ImportRuns(rs []csvimport.RunView, next string) api.ImportRunPage {
	out := api.ImportRunPage{Items: make([]api.ImportRun, 0, len(rs)), NextCursor: String(next)}
	for i := range rs {
		out.Items = append(out.Items, ImportRun(&rs[i]))
	}
	return out
}
