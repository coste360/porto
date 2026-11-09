package reservation

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// stateWriters are the store functions that write rental_bookings.status, hold_expires_at or the slot rows
// (INV-97): only this package may call them.
var stateWriters = regexp.MustCompile(`store\.(WriteRentalBookingState|InsertRentalBooking|InsertRentalSlots|InsertRentalSkipperSlots|` +
	`DeleteRentalBookingSlots|DeleteRentalBlockSlots|SetRentalBookingSlotsBooked|SetRentalBookingSlotsExpiry|TruncateRentalBookingSlots|` +
	`MarkRentalSlotsLost|InsertRentalBlock|UpdateRentalBlock|DeleteRentalBlock)\(`)

// rawWrites are SQL writes on the guarded tables or columns written outside the BOOKING store files.
var rawWrites = []*regexp.Regexp{
	regexp.MustCompile(`(?s)NewUpdate\(\)[^;]{0,200}?(rental_bookings|\(\*RentalBooking\)\(nil\))[^;]{0,400}?Set\("(status|hold_expires_at)`),
	regexp.MustCompile(`(?s)(NewInsert|NewUpdate|NewDelete)\(\)[^;]{0,120}?"(rental_slots|rental_skipper_slots)"`),
	regexp.MustCompile(`(?s)(NewInsert|NewUpdate|NewDelete)\(\)\.Model\(\(\*(RentalSlot|RentalSkipperSlot)\)`),
	regexp.MustCompile(`(?i)UPDATE\s+rental_bookings\s+SET[^"]*\bstatus\b`),
}

// bookingStoreFiles own the guarded statements.
var bookingStoreFiles = map[string]bool{
	"internal/store/rental_bookings.go": true, "internal/store/rental_slots.go": true, "internal/store/rental_blocks.go": true,
	"internal/store/rental_invariants.go": true,
}

// TestRental_StatusWritesOnlyInReservation scans the Go sources (INV-97): the status, hold expiry and slot rows of
// rentals are written only through internal/domain/rental/reservation (and the BOOKING store files it calls).
func TestRental_StatusWritesOnlyInReservation(t *testing.T) {
	root, err := filepath.Abs("../../../..")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("repository root not found: %v", err)
	}
	scanned := 0
	err = filepath.WalkDir(filepath.Join(root, "internal"), func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") || strings.HasSuffix(path, ".gen.go") {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		if strings.HasPrefix(rel, "internal/domain/rental/reservation/") || bookingStoreFiles[rel] {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		scanned++
		if m := stateWriters.Find(src); m != nil {
			t.Errorf("%s calls %s: rental status, holds and slot rows are written only by internal/domain/rental/reservation (INV-97)", rel, m)
		}
		if strings.HasPrefix(rel, "internal/store/") || strings.HasPrefix(rel, "internal/domain/") || strings.HasPrefix(rel, "internal/app/") {
			for _, re := range rawWrites {
				if m := re.Find(src); m != nil {
					t.Errorf("%s writes a guarded rental column or slot table directly: %.120q (INV-97)", rel, m)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if scanned < 100 {
		t.Fatalf("only %d files scanned", scanned)
	}
}

func TestRental_StatusWritesScanPatterns(t *testing.T) {
	bad := []string{
		`store.WriteRentalBookingState(ctx, tx, scope, b, 1, now)`,
		`tx.NewUpdate().Model((*RentalBooking)(nil)).Set("status = ?", x)`,
		`tx.NewUpdate().TableExpr("rental_bookings").Set("hold_expires_at = NULL")`,
		`tx.NewDelete().TableExpr("rental_slots").Where("booking_id = ?", id)`,
		`tx.NewInsert().Model((*RentalSkipperSlot)(nil))`,
	}
	for _, src := range bad {
		hit := stateWriters.MatchString(src)
		for _, re := range rawWrites {
			hit = hit || re.MatchString(src)
		}
		if !hit {
			t.Errorf("pattern misses %q", src)
		}
	}
	if stateWriters.MatchString(`store.GetRentalBooking(ctx, db, scope, id)`) {
		t.Error("read flagged")
	}
	if rawWrites[0].MatchString(`tx.NewUpdate().Model((*RentalBooking)(nil)).Set("deposit_status = ?", x)`) {
		t.Error("deposit column flagged")
	}
}
