package alertmanager

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDecodeWebhook(t *testing.T) {
	input := []byte(`{
		"version": "4",
		"groupKey": "{}:{alertname=\"TestAlert\"}",
		"status": "firing",
		"receiver": "tgcall",
		"groupLabels": {"alertname": "TestAlert"},
		"commonLabels": {"severity": "critical"},
		"commonAnnotations": {"summary": "Something is wrong"},
		"externalURL": "http://alertmanager:9093",
		"alerts": [
			{
				"status": "firing",
				"labels": {"alertname": "TestAlert", "severity": "critical"},
				"annotations": {"summary": "CPU usage above 90%"},
				"startsAt": "2024-01-01T00:00:00Z",
				"endsAt": "0001-01-01T00:00:00Z",
				"generatorURL": "http://prometheus:9090/graph?g0.expr=up",
				"fingerprint": "abc123"
			}
		],
		"truncatedAlerts": 0
	}`)

	p, err := DecodeWebhook(input)
	require.NoError(t, err)
	require.Equal(t, "4", p.Version)
	require.Equal(t, "firing", p.Status)
	require.True(t, p.IsFiring())
	require.Len(t, p.Alerts, 1)
	require.Equal(t, "TestAlert", p.Alerts[0].Labels["alertname"])
	require.NoError(t, p.Validate())
}

func TestWebhookPayload_Validate(t *testing.T) {
	tests := []struct {
		name    string
		p       WebhookPayload
		wantErr bool
	}{
		{"valid", WebhookPayload{Version: "4", GroupKey: "{}", Status: "firing"}, false},
		{"empty version", WebhookPayload{GroupKey: "{}", Status: "firing"}, true},
		{"empty groupKey", WebhookPayload{Version: "4", Status: "firing"}, true},
		{"empty status", WebhookPayload{Version: "4", GroupKey: "{}"}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.p.Validate()
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestWebhookPayload_IsFiring(t *testing.T) {
	require.True(t, WebhookPayload{Status: "firing"}.IsFiring())
	require.False(t, WebhookPayload{Status: "resolved"}.IsFiring())
	require.False(t, WebhookPayload{Status: ""}.IsFiring())
}

func TestWebhookPayload_String(t *testing.T) {
	s := WebhookPayload{Version: "4", GroupKey: "test", Status: "firing", Alerts: []Alert{{}}}.String()
	require.Contains(t, s, "firing")
	require.Contains(t, s, "test")
}

func TestDecodeWebhook_StandardLibrary(t *testing.T) {
	t.Run("json.Unmarshal", func(t *testing.T) {
		input := []byte(`{"version":"4","groupKey":"{}","status":"firing","receiver":"","alerts":[],"truncatedAlerts":0}`)
		var p WebhookPayload
		err := json.Unmarshal(input, &p)
		require.NoError(t, err)
		require.Equal(t, "4", p.Version)
		require.Equal(t, "firing", p.Status)
	})
}
