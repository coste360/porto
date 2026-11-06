package notify

import (
	"bytes"
	"embed"
	"fmt"
	htmltemplate "html/template"
	"strings"
	texttemplate "text/template"
)

// Template names.
const (
	TemplatePasswordReset = "password_reset"
	TemplateInvitation    = "invitation"
)

// Locales with templates; the first is the fallback (Italian is the source language).
var locales = []string{"it", "en"}

//go:embed templates/*/*.tmpl
var templateFS embed.FS

// RenderData is what templates see. Values are plain text; the HTML template escapes them.
type RenderData struct {
	Name        string `json:"name"`
	Link        string `json:"link"`
	ExpiresAt   string `json:"expires_at"`
	MarinaName  string `json:"marina_name,omitempty"`
	InviterName string `json:"inviter_name,omitempty"`
}

// Rendered is a rendered email.
type Rendered struct {
	Subject, Text, HTML string
}

type templateSet struct {
	subject, text *texttemplate.Template
	html          *htmltemplate.Template
}

var templates = func() map[string]templateSet {
	out := map[string]templateSet{}
	for _, loc := range locales {
		for _, name := range []string{TemplatePasswordReset, TemplateInvitation} {
			base := "templates/" + loc + "/" + name
			out[loc+"/"+name] = templateSet{
				subject: texttemplate.Must(texttemplate.ParseFS(templateFS, base+".subject.tmpl")),
				text:    texttemplate.Must(texttemplate.ParseFS(templateFS, base+".txt.tmpl")),
				html:    htmltemplate.Must(htmltemplate.ParseFS(templateFS, base+".html.tmpl")),
			}
		}
	}
	return out
}()

// normalizeLocale returns a supported locale ("it" when unknown).
func normalizeLocale(l string) string {
	l = strings.ToLower(strings.TrimSpace(l))
	if i := strings.IndexAny(l, "-_"); i > 0 {
		l = l[:i]
	}
	for _, s := range locales {
		if s == l {
			return l
		}
	}
	return locales[0]
}

// Render renders a template in a locale.
func Render(name, locale string, d RenderData) (Rendered, error) {
	set, ok := templates[normalizeLocale(locale)+"/"+name]
	if !ok {
		return Rendered{}, fmt.Errorf("notify: unknown template %q", name)
	}
	var subj, txt, html bytes.Buffer
	if err := set.subject.Execute(&subj, d); err != nil {
		return Rendered{}, fmt.Errorf("notify: render %s subject: %w", name, err)
	}
	if err := set.text.Execute(&txt, d); err != nil {
		return Rendered{}, fmt.Errorf("notify: render %s text: %w", name, err)
	}
	if err := set.html.Execute(&html, d); err != nil {
		return Rendered{}, fmt.Errorf("notify: render %s html: %w", name, err)
	}
	return Rendered{
		Subject: strings.TrimSpace(strings.ReplaceAll(subj.String(), "\n", " ")),
		Text:    txt.String(),
		HTML:    html.String(),
	}, nil
}
