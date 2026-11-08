package store

import (
	"context"
	"time"

	"github.com/uptrace/bun"
)

// Lost demand of the public booking page and the channels (m8-plan §9, §16 D17): anonymous inquiries with no
// customer, no vessel, no IP and no user agent.

// Inquiry outcomes used by online booking.
const (
	InquiryOpen            = "open"
	InquiryBooked          = "booked"
	InquiryLost            = "lost"
	InquiryDeclinedSize    = "declined_size"
	InquiryDeclinedNoSpace = "declined_no_space"
	InquiryDeclinedOther   = "declined_other"
)

// InsertAnonymousInquiry inserts a lost-demand inquiry (channel online or external, source public or channel,
// purpose transit). The row carries only dimensions, dates and the outcome.
func InsertAnonymousInquiry(ctx context.Context, db bun.IDB, in *Inquiry, now time.Time) error {
	now = UTC(now)
	if in.ID == "" {
		in.ID = NewID()
	}
	in.Purpose, in.VesselID, in.CustomerID, in.CreatedByUserID = ContractKindTransit, "", "", ""
	in.Version, in.CreatedAt, in.UpdatedAt = 1, now, now
	return InsertInquiry(ctx, db, in)
}
