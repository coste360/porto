package handover

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"mime"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/filestore"
	"github.com/coste360/porto/internal/qty"
	"github.com/coste360/porto/internal/reqctx"
	"github.com/coste360/porto/internal/store"
)

// Handover drafts (m11-plan §7, §8, §12): checklist, fuel, engine hours and notes (offline kind rental.checklist,
// one effect per client_ref, INV-83), photos (encrypted filestore, INV-74), identity and licence typed by the
// operator (KeyRing, AAD rental-identity:<handover_id>, never logged, INV-70). A draft never changes status, slots,
// charges or deposits (INV-71); a completed handover is locked (409 handover_locked).

const (
	// MaxPhotoBytes bounds a handover photo.
	MaxPhotoBytes = 8 << 20
	// OfflineBefore and OfflineAfter bound the device time of an offline save around its queued instant.
	OfflineBefore = 48 * time.Hour
	OfflineAfter  = 5 * time.Minute
	maxClientRef  = 64
	filePurpose   = "document"
)

var photoTypes = []string{filestore.TypeJPEG, filestore.TypePNG}

// Identity document types.
var documentTypes = []string{"identity_card", "passport", "driving_licence", "other"}

// identityData is the encrypted identity of a check-out (personal data: never logged, never in events).
type identityData struct {
	DocumentType      string        `json:"document_type"`
	DocumentNumber    string        `json:"document_number"`
	DocumentExpiresOn calendar.Date `json:"document_expires_on"`
	DocumentIssuer    string        `json:"document_issuer,omitempty"`
	LicenceNumber     string        `json:"licence_number,omitempty"`
	LicenceCategory   string        `json:"licence_category,omitempty"`
	LicenceExpiresOn  calendar.Date `json:"licence_expires_on,omitzero"`
}

func identityAAD(handoverID string) string { return "rental-identity:" + handoverID }

func validPhase(phase string) bool {
	return phase == store.RentalPhaseCheckout || phase == store.RentalPhaseCheckin
}

// phaseStatus is the booking status a phase's draft needs: confirmed for the check-out, checked_out for the
// check-in.
func phaseStatus(phase string) string {
	if phase == store.RentalPhaseCheckin {
		return store.RentalStatusCheckedOut
	}
	return store.RentalStatusConfirmed
}

func invalid(field, code string) error {
	v := &auth.ValidationError{}
	v.Add(field, code)
	return v.Err()
}

// mask keeps the last 3 characters of a number.
func mask(s string) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) <= 3 {
		return strings.Repeat("*", len(r))
	}
	return strings.Repeat("*", len(r)-3) + string(r[len(r)-3:])
}

// checkDeviceTime checks the device time of a save: an offline replay needs queued_at - 48 h <= t <= queued_at +
// 5 min; online a device time may not be more than 5 minutes in the future.
func (s *Service) checkDeviceTime(ctx context.Context, t time.Time) error {
	if t.IsZero() {
		return nil
	}
	ref := s.d.Clock.Now()
	from := time.Time{}
	if q, ok := reqctx.OfflineReplay(ctx); ok {
		ref, from = q, q.Add(-OfflineBefore)
	}
	if t.After(ref.Add(OfflineAfter)) || (!from.IsZero() && t.Before(from)) {
		return ErrOfflineTimeOutOfRange
	}
	return nil
}

// draftCtx is what the missing steps of a handover depend on.
type draftCtx struct {
	booking  *store.RentalBooking
	unit     *store.RentalUnit
	settings *store.RentalSettings
	handover *store.RentalHandover // nil = nothing saved yet
	files    []store.RentalHandoverFileRow
	identity *identityData
	signed   bool
}

func (s *Service) loadDraft(ctx context.Context, db bun.IDB, scope store.MarinaScope, b *store.RentalBooking, phase string) (*draftCtx, error) {
	unit, err := store.GetRentalUnit(ctx, db, scope, b.UnitID)
	if err != nil {
		return nil, err
	}
	set, err := store.LoadRentalSettings(ctx, db, scope)
	if err != nil {
		return nil, err
	}
	d := &draftCtx{booking: b, unit: unit, settings: set}
	h, err := store.GetRentalHandover(ctx, db, scope, b.ID, phase)
	switch {
	case err == nil:
		d.handover = h
		if d.files, err = store.RentalHandoverFilesOf(ctx, db, scope, []string{h.ID}); err != nil {
			return nil, err
		}
		d.identity = s.decryptIdentity(h)
	case !errors.Is(err, store.ErrNotFound):
		return nil, err
	}
	if phase == store.RentalPhaseCheckout {
		if r, err := store.RentalContractSignature(ctx, db, scope, b.ID); err == nil && r.Status == "signed" {
			d.signed = true
		} else if err != nil && !errors.Is(err, store.ErrNotFound) {
			return nil, err
		}
	}
	return d, nil
}

func (s *Service) decryptIdentity(h *store.RentalHandover) *identityData {
	if h == nil || h.IdentityEnc == "" || s.d.Keys == nil {
		return nil
	}
	raw, err := s.d.Keys.DecryptString(h.IdentityEnc, identityAAD(h.ID))
	if err != nil {
		return nil
	}
	var id identityData
	if json.Unmarshal([]byte(raw), &id) != nil {
		return nil
	}
	return &id
}

// hasEngine reports whether the unit has an engine (engine hours and fuel are recorded).
func hasEngine(u *store.RentalUnit) bool { return u.FuelType != store.RentalFuelNone }

// hasFuel reports whether the unit has a fuel tank (fuel level recorded).
func hasFuel(u *store.RentalUnit) bool {
	return u.FuelType == store.RentalFuelPetrol || u.FuelType == store.RentalFuelDiesel
}

// checklistItems returns the checklist keys of the settings that apply to a unit category.
func checklistItems(set *store.RentalSettings, category string) []string {
	var list []store.RentalChecklistItem
	_ = json.Unmarshal(set.Checklist, &list)
	out := []string{}
	for _, it := range list {
		if len(it.Categories) == 0 || slices.Contains(it.Categories, category) {
			out = append(out, it.Key)
		}
	}
	return out
}

func (d *draftCtx) count(kind string) int {
	n := 0
	for _, f := range d.files {
		if f.Kind == kind {
			n++
		}
	}
	return n
}

// missing returns the steps still missing before the check-out or the check-in of a booking (the same list as
// handover_incomplete).
func (d *draftCtx) missing(phase string) []string {
	out := []string{}
	h := d.handover
	if phase == store.RentalPhaseCheckout {
		if d.identity == nil || d.count(store.RentalFileIDDocument) == 0 {
			out = append(out, MissingIdentity)
		}
		if snapshotOf(d.booking).LicenceRequired &&
			(d.identity == nil || d.identity.LicenceNumber == "" || d.identity.LicenceExpiresOn.IsZero() ||
				d.count(store.RentalFileLicence) == 0) {
			out = append(out, MissingLicence)
		}
		if !d.signed {
			out = append(out, MissingContract)
		}
		switch d.booking.DepositStatus {
		case store.RentalDepositStatusHeld, store.RentalDepositStatusAuthorized, store.RentalDepositStatusWaived:
		default:
			if d.booking.DepositCents > 0 { // the booking's snapshot, not the unit's current deposit
				out = append(out, MissingDeposit)
			}
		}
	}
	items := checklistItems(d.settings, d.unit.Category)
	if h == nil || (len(items) > 0 && checklistLen(h) == 0) {
		out = append(out, MissingChecklist)
	}
	if hasFuel(d.unit) && (h == nil || h.FuelEighths == nil) {
		out = append(out, MissingFuel)
	}
	if hasEngine(d.unit) && (h == nil || h.EngineHoursMilli == nil) {
		out = append(out, MissingEngineHours)
	}
	minPhotos := d.settings.CheckoutMinPhotos
	if phase == store.RentalPhaseCheckin {
		minPhotos = d.settings.CheckinMinPhotos
	}
	if d.count(store.RentalFileBoat) < minPhotos {
		out = append(out, MissingPhotos)
	}
	return out
}

func checklistLen(h *store.RentalHandover) int {
	var items []ChecklistEntry
	_ = json.Unmarshal(h.Checklist, &items)
	return len(items)
}

func (s *Service) draftView(d *draftCtx, phase string) *View {
	v := &View{Missing: d.missing(phase), ContractSigned: d.signed, Checklist: []ChecklistEntry{}, Files: []FileView{}}
	if d.handover != nil {
		v.Handover = *d.handover
		_ = json.Unmarshal(d.handover.Checklist, &v.Checklist)
		if v.Checklist == nil {
			v.Checklist = []ChecklistEntry{}
		}
	} else {
		v.Handover = store.RentalHandover{BookingID: d.booking.ID, Phase: phase, MarinaID: d.booking.MarinaID}
	}
	if d.identity != nil {
		v.Identity = &IdentitySummary{
			DocumentType: d.identity.DocumentType, DocumentNumberMasked: mask(d.identity.DocumentNumber),
			DocumentExpiresOn: d.identity.DocumentExpiresOn, LicenceNumberMasked: mask(d.identity.LicenceNumber),
			LicenceCategory: d.identity.LicenceCategory, LicenceExpiresOn: d.identity.LicenceExpiresOn,
		}
	} else if d.handover != nil && !d.handover.IdentityPurgedAt.IsZero() {
		v.Identity = &IdentitySummary{Purged: true}
	}
	for _, f := range d.files {
		v.Files = append(v.Files, FileView{RentalHandoverFile: f.RentalHandoverFile, ContentType: f.ContentType})
	}
	return v
}

// GetHandover returns the draft of a phase (version 0 when nothing is saved yet).
func (s *Service) GetHandover(ctx context.Context, p *auth.Principal, marinaID, bookingID, phase string) (*View, error) {
	scope, err := s.bookingScope(ctx, p, marinaID, bookingID)
	if err != nil {
		return nil, err
	}
	if !validPhase(phase) {
		return nil, auth.ErrNotFound
	}
	b, err := store.GetRentalBooking(ctx, s.d.DB.DB, scope, bookingID)
	if err != nil {
		return nil, notFound(err)
	}
	d, err := s.loadDraft(ctx, s.d.DB.DB, scope, b, phase)
	if err != nil {
		return nil, err
	}
	return s.draftView(d, phase), nil
}

// lockDraftTx locks the booking (status of the phase) and the handover row of the phase, creating an empty one
// when none exists. A completed handover is ErrHandoverLocked.
func (s *Service) lockDraftTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, p *auth.Principal, bookingID, phase string) (*store.RentalBooking, *store.RentalHandover, error) {
	b, err := s.d.Reservations.LockBookingTx(ctx, tx, scope, bookingID)
	if err != nil {
		return nil, nil, notFound(err)
	}
	h, err := store.LockRentalHandover(ctx, tx, scope, bookingID, phase)
	switch {
	case err == nil:
		if !h.CompletedAt.IsZero() {
			return nil, nil, ErrHandoverLocked
		}
	case errors.Is(err, store.ErrNotFound):
		now := s.now()
		h = &store.RentalHandover{
			ID: store.NewID(), MarinaID: scope.ID(), BookingID: bookingID, Phase: phase, Checklist: store.JSON(`[]`),
			Version: 1, CreatedAt: now, UpdatedAt: now,
		}
		if err := store.InsertRentalHandover(ctx, tx, h); err != nil {
			return nil, nil, err
		}
	default:
		return nil, nil, err
	}
	if b.Status != phaseStatus(phase) {
		return nil, nil, ErrInvalidTransition
	}
	_ = p
	return b, h, nil
}

// SaveChecklist saves checklist, fuel, engine hours and notes (offline-capable, one effect per client_ref).
func (s *Service) SaveChecklist(ctx context.Context, p *auth.Principal, marinaID, bookingID, phase string, in ChecklistInput) (*View, error) {
	scope, err := s.bookingScope(ctx, p, marinaID, bookingID)
	if err != nil {
		return nil, err
	}
	if !validPhase(phase) {
		return nil, auth.ErrNotFound
	}
	if err := checkChecklist(&in); err != nil {
		return nil, err
	}
	if err := s.checkDeviceTime(ctx, in.DeviceAt); err != nil {
		return nil, err
	}
	if prev, err := store.RentalHandoverByClientRef(ctx, s.d.DB.DB, scope, in.ClientRef); err == nil {
		if prev.BookingID != bookingID || prev.Phase != phase {
			return nil, invalid("client_ref", "reused")
		}
		return s.GetHandover(ctx, p, marinaID, bookingID, phase) // a replay has one effect (INV-83)
	} else if !errors.Is(err, store.ErrNotFound) {
		return nil, err
	}
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		b, h, err := s.lockDraftTx(ctx, tx, scope, p, bookingID, phase)
		if err != nil {
			return err
		}
		if h.ClientRef == in.ClientRef {
			return nil // a concurrent replay committed first: one effect (INV-83)
		}
		if !in.DeviceAt.IsZero() && !h.DeviceAt.IsZero() && in.DeviceAt.Before(h.DeviceAt) {
			// A queued save older than the stored one (replayed after a newer save from another device or online)
			// never overwrites it: the answer is the current handover (INV-83, one effect per client_ref).
			return nil
		}
		unit, err := store.GetRentalUnit(ctx, tx, scope, b.UnitID)
		if err != nil {
			return err
		}
		set, err := store.LoadRentalSettings(ctx, tx, scope)
		if err != nil {
			return err
		}
		allowed := checklistItems(set, unit.Category)
		for _, it := range in.Items {
			if !slices.Contains(allowed, it.Key) {
				return invalid("items.key", "unknown")
			}
		}
		items := in.Items
		if items == nil {
			items = []ChecklistEntry{}
		}
		raw, err := json.Marshal(items)
		if err != nil {
			return err
		}
		h.Checklist = store.JSON(raw)
		h.FuelEighths = in.FuelEighths
		if in.EngineHours != nil {
			v := int64(*in.EngineHours)
			h.EngineHoursMilli = &v
		}
		if in.Refuel != nil {
			v := int64(*in.Refuel)
			h.RefuelMilli = &v
		}
		h.Notes = in.Notes
		h.ClientRef = in.ClientRef
		h.DeviceAt = store.UTC(in.DeviceAt)
		if in.DeviceAt.IsZero() {
			h.DeviceAt = s.now() // an online save is as recent as the server clock, so older queued saves never win
		}
		if err := store.UpdateRentalHandover(ctx, tx, scope, h, s.now()); err != nil {
			if errors.Is(err, store.ErrConflict) {
				return invalid("client_ref", "reused")
			}
			return err
		}
		if err := s.audit(ctx, tx, scope, p.Actor(), "rental_handover.save", "rental_handover", h.ID, map[string]any{
			"booking_id": b.ID, "phase": phase, "items": len(items), "fuel_eighths": h.FuelEighths,
			"engine_hours_milli": h.EngineHoursMilli, "refuel_milli": h.RefuelMilli,
		}); err != nil {
			return err
		}
		return s.changed(ctx, tx, scope, b, "handover")
	})
	if err != nil {
		return nil, err
	}
	return s.GetHandover(ctx, p, marinaID, bookingID, phase)
}

func checkChecklist(in *ChecklistInput) error {
	v := &auth.ValidationError{}
	in.ClientRef = strings.TrimSpace(in.ClientRef)
	if n := len(in.ClientRef); n < 8 || n > maxClientRef {
		v.Add("client_ref", "invalid")
	}
	if len(in.Items) > 30 {
		v.Add("items", "count")
	}
	seen := map[string]bool{}
	for _, it := range in.Items {
		if it.Key == "" || seen[it.Key] {
			v.Add("items.key", "invalid")
		}
		seen[it.Key] = true
		if utf8.RuneCountInString(it.Note) > 200 {
			v.Add("items.note", "too_long")
		}
	}
	if in.FuelEighths != nil && (*in.FuelEighths < 0 || *in.FuelEighths > 8) {
		v.Add("fuel_eighths", "range")
	}
	if in.EngineHours != nil && *in.EngineHours < 0 {
		v.Add("engine_hours_milli", "range")
	}
	if in.Refuel != nil && *in.Refuel < 0 {
		v.Add("refuel_milli", "range")
	}
	in.Notes = strings.TrimSpace(in.Notes)
	if utf8.RuneCountInString(in.Notes) > 1000 {
		v.Add("notes", "too_long")
	}
	return v.Err()
}

// UploadPhoto attaches a photo to a handover (encrypted filestore).
func (s *Service) UploadPhoto(ctx context.Context, p *auth.Principal, marinaID, bookingID, phase string, in PhotoInput) (*FileView, error) {
	scope, err := s.bookingScope(ctx, p, marinaID, bookingID)
	if err != nil {
		return nil, err
	}
	if !validPhase(phase) {
		return nil, auth.ErrNotFound
	}
	switch {
	case phase == store.RentalPhaseCheckout && (in.Kind == store.RentalFileIDDocument || in.Kind == store.RentalFileLicence || in.Kind == store.RentalFileBoat):
	case phase == store.RentalPhaseCheckin && (in.Kind == store.RentalFileBoat || in.Kind == store.RentalFileDamage):
	default:
		return nil, invalid("kind", "invalid")
	}
	if _, offline := reqctx.OfflineReplay(ctx); offline && in.Kind != store.RentalFileBoat {
		return nil, invalid("kind", "online_only") // identity, licence and damage photos are online (§12)
	}
	if (in.Kind == store.RentalFileDamage) != (in.DamageID != "") {
		return nil, invalid("damage_id", "invalid")
	}
	if len(in.ClientRef) > maxClientRef {
		in.ClientRef = ""
	}
	if in.ClientRef != "" {
		if prev, err := store.RentalHandoverFileByClientRef(ctx, s.d.DB.DB, scope, in.ClientRef); err == nil {
			return &FileView{RentalHandoverFile: prev.RentalHandoverFile, ContentType: prev.ContentType}, nil
		} else if !errors.Is(err, store.ErrNotFound) {
			return nil, err
		}
	}
	mt, _, perr := mime.ParseMediaType(in.ContentType)
	if perr != nil || !slices.Contains(photoTypes, mt) {
		return nil, ErrUnsupportedMedia
	}
	if len(in.Body) > MaxPhotoBytes {
		return nil, ErrFileTooLarge
	}
	if len(in.Body) == 0 {
		return nil, invalid("body", "required")
	}
	ct, w, h, err := filestore.Sniff(in.Body, mt)
	if err != nil {
		if errors.Is(err, filestore.ErrUnsupportedType) {
			return nil, ErrUnsupportedMedia
		}
		return nil, invalid("body", "invalid")
	}
	if s.d.Files == nil {
		return nil, errors.New("handover: file store not wired")
	}
	sum := sha256.Sum256(in.Body)
	digest := hex.EncodeToString(sum[:])
	fileID := store.NewID()
	blob, err := s.d.Files.Put(ctx, scope.ID(), fileID, bytes.NewReader(in.Body), MaxPhotoBytes)
	if err != nil {
		return nil, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = s.d.Files.Remove(blob.StorageKey)
		}
	}()
	var out *FileView
	stored := false
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		stored = false
		b, hv, err := s.lockDraftTx(ctx, tx, scope, p, bookingID, phase)
		if err != nil {
			return err
		}
		if in.DamageID != "" {
			dm, err := store.GetRentalDamage(ctx, tx, scope, in.DamageID)
			if err != nil || dm.BookingID != b.ID {
				return auth.ErrNotFound
			}
		}
		files, err := store.RentalHandoverFilesOf(ctx, tx, scope, []string{hv.ID})
		if err != nil {
			return err
		}
		for _, f := range files {
			if f.SHA256 == digest && f.Kind == in.Kind && f.DamageID == in.DamageID {
				out = &FileView{RentalHandoverFile: f.RentalHandoverFile, ContentType: f.ContentType}
				return nil // the same bytes again: the file already stored
			}
		}
		now := s.now()
		if err := store.InsertStoredFile(ctx, tx, &store.StoredFile{
			ID: fileID, MarinaID: scope.ID(), Purpose: filePurpose, ContentType: ct, SizeBytes: blob.Size,
			SHA256: blob.SHA256, StorageKey: blob.StorageKey, WidthPx: int64(w), HeightPx: int64(h),
			CreatedByUserID: p.UserID, CreatedAt: now, UpdatedAt: now,
		}); err != nil {
			return err
		}
		row := store.RentalHandoverFile{
			ID: store.NewID(), MarinaID: scope.ID(), HandoverID: hv.ID, DamageID: in.DamageID, Kind: in.Kind, FileID: fileID,
			ClientRef: in.ClientRef, CreatedByUserID: p.UserID, CreatedAt: now,
		}
		if err := store.InsertRentalHandoverFile(ctx, tx, &row); err != nil {
			return err
		}
		if err := s.audit(ctx, tx, scope, p.Actor(), "rental_handover.photo", "rental_handover", hv.ID, map[string]any{
			"booking_id": b.ID, "phase": phase, "kind": in.Kind, "file_id": fileID, "damage_id": in.DamageID,
		}); err != nil {
			return err
		}
		out = &FileView{RentalHandoverFile: row, ContentType: ct}
		stored = true
		return s.changed(ctx, tx, scope, b, "handover")
	})
	if err != nil {
		if errors.Is(err, store.ErrConflict) && in.ClientRef != "" {
			if prev, perr := store.RentalHandoverFileByClientRef(ctx, s.d.DB.DB, scope, in.ClientRef); perr == nil {
				return &FileView{RentalHandoverFile: prev.RentalHandoverFile, ContentType: prev.ContentType}, nil
			}
		}
		return nil, err
	}
	committed = stored // deduplicated bytes leave the new blob unreferenced: it is removed
	return out, nil
}

// PutIdentity stores the renter's identity and licence (checkout phase).
func (s *Service) PutIdentity(ctx context.Context, p *auth.Principal, marinaID, bookingID string, in IdentityInput) (*View, error) {
	scope, err := s.bookingScope(ctx, p, marinaID, bookingID)
	if err != nil {
		return nil, err
	}
	id, err := s.checkIdentity(ctx, scope, in)
	if err != nil {
		return nil, err
	}
	if s.d.Keys == nil {
		return nil, errors.New("handover: key ring not wired")
	}
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		b, h, err := s.lockDraftTx(ctx, tx, scope, p, bookingID, store.RentalPhaseCheckout)
		if err != nil {
			return err
		}
		if !id.LicenceExpiresOn.IsZero() && id.LicenceExpiresOn.Before(b.EndsOn) {
			return ErrLicenceExpired
		}
		raw, err := json.Marshal(id)
		if err != nil {
			return err
		}
		if h.IdentityEnc, err = s.d.Keys.EncryptString(string(raw), identityAAD(h.ID)); err != nil {
			return err
		}
		if err := store.UpdateRentalHandover(ctx, tx, scope, h, s.now()); err != nil {
			return err
		}
		if err := s.audit(ctx, tx, scope, p.Actor(), "rental_handover.identity", "rental_handover", h.ID, map[string]any{
			"booking_id": b.ID, "document_type": id.DocumentType, "has_licence": id.LicenceNumber != "",
		}); err != nil {
			return err
		}
		return s.changed(ctx, tx, scope, b, "handover")
	})
	if err != nil {
		return nil, err
	}
	return s.GetHandover(ctx, p, marinaID, bookingID, store.RentalPhaseCheckout)
}

func (s *Service) checkIdentity(ctx context.Context, scope store.MarinaScope, in IdentityInput) (*identityData, error) {
	v := &auth.ValidationError{}
	id := &identityData{
		DocumentType: in.DocumentType, DocumentNumber: strings.TrimSpace(in.DocumentNumber),
		DocumentExpiresOn: in.DocumentExpiresOn, DocumentIssuer: strings.TrimSpace(in.DocumentIssuer),
		LicenceNumber: strings.TrimSpace(in.LicenceNumber), LicenceCategory: strings.TrimSpace(in.LicenceCategory),
		LicenceExpiresOn: in.LicenceExpiresOn,
	}
	if !slices.Contains(documentTypes, id.DocumentType) {
		v.Add("document_type", "invalid")
	}
	if n := utf8.RuneCountInString(id.DocumentNumber); n == 0 || n > 32 {
		v.Add("document_number", "invalid")
	}
	today := s.today(ctx, s.d.DB.DB, scope)
	if id.DocumentExpiresOn.IsZero() {
		v.Add("document_expires_on", "required")
	} else if id.DocumentExpiresOn.Before(today) {
		v.Add("document_expires_on", "expired")
	}
	if utf8.RuneCountInString(id.DocumentIssuer) > 80 {
		v.Add("document_issuer", "too_long")
	}
	if utf8.RuneCountInString(id.LicenceNumber) > 32 {
		v.Add("licence_number", "too_long")
	}
	if utf8.RuneCountInString(id.LicenceCategory) > 32 {
		v.Add("licence_category", "too_long")
	}
	if (id.LicenceNumber == "") != id.LicenceExpiresOn.IsZero() {
		if id.LicenceNumber == "" {
			v.Add("licence_number", "required")
		} else {
			v.Add("licence_expires_on", "required")
		}
	}
	if err := v.Err(); err != nil {
		return nil, err
	}
	if !id.LicenceExpiresOn.IsZero() && id.LicenceExpiresOn.Before(today) {
		return nil, ErrLicenceExpired
	}
	return id, nil
}

// qtyPtr converts an optional stored milli value.
func qtyPtr(v *int64) *qty.Milli {
	if v == nil {
		return nil
	}
	m := qty.Milli(*v)
	return &m
}
