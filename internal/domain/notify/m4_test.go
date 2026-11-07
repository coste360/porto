package notify

import (
	"bytes"
	"encoding/base64"
	"io"
	"mime"
	"mime/multipart"
	"net/mail"
	"strings"
	"testing"
)

func TestRender_M4Templates(t *testing.T) {
	values := map[string]string{
		ValueMarinaName: "Marina <Demo>", ValueCustomerName: "Anna", ValueLink: "https://porto.test/b/m/m/quote/TOKEN",
		ValueContractCode: "ORM-1", ValueQuoteCode: "PRV-1", ValueAmount: "1.234,56 €", ValueDueOn: "01/02/2027",
		ValueCode: "123456", ValueDocumentTitle: "Contratto", ValueDeadlineKind: "Fine contratto", ValueSubject: "Avviso",
		ValuePeriod: "2027", ValueIBAN: "IT60X0542811101000000123456",
	}
	for _, loc := range []string{"it", "en"} {
		for _, name := range M4Templates() {
			r, err := Render(name, loc, RenderData{Values: values, BodyHTML: "<p>corpo</p>", BodyText: "corpo"})
			if err != nil {
				t.Fatalf("%s/%s: %v", loc, name, err)
			}
			if r.Subject == "" || strings.Contains(r.Subject, "\n") {
				t.Errorf("%s/%s subject %q", loc, name, r.Subject)
			}
			if strings.Contains(r.HTML, "<Demo>") {
				t.Errorf("%s/%s html not escaped", loc, name)
			}
		}
	}
	r, _ := Render(TemplateSignatureOTP, "it", RenderData{Values: values})
	if !strings.Contains(r.Text, "123456") {
		t.Fatal("otp code missing")
	}
	r, _ = Render(TemplateInstallmentPaymentRequest, "en", RenderData{Values: map[string]string{ValueAmount: "€10.00"}})
	if strings.Contains(r.Text, "IBAN") || !strings.Contains(r.Text, "€10.00") {
		t.Fatalf("missing values must drop their line:\n%s", r.Text)
	}
	r, _ = Render(TemplateCommunication, "it", RenderData{Values: values, BodyHTML: "<p>corpo</p>", BodyText: "corpo"})
	if !strings.Contains(r.HTML, "<p>corpo</p>") || !strings.Contains(r.Text, "TOKEN") {
		t.Fatal("communication body or unsubscribe link missing")
	}
}

func TestBuildRFC5322_Attachments(t *testing.T) {
	content := bytes.Repeat([]byte("%PDF-1.7 abc"), 50)
	raw, err := buildRFC5322("Porto <noreply@porto.test>", Message{
		ID: "m1", To: "a@example.test", Subject: "Ciao", Text: "testo", HTML: "<p>testo</p>", Date: t0,
		Attachments: []Attachment{{Name: "contratto.pdf", ContentType: "application/pdf", Content: content}},
		Headers:     map[string]string{"List-Unsubscribe": "<https://x/u>", "List-Unsubscribe-Post": "List-Unsubscribe=One-Click", "X-Evil": "no"},
	})
	if err != nil {
		t.Fatal(err)
	}
	msg, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	if msg.Header.Get("List-Unsubscribe") != "<https://x/u>" || msg.Header.Get("X-Evil") != "" {
		t.Fatal("headers")
	}
	mt, params, _ := mime.ParseMediaType(msg.Header.Get("Content-Type"))
	if mt != "multipart/mixed" {
		t.Fatalf("content type %s", mt)
	}
	mr := multipart.NewReader(msg.Body, params["boundary"])
	parts := 0
	for {
		p, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		parts++
		if parts == 2 {
			if p.FileName() != "contratto.pdf" {
				t.Fatalf("filename %q", p.FileName())
			}
			b, _ := io.ReadAll(p)
			dec, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(string(b), "\r\n", ""))
			if err != nil || !bytes.Equal(dec, content) {
				t.Fatal("attachment content")
			}
		}
	}
	if parts != 2 {
		t.Fatalf("%d parts", parts)
	}
}
