package charge

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/store"
)

// Descriptions of system charges, in the marina's first locale (m5-plan §3.1): codes and numbers only, never PII.

var descriptions = map[string]map[string]string{
	"it": {
		"installment": "Rata {seq} – contratto {code}", "trueup": "Conguaglio contratto {code}", "transit": "Transito {code}",
		"adjustment": "Rettifica di {code}", "termination": "Chiusura anticipata contratto {code}",
		"holder_management_fee": "Canone di gestione – rata {seq}", "holder_common_advance": "Oneri condominiali, acconto – rata {seq}",
		"holder_common_balance": "Oneri condominiali, conguaglio – rata {seq}", "holder_transfer_fee": "Diritto di subentro",
		"opening_balance": "Saldo di apertura {code}", "replace": "Sostituisce {code}", "reversal": "Storno di {code}",
	},
	"en": {
		"installment": "Instalment {seq} – contract {code}", "trueup": "True-up of contract {code}", "transit": "Transit {code}",
		"adjustment": "Adjustment of {code}", "termination": "Early termination of contract {code}",
		"holder_management_fee": "Management fee – instalment {seq}", "holder_common_advance": "Common charges, advance – instalment {seq}",
		"holder_common_balance": "Common charges, balance – instalment {seq}", "holder_transfer_fee": "Transfer fee",
		"opening_balance": "Opening balance {code}", "replace": "Replaces {code}", "reversal": "Reversal of {code}",
	},
}

// marinaLocale is the first locale of the marina (it by default).
func marinaLocale(ctx context.Context, db bun.IDB, scope store.MarinaScope) string {
	m, err := store.GetMarina(ctx, db, scope)
	if err != nil || m.DefaultLocale == "" {
		return "it"
	}
	l := strings.ToLower(m.DefaultLocale)
	if i := strings.IndexAny(l, "-_"); i > 0 {
		l = l[:i]
	}
	if _, ok := descriptions[l]; !ok {
		return "it"
	}
	return l
}

// describe returns the description of a system charge.
func (s *Service) describe(ctx context.Context, db bun.IDB, scope store.MarinaScope, key, code, seq string) string {
	t := descriptions[marinaLocale(ctx, db, scope)][key]
	if t == "" {
		t = descriptions["it"][key]
	}
	return strings.NewReplacer("{code}", code, "{seq}", seq).Replace(t)
}

// describeLines fills the empty line descriptions with the item names (marina locale), else fallback.
func (s *Service) describeLines(ctx context.Context, db bun.IDB, scope store.MarinaScope, lines []LineInput, fallback string) []LineInput {
	var ids []string
	for _, l := range lines {
		if l.Description == "" && l.ItemID != "" {
			ids = append(ids, l.ItemID)
		}
	}
	names := map[string]string{}
	if len(ids) > 0 {
		loc := marinaLocale(ctx, db, scope)
		if items, err := store.ItemsByIDs(ctx, db, scope, ids); err == nil {
			for i := range items {
				names[items[i].ID] = itemNameIn(&items[i], loc)
			}
		}
	}
	for i := range lines {
		if lines[i].Description != "" {
			continue
		}
		if n := names[lines[i].ItemID]; n != "" {
			lines[i].Description = n
		} else if lines[i].ItemCode != "" {
			lines[i].Description = lines[i].ItemCode
		} else {
			lines[i].Description = fallback
		}
	}
	return lines
}

// itemName is the Italian (source language) name of an item, else its code.
func itemName(it *store.Item) string { return itemNameIn(it, "it") }

func itemNameIn(it *store.Item, loc string) string {
	var n map[string]string
	if json.Unmarshal(it.Name, &n) == nil {
		if v := strings.TrimSpace(n[loc]); v != "" {
			return v
		}
		if v := strings.TrimSpace(n["it"]); v != "" {
			return v
		}
	}
	return it.Code
}
