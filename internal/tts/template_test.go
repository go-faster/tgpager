package tts

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/go-faster/tgpager/internal/alertmanager"
)

func TestTemplateRender(t *testing.T) {
	tests := []struct {
		name    string
		tmpl    string
		payload alertmanager.WebhookPayload
		want    string
	}{
		{
			"default", "",
			testPayload(),
			"firing alert. CPUHigh. Severity critical. CPU above 90 percent",
		},
		{
			"default without severity", "",
			alertmanager.WebhookPayload{
				Status:       "firing",
				CommonLabels: map[string]string{"alertname": "DiskFull"},
			},
			"firing alert. DiskFull.",
		},
		{
			"custom", "{{ .Status }} on {{ .CommonLabels.instance }}",
			alertmanager.WebhookPayload{
				Status:       "resolved",
				CommonLabels: map[string]string{"instance": "db-1"},
			},
			"resolved on db-1",
		},
		{
			"whitespace is collapsed", "line one\n\n   line   two\n",
			alertmanager.WebhookPayload{Status: "firing"},
			"line one line two",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpl, err := NewTemplate(tt.tmpl)
			require.NoError(t, err)

			got, err := tmpl.Render(tt.payload)
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

// TestTemplateCapsLength bounds an attacker-influenceable label: alert labels
// come from metrics, and an unbounded one should not become an unbounded bill.
func TestTemplateCapsLength(t *testing.T) {
	tmpl, err := NewTemplate("{{ .CommonLabels.alertname }}")
	require.NoError(t, err)

	got, err := tmpl.Render(alertmanager.WebhookPayload{
		CommonLabels: map[string]string{"alertname": strings.Repeat("a", MaxTextBytes*4)},
	})
	require.NoError(t, err)
	require.Len(t, got, MaxTextBytes)
}

func TestTemplateErrors(t *testing.T) {
	_, err := NewTemplate("{{ .Nope ")
	require.Error(t, err, "a bad template must fail at construction, not at 3am")

	tmpl, err := NewTemplate("{{ .CommonLabels.absent }}")
	require.NoError(t, err)
	_, err = tmpl.Render(alertmanager.WebhookPayload{})
	require.Error(t, err, "rendering nothing would be a silent call")
}
