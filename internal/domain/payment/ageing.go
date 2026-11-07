package payment

import (
	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/money"
)

// Ageing buckets of open residuals by due date against today (spec 11.4, 16.4; m5-plan §4.5).
const (
	BucketNotDue  = "not_due"
	BucketD0_30   = "d0_30"
	BucketD31_60  = "d31_60"
	BucketD61_90  = "d61_90"
	BucketD90Plus = "d90_plus"
)

// Buckets lists the ageing buckets in display order.
var Buckets = []string{BucketNotDue, BucketD0_30, BucketD31_60, BucketD61_90, BucketD90Plus}

// BucketOf returns the ageing bucket of a due date (pure): not due until the due date included, then 0–30,
// 31–60, 61–90 and over 90 days past due.
func BucketOf(due, today calendar.Date) string {
	late := due.DaysUntil(today)
	switch {
	case late <= 0:
		return BucketNotDue
	case late <= 30:
		return BucketD0_30
	case late <= 60:
		return BucketD31_60
	case late <= 90:
		return BucketD61_90
	default:
		return BucketD90Plus
	}
}

// DueRange returns the due date range [from, to] of a bucket (zero = open end), used to push a bucket filter down
// to the query.
func DueRange(bucket string, today calendar.Date) (from, to calendar.Date) {
	switch bucket {
	case BucketNotDue:
		return today, calendar.Date{}
	case BucketD0_30:
		return today.AddDays(-30), today.AddDays(-1)
	case BucketD31_60:
		return today.AddDays(-60), today.AddDays(-31)
	case BucketD61_90:
		return today.AddDays(-90), today.AddDays(-61)
	case BucketD90Plus:
		return calendar.Date{}, today.AddDays(-91)
	}
	return calendar.Date{}, calendar.Date{}
}

// Ageing is an amount per bucket with the total.
type Ageing struct {
	NotDue, D0To30, D31To60, D61To90, D90Plus, Total money.Cents
}

// Add adds an amount to a bucket.
func (a *Ageing) Add(bucket string, v money.Cents) {
	switch bucket {
	case BucketNotDue:
		a.NotDue += v
	case BucketD0_30:
		a.D0To30 += v
	case BucketD31_60:
		a.D31To60 += v
	case BucketD61_90:
		a.D61To90 += v
	case BucketD90Plus:
		a.D90Plus += v
	default:
		return
	}
	a.Total += v
}

// Plus returns a + b.
func (a Ageing) Plus(b Ageing) Ageing {
	return Ageing{
		NotDue: a.NotDue + b.NotDue, D0To30: a.D0To30 + b.D0To30, D31To60: a.D31To60 + b.D31To60, D61To90: a.D61To90 + b.D61To90,
		D90Plus: a.D90Plus + b.D90Plus, Total: a.Total + b.Total,
	}
}
