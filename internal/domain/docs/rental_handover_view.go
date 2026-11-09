package docs

import (
	"github.com/coste360/porto/internal/domain/docs/views"
	"github.com/coste360/porto/internal/money"
)

// The rental handover document (M11, kind rental_handover, m11-plan §10; owned by HANDOVER): one document per phase
// (check-out, check-in) of a booking, composed after the commit of the phase and rendered by the docs job. It lists
// the checklist outcomes, fuel, engine hours, damages with estimates, late fee and deposit decision. Photos stay in
// the encrypted file store (served by signed links); the document records how many were taken.

// RentalCheckItem is a checklist outcome.
type RentalCheckItem struct {
	Label string `json:"label" doc:"label,fields.rental.check_label"`
	OK    bool   `json:"ok" doc:"ok,fields.rental.check_ok"`
	Note  string `json:"note" doc:"note,fields.rental.check_note"`
}

// RentalDamageRow is a damage of the handover document.
type RentalDamageRow struct {
	Description string      `json:"description" doc:"description,fields.rental.damage_description"`
	Estimate    money.Cents `json:"estimate_cents" doc:"estimate,fields.rental.damage_estimate"`
	Status      string      `json:"status" doc:"status,fields.rental.damage_status"`
}

// RentalPhaseInfo are the data of a handover phase.
type RentalPhaseInfo struct {
	Phase       string `json:"phase" doc:"phase,fields.rental.phase"` // localised: consegna, riconsegna
	At          string `json:"at" doc:"at,fields.rental.at"`          // local actual instant
	Operator    string `json:"operator" doc:"operator,fields.rental.operator"`
	FuelEighths int    `json:"fuel_eighths" doc:"fuel_eighths,fields.rental.fuel_eighths"`
	EngineHours string `json:"engine_hours" doc:"engine_hours,fields.rental.engine_hours"` // formatted hours
	Photos      int    `json:"photos" doc:"photos,fields.rental.photos"`
	Notes       string `json:"notes" doc:"notes,fields.rental.handover_notes"`
}

// RentalHandoverView is the data of kind rental_handover.
type RentalHandoverView struct {
	Marina          views.Marina      `json:"marina" doc:"marina,fields.groups.marina"`
	Renter          views.Party       `json:"renter" doc:"renter,fields.rental.renter"`
	Vessel          views.Vessel      `json:"vessel" doc:"vessel,fields.groups.vessel"`
	Rental          RentalInfo        `json:"rental" doc:"rental,fields.groups.rental"`
	Handover        RentalPhaseInfo   `json:"handover" doc:"handover,fields.rental.handover"`
	Checklist       []RentalCheckItem `json:"checklist" doc:"checklist,fields.rental.checklist"`
	Damages         []RentalDamageRow `json:"damages" doc:"damages,fields.rental.damages"`
	LateMinutes     int               `json:"late_minutes" doc:"late_minutes,fields.rental.late_minutes"`
	LateFee         money.Cents       `json:"late_fee_cents" doc:"late_fee,fields.rental.late_fee"`
	Fuel            money.Cents       `json:"fuel_cents" doc:"fuel,fields.rental.fuel"`
	ReturnTotal     money.Cents       `json:"return_total_cents" doc:"return_total,fields.rental.return_total"`
	Deposit         money.Cents       `json:"deposit_cents" doc:"deposit,fields.rental.deposit"`
	DepositMode     string            `json:"deposit_mode" doc:"deposit_mode,fields.rental.deposit_mode"`
	DepositRetained money.Cents       `json:"deposit_retained_cents" doc:"deposit_retained,fields.rental.deposit_retained"`
	DepositReleased money.Cents       `json:"deposit_released_cents" doc:"deposit_released,fields.rental.deposit_released"`
}

func init() {
	kindTitles["it"][KindRentalHandover] = "Verbale di consegna del noleggio"
	kindTitles["en"][KindRentalHandover] = "Rental handover report"
}

// rentalHandoverDemo is the demo data of kind rental_handover: the check-in of m11-plan §8.2.
func rentalHandoverDemo() RentalHandoverView {
	return RentalHandoverView{
		Marina: demoMarina, Renter: views.Party{Kind: "person", DisplayName: "Mario Rossi"}, Vessel: demoRentalVessel,
		Rental: demoRental,
		Handover: RentalPhaseInfo{
			Phase: "riconsegna", At: "12/06/2027 13:50", Operator: "Luca Verdi", FuelEighths: 5, EngineHours: "125,0", Photos: 2,
		},
		Checklist: []RentalCheckItem{
			{Label: "Scafo", OK: false, Note: "Graffio a dritta"}, {Label: "Motore", OK: true}, {Label: "Giubbotti salvagente", OK: true},
		},
		Damages:     []RentalDamageRow{{Description: "Graffio profondo sul tubolare di dritta", Estimate: 18000, Status: "charged"}},
		LateMinutes: 35, LateFee: 6000, Fuel: 4725, ReturnTotal: 10725, Deposit: 50000, DepositMode: "cash",
		DepositRetained: 18000, DepositReleased: 32000,
	}
}
