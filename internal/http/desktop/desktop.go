// Package desktop implements the /api/desktop/v1 operations of the desktop edition (m6a-plan §3.4) as thin
// handlers over desktopport.Runtime: decode, call the runtime (which checks the owner), map errors, encode. The
// router reaches them only on the loopback listener from a loopback peer (middleware.LoopbackOnly); with a nil
// Runtime (server edition) every operation answers 404.
package desktop

import (
	"errors"
	"net/http"
	"time"

	"github.com/coste360/porto/internal/desktopport"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/http/api"
	"github.com/coste360/porto/internal/http/middleware"
	"github.com/coste360/porto/internal/http/problem"
	"github.com/coste360/porto/internal/http/respond"
)

// InstanceTokenHeader carries the activation token of a second launch (security scheme instanceToken).
const InstanceTokenHeader = "Porto-Instance-Token"

// Handler serves the desktop API.
type Handler struct {
	// Runtime is nil in the server edition: every operation is then 404.
	Runtime desktopport.Runtime
	// SecureCookies is config.SecureCookies (false on the desktop loopback over plain http).
	SecureCookies bool
}

// available writes 404 and returns false when the instance is not the desktop edition.
func (h *Handler) available(w http.ResponseWriter, r *http.Request) bool {
	if h == nil || h.Runtime == nil {
		problem.Write(w, r, problem.NotFound)
		return false
	}
	return true
}

func principal(r *http.Request) *auth.Principal {
	p, _ := auth.PrincipalFrom(r.Context())
	return p
}

func timePtr(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// GetDesktopStatus implements api.ServerInterface.
func (h *Handler) GetDesktopStatus(w http.ResponseWriter, r *http.Request) {
	if !h.available(w, r) {
		return
	}
	st := h.Runtime.Status(r.Context())
	out := api.DesktopStatus{
		Version: st.Version, Mode: api.DesktopMode(st.Mode), FirstRunPending: st.FirstRunPending,
		SecretStore: api.DesktopStatusSecretStore(st.SecretStore),
	}
	if out.Mode == "" {
		out.Mode = api.DesktopMode(desktopport.ModeLocal)
	}
	if out.SecretStore == "" {
		out.SecretStore = api.DesktopStatusSecretStore(desktopport.SecretStoreKeyring)
	}
	if st.Update != nil {
		out.Update = &api.DesktopUpdate{Version: st.Update.Version, Url: st.Update.URL}
	}
	out.Kiosk = st.Kiosk
	if st.Remote != nil {
		out.Remote = remoteStatusView(*st.Remote)
	}
	respond.JSON(w, http.StatusOK, out)
}

// DesktopFirstRun implements api.ServerInterface.
func (h *Handler) DesktopFirstRun(w http.ResponseWriter, r *http.Request) {
	if !h.available(w, r) {
		return
	}
	var in api.DesktopFirstRunRequest
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	res, err := h.Runtime.FirstRun(r.Context(), desktopport.FirstRunInput{
		Token: in.Token, Organization: in.Organization, Name: in.Name, Email: in.Email, Password: in.Password,
		UpdateCheck: in.UpdateCheck,
	})
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	if err := middleware.SetSessionCookies(w, res.SessionToken, res.ExpiresAt, middleware.CookieSecure(r, h.SecureCookies)); err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusCreated, api.DesktopFirstRunResult{UserId: res.UserID})
}

// DesktopFirstRunRestore implements api.ServerInterface: the uploaded archive is restored with the first-run token
// (header), which is burnt once the restore is prepared.
func (h *Handler) DesktopFirstRunRestore(w http.ResponseWriter, r *http.Request, params api.DesktopFirstRunRestoreParams) {
	if !h.available(w, r) {
		return
	}
	src := desktopport.RestoreSource{Upload: r.Body}
	if err := h.Runtime.FirstRunRestore(r.Context(), params.PortoFirstRunToken, src); err != nil {
		respond.Error(w, r, err)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

func (h *Handler) restore(w http.ResponseWriter, r *http.Request, p *auth.Principal, src desktopport.RestoreSource) {
	if err := h.Runtime.Restore(r.Context(), p, src); err != nil {
		respond.Error(w, r, err)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

func settingsView(s desktopport.Settings) api.DesktopSettings {
	return api.DesktopSettings{
		Mode: api.DesktopMode(s.Mode), Port: s.Port, LanEnabled: s.LANEnabled, LanPort: s.LANPort,
		LanAddress: s.LANAddress, BackupDir: s.BackupDir, BackupKeepDaily: s.BackupKeepDaily,
		BackupKeepExit: s.BackupKeepExit, UpdateCheck: s.UpdateCheck, DismissedUpdate: s.DismissedUpdate,
		DataDir: strPtr(s.DataDir), LogsDir: strPtr(s.LogsDir),
	}
}

// GetDesktopSettings implements api.ServerInterface.
func (h *Handler) GetDesktopSettings(w http.ResponseWriter, r *http.Request) {
	if !h.available(w, r) {
		return
	}
	s, err := h.Runtime.Settings(r.Context(), principal(r))
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, settingsView(s))
}

// UpdateDesktopSettings implements api.ServerInterface.
func (h *Handler) UpdateDesktopSettings(w http.ResponseWriter, r *http.Request) {
	if !h.available(w, r) {
		return
	}
	var in api.DesktopSettingsPatch
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	s, err := h.Runtime.UpdateSettings(r.Context(), principal(r), desktopport.SettingsPatch{
		BackupDir: in.BackupDir, BackupKeepDaily: in.BackupKeepDaily, BackupKeepExit: in.BackupKeepExit,
		UpdateCheck: in.UpdateCheck, DismissedUpdate: in.DismissedUpdate,
	})
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, settingsView(s))
}

func lanView(s desktopport.LANStatus) api.LanStatus {
	addrs := s.Addresses
	if addrs == nil {
		addrs = []string{}
	}
	return api.LanStatus{
		Enabled: s.Enabled, Listening: s.Listening, Url: s.URL, Address: s.Address, Addresses: addrs, Port: s.Port,
		Fingerprint: s.Fingerprint, CaFingerprint: s.CAFingerprint, CertExpiresAt: timePtr(s.CertExpiresAt),
		Error: strPtr(s.Error), CaConstrained: s.CAConstrained, CaDownloadUrl: s.CADownloadURL,
	}
}

// GetDesktopLan implements api.ServerInterface. The owner check is the runtime's Settings call.
func (h *Handler) GetDesktopLan(w http.ResponseWriter, r *http.Request) {
	if !h.available(w, r) {
		return
	}
	if _, err := h.Runtime.Settings(r.Context(), principal(r)); err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, lanView(h.Runtime.LAN().Status()))
}

// SetDesktopLan implements api.ServerInterface. The runtime's controller checks the owner of the context.
func (h *Handler) SetDesktopLan(w http.ResponseWriter, r *http.Request) {
	if !h.available(w, r) {
		return
	}
	var in api.LanUpdate
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	lan := h.Runtime.LAN()
	if !in.Enabled {
		if err := lan.Disable(r.Context()); err != nil {
			respond.Error(w, r, err)
			return
		}
		respond.JSON(w, http.StatusOK, lanView(lan.Status()))
		return
	}
	o := desktopport.LANOptions{}
	if in.Port != nil {
		o.Port = *in.Port
	}
	if in.Address != nil {
		o.Address = *in.Address
	}
	st, err := lan.Enable(r.Context(), o)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, lanView(st))
}

// RotateLanCertificate implements api.ServerInterface.
func (h *Handler) RotateLanCertificate(w http.ResponseWriter, r *http.Request) {
	if !h.available(w, r) {
		return
	}
	var in api.LanCertificateRotate
	if !respond.DecodeOrFail(w, r, &in, true) {
		return
	}
	st, err := h.Runtime.LAN().RotateCertificate(r.Context(), in.All)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, lanView(st))
}

func backupView(f desktopport.BackupFile) api.DesktopBackupFile {
	return api.DesktopBackupFile{
		Id: f.ID, Kind: api.DesktopBackupKind(f.Kind), SizeBytes: f.SizeBytes, CreatedAt: f.CreatedAt, Sha256: f.SHA256,
	}
}

// ListDesktopBackups implements api.ServerInterface.
func (h *Handler) ListDesktopBackups(w http.ResponseWriter, r *http.Request) {
	if !h.available(w, r) {
		return
	}
	files, st, err := h.Runtime.Backups(r.Context(), principal(r))
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	out := api.DesktopBackupList{
		Items: make([]api.DesktopBackupFile, 0, len(files)),
		Status: api.DesktopBackupStatus{
			Dir: st.Dir, Fallback: st.Fallback, LastDailyAt: timePtr(st.LastDailyAt), LastError: strPtr(st.LastError),
			NextDueAt: timePtr(st.NextDueAt),
		},
	}
	for _, f := range files {
		out.Items = append(out.Items, backupView(f))
	}
	respond.JSON(w, http.StatusOK, out)
}

// CreateDesktopBackup implements api.ServerInterface (the Idempotency-Key is handled by the middleware).
func (h *Handler) CreateDesktopBackup(w http.ResponseWriter, r *http.Request, _ api.CreateDesktopBackupParams) {
	if !h.available(w, r) {
		return
	}
	f, err := h.Runtime.BackupNow(r.Context(), principal(r))
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusCreated, backupView(f))
}

// RestoreDesktopBackup implements api.ServerInterface.
func (h *Handler) RestoreDesktopBackup(w http.ResponseWriter, r *http.Request, backupID string) {
	if !h.available(w, r) {
		return
	}
	p := principal(r)
	if p == nil {
		respond.Error(w, r, auth.ErrUnauthenticated)
		return
	}
	h.restore(w, r, p, desktopport.RestoreSource{BackupID: backupID})
}

// UploadDesktopRestore implements api.ServerInterface: the raw archive body is streamed to disk by the runtime.
func (h *Handler) UploadDesktopRestore(w http.ResponseWriter, r *http.Request) {
	if !h.available(w, r) {
		return
	}
	p := principal(r)
	if p == nil {
		respond.Error(w, r, auth.ErrUnauthenticated)
		return
	}
	h.restore(w, r, p, desktopport.RestoreSource{Upload: r.Body})
}

// RevealDesktopSecretKey implements api.ServerInterface.
func (h *Handler) RevealDesktopSecretKey(w http.ResponseWriter, r *http.Request) {
	if !h.available(w, r) {
		return
	}
	var in api.DesktopSecretRevealRequest
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	key, err := h.Runtime.RevealSecretKey(r.Context(), principal(r), in.Password)
	if errors.Is(err, auth.ErrInvalidCredentials) {
		// 400, not 401: the session is fine, only the re-entered password is wrong.
		respond.Problem(w, r, problem.InvalidCredentials, problem.Status(http.StatusBadRequest))
		return
	}
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	respond.JSON(w, http.StatusOK, api.DesktopSecretKey{SecretKey: key})
}

// OpenDesktopFolder implements api.ServerInterface.
func (h *Handler) OpenDesktopFolder(w http.ResponseWriter, r *http.Request) {
	if !h.available(w, r) {
		return
	}
	var in api.DesktopOpenRequest
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	if err := h.Runtime.Open(r.Context(), principal(r), string(in.Target)); err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.NoContent(w)
}

// ActivateDesktop implements api.ServerInterface: the middleware treats it as public, the token is checked here
// by the runtime (constant time against the hash in instance.json).
func (h *Handler) ActivateDesktop(w http.ResponseWriter, r *http.Request) {
	if !h.available(w, r) {
		return
	}
	if err := h.Runtime.Activate(r.Context(), r.Header.Get(InstanceTokenHeader)); err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.NoContent(w)
}
