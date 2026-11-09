//go:build integration

package app_test

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"log/slog"
	"testing"
	"time"

	"github.com/coste360/porto/internal/app"
	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/clock"
	"github.com/coste360/porto/internal/config"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/fixture"
	"github.com/coste360/porto/internal/testutil/testdb"
	"github.com/coste360/porto/pkg/ext"
)

type seedDaySum struct {
	Day   calendar.Date `bun:"day"`
	Cents int64         `bun:"cents"`
}

// TestSeed_DemoLedgerSpread: the demo charges (issue date) and payments (receipt date) of the current year are
// spread over the months, as those of the previous-year history, instead of all landing on the day the seed ran
// (assertLedgerSpread). Seeded early in a month (little of the month elapsed); TestSeed_DemoHistoryAndOnlineBooking
// checks the same in the summer.
func TestSeed_DemoLedgerSpread(t *testing.T) {
	now := time.Date(2026, time.November, 9, 9, 0, 0, 0, time.UTC)
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		key := make([]byte, 32)
		_, _ = rand.Read(key)
		vars := map[string]string{
			"PORTO_SECRET_KEY": base64.StdEncoding.EncodeToString(key), "PORTO_DEV": "true", "PORTO_DATA_DIR": t.TempDir(),
		}
		cfg, err := config.Load(func(k string) (string, bool) { v, ok := vars[k]; return v, ok })
		if err != nil {
			t.Fatal(err)
		}
		a, err := app.NewWithOptions(context.Background(), cfg, "test", ext.Extensions{}, slog.New(slog.DiscardHandler),
			app.Options{DB: db, Argon2: auth.TestArgon2Params, Transport: &fixture.Mailbox{}, Clock: clock.NewFake(now)})
		if err != nil {
			t.Fatal(err)
		}
		ctx := fixture.Ctx()
		res, err := a.SeedDemo(ctx, "demo-password-123")
		if err != nil {
			t.Fatal(err)
		}
		assertLedgerSpread(ctx, t, db, res.MarinaID, now)
	})
}

// assertLedgerSpread checks the demo ledger of the current year seeded at now: no single day holds more than a
// fifth of the year-to-date charges or payments, and the month to date and the year to date compare with the same
// periods one year earlier within 0.5x..2x (the dashboard "Mese in corso" and "Da inizio anno", m7-plan §4).
func assertLedgerSpread(ctx context.Context, t *testing.T, db *store.DB, marinaID string, now time.Time) {
	t.Helper()
	loc, err := calendar.LoadLocation("Europe/Rome")
	if err != nil {
		t.Fatal(err)
	}
	today := calendar.In(now, loc)
	jan1 := calendar.New(today.Year(), time.January, 1)
	month1 := calendar.New(today.Year(), today.Month(), 1)
	ago := func(d calendar.Date) calendar.Date { return calendar.New(d.Year()-1, d.Month(), d.Day()) }
	for _, l := range []struct{ table, day, amount string }{
		{"charges", "issued_on", "total_cents"},
		{"payments", "received_on", "amount_cents"},
	} {
		var days []seedDaySum
		if err := db.NewSelect().TableExpr(l.table).ColumnExpr(l.day+" AS day, SUM("+l.amount+") AS cents").
			Where("marina_id = ?", marinaID).Where(l.day+" >= ? AND "+l.day+" <= ?", ago(jan1), today).
			GroupExpr(l.day).Scan(ctx, &days); err != nil {
			t.Fatal(err)
		}
		sum := func(from, to calendar.Date) int64 {
			var s int64
			for _, d := range days {
				if !d.Day.Before(from) && !d.Day.After(to) {
					s += d.Cents
				}
			}
			return s
		}
		ytd := sum(jan1, today)
		var peak seedDaySum
		for _, d := range days {
			if !d.Day.Before(jan1) && d.Cents > peak.Cents {
				peak = d
			}
		}
		mtd, mtdAgo := sum(month1, today), sum(ago(month1), ago(today))
		ytdAgo := sum(ago(jan1), ago(today))
		t.Logf("%s: YTD %d vs %d, MTD %d vs %d, peak day %s %d (%d %%)", l.table, ytd/100, ytdAgo/100, mtd/100,
			mtdAgo/100, peak.Day, peak.Cents/100, peak.Cents*100/max(ytd, 1))
		if ytd <= 0 || mtd <= 0 || mtdAgo <= 0 || ytdAgo <= 0 {
			t.Fatalf("%s: an empty period (YTD %d / %d, MTD %d / %d)", l.table, ytd, ytdAgo, mtd, mtdAgo)
		}
		if peak.Cents*5 > ytd {
			t.Errorf("%s: %s holds %d of the %d of the year to date (more than a fifth)", l.table, peak.Day, peak.Cents, ytd)
		}
		if mtd*2 < mtdAgo || mtd > 2*mtdAgo {
			t.Errorf("%s: month to date %d vs %d one year earlier (outside 0.5x..2x)", l.table, mtd, mtdAgo)
		}
		if ytd*2 < ytdAgo || ytd > 2*ytdAgo {
			t.Errorf("%s: year to date %d vs %d one year earlier (outside 0.5x..2x)", l.table, ytd, ytdAgo)
		}
	}
}
