package store

import (
	"context"
	"fmt"

	"github.com/uptrace/bun"
)

// Numbering of contracts, transits and quotes (m3-plan §4.5, §13 D20): one counter row per marina, series and
// local year. The unique indexes contracts_number_uidx and quotes_number_uidx are the guarantee; the counter
// only hands out the next value. Taking a number inside the write transaction that inserts the document keeps
// the sequence gap-free: a rolled-back transaction rolls the counter back too.

// NextNumber returns the next number of series in year for the marina of s and advances the counter. It must run
// inside the transaction that inserts the numbered row.
func NextNumber(ctx context.Context, tx bun.IDB, s MarinaScope, series string, year int) (int64, error) {
	seed := &NumberSequence{MarinaID: s.ID(), Series: series, Year: year, NextNumber: 1}
	if _, err := tx.NewInsert().Model(seed).On("CONFLICT DO NOTHING").Exec(ctx); err != nil {
		return 0, fmt.Errorf("number sequence seed: %w", err)
	}
	var n int64
	err := tx.NewRaw(
		"UPDATE number_sequences SET next_number = next_number + 1 WHERE marina_id = ? AND series = ? AND year = ? RETURNING next_number - 1",
		s.ID(), series, year,
	).Scan(ctx, &n)
	if err != nil {
		return 0, fmt.Errorf("number sequence next: %w", err)
	}
	return n, nil
}
