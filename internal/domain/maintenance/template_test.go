package maintenance

import (
	"strings"
	"testing"

	"github.com/coste360/porto/internal/domain/notify"
)

// TestTemplate_MaintenanceDue: the maintenance_due notice renders in both locales with the task values.
func TestTemplate_MaintenanceDue(t *testing.T) {
	if TemplateMaintenanceDue != notify.TemplateMaintenanceDue {
		t.Fatalf("template name drift: %q", TemplateMaintenanceDue)
	}
	d := notify.RenderData{Values: map[string]string{
		"task_title": "Ispezione <subacquea>", "element_code": "A-12", "due_on": "15/07/2027", "marina_name": "Marina Demo",
		"link": "http://localhost:8080/admin/m/x/maintenance",
	}}
	for _, loc := range []string{"it", "en"} {
		r, err := notify.Render(TemplateMaintenanceDue, loc, d)
		if err != nil {
			t.Fatalf("%s: %v", loc, err)
		}
		if !strings.Contains(r.Subject, "15/07/2027") || !strings.Contains(r.Text, "A-12") || strings.Contains(r.HTML, "<subacquea>") {
			t.Fatalf("%s: %+v", loc, r)
		}
	}
}
