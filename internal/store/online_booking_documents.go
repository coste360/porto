package store

import (
	"context"
	"time"

	"github.com/uptrace/bun"
)

// OnlineBookingDocument links a document to the online booking it was uploaded through. Token-only surfaces (the
// public booking view and the /pay required-documents check) list and count only these documents, never the
// older documents of a customer the booking was matched to by email (spec 23.3, INV-88).
type OnlineBookingDocument struct {
	bun.BaseModel `bun:"table:online_booking_documents"`

	ID              string    `bun:"id,pk"`
	MarinaID        string    `bun:"marina_id"`
	OnlineBookingID string    `bun:"online_booking_id"`
	DocumentID      string    `bun:"document_id"`
	CreatedAt       time.Time `bun:"created_at"`
}

// LinkOnlineBookingDocument records that a document was uploaded through a booking of the marina.
func LinkOnlineBookingDocument(ctx context.Context, db bun.IDB, s MarinaScope, bookingID, documentID string, at time.Time) error {
	_, err := db.NewInsert().Model(&OnlineBookingDocument{
		ID: NewID(), MarinaID: s.ID(), OnlineBookingID: bookingID, DocumentID: documentID, CreatedAt: UTC(at),
	}).Exec(ctx)
	return err
}

// DocumentsOfOnlineBooking returns the documents uploaded through a booking of the marina, newest first.
func DocumentsOfOnlineBooking(ctx context.Context, db bun.IDB, s MarinaScope, bookingID string) ([]Document, error) {
	out := []Document{}
	if !ValidID(bookingID) {
		return out, nil
	}
	err := db.NewSelect().Model(&out).Where("marina_id = ?", s.ID()).
		Where("id IN (?)", db.NewSelect().Table("online_booking_documents").Column("document_id").
			Where("marina_id = ?", s.ID()).Where("online_booking_id = ?", bookingID)).
		OrderExpr("created_at DESC, id DESC").Scan(ctx)
	return out, err
}
