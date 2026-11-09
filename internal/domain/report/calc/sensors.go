package calc

import (
	"cmp"
	"slices"
	"time"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/sensor/nights"
)

// R-sensors (M12, m12-plan §10.2-§10.3): the physical night of a sensor (majority of [22:00, 06:00) local, pure
// nights.Classify) against the contractual status of the night from allocations and releases, plus the presence of
// the expected vessel at 02:00. SensorNights is shared by the report, the per-resource history and the map of a
// past date; BuildSensors folds the discrepancy rows (Totals = Σ rows, INV-62). Codes and ids only: never vessel
// names or customers.

// Contractual statuses of a night (OpenAPI SensorContractual).
const (
	SensorContractFree           = "free"
	SensorContractContract       = "contract"
	SensorContractTransit        = "transit"
	SensorContractReleased       = "released"
	SensorContractReleasedResold = "released_resold"
	SensorContractBlock          = "block"
	SensorContractMaintenance    = "maintenance"
)

// Discrepancy kinds (OpenAPI SensorDiscrepancyKind).
const (
	SensorKindOccupiedUnexpected = "occupied_unexpected"
	SensorKindAbsent             = "absent"
)

// SensorsGroups are the group_by values of the sensors report.
var SensorsGroups = []string{"resource", "map", "group", "kind", "night", "week", "month"}

// SensorSubject is a device for the night classification. TargetID is the resource of the contractual facts (the
// berth of a pedestal device); Informational devices are never compared.
type SensorSubject struct {
	DeviceID, DeviceCode     string
	ResourceType, ResourceID string
	ResourceCode             string
	TargetID                 string
	MapID, MapName           string
	FromCm, ToCm             *int64
	Groups                   []string
	Informational            bool
	StateBefore              string          // confirmed state before the first change (unknown when "")
	Changes                  []nights.Change // ascending
}

// SensorStayRow is an allocation of a target resource.
type SensorStayRow struct {
	AllocationID, ResourceID, VesselID string
	AllocationKind, ContractKind       string
	FirstNight, LastNight              calendar.Date
	XCm                                *int64
	LoaCm                              int64
}

// SensorReleaseRow is an active release of a target resource.
type SensorReleaseRow struct {
	ID, AllocationID, ResourceID string
	FromNight, ToNight           calendar.Date
}

// SensorPresenceRow is a presence event of a vessel.
type SensorPresenceRow struct {
	VesselID, Event string
	At              time.Time
}

// SensorAlertRow is an alert of a device.
type SensorAlertRow struct {
	ID, DeviceID string
	OpenedAt     time.Time
	ClearedAt    *time.Time
}

// SensorNightsInput is what SensorNights reads.
type SensorNightsInput struct {
	From, To  calendar.Date
	Loc       *time.Location
	Subjects  []SensorSubject
	Stays     []SensorStayRow
	Releases  []SensorReleaseRow
	Presences []SensorPresenceRow // ascending per vessel
	Alerts    []SensorAlertRow
}

// SensorNight is the comparison of one device on one night.
type SensorNight struct {
	DeviceID    string
	Night       calendar.Date
	Contractual string
	Physical    string
	Presence    string // in_port, out, ashore, yard at 02:00 of the single expected vessel ("" = unknown)
	Kind        string // occupied_unexpected, absent, "" (no discrepancy, or informational)
	AlertIDs    []string
}

func coversNight(first, last, d calendar.Date) bool { return !d.Before(first) && !d.After(last) }

// intersects reports whether an area placement [x, x+loa) meets the segment [from, to) (no position: yes).
func intersects(st SensorStayRow, from, to *int64) bool {
	if from == nil || to == nil || st.XCm == nil {
		return true
	}
	return *st.XCm < *to && *st.XCm+st.LoaCm > *from
}

// presenceStateAfter is the vessel state after a presence event.
func presenceStateAfter(ev string) string {
	switch ev {
	case "departed":
		return "out"
	case "hauled_out":
		return "ashore"
	default:
		return "in_port"
	}
}

// SensorContractual returns the contractual status of a night from the stays and releases intersecting the
// subject, and the vessel of the single non-released vessel stay ("" otherwise).
func SensorContractual(stays []SensorStayRow, rels []SensorReleaseRow, d calendar.Date) (string, string) {
	released := map[string]bool{}
	anyRelease := false
	for _, r := range rels {
		if coversNight(r.FromNight, r.ToNight, d) {
			released[r.AllocationID] = true
		}
	}
	var vessels []SensorStayRow
	block, maint, releasedOnly := false, false, false
	for _, s := range stays {
		if !coversNight(s.FirstNight, s.LastNight, d) {
			continue
		}
		switch {
		case s.AllocationKind == "block":
			block = true
		case s.AllocationKind == "maintenance":
			maint = true
		case s.VesselID == "":
		case released[s.AllocationID]:
			releasedOnly, anyRelease = true, true
		default:
			vessels = append(vessels, s)
		}
	}
	vessel := ""
	if len(vessels) == 1 {
		vessel = vessels[0].VesselID
	}
	switch {
	case len(vessels) > 0 && anyRelease:
		return SensorContractReleasedResold, vessel
	case len(vessels) > 0:
		for _, s := range vessels {
			if s.AllocationKind == "transit" || s.ContractKind == "transit" {
				return SensorContractTransit, vessel
			}
		}
		return SensorContractContract, vessel
	case releasedOnly:
		return SensorContractReleased, ""
	case block:
		return SensorContractBlock, ""
	case maint:
		return SensorContractMaintenance, ""
	}
	return SensorContractFree, ""
}

// SensorDiscrepancyKind is the discrepancy of a physical night against its contractual status (m12-plan §10.3).
func SensorDiscrepancyKind(physical, contractual string) string {
	switch {
	case physical == nights.Occupied && (contractual == SensorContractFree || contractual == SensorContractReleased ||
		contractual == SensorContractBlock || contractual == SensorContractMaintenance):
		return SensorKindOccupiedUnexpected
	case physical == nights.Free && (contractual == SensorContractContract || contractual == SensorContractTransit ||
		contractual == SensorContractReleasedResold):
		return SensorKindAbsent
	}
	return ""
}

// SensorNightInstant is 02:00 local of the morning after night d (presence and past map state of a night).
func SensorNightInstant(d calendar.Date, loc *time.Location) time.Time {
	n := d.AddDays(1)
	return time.Date(n.Year(), n.Month(), n.Day(), 2, 0, 0, 0, loc)
}

// SensorNights classifies every night of [From, To] of every subject (ordered by subject order, then night).
func SensorNights(in SensorNightsInput) []SensorNight {
	loc := in.Loc
	if loc == nil {
		loc = time.UTC
	}
	staysOf := map[string][]SensorStayRow{}
	for _, s := range in.Stays {
		staysOf[s.ResourceID] = append(staysOf[s.ResourceID], s)
	}
	relsOf := map[string][]SensorReleaseRow{}
	for _, r := range in.Releases {
		relsOf[r.ResourceID] = append(relsOf[r.ResourceID], r)
	}
	presOf := map[string][]SensorPresenceRow{}
	for _, p := range in.Presences {
		presOf[p.VesselID] = append(presOf[p.VesselID], p)
	}
	alertsOf := map[string][]SensorAlertRow{}
	for _, a := range in.Alerts {
		alertsOf[a.DeviceID] = append(alertsOf[a.DeviceID], a)
	}
	var out []SensorNight
	for _, sub := range in.Subjects {
		phys := nights.Classify(sub.StateBefore, sub.Changes, in.From, in.To, loc)
		var stays []SensorStayRow
		for _, s := range staysOf[sub.TargetID] {
			if intersects(s, sub.FromCm, sub.ToCm) {
				stays = append(stays, s)
			}
		}
		for d := in.From; !d.After(in.To); d = d.AddDays(1) {
			n := SensorNight{DeviceID: sub.DeviceID, Night: d, Physical: phys[d], Contractual: SensorContractFree}
			if sub.Informational || sub.TargetID == "" {
				out = append(out, n)
				continue
			}
			var vessel string
			n.Contractual, vessel = SensorContractual(stays, relsOf[sub.TargetID], d)
			if vessel != "" {
				at := SensorNightInstant(d, loc)
				for _, p := range presOf[vessel] {
					if p.At.After(at) {
						break
					}
					n.Presence = presenceStateAfter(p.Event)
				}
			}
			n.Kind = SensorDiscrepancyKind(n.Physical, n.Contractual)
			_, end := nights.Window(d, loc)
			dayStart := d.Time(loc)
			for _, a := range alertsOf[sub.DeviceID] {
				if a.OpenedAt.Before(end) && (a.ClearedAt == nil || !a.ClearedAt.Before(dayStart)) {
					n.AlertIDs = append(n.AlertIDs, a.ID)
				}
			}
			out = append(out, n)
		}
	}
	return out
}

// SensorsTotals (OpenAPI SensorsReportTotals): Records, OccupiedUnexpected, Absent and ResourcesWithDiscrepancy fold
// the rows; NightsCompared and CoverageBP are the summary over every compared night (top level only).
type SensorsTotals struct {
	Records                  int64
	OccupiedUnexpected       Opt[int64]
	Absent                   Opt[int64]
	ResourcesWithDiscrepancy Opt[int64]
	NightsCompared           Opt[int64]
	CoverageBP               BP
}

// SensorsRecord is one (resource, night) discrepancy (OpenAPI SensorsReportRecord).
type SensorsRecord struct {
	Night                    calendar.Date
	MapID, MapName           string
	ResourceType, ResourceID string
	ResourceCode             string
	FromCm, ToCm             *int64
	Contractual, Presence    string
	Physical, Kind           string
	AlertIDs                 []string
	Groups                   []string
	Ref                      Ref
}

// SensorsResult is the sensors report.
type SensorsResult = Result[SensorsTotals, SensorsRecord]

// SensorsInput is the sensors report input: the night comparison and the filters.
type SensorsInput struct {
	SensorNightsInput
	GroupBy, MapID, GroupID, Kind string
}

// FoldSensors folds discrepancy rows.
func FoldSensors(rs []SensorsRecord) SensorsTotals {
	t := SensorsTotals{OccupiedUnexpected: Some[int64](0), Absent: Some[int64](0), ResourcesWithDiscrepancy: Some[int64](0)}
	res := map[string]bool{}
	for _, r := range rs {
		t.Records++
		switch r.Kind {
		case SensorKindOccupiedUnexpected:
			t.OccupiedUnexpected.Value++
		case SensorKindAbsent:
			t.Absent.Value++
		}
		res[r.ResourceID] = true
	}
	t.ResourcesWithDiscrepancy.Value = int64(len(res))
	return t
}

// BuildSensors builds the sensors report, view discrepancies.
func BuildSensors(in SensorsInput) SensorsResult {
	keep := map[string]SensorSubject{}
	var subs []SensorSubject
	for _, s := range in.Subjects {
		if in.MapID != "" && s.MapID != in.MapID {
			continue
		}
		if in.GroupID != "" && !slices.Contains(s.Groups, in.GroupID) {
			continue
		}
		keep[s.DeviceID] = s
		subs = append(subs, s)
	}
	nin := in.SensorNightsInput
	nin.Subjects = subs
	var compared, covered int64
	recs := []SensorsRecord{}
	for _, n := range SensorNights(nin) {
		s := keep[n.DeviceID]
		if s.Informational || s.TargetID == "" {
			continue
		}
		compared++
		if n.Physical != nights.Unknown {
			covered++
		}
		if n.Kind == "" || (in.Kind != "" && n.Kind != in.Kind) {
			continue
		}
		ids := n.AlertIDs
		if ids == nil {
			ids = []string{}
		}
		recs = append(recs, SensorsRecord{
			Night: n.Night, MapID: s.MapID, MapName: s.MapName, ResourceType: s.ResourceType, ResourceID: s.ResourceID,
			ResourceCode: s.ResourceCode, FromCm: s.FromCm, ToCm: s.ToCm, Contractual: n.Contractual, Presence: n.Presence,
			Physical: n.Physical, Kind: n.Kind, AlertIDs: ids, Groups: s.Groups,
			Ref: sensorRef(s),
		})
	}
	slices.SortStableFunc(recs, func(a, b SensorsRecord) int {
		if c := cmpDate(a.Night, b.Night); c != 0 {
			return c
		}
		if c := cmp.Compare(a.ResourceCode, b.ResourceCode); c != 0 {
			return c
		}
		return cmp.Compare(ptrVal(a.FromCm), ptrVal(b.FromCm))
	})
	res := SensorsResult{Records: recs, Totals: FoldSensors(recs)}
	res.Totals.NightsCompared = Some(compared)
	res.Totals.CoverageBP = Ratio(covered, compared)
	key := map[string]func(SensorsRecord) []string{
		"resource": func(r SensorsRecord) []string { return []string{orNone(r.ResourceCode)} },
		"map":      func(r SensorsRecord) []string { return []string{orNone(r.MapName)} },
		"group": func(r SensorsRecord) []string {
			if len(r.Groups) == 0 {
				return []string{"none"}
			}
			return r.Groups
		},
		"kind":  func(r SensorsRecord) []string { return []string{r.Kind} },
		"night": func(r SensorsRecord) []string { return []string{r.Night.String()} },
		"week":  func(r SensorsRecord) []string { return []string{WeekKey(r.Night)} },
		"month": func(r SensorsRecord) []string { return []string{MonthKey(r.Night)} },
	}[in.GroupBy]
	if key != nil {
		res.Groups = GroupRecords(recs, key, FoldSensors)
	}
	return res
}

// sensorRef links a row to the history of its resource (ReportRef types berth and area; a pedestal device to the
// berth of its socket).
func sensorRef(s SensorSubject) Ref {
	switch s.ResourceType {
	case "dynamic_area":
		return Ref{Type: "area", ID: s.ResourceID, Label: s.ResourceCode}
	case "pedestal":
		return Ref{Type: "berth", ID: s.TargetID, Label: s.ResourceCode}
	}
	return Ref{Type: "berth", ID: s.ResourceID, Label: s.ResourceCode}
}

func ptrVal(p *int64) int64 {
	if p == nil {
		return -1
	}
	return *p
}
