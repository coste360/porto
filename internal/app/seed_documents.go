package app

import (
	"context"
	"fmt"

	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/comms"
	"github.com/coste360/porto/internal/domain/docs"
	"github.com/coste360/porto/internal/store"
)

// demoTemplateCode is the code of the demo marina's customised annual contract template.
const demoTemplateCode = "contratto-annuale"

// seedDocuments seeds the M4 demo data owned by DOCUMENTS (m4-plan §10): the built-in annual contract template
// materialised as a marina template (it and en) with a customised published Italian version, one service and one
// promotional communication sent to the demo customers, and a deadline engine run. Idempotent: it runs only when
// the demo marina has no template with the demo code.
func (a *App) seedDocuments(ctx context.Context, p *auth.Principal, res *SeedResult) error {
	if a.Docs == nil || a.Comms == nil || a.Deadlines == nil {
		return nil
	}
	scope, err := store.ResolveMarinaScope(ctx, a.DB.DB, res.MarinaID)
	if err != nil {
		return err
	}
	existing, err := store.ListTemplates(ctx, a.DB.DB, scope, string(docs.KindContractAnnual))
	if err != nil {
		return err
	}
	for _, t := range existing {
		if t.Code == demoTemplateCode {
			return nil
		}
	}
	tp, err := a.Docs.CreateTemplate(ctx, p, res.MarinaID, docs.TemplateInput{
		Kind: string(docs.KindContractAnnual), Code: demoTemplateCode, Name: "Contratto annuale", IsDefault: true, FromBuiltin: true,
	})
	if err != nil {
		return fmt.Errorf("template: %w", err)
	}
	res.Templates++
	for _, loc := range []string{"it", "en"} {
		if _, err := a.Docs.PublishTemplateVersion(ctx, p, res.MarinaID, tp.ID, 1, loc); err != nil {
			return fmt.Errorf("publish %s: %w", loc, err)
		}
	}
	src, _, _ := docs.Builtin(docs.KindContractAnnual, "it")
	custom := src.Body + "\nIl presente contratto è redatto secondo il modello del porto, versione personalizzata.\n"
	v, err := a.Docs.CreateTemplateVersion(ctx, p, res.MarinaID, tp.ID, docs.VersionInput{
		Locale: "it", Body: custom, Header: src.Header, Footer: src.Footer, MarginMM: 18,
	})
	if err != nil {
		return fmt.Errorf("custom version: %w", err)
	}
	if _, err := a.Docs.PublishTemplateVersion(ctx, p, res.MarinaID, tp.ID, v.Version, "it"); err != nil {
		return fmt.Errorf("publish custom: %w", err)
	}

	// Two communications to every demo customer: service (customers with an active relation) and promotional
	// (customers with the marketing consent).
	for _, in := range []comms.Input{
		{
			Name: "Lavori al pontile B", Category: comms.CategoryService,
			Subject: "Lavori di manutenzione al pontile", BodyMD: "Gentile {{customer.display_name}},\n\nnei prossimi giorni il porto **{{marina.name}}** eseguirà lavori di manutenzione.",
		},
		{
			Name: "Offerta rimessaggio", Category: comms.CategoryPromotional,
			Subject: "Offerta rimessaggio invernale", BodyMD: "Gentile {{customer.display_name}},\n\nprenota il rimessaggio invernale entro fine mese.",
		},
	} {
		c, err := a.Comms.Create(ctx, p, res.MarinaID, in)
		if err != nil {
			return fmt.Errorf("communication: %w", err)
		}
		if _, err := a.Comms.Send(ctx, p, res.MarinaID, c.ID, c.Version); err != nil {
			return fmt.Errorf("send communication: %w", err)
		}
		res.Communications++
	}
	if err := a.Comms.RunSend(ctx); err != nil {
		return fmt.Errorf("communications: %w", err)
	}
	sync, err := a.Deadlines.Sync(ctx, p, res.MarinaID)
	if err != nil {
		return fmt.Errorf("deadlines: %w", err)
	}
	res.Deadlines += sync.Created
	return nil
}
