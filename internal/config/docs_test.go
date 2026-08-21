package config

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/go-faster/sdk/gold"
	"github.com/stretchr/testify/require"
)

// TestReferenceIsCurrent keeps CONFIG.md generated from the descriptor, so a
// configuration change that never reached the reference fails here instead of
// shipping as a stale document.
func TestReferenceIsCurrent(t *testing.T) {
	got, err := Reference()
	require.NoError(t, err)

	want, err := os.ReadFile("../../CONFIG.md")
	require.NoError(t, err)

	require.Equal(t, gold.NormalizeNewlines(string(want)), string(got),
		"CONFIG.md is stale, run go generate ./internal/config")
}

// TestJSONSchemaIsCurrent keeps config.schema.json generated from the
// descriptor, so editor completion cannot drift from what actually loads.
func TestJSONSchemaIsCurrent(t *testing.T) {
	got, err := JSONSchema()
	require.NoError(t, err)

	want, err := os.ReadFile("../../config.schema.json")
	require.NoError(t, err)

	require.JSONEq(t, string(want), string(got),
		"config.schema.json is stale, run go generate ./internal/config")
}

// TestJSONSchemaKeepsSecrets guards a subtlety: figureout.Secret implies
// Hidden, which removes a field from the Markdown reference. The schema must
// still carry it, or an editor would flag a valid app_hash as unknown.
//
// A credential is a oneOf now — a scalar, or {value|env|file} — so the marking
// lives on the object branch's value. The scalar branch carries no writeOnly,
// which is a figureout gap rather than a decision here.
func TestJSONSchemaKeepsSecrets(t *testing.T) {
	raw, err := JSONSchema()
	require.NoError(t, err)

	var schema struct {
		Properties map[string]struct {
			Properties map[string]struct {
				OneOf []struct {
					Type       any `json:"type"`
					Properties map[string]struct {
						WriteOnly bool `json:"writeOnly"`
					} `json:"properties"`
				} `json:"oneOf"`
			} `json:"properties"`
		} `json:"properties"`
	}
	require.NoError(t, json.Unmarshal(raw, &schema))

	for _, tt := range []struct{ section, field string }{
		{"telegram", "app_hash"},
		{"webhook", "token"},
	} {
		t.Run(tt.section+"."+tt.field, func(t *testing.T) {
			got, ok := schema.Properties[tt.section].Properties[tt.field]
			require.True(t, ok, "must stay in the schema")
			require.Len(t, got.OneOf, 2, "a scalar and an object spelling")

			var marked bool
			for _, branch := range got.OneOf {
				if v, ok := branch.Properties["value"]; ok && v.WriteOnly {
					marked = true
				}
			}
			require.True(t, marked, "the literal value must be marked writeOnly")
		})
	}
}
