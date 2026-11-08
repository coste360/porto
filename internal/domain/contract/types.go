package contract

import (
	"fmt"

	"github.com/coste360/porto/internal/store"
)

// Kinds (contracts.kind). M3 produces mooring and transit; storage (M9) and charter (M4) are in the CHECK only.
const (
	KindMooring = store.ContractKindMooring
	KindTransit = store.ContractKindTransit
	KindStorage = store.ContractKindStorage
	KindCharter = store.ContractKindCharter
)

// Mooring terms.
const (
	TermAnnual   = "annual"
	TermSeasonal = "seasonal"
	TermMonthly  = "monthly"
	TermPeriod   = "period"
)

// Numbering series (number_sequences.series, m3-plan §4.5).
const (
	SeriesMooring = "mooring"
	SeriesTransit = "transit"
	SeriesQuote   = "quote"
	SeriesCharter = "charter" // M4
)

// Confirmation sources: M3 confirms with the manual driver only; the M5/M8 payment webhook calls the same
// ConfirmTransit with SourceProvider (INV-09). SourceChannel (M8, ADR 0013) is a verified channel reservation
// flagged as paid: the channel is the merchant of record (m8-plan §8.3).
const (
	SourceManual   = "manual"
	SourceProvider = "provider"
	SourceChannel  = "channel"
)

// Channels of contracts.channel. Desk and API create transits through TransitInput; online and external (M8)
// only through CreateOnlineTransitTx (m8-plan §4.3, §8.2).
const (
	ChannelDesk     = "desk"
	ChannelAPI      = "api"
	ChannelOnline   = "online"
	ChannelExternal = "external"
)

// ManualConfirmation is the manual driver input of a transit confirmation (m3-plan §4.2). Reference is free
// text, stored in confirmation_ref and never logged or emitted.
type ManualConfirmation struct {
	Reference      string
	PayAtDeparture bool
}

// Release policies of a contract.
const (
	ReleaseNever       = "never"
	ReleaseWithConsent = "with_consent"
)

// ContractCode is the display code of a contract, transit or quote: ORM-2026-0001, TRN-2026-0001,
// PRV-2026-0001 (m3-plan §4.5, §13 D20).
func ContractCode(series string, year int, number int64) string { //nolint:revive // name fixed by m3-plan §4.5
	prefix := map[string]string{SeriesMooring: "ORM", SeriesTransit: "TRN", SeriesQuote: "PRV", SeriesCharter: "CHR"}[series]
	if prefix == "" {
		prefix = "CTR"
	}
	return fmt.Sprintf("%s-%04d-%04d", prefix, year, number)
}
