// Package alertmanager decodes Alertmanager webhook payloads (schema version 4).
package alertmanager

import (
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/go-faster/errors"
)

// WebhookPayload is the Alertmanager webhook body, as posted by a
// config.WebhookConfig receiver.
type WebhookPayload struct {
	Version           string            `json:"version"`
	GroupKey          string            `json:"groupKey"`
	Status            string            `json:"status"`
	Receiver          string            `json:"receiver"`
	GroupLabels       map[string]string `json:"groupLabels"`
	CommonLabels      map[string]string `json:"commonLabels"`
	CommonAnnotations map[string]string `json:"commonAnnotations"`
	ExternalURL       string            `json:"externalURL"`
	Alerts            []Alert           `json:"alerts"`
	TruncatedAlerts   int               `json:"truncatedAlerts"`
}

// Alert is one alert inside a [WebhookPayload].
type Alert struct {
	Status       string            `json:"status"`
	Labels       map[string]string `json:"labels"`
	Annotations  map[string]string `json:"annotations"`
	StartsAt     time.Time         `json:"startsAt"`
	EndsAt       time.Time         `json:"endsAt"`
	GeneratorURL string            `json:"generatorURL"`
	Fingerprint  string            `json:"fingerprint"`
}

func DecodeReader(r io.Reader) (WebhookPayload, error) {
	var p WebhookPayload
	if err := json.NewDecoder(r).Decode(&p); err != nil {
		return WebhookPayload{}, errors.Wrap(err, "decode webhook payload")
	}
	return p, nil
}

func DecodeWebhook(data []byte) (WebhookPayload, error) {
	var p WebhookPayload
	if err := json.Unmarshal(data, &p); err != nil {
		return WebhookPayload{}, errors.Wrap(err, "decode webhook payload")
	}
	return p, nil
}

func (p WebhookPayload) Validate() error {
	switch {
	case p.Version == "":
		return errors.New("version is required")
	case p.GroupKey == "":
		return errors.New("groupKey is required")
	case p.Status == "":
		return errors.New("status is required")
	}
	return nil
}

func (p WebhookPayload) IsFiring() bool {
	return p.Status == "firing"
}

func (p WebhookPayload) String() string {
	return fmt.Sprintf("WebhookPayload{version=%s, groupKey=%s, status=%s, alerts=%d}",
		p.Version, p.GroupKey, p.Status, len(p.Alerts))
}
