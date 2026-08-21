package tts

import (
	"strings"
	"text/template"

	"github.com/go-faster/errors"

	"github.com/go-faster/tgpager/internal/alertmanager"
)

// DefaultTemplate says what fired, how badly, and why.
const DefaultTemplate = `{{ .Status }} alert.` +
	`{{ with .CommonLabels.alertname }} {{ . }}.{{ end }}` +
	`{{ with .CommonLabels.severity }} Severity {{ . }}.{{ end }}` +
	`{{ with .CommonAnnotations.summary }} {{ . }}{{ end }}`

// MaxTextBytes bounds rendered speech. Alert labels come from metrics and are
// attacker-influenceable; an unbounded label should not become an unbounded
// bill, nor a call that never ends.
const MaxTextBytes = 1024

// Template renders an alert into the sentence a callee hears.
type Template struct {
	tmpl *template.Template
}

func NewTemplate(text string) (*Template, error) {
	if strings.TrimSpace(text) == "" {
		text = DefaultTemplate
	}
	tmpl, err := template.New("speech").Option("missingkey=zero").Parse(text)
	if err != nil {
		return nil, errors.Wrap(err, "parse template")
	}
	return &Template{tmpl: tmpl}, nil
}

// Render produces the text to speak, collapsing whitespace so a multi-line
// template does not become a stuttering read.
func (t *Template) Render(payload alertmanager.WebhookPayload) (string, error) {
	var sb strings.Builder
	if err := t.tmpl.Execute(&sb, payload); err != nil {
		return "", errors.Wrap(err, "render template")
	}

	text := strings.Join(strings.Fields(sb.String()), " ")
	if text == "" {
		return "", errors.New("template rendered no text")
	}
	if len(text) > MaxTextBytes {
		text = text[:MaxTextBytes]
	}
	return text, nil
}
