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
func TestJSONSchemaKeepsSecrets(t *testing.T) {
	raw, err := JSONSchema()
	require.NoError(t, err)

	var schema struct {
		Properties map[string]struct {
			Properties map[string]struct {
				WriteOnly bool `json:"writeOnly"`
			} `json:"properties"`
		} `json:"properties"`
	}
	require.NoError(t, json.Unmarshal(raw, &schema))

	appHash, ok := schema.Properties["telegram"].Properties["app_hash"]
	require.True(t, ok, "app_hash must stay in the schema")
	require.True(t, appHash.WriteOnly, "app_hash must be marked writeOnly")

	token, ok := schema.Properties["webhook"].Properties["token"]
	require.True(t, ok, "token must stay in the schema")
	require.True(t, token.WriteOnly, "token must be marked writeOnly")
}
