package docs

import (
	"time"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/docs/views"
	"github.com/coste360/porto/internal/money"
)

// Demo data of every kind for template previews and the golden layout tests (m4-plan §6.4, §9.2). Fixed values,
// no database access; names are fictitious.

var (
	demoMarina = views.Marina{
		Name: "Porto Demo", LegalName: "Porto Demo S.r.l.", Address: "Via del Porto 1, 04024 Gaeta (LT)",
		VATNumber: "01234567890", FiscalCode: "01234567890", Email: "info@porto-demo.example", Phone: "+39 0771 000000",
		Website: "https://porto-demo.example", ConcessionExpiresOn: calendar.MustParse("2040-12-31"),
		Bank: views.Bank{IBAN: "IT60X0542811101000000123456", Holder: "Porto Demo S.r.l.", Bank: "Banca Demo", BIC: "BDEMITMM"},
	}
	demoCustomer = views.Party{
		Code: "C-0042", Kind: "person", DisplayName: "Mario Rossi", FirstName: "Mario", LastName: "Rossi",
		FiscalCode: "RSSMRA80A01H501U", Address: "Via Roma 10, 00100 Roma (RM)", Email: "mario.rossi@example.com",
		Phone: "+39 333 0000000",
	}
	demoHolder2 = views.Party{Code: "C-0043", Kind: "person", DisplayName: "Anna Bianchi", FirstName: "Anna", LastName: "Bianchi"}
	demoVessel  = views.Vessel{
		Name: "Libeccio", Registration: "RM 1234 D", Flag: "IT", HullType: "sailboat", LOA: 1134, Beam: 380, Draft: 190,
	}
	demoBerth = views.Place{
		Code: "B-12", Type: "berth", Structure: "Pontile B", Map: "Porto", Length: 1200, Width: 400,
		FirstNight: calendar.MustParse("2027-01-01"), LastNight: calendar.MustParse("2027-12-31"),
	}
	demoLines = []views.PriceLine{
		{Description: "Ormeggio annuale posto B-12", Qty: 1000, Unit: "anno", UnitPrice: 450000, Amount: 450000, VATRate: 2200},
		{Description: "Acqua ed energia", Qty: 1000, Unit: "forfait", UnitPrice: 35000, Amount: 35000, VATRate: 2200},
	}
	demoPrice = views.Price{
		Total: 485000, Net: 397541, VAT: 87459, Deposit: 145500, Lines: demoLines,
		Adjustments: []views.Adjustment{{Kind: "manual", Reason: "Cliente storico", Amount: -10000}},
	}
	demoInstallments = []views.Installment{
		{Seq: 1, Kind: "deposit", IssueOn: calendar.MustParse("2026-11-07"), DueOn: calendar.MustParse("2026-11-21"), Amount: 145500, Status: "issued"},
		{Seq: 2, Kind: "installment", IssueOn: calendar.MustParse("2027-03-15"), DueOn: calendar.MustParse("2027-03-31"), Amount: 113166, Status: "scheduled"},
		{Seq: 3, Kind: "installment", IssueOn: calendar.MustParse("2027-06-15"), DueOn: calendar.MustParse("2027-06-30"), Amount: 113166, Status: "scheduled"},
		{Seq: 4, Kind: "installment", IssueOn: calendar.MustParse("2027-09-15"), DueOn: calendar.MustParse("2027-09-30"), Amount: 113168, Status: "scheduled"},
	}
	demoPolicy = views.Policy{
		DepositBP: 3000, DepositDueDays: 14, InstallmentsCount: 3, EarlyPaymentDiscountBP: 300, TacitRenewal: true,
		RenewalNoticeDays: 60, CancellationNoticeDays: 90, Refund: "pro_rata", PenaltyBP: 1000,
	}
	demoContract = views.ContractInfo{
		Code: "ORM-2027-0042", Kind: "mooring", Term: "annual", StartsOn: calendar.MustParse("2027-01-01"),
		EndsOn: calendar.MustParse("2028-01-01"), IssuedOn: calendar.MustParse("2026-11-07"),
		Notes: "Posto con colonnina dedicata.\nAccesso al pontile con badge.",
	}
	demoRight = views.RightInfo{
		Kind: "long_term_use", Berth: demoBerth, FromOn: calendar.MustParse("2020-01-01"),
		ToOn: calendar.MustParse("2040-12-31"), TitleDeed: "Atto di cessione", TitleNumber: "Rep. 1234",
		TitleNotary: "Notaio Demo", TitleDate: calendar.MustParse("2019-12-15"),
	}
	demoAt = time.Date(2026, 11, 7, 10, 30, 0, 0, time.UTC)
)

// DemoView returns the demo data of a kind (nil for kinds without a view).
func DemoView(kind Kind) any {
	switch kind {
	case KindQuote:
		return views.QuoteView{
			Marina: demoMarina, Customer: demoCustomer, Vessel: demoVessel,
			Quote: views.QuoteInfo{
				Code: "PRV-2026-0101", IssuedOn: calendar.MustParse("2026-11-07"), Kind: "mooring", Term: "annual",
				StartsOn: calendar.MustParse("2027-01-01"), EndsOn: calendar.MustParse("2028-01-01"),
			},
			Options: []views.QuoteOption{
				{Seq: 1, Label: "Pontile B", Placements: []views.Place{demoBerth}, Price: demoPrice},
				{Seq: 2, Label: "Pontile C", Placements: []views.Place{{
					Code: "C-03", Type: "berth", Structure: "Pontile C",
					Length: 1200, FirstNight: demoBerth.FirstNight, LastNight: demoBerth.LastNight,
				}}, Price: views.Price{Total: 452000, Lines: demoLines[:1]}},
			},
			ValidUntil: calendar.MustParse("2026-11-30"), Link: "https://porto-demo.example/b/demo/m/quote/DEMO",
		}
	case KindContractAnnual, KindContractSeasonal, KindContractMonthly, KindContractPeriod, KindCharter:
		v := views.ContractView{
			Marina: demoMarina, Customer: demoCustomer, BillingCustomer: demoCustomer, Vessel: demoVessel,
			Contract: demoContract, Berths: []views.Place{demoBerth}, Lines: demoLines, Price: demoPrice,
			Installments: demoInstallments, Policy: demoPolicy,
		}
		if kind == KindCharter {
			v.Contract.Kind = "charter"
			v.Fleet = &views.Fleet{Name: "Flotta Demo", Vessels: []views.Vessel{demoVessel}, ChangeoverWeekday: 6, PricingMode: "per_berth"}
		}
		return v
	case KindGeneralConditions, KindHarbourRules:
		return views.MarinaView{Marina: demoMarina}
	case KindLetter:
		return views.LetterView{Marina: demoMarina, Customer: demoCustomer}
	case KindAddendum:
		nv := demoVessel
		nv.Name, nv.LOA = "Grecale", 1190
		return views.AddendumView{
			Marina: demoMarina, Customer: demoCustomer, Vessel: demoVessel, Contract: demoContract,
			Addendum: views.AddendumInfo{
				Seq: 1, Kind: "vessel_change", EffectiveOn: calendar.MustParse("2027-04-01"),
				Description: "Cambio dell'unità ormeggiata.", OldTotal: 485000, NewTotal: 497000, Delta: 12000, NewVessel: &nv,
			},
			Lines: demoLines, Installments: demoInstallments[2:],
		}
	case KindRenewalProposal:
		nc := demoContract
		nc.Code, nc.StartsOn, nc.EndsOn = "ORM-2028-0042", calendar.MustParse("2028-01-01"), calendar.MustParse("2029-01-01")
		np := demoPrice
		np.Total = 499550
		return views.RenewalView{
			Marina: demoMarina, Customer: demoCustomer, Vessel: demoVessel, Contract: demoContract, NewContract: nc,
			Berths: []views.Place{demoBerth}, OldPrice: demoPrice, NewPrice: np, EarlyPaymentTotal: 484564,
			Installments: demoInstallments, Deadline: calendar.MustParse("2027-11-30"),
			Link: "https://porto-demo.example/b/demo/m/renewal/DEMO", Message: "Grazie per essere stato con noi.",
		}
	case KindPaymentRequest, KindReminder, KindInstallmentNotice:
		c := demoContract
		return views.InstallmentView{
			Marina: demoMarina, Customer: demoCustomer, Contract: &c, Installment: demoInstallments[0],
			Overdue: kind == KindReminder, PaymentReference: demoContract.Code, Open: demoInstallments,
		}
	case KindHolderChain:
		return views.HolderChainView{Marina: demoMarina, Berth: demoBerth, Entries: []views.HolderChainEntry{
			{Right: demoRight, Status: "active", Holders: []views.Holder{{Party: demoCustomer, Share: 667}, {Party: demoHolder2, Share: 333}}},
		}}
	case KindMandateStatement:
		return views.MandateStatementView{
			Marina: demoMarina, Holder: demoCustomer, Right: demoRight, From: calendar.MustParse("2027-07-01"),
			To: calendar.MustParse("2027-08-31"), OwnerShareBP: 6000, MinGuaranteed: 100000,
			Lines: []views.MandateLine{{
				ContractCode: "TRN-2027-0901", FirstNight: calendar.MustParse("2027-07-10"),
				LastNight: calendar.MustParse("2027-07-16"), Nights: 7, Revenue: 70000, OwnerShare: 42000,
			}},
			Revenue: 70000, OwnerShare: 42000, TopUp: 58000,
		}
	case KindFeeNotice:
		return views.FeeNoticeView{
			Marina: demoMarina, Holder: demoCustomer, Right: demoRight, Year: 2027, Base: 120000, IstatBP: 180,
			Updated: 122160, Days: 365, Amount: 122160, Share: 667, HolderAmount: 81481,
			Installments: []views.Installment{{Seq: 1, DueOn: calendar.MustParse("2027-03-31"), Amount: 40740}, {Seq: 2, DueOn: calendar.MustParse("2027-09-30"), Amount: 40741}},
		}
	case KindCommonChargesHolder:
		return views.CommonChargesHolderView{
			Marina: demoMarina, Holder: demoCustomer, Year: 2027, Kind: "advance",
			Lines: []views.CommonChargeLine{{
				Item: "Pulizia specchio acqueo", Table: "Generale", Berth: "B-12", Millis: 12,
				ItemAmount: 2000000, BerthShare: 24000, HolderAmount: 16008,
			}},
			Total: 16008,
		}
	case KindCommonChargesItems:
		return views.CommonChargesItemsView{Marina: demoMarina, Year: 2027, Kind: "advance", Items: []views.CommonChargeItemTotal{
			{Item: "Pulizia specchio acqueo", Table: "Generale", Amount: 2000000, Allocated: 2000000, MarinaShare: 300000},
		}, Total: 2000000}
	case KindTransfer:
		return views.TransferView{
			Marina: demoMarina, Right: demoRight, EffectiveOn: calendar.MustParse("2027-05-01"),
			From: []views.Holder{{Party: demoCustomer, Share: 1000}}, To: []views.Holder{{Party: demoHolder2, Share: 1000}},
			DeclaredPrice: money.Cents(9000000), TakeoverFee: 50000,
		}
	case KindCommunication:
		return views.CommunicationView{
			Marina: demoMarina, Customer: demoCustomer, Vessels: []views.Vessel{demoVessel},
			Contracts: []views.ContractInfo{demoContract},
		}
	case KindSignatureEvidence:
		return views.EvidenceView{
			Marina: demoMarina, DocumentTitle: "Contratto di ormeggio annuale ORM-2027-0042",
			DocumentSHA256: "9f2b5c0e6a1d4b3f8e7a6c5d4b3a2f1e0d9c8b7a6f5e4d3c2b1a0f9e8d7c6b5a", RequestID: "01900000-0000-7000-8000-000000000001",
			Signers: []views.EvidenceSigner{{
				Role: "customer", Name: "Mario Rossi", DeclaredIdentity: "Mario Rossi, RSSMRA80A01H501U",
				EmailHash: "3a7bd3e2360a3d29eea436fcfb7e44c735d117c42d1c1835420b6b9942dd4f1b", IP: "192.0.2.10",
				UserAgentSHA256: "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
				OpenedAt:        demoAt, OTPSentAt: demoAt.Add(2 * time.Minute), SignedAt: demoAt.Add(4 * time.Minute),
			}},
		}
	case KindReceipt:
		c := demoCustomer
		return views.ReceiptView{
			Marina: demoMarina, Customer: &c, Number: "P2027-000045", Date: calendar.MustParse("2027-07-12"),
			Method: "card", AmountCents: 7320,
			Lines: []views.ReceiptLine{
				{Description: "Gettone docce", Qty: 2000, Gross: 1220, VATRate: 2200},
				{Description: "Carburante", Qty: 1000, Gross: 6100, VATRate: 2200},
			},
			VAT:          []views.VATRow{{RateBP: 2200, Net: 6000, VAT: 1320, Gross: 7320}},
			FiscalNumber: "20270712-0007", Footer: "Grazie e buona navigazione.",
		}
	case KindStatementOfAccount:
		return views.StatementView{
			Marina: demoMarina, Customer: demoCustomer, From: calendar.MustParse("2027-01-01"),
			To: calendar.MustParse("2027-06-30"), Opening: 0, Closing: 113166, Credit: 0,
			Rows: []views.StatementRow{
				{Date: calendar.MustParse("2026-11-07"), Kind: "charge", Number: "A2026-000042", Description: "Acconto ORM-2027-0042", DueOn: calendar.MustParse("2026-11-21"), Debit: 145500, Balance: 145500},
				{Date: calendar.MustParse("2026-11-18"), Kind: "payment", Number: "P2026-000031", Description: "Bonifico", Credit: 145500, Balance: 0},
				{Date: calendar.MustParse("2027-03-15"), Kind: "charge", Number: "A2027-000110", Description: "Rata 2 ORM-2027-0042", DueOn: calendar.MustParse("2027-03-31"), Debit: 113166, Balance: 113166},
			},
		}
	case KindChargeExportSummary:
		return views.ChargeExportSummaryView{
			Marina: demoMarina, Number: "E2027-0012", Kind: "export", PeriodFrom: calendar.MustParse("2027-03-01"),
			PeriodTo: calendar.MustParse("2027-03-31"), CreatedAt: demoAt, ChargeCount: 2, LineCount: 3,
			VAT: []views.VATRow{{RateBP: 2200, Net: 98783, VAT: 21733, Gross: 120516}},
			ByOrigin: []views.TotalRow{
				{Key: "installment", Count: 1, Gross: 113166},
				{Key: "counter_sale", Count: 1, Gross: 7350},
			},
			ByRevenueClass: []views.TotalRow{{Key: "mooring", Count: 1, Gross: 113166}, {Key: "services", Count: 1, Gross: 7350}},
			Charges: []views.ExportChargeRow{
				{Number: "A2027-000110", IssuedOn: calendar.MustParse("2027-03-15"), Customer: "Mario Rossi", OriginType: "installment", Net: 92759, VAT: 20407, Total: 113166},
				{Number: "A2027-000111", IssuedOn: calendar.MustParse("2027-03-16"), Customer: "Anna Bianchi", OriginType: "counter_sale", Net: 6024, VAT: 1326, Total: 7350},
			},
			Net: 98783, VATTotal: 21733, Total: 120516,
		}
	case KindMapPrint:
		return views.MapPrintView{Marina: demoMarina, MapName: "Porto", Date: calendar.MustParse("2026-11-07"), Lens: "occupancy"}
	}
	return nil
}
