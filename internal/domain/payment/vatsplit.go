package payment

import (
	"encoding/json"
	"slices"

	"github.com/coste360/porto/internal/domain/chargeplan"
	"github.com/coste360/porto/internal/money"
	"github.com/coste360/porto/internal/store"
)

// VAT split of payments (m5-plan §4.2 step 4, pure). What a payment paid of a charge is split over the charge's
// VAT buckets cumulatively: the share of the buckets covered by the charge's paid amount P is
// money.Allocate(P, bucket grosses), and a match moving P from before to after takes the difference. So the shares
// of every payment of a fully paid charge sum to the charge's buckets exactly (TestVATSplit_PaymentShare), and a
// reversal gives back exactly what its match took.

// Bucket is a VAT bucket (rate or nature) with net, VAT and gross.
type Bucket = chargeplan.VATBucket

// bucketsOf converts the store rows of one charge.
func bucketsOf(rows []store.ChargeVATBucket) []Bucket {
	out := make([]Bucket, 0, len(rows))
	for _, r := range rows {
		out = append(out, Bucket{RateBP: r.VATRateBP, Nature: r.VATNature, Net: money.Cents(r.Net), VAT: money.Cents(r.VAT), Gross: money.Cents(r.Gross)})
	}
	return out
}

// coveredBy returns the part of the buckets covered by a paid amount (0 ≤ paid ≤ Σ gross).
func coveredBy(buckets []Bucket, paid money.Cents) []Bucket {
	out := make([]Bucket, len(buckets))
	weights := make([]int64, len(buckets))
	var positive bool
	for i, b := range buckets {
		out[i] = Bucket{RateBP: b.RateBP, Nature: b.Nature}
		if b.Gross > 0 {
			weights[i] = int64(b.Gross)
			positive = true
		}
	}
	if paid == 0 || len(buckets) == 0 {
		return out
	}
	if !positive {
		// Degenerate charge (no positive bucket): everything goes to the first bucket.
		weights[0] = 1
	}
	gross, err := money.Allocate(paid, weights)
	if err != nil {
		return out
	}
	for i, b := range buckets {
		out[i].Gross = gross[i]
		switch {
		case b.Gross == 0 || gross[i] == 0:
		case gross[i] == b.Gross:
			out[i].Net = b.Net
		default:
			out[i].Net = b.Net.MulRat(int64(gross[i]), int64(b.Gross))
		}
		out[i].VAT = out[i].Gross - out[i].Net
	}
	return out
}

// ShareOf is the VAT split of a match moving the paid amount of a charge from paidBefore by delta (negative for a
// reversal).
func ShareOf(buckets []Bucket, paidBefore, delta money.Cents) []Bucket {
	before := coveredBy(buckets, paidBefore)
	after := coveredBy(buckets, paidBefore+delta)
	out := make([]Bucket, len(buckets))
	for i := range buckets {
		out[i] = Bucket{
			RateBP: buckets[i].RateBP, Nature: buckets[i].Nature,
			Net: after[i].Net - before[i].Net, VAT: after[i].VAT - before[i].VAT, Gross: after[i].Gross - before[i].Gross,
		}
	}
	return out
}

// AddBuckets adds b to a split keyed by (rate, nature), dropping zero buckets and keeping the canonical order.
func AddBuckets(split []Bucket, b []Bucket) []Bucket {
	out := slices.Clone(split)
	for _, x := range b {
		found := false
		for i := range out {
			if out[i].RateBP == x.RateBP && out[i].Nature == x.Nature {
				out[i].Net += x.Net
				out[i].VAT += x.VAT
				out[i].Gross += x.Gross
				found = true
				break
			}
		}
		if !found {
			out = append(out, x)
		}
	}
	out = slices.DeleteFunc(out, func(x Bucket) bool { return x.Net == 0 && x.VAT == 0 && x.Gross == 0 })
	chargeplan.SortBuckets(out)
	return out
}

func decodeSplit(j store.JSON) []Bucket {
	var out []Bucket
	if len(j) > 0 {
		_ = json.Unmarshal(j, &out)
	}
	return out
}

func encodeSplit(b []Bucket) store.JSON {
	if b == nil {
		b = []Bucket{}
	}
	return store.MustJSON(b)
}
