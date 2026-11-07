package notify

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"net/smtp"
	"net/textproto"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Message is an email ready to send.
type Message struct {
	ID            string // message_log id, also used for Message-ID
	Template      string
	Locale        string
	To            string
	RecipientHash string
	Subject       string
	Text          string
	HTML          string
	Date          time.Time
	Attachments   []Attachment      // M4: multipart/mixed when present
	Headers       map[string]string // M4: List-Unsubscribe, List-Unsubscribe-Post
}

// Transport sends messages.
type Transport interface {
	Send(ctx context.Context, m Message) (providerID string, err error)
}

// buildRFC5322 renders m as a multipart/alternative message, wrapped in multipart/mixed with the attachments when
// there are any.
func buildRFC5322(from string, m Message) ([]byte, error) {
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	contentType := "multipart/alternative; boundary=" + mw.Boundary()
	alt := mw
	var mixed *multipart.Writer
	if len(m.Attachments) > 0 {
		mixed = mw
		contentType = "multipart/mixed; boundary=" + mixed.Boundary()
		var inner bytes.Buffer
		alt = multipart.NewWriter(&inner)
		if err := writeAlternative(alt, m); err != nil {
			return nil, err
		}
		h := textproto.MIMEHeader{}
		h.Set("Content-Type", "multipart/alternative; boundary="+alt.Boundary())
		w, err := mixed.CreatePart(h)
		if err != nil {
			return nil, err
		}
		if _, err := w.Write(inner.Bytes()); err != nil {
			return nil, err
		}
		for _, a := range m.Attachments {
			h := textproto.MIMEHeader{}
			h.Set("Content-Type", a.ContentType)
			h.Set("Content-Transfer-Encoding", "base64")
			h.Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": a.Name}))
			w, err := mixed.CreatePart(h)
			if err != nil {
				return nil, err
			}
			enc := base64.StdEncoding.EncodeToString(a.Content)
			for len(enc) > 76 {
				if _, err := io.WriteString(w, enc[:76]+"\r\n"); err != nil {
					return nil, err
				}
				enc = enc[76:]
			}
			if _, err := io.WriteString(w, enc+"\r\n"); err != nil {
				return nil, err
			}
		}
		if err := mixed.Close(); err != nil {
			return nil, err
		}
	} else if err := writeAlternative(alt, m); err != nil {
		return nil, err
	}
	return assemble(from, m, contentType, body.Bytes()), nil
}

func writeAlternative(mw *multipart.Writer, m Message) error {
	for _, part := range []struct{ ctype, content string }{
		{"text/plain; charset=utf-8", m.Text},
		{"text/html; charset=utf-8", m.HTML},
	} {
		h := textproto.MIMEHeader{}
		h.Set("Content-Type", part.ctype)
		h.Set("Content-Transfer-Encoding", "quoted-printable")
		w, err := mw.CreatePart(h)
		if err != nil {
			return err
		}
		qp := quotedprintable.NewWriter(w)
		if _, err := qp.Write([]byte(part.content)); err != nil {
			return err
		}
		if err := qp.Close(); err != nil {
			return err
		}
	}
	return mw.Close()
}

func assemble(from string, m Message, contentType string, body []byte) []byte {
	domain := "porto.local"
	if a, err := mail.ParseAddress(from); err == nil {
		if _, d, ok := strings.Cut(a.Address, "@"); ok {
			domain = d
		}
	}
	var out bytes.Buffer
	header := func(k, v string) { fmt.Fprintf(&out, "%s: %s\r\n", k, v) }
	header("From", from)
	header("To", m.To)
	header("Subject", mime.QEncoding.Encode("utf-8", m.Subject))
	header("Date", m.Date.UTC().Format(time.RFC1123Z))
	header("Message-ID", "<"+m.ID+"@"+domain+">")
	header("MIME-Version", "1.0")
	header("Content-Type", contentType)
	header("Auto-Submitted", "auto-generated")
	for _, k := range []string{"List-Unsubscribe", "List-Unsubscribe-Post"} {
		if v := m.Headers[k]; v != "" && !strings.ContainsAny(v, "\r\n") {
			header(k, v)
		}
	}
	out.WriteString("\r\n")
	out.Write(body)
	return out.Bytes()
}

// FileTransport writes each message as an RFC 5322 .eml file (development and tests).
type FileTransport struct {
	dir  string
	from string
}

// NewFileTransport returns a Transport writing into dir.
func NewFileTransport(dir string) Transport {
	return &FileTransport{dir: dir, from: "Porto <noreply@localhost>"}
}

// NewFileTransportFrom is NewFileTransport with an explicit From address.
func NewFileTransportFrom(dir, from string) Transport { return &FileTransport{dir: dir, from: from} }

// Send implements Transport.
func (t *FileTransport) Send(_ context.Context, m Message) (string, error) {
	raw, err := buildRFC5322(t.from, m)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(t.dir, 0o700); err != nil {
		return "", fmt.Errorf("notify: mail dir: %w", err)
	}
	name := m.Date.UTC().Format("20060102T150405Z") + "-" + m.Template + "-" + m.ID + ".eml"
	if err := os.WriteFile(filepath.Join(t.dir, name), raw, 0o600); err != nil {
		return "", fmt.Errorf("notify: write eml: %w", err)
	}
	return "file:" + name, nil
}

// LogTransport only logs that a message would be sent (template and recipient hash, never the address or the
// content, which may hold a token).
type LogTransport struct{ log *slog.Logger }

// NewLogTransport returns a Transport that logs.
func NewLogTransport(log *slog.Logger) Transport { return &LogTransport{log: log} }

// Send implements Transport.
func (t *LogTransport) Send(ctx context.Context, m Message) (string, error) {
	t.log.InfoContext(ctx, "email not sent: no transport configured", "message_id", m.ID, "template", m.Template,
		"locale", m.Locale, "recipient_hash", m.RecipientHash)
	return "log", nil
}

// SMTPTransport sends through an SMTP server.
type SMTPTransport struct {
	host     string
	port     string
	implicit bool // smtps://
	auth     smtp.Auth
	user     string
	from     string
	fromAddr string
	timeout  time.Duration
}

// NewSMTPTransport parses smtp://[user:pass@]host[:port] (STARTTLS when offered, required with credentials) or
// smtps://… (implicit TLS).
func NewSMTPTransport(rawURL, from string, timeout time.Duration) (Transport, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, errors.New("notify: invalid SMTP URL")
	}
	t := &SMTPTransport{host: u.Hostname(), port: u.Port(), from: from, timeout: timeout}
	switch u.Scheme {
	case "smtp":
		if t.port == "" {
			t.port = "587"
		}
	case "smtps":
		t.implicit = true
		if t.port == "" {
			t.port = "465"
		}
	default:
		return nil, errors.New("notify: SMTP URL scheme must be smtp or smtps")
	}
	if t.host == "" {
		return nil, errors.New("notify: SMTP URL has no host")
	}
	a, err := mail.ParseAddress(from)
	if err != nil {
		return nil, errors.New("notify: invalid From address")
	}
	t.fromAddr = a.Address
	if u.User != nil {
		pass, _ := u.User.Password()
		t.user = u.User.Username()
		t.auth = smtp.PlainAuth("", t.user, pass, t.host)
	}
	if t.timeout <= 0 {
		t.timeout = 30 * time.Second
	}
	return t, nil
}

// Send implements Transport.
func (t *SMTPTransport) Send(ctx context.Context, m Message) (string, error) {
	raw, err := buildRFC5322(t.from, m)
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(ctx, t.timeout)
	defer cancel()
	addr := net.JoinHostPort(t.host, t.port)
	tlsCfg := &tls.Config{ServerName: t.host, MinVersion: tls.VersionTLS12}

	var conn net.Conn
	d := &net.Dialer{}
	if t.implicit {
		conn, err = (&tls.Dialer{NetDialer: d, Config: tlsCfg}).DialContext(ctx, "tcp", addr)
	} else {
		conn, err = d.DialContext(ctx, "tcp", addr)
	}
	if err != nil {
		return "", fmt.Errorf("notify: smtp dial: %w", err)
	}
	if dl, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(dl)
	}
	c, err := smtp.NewClient(conn, t.host)
	if err != nil {
		_ = conn.Close()
		return "", fmt.Errorf("notify: smtp hello: %w", err)
	}
	defer c.Close()
	if !t.implicit {
		if ok, _ := c.Extension("STARTTLS"); ok {
			if err := c.StartTLS(tlsCfg); err != nil {
				return "", fmt.Errorf("notify: smtp starttls: %w", err)
			}
		} else if t.auth != nil {
			return "", errors.New("notify: smtp server does not offer STARTTLS; refusing to send credentials in clear")
		}
	}
	if t.auth != nil {
		if err := c.Auth(t.auth); err != nil {
			return "", fmt.Errorf("notify: smtp auth: %w", err)
		}
	}
	if err := c.Mail(t.fromAddr); err != nil {
		return "", fmt.Errorf("notify: smtp mail from: %w", err)
	}
	to, err := mail.ParseAddress(m.To)
	if err != nil {
		return "", errors.New("notify: invalid recipient")
	}
	if err := c.Rcpt(to.Address); err != nil {
		return "", fmt.Errorf("notify: smtp rcpt: %w", err)
	}
	w, err := c.Data()
	if err != nil {
		return "", fmt.Errorf("notify: smtp data: %w", err)
	}
	if _, err := w.Write(raw); err != nil {
		return "", fmt.Errorf("notify: smtp write: %w", err)
	}
	if err := w.Close(); err != nil {
		return "", fmt.Errorf("notify: smtp end data: %w", err)
	}
	_ = c.Quit()
	return "smtp:" + m.ID, nil
}
