package payment

import (
	"testing"

	"pgregory.net/rapid"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/money"
)

func d(s string) calendar.Date { return calendar.MustParse(s) }

func TestAgeing_Buckets(t *testing.T) {
	today := d("2027-06-30")
	cases := []struct {
		due  string
		want string
	}{
		{"2027-07-15", BucketNotDue},
		{"2027-06-30", BucketNotDue}, // due today is not late
		{"2027-06-29", BucketD0_30},
		{"2027-05-31", BucketD0_30}, // 30 days late
		{"2027-05-30", BucketD31_60},
		{"2027-05-01", BucketD31_60}, // 60 days
		{"2027-04-30", BucketD61_90},
		{"2027-04-01", BucketD61_90}, // 90 days
		{"2027-03-31", BucketD90Plus},
		{"2020-01-01", BucketD90Plus},
	}
	for _, c := range cases {
		if got := BucketOf(d(c.due), today); got != c.want {
			t.Errorf("BucketOf(%s) = %s, want %s", c.due, got, c.want)
		}
		from, to := DueRange(c.want, today)
		due := d(c.due)
		if (!from.IsZero() && due.Before(from)) || (!to.IsZero() && due.After(to)) {
			t.Errorf("DueRange(%s) = [%s, %s] excludes %s", c.want, from, to, c.due)
		}
	}
	var a Ageing
	a.Add(BucketNotDue, 100)
	a.Add(BucketD90Plus, 50)
	a.Add("unknown", 999)
	if a.Total != 150 || a.NotDue != 100 || a.D90Plus != 50 {
		t.Fatalf("ageing %+v", a)
	}
}

func TestStatement_RunningBalance(t *testing.T) {
	entries := []Entry{
		{Date: d("2027-01-10"), Kind: RowCharge, Number: "A2027-000001", Amount: 100_00},
		{Date: d("2027-01-20"), Kind: RowPayment, Number: "P2027-000001", Amount: -60_00},
		{Date: d("2027-02-01"), Kind: RowRefund, Number: "R2027-000001", Amount: 10_00},
		{Date: d("2027-02-01"), Kind: RowCharge, Number: "A2027-000002", Amount: 50_00}, // charges first on a day
		{Date: d("2027-03-01"), Kind: RowCredit, Number: "P2027-000002", Amount: -20_00},
		{Date: d("2027-04-01"), Kind: RowCharge, Number: "A2027-000003", Amount: -5_00}, // negative adjustment
	}
	st := BuildStatement(entries, d("2027-02-01"), d("2027-03-31"))
	if st.Opening != 40_00 {
		t.Fatalf("opening %d", st.Opening)
	}
	if len(st.Rows) != 3 || st.Rows[0].Kind != RowCharge || st.Rows[1].Kind != RowRefund {
		t.Fatalf("rows %+v", st.Rows)
	}
	want := []money.Cents{90_00, 100_00, 80_00}
	for i, r := range st.Rows {
		if r.Balance != want[i] {
			t.Fatalf("row %d balance %d want %d", i, r.Balance, want[i])
		}
	}
	if st.Rows[2].Credit != 20_00 || st.Rows[2].Debit != 0 || st.Rows[0].Debit != 50_00 {
		t.Fatalf("debit/credit %+v", st.Rows)
	}
	if st.Closing != 80_00 {
		t.Fatalf("closing %d", st.Closing)
	}
	all := BuildStatement(entries, calendar.Date{}, calendar.Date{})
	if all.Opening != 0 || all.Closing != 75_00 || len(all.Rows) != len(entries) {
		t.Fatalf("open range %+v", all)
	}
	rapid.Check(t, func(t *rapid.T) {
		n := rapid.IntRange(0, 40).Draw(t, "n")
		var es []Entry
		var sum money.Cents
		for i := range n {
			a := money.Cents(rapid.Int64Range(-100_000, 100_000).Draw(t, "a"))
			es = append(es, Entry{Date: d("2027-01-01").AddDays(rapid.IntRange(0, 365).Draw(t, "day")), Kind: RowCharge, Number: string(rune('a' + i%26)), Amount: a})
			sum += a
		}
		from := d("2027-01-01").AddDays(rapid.IntRange(0, 365).Draw(t, "from"))
		to := from.AddDays(rapid.IntRange(0, 365).Draw(t, "len"))
		st := BuildStatement(es, from, to)
		bal := st.Opening
		for _, r := range st.Rows {
			bal += r.Debit - r.Credit
			if r.Balance != bal {
				t.Fatalf("running balance %d ≠ %d", r.Balance, bal)
			}
		}
		if st.Closing != bal {
			t.Fatalf("closing %d ≠ %d", st.Closing, bal)
		}
		whole := BuildStatement(es, calendar.Date{}, calendar.Date{})
		if whole.Closing != sum {
			t.Fatalf("whole closing %d ≠ Σ %d", whole.Closing, sum)
		}
	})
}

func TestMatchPlan_OldestFirst(t *testing.T) {
	ms, rest := PlanOldestFirst([]Open{{"a", 30_00}, {"b", 0}, {"c", 50_00}}, 60_00)
	if len(ms) != 2 || ms[0] != (MatchInput{"a", 30_00}) || ms[1] != (MatchInput{"c", 30_00}) || rest != 0 {
		t.Fatalf("plan %+v rest %d", ms, rest)
	}
	rapid.Check(t, func(t *rapid.T) {
		n := rapid.IntRange(0, 30).Draw(t, "n")
		open := make([]Open, n)
		var total money.Cents
		for i := range open {
			open[i] = Open{ChargeID: string(rune('A' + i)), Residual: money.Cents(rapid.Int64Range(-100, 500_000).Draw(t, "r"))}
			if open[i].Residual > 0 {
				total += open[i].Residual
			}
		}
		amount := money.Cents(rapid.Int64Range(0, 2_000_000).Draw(t, "amount"))
		ms, rest := PlanOldestFirst(open, amount)
		var sum money.Cents
		byID := map[string]money.Cents{}
		for _, o := range open {
			byID[o.ChargeID] = o.Residual
		}
		for i, m := range ms {
			if m.Amount <= 0 || m.Amount > byID[m.ChargeID] {
				t.Fatalf("match %+v above residual %d", m, byID[m.ChargeID])
			}
			if i < len(ms)-1 && m.Amount != byID[m.ChargeID] {
				t.Fatalf("an earlier charge is not filled before the next: %+v", ms)
			}
			sum += m.Amount
		}
		if sum > amount {
			t.Fatalf("Σ %d above the payment %d", sum, amount)
		}
		if rest != amount-sum {
			t.Fatalf("remainder %d ≠ %d", rest, amount-sum)
		}
		if want := min(amount, total); sum != want {
			t.Fatalf("Σ %d ≠ min(amount, Σ residuals) %d", sum, want)
		}
	})
}

func TestVATSplit_PaymentShare(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		nb := rapid.IntRange(1, 4).Draw(t, "buckets")
		buckets := make([]Bucket, nb)
		var total money.Cents
		for i := range buckets {
			gross := money.Cents(rapid.Int64Range(1, 1_000_000).Draw(t, "gross"))
			rate := rapid.SampledFrom([]int64{0, 400, 1000, 2200}).Draw(t, "rate")
			net, vat := money.VATSplit(gross, rate)
			nature := ""
			if rate == 0 {
				nature = "N1"
			}
			buckets[i] = Bucket{RateBP: rate + int64(i), Nature: nature, Net: net, VAT: vat, Gross: gross}
			total += gross
		}
		// Random partition of the total in payments, with a reversal now and then.
		var paid money.Cents
		var split []Bucket
		for paid < total {
			step := money.Cents(rapid.Int64Range(1, int64(total-paid)).Draw(t, "step"))
			split = AddBuckets(split, ShareOf(buckets, paid, step))
			paid += step
			if paid > 1 && rapid.Bool().Draw(t, "reverse") {
				back := money.Cents(rapid.Int64Range(1, int64(paid)).Draw(t, "back"))
				split = AddBuckets(split, ShareOf(buckets, paid, -back))
				paid -= back
			}
		}
		for _, b := range buckets {
			var got Bucket
			for _, x := range split {
				if x.RateBP == b.RateBP && x.Nature == b.Nature {
					got = x
				}
			}
			if got.Gross != b.Gross || got.Net != b.Net || got.VAT != b.VAT {
				t.Fatalf("bucket %+v: Σ shares %+v", b, got)
			}
		}
		for _, x := range split {
			if x.Net+x.VAT != x.Gross {
				t.Fatalf("share %+v: net + vat ≠ gross", x)
			}
		}
	})
}

func TestLensState(t *testing.T) {
	today := d("2027-06-30")
	cases := []struct {
		name     string
		dues     []lensDue
		disputed bool
		want     string
	}{
		{"nothing owed", nil, false, LensOK},
		{"far", []lensDue{{DueOn: d("2027-08-30"), Residual: 1}}, false, LensOK},
		{"soon", []lensDue{{DueOn: d("2027-07-15"), Residual: 1}}, false, LensDueSoon},
		{"overdue", []lensDue{{DueOn: d("2027-06-29"), Residual: 1}, {DueOn: d("2027-07-01"), Residual: 1}}, false, LensOverdue},
		{"overdue 60", []lensDue{{DueOn: d("2027-04-30"), Residual: 1}}, false, LensOverdue60},
		{"paid ignored", []lensDue{{DueOn: d("2027-01-01"), Residual: 0}}, false, LensOK},
		{"disputed wins", []lensDue{{DueOn: d("2027-01-01"), Residual: 1}}, true, LensDisputed},
	}
	for _, c := range cases {
		if got := LensState(c.dues, c.disputed, today); got != c.want {
			t.Errorf("%s: %s, want %s", c.name, got, c.want)
		}
	}
}

func TestActorString(t *testing.T) {
	if got := actorString(ProviderActor("stripe")); got != "provider:stripe" {
		t.Fatal(got)
	}
	if got := actorString(JobActor("payments.reminders")); got != "system:payments.reminders" {
		t.Fatal(got)
	}
	if p := principalOf(JobActor("x")); p != nil {
		t.Fatal("system actor has no principal")
	}
}
