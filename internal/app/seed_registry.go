package app

import (
	"context"
	"fmt"
	"math/rand/v2"
	"strconv"
	"strings"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/customer"
	"github.com/coste360/porto/internal/domain/document"
	"github.com/coste360/porto/internal/domain/vessel"
	"github.com/coste360/porto/internal/store"
)

// Demo registry (m1-plan §6): 250 fictional customers (200 people, 50 companies) with checksum-valid synthetic
// fiscal codes and VAT numbers, example.invalid emails and +39 000 phones; 260 vessels with owners, billing and
// skipper parties, 60 of them measured; about 450 documents (metadata only) expiring from −30 to +400 days.
// Everything goes through the registry services, so audit, outbox and deadlines are written as in production.

const (
	seedPeople    = 200
	seedCompanies = 50
	seedVessels   = 260
	seedMeasured  = 60
)

var (
	seedFirstNames = []string{
		"Marco", "Giulia", "Luca", "Francesca", "Alessandro", "Chiara", "Matteo", "Sara", "Lorenzo", "Elena",
		"Davide", "Martina", "Simone", "Valentina", "Andrea", "Federica", "Stefano", "Alessia", "Roberto", "Silvia",
		"Paolo", "Laura", "Giorgio", "Anna", "Riccardo", "Ilaria", "Fabio", "Serena", "Nicola", "Paola",
		"Emanuele", "Marta", "Tommaso", "Beatrice", "Pietro", "Camilla", "Claudio", "Giorgia", "Enrico", "Noemi",
	}
	seedFemale    = map[string]bool{"Giulia": true, "Francesca": true, "Chiara": true, "Sara": true, "Elena": true, "Martina": true, "Valentina": true, "Federica": true, "Alessia": true, "Silvia": true, "Laura": true, "Anna": true, "Ilaria": true, "Serena": true, "Paola": true, "Marta": true, "Beatrice": true, "Camilla": true, "Giorgia": true, "Noemi": true}
	seedLastNames = []string{
		"Marinelli", "Scogliera", "Fortunato", "Ventura", "Lanterna", "Moletti", "Approdi", "Salvemini", "Corallo", "Bonaventura",
		"Rivabella", "Gabbiani", "Delfino", "Maestrale", "Libeccio", "Ancorini", "Pontili", "Remigi", "Vela", "Timoni",
		"Brezza", "Onda", "Spiaggiari", "Faro", "Golfi", "Nocchieri", "Sartiame", "Banchina", "Caravella", "Tramontana",
	}
	seedCompanyWords = []string{"Nautica", "Charter", "Yachting", "Mare", "Vele", "Diporto", "Blu", "Costa", "Rotta", "Porto"}
	seedCompanyTails = []string{"SRL", "SPA", "SAS", "SNC"}
	seedTowns        = []string{"H501", "F205", "L219", "A662", "F839", "D969", "G273", "L736", "C351", "E625"}
	seedCities       = []string{"Roma", "Milano", "Torino", "Bari", "Napoli", "Genova", "Palermo", "Venezia", "Catania", "Livorno"}
	seedVesselNames  = []string{
		"Aurora", "Libeccio", "Stella Maris", "Gabbiano", "Nausicaa", "Sirena", "Maestrale", "Calypso", "Tritone", "Andromeda",
		"Zefiro", "Brezza", "Perla", "Orsa", "Vega", "Alcione", "Delfina", "Scirocco", "Ondina", "Marea",
	}
	seedVesselModels = []string{"Cruiser 30", "Sport 34", "Fly 42", "Sail 38", "Cat 40", "Rib 7", "Gozzo 8", "Trawler 45"}
	seedHullTypes    = []string{"monohull_motor", "monohull_motor", "monohull_sail", "monohull_sail", "catamaran", "rib", "gozzo", "trimaran"}
	// seedLengthsCm cover the demo price classes L8 … L25.
	seedLengthsCm = []int64{650, 780, 900, 990, 1080, 1180, 1260, 1390, 1480, 1590, 1720, 1880, 1990, 2240, 2480}
)

// fiscalLetters builds the three letters of a name for a synthetic fiscal code: consonants, then vowels,
// padded with X.
func fiscalLetters(name string, first bool) string {
	var cons, vows []byte
	for _, r := range strings.ToUpper(name) {
		switch {
		case r < 'A' || r > 'Z':
		case strings.ContainsRune("AEIOU", r):
			vows = append(vows, byte(r))
		default:
			cons = append(cons, byte(r))
		}
	}
	if first && len(cons) > 3 {
		cons = []byte{cons[0], cons[2], cons[3]}
	}
	out := string(append(cons, vows...)) + "XXX"
	return out[:3]
}

// syntheticFiscalCode returns a checksum-valid fiscal code for a fictional person.
func syntheticFiscalCode(first, last string, year, month, day int, female bool, town string) string {
	if female {
		day += 40
	}
	s := fiscalLetters(last, false) + fiscalLetters(first, true) + fmt.Sprintf("%02d", year%100) +
		string("ABCDEHLMPRST"[month-1]) + fmt.Sprintf("%02d", day) + town
	c, _ := customer.PersonCheckChar(s)
	return s + string(c)
}

// syntheticVAT returns a checksum-valid Italian VAT number starting with the given 10 digits.
func syntheticVAT(first10 string) string {
	for d := range 10 {
		s := first10 + strconv.Itoa(d)
		if customer.ValidItalianVAT(s) {
			return s
		}
	}
	return ""
}

func ptr[T any](v T) *T { return &v }

// seedRegistry seeds the demo customers, vessels and documents (m1-plan §6) through the registry services when
// the marina has no customers, and adds the counts to res.
func (a *App) seedRegistry(ctx context.Context, p *auth.Principal, res *SeedResult) error {
	scope, err := store.ResolveMarinaScope(ctx, a.DB.DB, res.MarinaID)
	if err != nil {
		return err
	}
	if n, err := store.CountCustomers(ctx, a.DB.DB, scope); err != nil || n > 0 {
		return err
	}
	tz, err := store.MarinaTimezone(ctx, a.DB.DB, scope)
	if err != nil {
		return err
	}
	loc, err := calendar.LoadLocation(tz)
	if err != nil {
		return err
	}
	today := calendar.In(a.Clock.Now(), loc)
	rng := rand.New(rand.NewPCG(2026, 11)) //nolint:gosec // demo data: deterministic by design, not security relevant
	m := res.MarinaID

	type person struct {
		id      string
		company bool
	}
	people := make([]person, 0, seedPeople+seedCompanies)
	categories := []string{"member", "resident", "affiliated", "charter", "yard", ""}
	for i := range seedPeople {
		first := seedFirstNames[rng.IntN(len(seedFirstNames))]
		last := seedLastNames[rng.IntN(len(seedLastNames))]
		cityIdx := rng.IntN(len(seedCities))
		fc := syntheticFiscalCode(first, last, 1950+rng.IntN(55), 1+rng.IntN(12), 1+rng.IntN(28), seedFemale[first], seedTowns[cityIdx])
		email := fmt.Sprintf("%s.%s%d@example.invalid", strings.ToLower(first), strings.ToLower(last), i)
		phone := fmt.Sprintf("+39 000 %07d", rng.IntN(10_000_000))
		tags := []string{}
		if i%7 == 0 {
			tags = append(tags, "vip")
		}
		if i%11 == 0 {
			tags = append(tags, "socio-storico")
		}
		cat := categories[i%len(categories)]
		in := customer.Patch{
			Kind: ptr(customer.KindPerson), FirstName: &first, LastName: &last, FiscalCode: &fc,
			Emails:         &[]customer.Contact{{Label: "personale", Value: email}},
			Phones:         &[]customer.Contact{{Label: "cellulare", Value: phone}},
			BillingAddress: &customer.Address{Line1: fmt.Sprintf("Via del Porto %d", 1+i), PostalCode: "00100", City: seedCities[cityIdx], Country: "IT"},
			Nationality:    ptr("IT"), Locale: ptr("it"), Tags: &tags,
		}
		if cat != "" {
			in.Category = &cat
		}
		c, err := a.Customers.Create(ctx, p, m, in)
		if err != nil {
			return fmt.Errorf("customer %d: %w", i, err)
		}
		people = append(people, person{id: c.Customer.ID})
		if _, err := a.Customers.RecordConsent(ctx, p, m, c.Customer.ID, customer.ConsentInput{Kind: "privacy", PolicyVersion: "2026-01", Granted: true, Source: "desk"}); err != nil {
			return err
		}
		if i%5 < 3 {
			if _, err := a.Customers.RecordConsent(ctx, p, m, c.Customer.ID, customer.ConsentInput{Kind: "marketing", PolicyVersion: "2026-01", Granted: i%2 == 0, Source: "desk"}); err != nil {
				return err
			}
		}
		res.Customers++
	}
	for i := range seedCompanies {
		name := fmt.Sprintf("%s %s %s", seedCompanyWords[i%len(seedCompanyWords)], seedLastNames[(i*7)%len(seedLastNames)], seedCompanyTails[i%len(seedCompanyTails)])
		vat := syntheticVAT(fmt.Sprintf("%010d", 1_000_000_000+rng.IntN(8_999_999_999)))
		email := fmt.Sprintf("amministrazione%d@example.invalid", i)
		in := customer.Patch{
			Kind: ptr(customer.KindCompany), CompanyName: &name, FiscalCode: &vat, VATNumber: ptr("IT" + vat),
			SDICode: ptr("0000000"), Emails: &[]customer.Contact{{Label: "amministrazione", Value: email}},
			Phones:         &[]customer.Contact{{Label: "ufficio", Value: fmt.Sprintf("+39 000 %07d", rng.IntN(10_000_000))}},
			BillingAddress: &customer.Address{Line1: fmt.Sprintf("Viale della Marina %d", 1+i), PostalCode: "16100", City: seedCities[i%len(seedCities)], Country: "IT"},
			Locale:         ptr("it"), PaymentTerms: &customer.PaymentTerms{Days: 30, EndOfMonth: true},
		}
		if i%3 == 0 {
			in.Category = ptr("charter")
		}
		c, err := a.Customers.Create(ctx, p, m, in)
		if err != nil {
			return fmt.Errorf("company %d: %w", i, err)
		}
		people = append(people, person{id: c.Customer.ID, company: true})
		if _, err := a.Customers.RecordConsent(ctx, p, m, c.Customer.ID, customer.ConsentInput{Kind: "privacy", PolicyVersion: "2026-01", Granted: true, Source: "desk"}); err != nil {
			return err
		}
		res.Customers++
	}

	expiry := func() *calendar.Date { d := today.AddDays(-30 + rng.IntN(431)); return &d }
	issued := func(exp calendar.Date) calendar.Date { return exp.AddDays(-365 * (1 + rng.IntN(5))) }
	addDoc := func(in document.Input) error {
		if _, err := a.Documents.Create(ctx, p, m, in); err != nil {
			return err
		}
		res.Documents++
		return nil
	}
	// Identity documents for 150 people.
	idTypes := []string{"identity_card", "passport", "driving_licence"}
	for i := range 150 {
		exp := expiry()
		if err := addDoc(document.Input{
			OwnerType: document.OwnerCustomer, CustomerID: people[i].id, Type: idTypes[i%len(idTypes)],
			Number: fmt.Sprintf("CA%05dXY", rng.IntN(100_000)), Issuer: "Comune", IssuedOn: issued(*exp), ExpiresOn: *exp,
		}); err != nil {
			return fmt.Errorf("identity document %d: %w", i, err)
		}
	}

	companies := people[seedPeople:]
	for i := range seedVessels {
		loa := seedLengthsCm[rng.IntN(len(seedLengthsCm))]
		beam := loa*10/33 + int64(rng.IntN(30))
		draft := loa / 9
		hull := seedHullTypes[rng.IntN(len(seedHullTypes))]
		if hull == "catamaran" {
			beam = loa / 2
		}
		name := fmt.Sprintf("%s %s", seedVesselNames[i%len(seedVesselNames)], romanSuffix(i/len(seedVesselNames)))
		owner := people[i%seedPeople].id
		parties := []vessel.PartyInput{{CustomerID: owner, Role: "owner", ShareMillis: ptr(int64(1000))}}
		if i < 20 { // two owners at 500/500
			parties = []vessel.PartyInput{
				{CustomerID: owner, Role: "owner", ShareMillis: ptr(int64(500))},
				{CustomerID: people[(i+100)%seedPeople].id, Role: "owner", ShareMillis: ptr(int64(500))},
			}
		}
		if i >= seedVessels-15 { // charter boats: the charges go to a company
			parties = append(parties, vessel.PartyInput{CustomerID: companies[i%len(companies)].id, Role: "billing"})
		}
		if i%8 == 4 && i/8 < 30 {
			parties = append(parties, vessel.PartyInput{CustomerID: people[(i+37)%seedPeople].id, Role: "skipper"})
		}
		patch := vessel.Patch{
			Name: &name, Registration: ptr(fmt.Sprintf("%s%05d", []string{"RM", "GE", "NA", "LI", "PA"}[i%5], 1000+i)),
			RegistryOffice: ptr("Capitaneria di porto"), Flag: ptr("IT"), HullType: &hull,
			Model: ptr(seedVesselModels[i%len(seedVesselModels)]), YearBuilt: ptr(int64(1985 + rng.IntN(40))),
			LOACm: &loa, BeamCm: &beam, DraftCm: &draft, Fuel: ptr([]string{"diesel", "petrol", "diesel", "electric"}[i%4]),
			PowerRequiredA: ptr([]int64{16, 16, 32, 63}[min(int(loa/700), 3)]),
		}
		if hull == "monohull_sail" || hull == "catamaran" {
			patch.Fuel = ptr("diesel")
		}
		v, err := a.Vessels.Create(ctx, p, m, patch, parties)
		if err != nil {
			return fmt.Errorf("vessel %d: %w", i, err)
		}
		res.Vessels++
		if i%(seedVessels/seedMeasured) == 0 && i/(seedVessels/seedMeasured) < seedMeasured {
			if _, err := a.Vessels.RecordMeasurement(ctx, p, m, v.Row.ID, vessel.Measurement{LOACm: loa + int64(rng.IntN(40)), BeamCm: beam + int64(rng.IntN(10))}); err != nil {
				return fmt.Errorf("measurement %d: %w", i, err)
			}
		}
		exp := expiry()
		if err := addDoc(document.Input{
			OwnerType: document.OwnerVessel, VesselID: v.Row.ID, Type: "insurance_policy",
			Number: fmt.Sprintf("POL-%07d", rng.IntN(10_000_000)), Issuer: "Assicurazioni Demo", IssuedOn: exp.AddDays(-365), ExpiresOn: *exp,
		}); err != nil {
			return fmt.Errorf("insurance %d: %w", i, err)
		}
		if i%6 == 0 && i/6 < 40 {
			exp := expiry()
			if err := addDoc(document.Input{
				OwnerType: document.OwnerVessel, VesselID: v.Row.ID, Type: "navigation_licence",
				Number: fmt.Sprintf("LN%06d", rng.IntN(1_000_000)), Issuer: "Capitaneria di porto", IssuedOn: issued(*exp), ExpiresOn: *exp,
			}); err != nil {
				return fmt.Errorf("licence %d: %w", i, err)
			}
		}
	}
	return nil
}

// romanSuffix numbers repeated vessel names: "", "II", "III", …
func romanSuffix(n int) string {
	suffixes := []string{"", "II", "III", "IV", "V", "VI", "VII", "VIII", "IX", "X", "XI", "XII", "XIII", "XIV"}
	if n < len(suffixes) {
		return suffixes[n]
	}
	return strconv.Itoa(n + 1)
}
