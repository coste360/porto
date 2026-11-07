package admin

import (
	"net/http"

	"github.com/coste360/porto/internal/domain/charge"
	"github.com/coste360/porto/internal/http/api"
	"github.com/coste360/porto/internal/http/respond"
	"github.com/coste360/porto/internal/http/view"
)

// Opening balances import (spec 11.6; m5-plan §3.8, §7.3.1): CSV or Excel as base64 JSON, dry run, undo.

// ImportOpeningBalances implements api.ServerInterface (POST .../opening-balances/import).
func (h *Handler) ImportOpeningBalances(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, _ api.ImportOpeningBalancesParams) {
	var in api.OpeningBalanceImportInput
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	m := in.Mapping
	oi := charge.OpeningInput{
		Format: string(in.Format), FileBase64: in.FileBase64, HasHeader: in.HasHeader, DryRun: in.DryRun,
		Mapping: charge.OpeningMapping{
			CustomerMatch: string(m.CustomerMatch), CustomerColumn: m.CustomerColumn, AmountColumn: m.AmountColumn,
			DueOnColumn: m.DueOnColumn, DescriptionColumn: intOr(m.DescriptionColumn, -1), ReferenceColumn: intOr(m.ReferenceColumn, -1),
		},
	}
	if in.Delimiter != nil {
		oi.Delimiter = string(*in.Delimiter)
	}
	rec, err := h.Charges.ImportOpeningBalances(r.Context(), principal(r), marinaID, oi)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.SetETag(w, rec.Version)
	respond.JSON(w, http.StatusOK, view.ChargeImport(rec))
}

// ListOpeningBalanceImports implements api.ServerInterface (GET .../opening-balances/imports).
func (h *Handler) ListOpeningBalanceImports(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, params api.ListOpeningBalanceImportsParams) {
	page, err := h.Charges.ListOpeningImports(r.Context(), principal(r), marinaID, str(params.Cursor), intOr(params.Limit, 0))
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, view.ChargeImportPage(page))
}

// UndoOpeningBalanceImport implements api.ServerInterface (POST .../opening-balances/imports/{chargeImportId}/undo).
func (h *Handler) UndoOpeningBalanceImport(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, id api.ChargeImportId, params api.UndoOpeningBalanceImportParams) {
	ifMatch, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	rec, err := h.Charges.UndoOpeningBalances(r.Context(), principal(r), marinaID, id, ifMatch)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.SetETag(w, rec.Version)
	respond.JSON(w, http.StatusOK, view.ChargeImport(rec))
}
