package collect

import (
	"strings"
	"testing"

	"github.com/coste360/porto/internal/domain/collect/levels"
	"github.com/coste360/porto/internal/domain/notify"
)

// TestTemplates_M5Render: every M5 email template exists in it and en and renders the display values.
func TestTemplates_M5Render(t *testing.T) {
	vals := map[string]string{
		"marina_name": "Porto Demo", "customer_name": "Mario Rossi", "amount": "120,00 €", "due_on": "31/03/2027",
		"link": "https://porto.test/b/demo/pay/TOKEN", "iban": "IT60X0542811101000000123456", "bank_holder": "Porto Demo Srl",
		"reference": "A2027-000001", "charges": "A2027-000001 · 31/03/2027 · 120,00 €", "contract_code": "T2027-000004",
		"payment_number": "P2027-000001", "received_on": "2027-03-31", "method": "cash", "expires_at": "30/05/2027",
	}
	names := notify.M5Templates()
	if len(names) != 9 {
		t.Fatalf("templates %v", names)
	}
	for _, name := range names {
		for _, loc := range []string{"it", "en"} {
			r, err := notify.Render(name, loc, notify.RenderData{Values: vals})
			if err != nil {
				t.Fatalf("%s/%s: %v", name, loc, err)
			}
			if r.Subject == "" || !strings.Contains(r.Text, "Porto Demo") || !strings.Contains(r.HTML, "Porto Demo") {
				t.Fatalf("%s/%s rendered %+v", name, loc, r)
			}
			if strings.HasPrefix(name, "reminder_") || name == notify.TemplatePaymentRequest {
				if !strings.Contains(r.Text, vals["link"]) || !strings.Contains(r.Text, vals["iban"]) {
					t.Fatalf("%s/%s lacks link or bank details", name, loc)
				}
			}
		}
	}
	for _, l := range levels.Order {
		if !strings.HasPrefix(ReminderTemplate(l), "reminder_") {
			t.Fatalf("level %s", l)
		}
	}
}

func TestPayToken_HashOnly(t *testing.T) {
	if h := HashToken("ABC"); len(h) != 64 || h == "ABC" {
		t.Fatalf("hash %q", h)
	}
}
