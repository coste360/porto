package docs

import (
	"github.com/coste360/porto/internal/domain/docs/views"
	"github.com/coste360/porto/internal/money"
)

// The rental contract document (M11, kind rental_contract, m11-plan §10; owned by HANDOVER): composed at the
// confirmation of a booking (subject rental_booking) and signed at check-out (on screen, OTP or paper). The renter's
// data are decrypted by the data source (internal/domain/rental/handover) inside the encrypted snapshot only. Local
// times are formatted by the data source in the marina time zone (the template engine prints instants in UTC).

// SubjectRentalBooking is generated_documents.subject_type of the rental contract and handover documents.
const SubjectRentalBooking SubjectType = "rental_booking"

// RentalInfo are the data of a rental booking printed on its documents.
type RentalInfo struct {
	Number          string      `json:"number" doc:"number,fields.rental.number"`
	UnitCode        string      `json:"unit_code" doc:"unit_code,fields.rental.unit_code"`
	Category        string      `json:"category" doc:"category,fields.rental.category"`
	Seats           int         `json:"seats" doc:"seats,fields.rental.seats"`
	Formula         string      `json:"formula" doc:"formula,fields.rental.formula"`
	Kind            string      `json:"kind" doc:"kind,fields.rental.kind"`
	Start           string      `json:"start" doc:"start,fields.rental.start"` // local "dd/mm/yyyy hh:mm"
	End             string      `json:"end" doc:"end,fields.rental.end"`
	Skipper         string      `json:"skipper" doc:"skipper,fields.rental.skipper"`
	LicenceRequired bool        `json:"licence_required" doc:"licence_required,fields.rental.licence_required"`
	FuelMetered     bool        `json:"fuel_metered" doc:"fuel_metered,fields.rental.fuel_metered"`
	GraceMinutes    int         `json:"grace_minutes" doc:"grace_minutes,fields.rental.grace_minutes"`
	LateStepMinutes int         `json:"late_step_minutes" doc:"late_step_minutes,fields.rental.late_step_minutes"`
	QR              string      `json:"qr" doc:"qr,fields.rental.qr"` // PR1:<booking id> (D23)
	Notes           views.Notes `json:"notes" doc:"notes,fields.rental.notes"`
}

// RentalLine is a price line of a rental document.
type RentalLine struct {
	Seq         int         `json:"seq" doc:"seq,fields.rental.line_seq"`
	Description string      `json:"description" doc:"description,fields.rental.line_description"`
	Qty         string      `json:"qty" doc:"qty,fields.rental.line_qty"` // formatted quantity
	Amount      money.Cents `json:"amount_cents" doc:"amount,fields.rental.line_amount"`
}

// RentalContractView is the data of kind rental_contract.
type RentalContractView struct {
	Marina      views.Marina `json:"marina" doc:"marina,fields.groups.marina"`
	Renter      views.Party  `json:"renter" doc:"renter,fields.rental.renter"`
	Vessel      views.Vessel `json:"vessel" doc:"vessel,fields.groups.vessel"`
	Rental      RentalInfo   `json:"rental" doc:"rental,fields.groups.rental"`
	Lines       []RentalLine `json:"lines" doc:"lines,fields.rental.lines"`
	Total       money.Cents  `json:"total_cents" doc:"total,fields.rental.total"`
	Deposit     money.Cents  `json:"deposit_cents" doc:"deposit,fields.rental.deposit"`
	DepositMode string       `json:"deposit_mode" doc:"deposit_mode,fields.rental.deposit_mode"`
}

func init() {
	kindTitles["it"][KindRentalContract] = "Contratto di noleggio"
	kindTitles["en"][KindRentalContract] = "Rental contract"
}

var demoRental = RentalInfo{
	Number: "NL2027-000012", UnitCode: "GOM58", Category: "rib", Seats: 8, Formula: "Mezza giornata mattina",
	Kind: "half_day", Start: "12/06/2027 08:00", End: "12/06/2027 13:00", LicenceRequired: true, FuelMetered: true,
	GraceMinutes: 15, LateStepMinutes: 30, QR: "PR1:01900000-0000-7000-8000-000000000000",
}

var demoRentalVessel = views.Vessel{Name: "Gommone 5,8", Registration: "LT 0058", Flag: "IT", HullType: "rib", LOA: 580, Beam: 240}

// rentalContractDemo is the demo data of kind rental_contract: the half-day rental of m11-plan §8.2.
func rentalContractDemo() RentalContractView {
	return RentalContractView{
		Marina: demoMarina, Renter: views.Party{Kind: "person", DisplayName: "Mario Rossi", Phone: "+39 333 0000000"},
		Vessel: demoRentalVessel, Rental: demoRental,
		Lines: []RentalLine{
			{Seq: 1, Description: "Gommone 5,8 m - mezza giornata", Qty: "1", Amount: 15000},
			{Seq: 2, Description: "Frigo portatile", Qty: "1", Amount: 500},
			{Seq: 3, Description: "Kit snorkeling", Qty: "1", Amount: 1000},
		},
		Total: 16500, Deposit: 50000, DepositMode: "cash",
	}
}
