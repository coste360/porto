package pricing

import (
	"encoding/json"
	"os" //nolint:depguard // tests read the web locale files; the engine itself does no I/O
	"path/filepath"
	"strings"
	"testing"
)

// TestI18n_CodesHaveLabels: every trace code, issue code, no-price reason and warning has an it and an en label
// in web/shared/src/i18n/locales/<locale>/pricingEnums.json (the web shows them as pricingEnums:<group>.<code>).
func TestI18n_CodesHaveLabels(t *testing.T) {
	for _, locale := range []string{"it", "en"} {
		b, err := os.ReadFile(filepath.Join("..", "..", "..", "web", "shared", "src", "i18n", "locales", locale, "pricingEnums.json"))
		if err != nil {
			t.Fatal(err)
		}
		var doc map[string]any
		if err := json.Unmarshal(b, &doc); err != nil {
			t.Fatal(err)
		}
		has := func(path string) bool {
			var cur any = doc
			for _, part := range strings.Split(path, ".") {
				m, ok := cur.(map[string]any)
				if !ok {
					return false
				}
				cur = m[part]
			}
			s, ok := cur.(string)
			return ok && s != ""
		}
		var want []string
		for _, c := range TraceCodes() {
			want = append(want, "trace."+c)
		}
		for _, c := range IssueCodes() {
			want = append(want, "issues."+c)
		}
		for _, r := range []string{ReasonNoRule, ReasonNoBand, ReasonNoAmount, ReasonOutsideValidity, ReasonNoPrimaryItem, ReasonUnitMissing} {
			want = append(want, "noPriceReasons."+r)
		}
		want = append(want, "warnings."+WarningStayExceedsValidity)
		for _, ct := range ContractTypes() {
			want = append(want, "contractTypes."+string(ct))
		}
		for _, b := range Bases() {
			want = append(want, "bases."+string(b))
		}
		for _, u := range TimeUnits() {
			want = append(want, "timeUnits."+string(u))
		}
		for _, it := range ItemTypes() {
			want = append(want, "itemTypes."+string(it))
		}
		for _, ch := range Channels() {
			want = append(want, "channels."+string(ch))
		}
		for _, k := range want {
			if !has(k) {
				t.Errorf("%s: pricingEnums:%s has no label", locale, k)
			}
		}
	}
}
