package contract

import (
	"context"
	"encoding/json"
	"errors"
	"slices"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/domain/pricing"
	"github.com/coste360/porto/internal/store"
)

// Storage contracts (spec 17.1, m9-plan §3). The storage domain service (internal/domain/storage) authorises
// (storage.manage), validates and prices with PrepareStorage, chooses the position (optimizer.PlaceOne when none is
// given) and checks fit inside its transaction, then calls CreateStorageTx: every storage contract state change
// still goes through this package with audit and outbox in the same transaction (INV-30). Activation, cancel,
// termination, expiry, instalments and charges use the mooring paths unchanged (StorageTransitions = the mooring
// table, m9-plan D1).

// SeriesStorage is the numbering series of storage contracts (display prefix RIM).
const SeriesStorage = "storage"

// ChangeStorageDetails is the contract.changed change of a details update.
const ChangeStorageDetails = "storage_details"

// Storage supports and owners (storage_contract_details).
var (
	storageSupports = []string{store.StorageSupportCradle, store.StorageSupportTrailer, store.StorageSupportRack, store.StorageSupportNone}
	supportOwners   = []string{store.StorageSupportOwn, store.StorageSupportMarina}
)

// maxHandlingNotes bounds storage_contract_details.notes_handling.
const maxHandlingNotes = 500

// StorageInput is a storage contract to create. Exactly one target: a surface dynamic area with a placement
// (Segment with ResourceType dynamic_area, X, Y, RotationDd set by the caller) or a land resource of kind
// trailer_spot. Nights cover [StartsOn, EndsOn).
type StorageInput struct {
	Term              string // annual, seasonal, monthly, period
	CustomerID        string
	BillingCustomerID string
	VesselID          string
	StartsOn          calendar.Date
	EndsOn            *calendar.Date // default by term (m9-plan §3.1)
	Placement         Segment        // the single resource line
	Cradle            *ExtraResource // marina cradle (land resource kind cradle) for the whole period
	Services          []ServiceRequest
	Policy            *PolicyInput
	Notes             string
	Activate          bool
	FitOverride       *FitOverrideInput
	Details           StorageDetailsInput
	// Prepared is the result of PrepareStorage (required by CreateStorageTx).
	Prepared *PreparedStorage
}

// StorageDetailsInput are the storage_contract_details columns (m9-plan §3.1).
type StorageDetailsInput struct {
	Support       string // cradle, trailer, rack, none
	SupportOwner  string // own, marina
	HaulOutOn     *calendar.Date
	LaunchOn      *calendar.Date
	NotesHandling string
}

// StorageDetailsPatch changes the details; nil leaves a field unchanged, ClearHaulOut / ClearLaunch set it to null.
type StorageDetailsPatch struct {
	Support       *string
	SupportOwner  *string
	HaulOutOn     *calendar.Date
	ClearHaulOut  bool
	LaunchOn      *calendar.Date
	ClearLaunch   bool
	NotesHandling *string
}

// PreparedStorage is a validated and priced storage contract input (PrepareStorage): the period, the price and
// the merged policy, computed outside the write transaction.
type PreparedStorage struct {
	EndsOn calendar.Date
	price  *priced
	policy Policy
	notes  string
	st     *store.OccupancySettings
}

// StorageExitOn is the exit date of a storage contract (m9-plan §3.1, D5): the planned launch, else the end.
func StorageExitOn(d *store.StorageContractDetails, endsOn calendar.Date) calendar.Date {
	if d != nil && !d.LaunchOn.IsZero() {
		return d.LaunchOn
	}
	return endsOn
}

// StorageArrivalOn is the arrival date of a storage contract: the planned haul-out, else the start, clamped into
// [startsOn, endsOn).
func StorageArrivalOn(d *store.StorageContractDetails, startsOn, endsOn calendar.Date) calendar.Date {
	a := startsOn
	if d != nil && !d.HaulOutOn.IsZero() {
		a = d.HaulOutOn
	}
	if a.Before(startsOn) {
		a = startsOn
	}
	if !a.Before(endsOn) {
		a = endsOn.AddDays(-1)
	}
	return a
}

// storageSeasonEnd returns the end (exclusive) of the season window of the active storage list covering starts.
func storageSeasonEnd(ctx context.Context, db bun.IDB, scope store.MarinaScope, starts calendar.Date) (calendar.Date, bool) {
	lists, err := store.ActivePriceLists(ctx, db, scope)
	if err != nil {
		return calendar.Date{}, false
	}
	for i := range lists {
		l := &lists[i]
		if !slices.Contains(l.ContractTypes, string(pricing.ContractStorage)) || starts.Before(l.ValidFrom) || starts.After(l.ValidTo) {
			continue
		}
		var set pricing.ListSettings
		if json.Unmarshal(l.Settings, &set) != nil || set.SeasonWindow == nil || set.SeasonWindow.To.Before(starts) {
			return calendar.Date{}, false
		}
		return set.SeasonWindow.To.AddDays(1), true
	}
	return calendar.Date{}, false
}

// checkStorageDetails validates the details against the period [starts, ends).
func checkStorageDetails(v *auth.ValidationError, d *StorageDetailsInput, starts, ends calendar.Date) {
	if !slices.Contains(storageSupports, d.Support) {
		v.Add("support", "invalid")
	}
	if d.SupportOwner == "" {
		d.SupportOwner = store.StorageSupportOwn
	}
	if !slices.Contains(supportOwners, d.SupportOwner) {
		v.Add("support_owner", "invalid")
	}
	notes, ok := cleanText(d.NotesHandling, maxHandlingNotes)
	if !ok {
		v.Add("notes_handling", "too_long")
	}
	d.NotesHandling = notes
	checkStorageDates(v, d.HaulOutOn, d.LaunchOn, starts, ends)
}

// checkStorageDates checks the planned haul-out and launch: inside [starts, ends] and launch not before haul-out.
func checkStorageDates(v *auth.ValidationError, haul, launch *calendar.Date, starts, ends calendar.Date) {
	if haul != nil && (haul.Before(starts) || haul.After(ends)) {
		v.Add("haul_out_on", "out_of_period")
	}
	if launch != nil && (launch.Before(starts) || launch.After(ends)) {
		v.Add("launch_on", "out_of_period")
	}
	if haul != nil && launch != nil && launch.Before(*haul) {
		v.Add("launch_on", "before_haul_out")
	}
}

// PrepareStorage validates a storage contract input and prices it with the principal's rights (pricing.read): the
// period (default end by term, m9-plan §3.1), the details, the policy, the parties and the insurance policy. The
// caller has authorised storage.manage (and occupancy.override_fit for an override, AuthorizeFitOverride). It sets
// in.Prepared and the period of in.Placement.
func (s *Service) PrepareStorage(ctx context.Context, p *auth.Principal, scope store.MarinaScope, in *StorageInput) error {
	st, err := store.ContractOccupancySettings(ctx, s.d.DB.DB, scope, s.now())
	if err != nil {
		return err
	}
	v := &auth.ValidationError{}
	if !slices.Contains(terms, in.Term) {
		v.Add("term", "invalid")
	}
	if in.StartsOn.IsZero() {
		v.Add("starts_on", "required")
	}
	var ends calendar.Date
	switch {
	case in.EndsOn != nil:
		ends = *in.EndsOn
	case in.StartsOn.IsZero():
	case in.Term == TermSeasonal:
		e, ok := storageSeasonEnd(ctx, s.d.DB.DB, scope, in.StartsOn)
		if !ok {
			v.Add("ends_on", "required")
		}
		ends = e
	default:
		e, ok := DefaultEndsOn(in.Term, in.StartsOn)
		if !ok {
			v.Add("ends_on", "required")
		}
		ends = e
	}
	if len(v.Fields) == 0 {
		checkStay(v, in.StartsOn, ends, "starts_on", "ends_on", maxContractNights)
		checkStorageDetails(v, &in.Details, in.StartsOn, ends)
	}
	if in.Placement.ResourceType != store.ResourceArea && in.Placement.ResourceType != store.ResourceLand {
		v.Add("area_id", "required")
	}
	if in.Cradle != nil && !store.ValidID(in.Cradle.LandResourceID) {
		v.Add("cradle_resource_id", "invalid")
	}
	policy := mergePolicy(v, defaultPolicy(st), in.Policy)
	notes, ok := cleanText(in.Notes, 2000)
	if !ok {
		v.Add("notes", "too_long")
	}
	if in.FitOverride != nil && cleanReason(in.FitOverride) == "" {
		v.Add("fit_override.reason", "required")
	}
	if in.CustomerID == "" {
		v.Add("customer_id", "required")
	}
	if in.VesselID == "" {
		v.Add("vessel_id", "required")
	}
	if err := v.Err(); err != nil {
		return err
	}
	if err := checkParties(ctx, s.d.DB.DB, scope, in.VesselID, in.CustomerID, in.BillingCustomerID); err != nil {
		return err
	}
	cs, err := s.settingsTx(ctx, s.d.DB.DB, scope)
	if err != nil {
		return err
	}
	if _, err := checkInsurance(ctx, s.d.DB.DB, scope, cs, in.VesselID, in.StartsOn); err != nil {
		return err
	}
	in.Placement.First, in.Placement.Last = in.StartsOn, ends.AddDays(-1)
	price, err := s.price(ctx, p, scope.ID(), priceRequest{
		contractType: PricingType(KindStorage, in.Term), arrival: in.StartsOn, departure: ends, first: in.Placement,
		vesselID: in.VesselID, customerID: in.CustomerID, channel: ChannelDesk, services: in.Services,
	})
	if err != nil {
		return err
	}
	in.Prepared = &PreparedStorage{EndsOn: ends, price: price, policy: policy, notes: notes, st: st}
	return nil
}

// AuthorizeFitOverride checks occupancy.override_fit when a fit override is requested.
func (s *Service) AuthorizeFitOverride(ctx context.Context, p *auth.Principal, marinaID string, in *FitOverrideInput) error {
	return s.authorizeOverride(ctx, p, marinaID, overrideReason(in))
}

// CreateStorageTx writes the storage contract (kind storage, number series storage) with its lines, allocations
// (hold for a draft, contract when activated), storage_contract_details, instalment plan, audit contract.create and
// the events of StorageTransitions, all in tx. The caller has authorised (storage.manage), prepared the input
// (PrepareStorage) and checked fit. The returned Detail carries the row and the code only; read the full view with
// StorageDetail after the commit.
func (s *Service) CreateStorageTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, who Who, in StorageInput) (*Detail, error) {
	pr := in.Prepared
	if pr == nil {
		return nil, errors.New("contract: storage input not prepared")
	}
	var extras []extraLine
	if in.Cradle != nil {
		extras = append(extras, extraLine(*in.Cradle))
	}
	a := who.actor()
	c, err := s.createMooringTx(ctx, tx, scope, a, newContract{
		kind: KindStorage, term: in.Term, customerID: in.CustomerID, billingCustomerID: in.BillingCustomerID,
		vesselID: in.VesselID, startsOn: in.StartsOn, endsOn: pr.EndsOn, channel: ChannelDesk, price: pr.price,
		policy: pr.policy, notes: pr.notes, segments: []Segment{in.Placement}, extras: extras, services: in.Services,
		arrival: true, fit: fitOverride(a, in.FitOverride),
	}, in.Activate, pr.st)
	if err != nil {
		return nil, err
	}
	now := s.now()
	d := &store.StorageContractDetails{
		ContractID: c.ID, MarinaID: scope.ID(), Support: in.Details.Support, SupportOwner: in.Details.SupportOwner,
		NotesHandling: in.Details.NotesHandling, Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	if in.Details.HaulOutOn != nil {
		d.HaulOutOn = *in.Details.HaulOutOn
	}
	if in.Details.LaunchOn != nil {
		d.LaunchOn = *in.Details.LaunchOn
	}
	if err := store.InsertStorageDetails(ctx, tx, d); err != nil {
		return nil, err
	}
	// The full view (parties, display rights) is read after the commit: reading the principal's rights through the
	// pool while this write transaction holds the SQLite writer would wait for it.
	return &Detail{Row: *c, Code: Code(c)}, nil
}

// UpdateStorageDetailsTx locks the contract, checks the details version (ErrVersionMismatch) and writes the patch,
// audit storage.details_update and contract.changed, in tx.
func (s *Service) UpdateStorageDetailsTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, who Who, contractID string,
	version int64, patch StorageDetailsPatch,
) (*store.StorageContractDetails, error) {
	c, err := lockContract(ctx, tx, scope, contractID, KindStorage, 0)
	if err != nil {
		return nil, err
	}
	if c.Kind != KindStorage {
		return nil, notFound(store.ErrNotFound)
	}
	d, err := store.LockStorageDetails(ctx, tx, scope, contractID)
	if err != nil {
		return nil, notFound(err)
	}
	if version != 0 && int64(d.Version) != version {
		return nil, ErrVersionMismatch
	}
	if c.Status == string(StatusCancelled) || c.Status == string(StatusTerminated) || c.Status == string(StatusExpired) {
		return nil, ErrInvalidTransition
	}
	before := *d
	in := StorageDetailsInput{Support: d.Support, SupportOwner: d.SupportOwner, NotesHandling: d.NotesHandling}
	if !d.HaulOutOn.IsZero() {
		h := d.HaulOutOn
		in.HaulOutOn = &h
	}
	if !d.LaunchOn.IsZero() {
		l := d.LaunchOn
		in.LaunchOn = &l
	}
	if patch.Support != nil {
		in.Support = *patch.Support
	}
	if patch.SupportOwner != nil {
		in.SupportOwner = *patch.SupportOwner
	}
	if patch.NotesHandling != nil {
		in.NotesHandling = *patch.NotesHandling
	}
	switch {
	case patch.ClearHaulOut:
		in.HaulOutOn = nil
	case patch.HaulOutOn != nil:
		in.HaulOutOn = patch.HaulOutOn
	}
	switch {
	case patch.ClearLaunch:
		in.LaunchOn = nil
	case patch.LaunchOn != nil:
		in.LaunchOn = patch.LaunchOn
	}
	v := &auth.ValidationError{}
	checkStorageDetails(v, &in, c.StartsOn, c.EndsOn)
	if err := v.Err(); err != nil {
		return nil, err
	}
	d.Support, d.SupportOwner, d.NotesHandling = in.Support, in.SupportOwner, in.NotesHandling
	d.HaulOutOn, d.LaunchOn = calendar.Date{}, calendar.Date{}
	if in.HaulOutOn != nil {
		d.HaulOutOn = *in.HaulOutOn
	}
	if in.LaunchOn != nil {
		d.LaunchOn = *in.LaunchOn
	}
	if err := store.UpdateStorageDetails(ctx, tx, scope, d, before.Version, s.now()); err != nil {
		return nil, versionErr(err)
	}
	diff := map[string]any{
		"support": [2]string{before.Support, d.Support}, "support_owner": [2]string{before.SupportOwner, d.SupportOwner},
		"haul_out_on": [2]calendar.Date{before.HaulOutOn, d.HaulOutOn}, "launch_on": [2]calendar.Date{before.LaunchOn, d.LaunchOn},
		"notes_changed": before.NotesHandling != d.NotesHandling,
	}
	if err := s.writeAudit(ctx, tx, scope, who.actor(), "storage.details_update", entityContract, c.ID, diff); err != nil {
		return nil, err
	}
	if err := s.emit(ctx, tx, scope, event.ContractChanged, event.ContractChangedData{ContractID: c.ID, Change: ChangeStorageDetails}); err != nil {
		return nil, err
	}
	return d, nil
}

// StorageDetail reads a contract with the principal's display rights, without a permission check: the storage
// service has authorised storage.read (yard_mgr reads its storage contracts without contracts.read).
func (s *Service) StorageDetail(ctx context.Context, db bun.IDB, scope store.MarinaScope, p *auth.Principal, id string) (*Detail, error) {
	d, err := s.detail(ctx, db, scope, p, id)
	if err != nil {
		return nil, err
	}
	if d.Row.Kind != KindStorage {
		return nil, notFound(store.ErrNotFound)
	}
	return d, nil
}
