package notify

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"mime"
	"mime/multipart"
	"net/mail"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var t0 = time.Date(2026, 11, 6, 10, 0, 0, 0, time.UTC)

func TestRender_AllTemplatesAllLocales(t *testing.T) {
	d := RenderData{Name: "Anna <b>", Link: "http://localhost:8080/admin/reset-password#token=abc&invite=1", ExpiresAt: "x", MarinaName: "Marina Demo", InviterName: "Bruno"}
	for _, loc := range []string{"it", "en", "fr", "en-GB", ""} {
		for _, name := range []string{TemplatePasswordReset, TemplateInvitation} {
			r, err := Render(name, loc, d)
			if err != nil {
				t.Fatalf("%s/%s: %v", loc, name, err)
			}
			if r.Subject == "" || strings.Contains(r.Subject, "\n") {
				t.Errorf("%s/%s subject %q", loc, name, r.Subject)
			}
			if !strings.Contains(r.Text, d.Link) || !strings.Contains(r.Text, "Porto — Coste360") {
				t.Errorf("%s/%s text lacks link or footer", loc, name)
			}
			if strings.Contains(r.HTML, "<b>") || !strings.Contains(r.HTML, "Anna &lt;b&gt;") {
				t.Errorf("%s/%s html not escaped", loc, name)
			}
		}
	}
	it, _ := Render(TemplatePasswordReset, "it", d)
	en, _ := Render(TemplatePasswordReset, "en", d)
	if it.Subject == en.Subject {
		t.Fatal("locales render the same subject")
	}
	if _, err := Render("nope", "it", d); err == nil {
		t.Fatal("unknown template rendered")
	}
}

func TestTransport_File(t *testing.T) {
	dir := t.TempDir()
	tr := NewFileTransportFrom(dir, "Porto <noreply@porto.example>")
	m := Message{
		ID: "0190abcd", Template: TemplatePasswordReset, Locale: "it", To: "anna@example.test",
		Subject: "Reimposta la password — Porto", Text: "Ciao Anna, è il link.\n", HTML: "<p>Ciao Anna</p>", Date: t0,
	}
	id, err := tr.Send(context.Background(), m)
	if err != nil || !strings.HasPrefix(id, "file:") {
		t.Fatalf("send: %q %v", id, err)
	}
	files, _ := filepath.Glob(filepath.Join(dir, "*.eml"))
	if len(files) != 1 {
		t.Fatalf("files: %v", files)
	}
	info, _ := os.Stat(files[0])
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("perm %v", info.Mode())
	}
	raw, _ := os.ReadFile(files[0])
	msg, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	dec := new(mime.WordDecoder)
	subj, _ := dec.DecodeHeader(msg.Header.Get("Subject"))
	if subj != m.Subject || msg.Header.Get("To") != m.To || msg.Header.Get("Message-ID") != "<0190abcd@porto.example>" {
		t.Fatalf("headers: %v (subject %q)", msg.Header, subj)
	}
	_, params, err := mime.ParseMediaType(msg.Header.Get("Content-Type"))
	if err != nil {
		t.Fatal(err)
	}
	mr := multipart.NewReader(msg.Body, params["boundary"])
	var types []string
	for {
		p, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		types = append(types, p.Header.Get("Content-Type"))
		b, _ := io.ReadAll(p) // multipart reader decodes quoted-printable
		if strings.HasPrefix(p.Header.Get("Content-Type"), "text/plain") && strings.ReplaceAll(string(b), "\r\n", "\n") != m.Text {
			t.Fatalf("text part %q", b)
		}
	}
	if len(types) != 2 {
		t.Fatalf("parts: %v", types)
	}
}

func TestTransport_LogHasNoRecipient(t *testing.T) {
	var buf bytes.Buffer
	tr := NewLogTransport(slog.New(slog.NewJSONHandler(&buf, nil)))
	_, err := tr.Send(context.Background(), Message{ID: "1", Template: "invitation", To: "secret.person@example.test", RecipientHash: "abc", Text: "token=xyz"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(buf.String(), "secret.person") || strings.Contains(buf.String(), "xyz") || !strings.Contains(buf.String(), "abc") {
		t.Fatalf("log: %s", buf.String())
	}
}

func TestNewSMTPTransport_Validation(t *testing.T) {
	for _, bad := range []string{"http://x", "smtp://", "::"} {
		if _, err := NewSMTPTransport(bad, "Porto <a@b.c>", time.Second); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
	tr, err := NewSMTPTransport("smtps://u:p@mail.example.com", "Porto <a@b.c>", 0)
	if err != nil {
		t.Fatal(err)
	}
	st := tr.(*SMTPTransport)
	if st.port != "465" || !st.implicit || st.auth == nil || st.fromAddr != "a@b.c" {
		t.Fatalf("%+v", st)
	}
	tr, _ = NewSMTPTransport("smtp://mail.example.com", "Porto <a@b.c>", 0)
	if tr.(*SMTPTransport).port != "587" {
		t.Fatal("default port")
	}
}

func TestRetryDelay(t *testing.T) {
	want := []time.Duration{time.Minute, 2 * time.Minute, 4 * time.Minute, 8 * time.Minute, 16 * time.Minute, 32 * time.Minute, time.Hour, time.Hour}
	for i, w := range want {
		if got := retryDelay(i + 1); got != w {
			t.Errorf("retryDelay(%d) = %s want %s", i+1, got, w)
		}
	}
}
