package admin

import (
	"net/http"

	"github.com/coste360/porto/internal/csvimport"
	"github.com/coste360/porto/internal/http/api"
	"github.com/coste360/porto/internal/http/respond"
	"github.com/coste360/porto/internal/http/view"
)

// CSV imports (m4-plan §3.7, §7.3.5). The runs use the deadline service's environment (database, clock and audit
// writer), the only one the handler can reach without new wiring.

func (h *Handler) importEnv() csvimport.Env { return h.Deadlines.ImportEnv() }

// CreateImport implements api.ServerInterface (POST /api/admin/v1/marinas/{marinaId}/imports).
func (h *Handler) CreateImport(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, _ api.CreateImportParams) {
	var in api.ImportInput
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	ii := csvimport.Input{
		Entity: string(in.Entity), CSV: in.Csv, HasHeader: in.HasHeader, Mapping: csvimport.Mapping(in.Mapping), DryRun: in.DryRun,
	}
	if in.Delimiter != nil {
		ii.Delimiter = string(*in.Delimiter)
	}
	rep, err := h.Imports.Run(r.Context(), h.importEnv(), principal(r), marinaID, ii)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, view.ImportReport(rep))
}

// ListImports implements api.ServerInterface (GET /api/admin/v1/marinas/{marinaId}/imports).
func (h *Handler) ListImports(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, params api.ListImportsParams) {
	page, err := h.Imports.List(r.Context(), h.importEnv(), principal(r), marinaID, str(enumPtr(params.Entity)), str(params.Cursor), intOr(params.Limit, 0))
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, view.ImportRuns(page.Items, page.NextCursor))
}

// GetImport implements api.ServerInterface (GET /api/admin/v1/marinas/{marinaId}/imports/{importRunId}).
func (h *Handler) GetImport(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, id api.ImportRunId) {
	run, err := h.Imports.Get(r.Context(), h.importEnv(), principal(r), marinaID, id)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.SetETag(w, run.Version)
	respond.JSON(w, http.StatusOK, view.ImportRun(run))
}

// UndoImport implements api.ServerInterface (POST /api/admin/v1/marinas/{marinaId}/imports/{importRunId}/undo).
func (h *Handler) UndoImport(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, id api.ImportRunId, params api.UndoImportParams) {
	version, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	run, err := h.Imports.Undo(r.Context(), h.importEnv(), principal(r), marinaID, id, version)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.SetETag(w, run.Version)
	respond.JSON(w, http.StatusOK, view.ImportRun(run))
}
