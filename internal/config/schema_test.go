package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestSchemaDocumentIsValidJSON guards schema/repospec.schema.json
// against syntax regressions. Nothing else in the build parses this file, so
// a malformed document (an unbalanced brace, an unsupported regex flag) would
// otherwise reach users only when an editor or an external validator loads
// it.
func TestSchemaDocumentIsValidJSON(t *testing.T) {
	t.Parallel()
	path := filepath.Join("..", "..", "schema", "repospec.schema.json")
	data, err := os.ReadFile(path) //nolint:gosec // the test path comes from t.TempDir
	if err != nil {
		t.Fatalf("read schema document: %v", err)
	}
	var document map[string]any
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatalf("schema document is not valid JSON: %v", err)
	}
}

// TestExampleConfigurationValidatesAgainstConfigContract keeps
// docs/examples/bootstrap.yaml, the configuration referenced from the
// README, in sync with the typed contract it is meant to demonstrate.
func TestExampleConfigurationValidatesAgainstConfigContract(t *testing.T) {
	t.Parallel()
	path := filepath.Join("..", "..", "docs", "examples", "bootstrap.yaml")
	if _, err := Load(path); err != nil {
		t.Fatalf("Load(%q) error = %v", path, err)
	}
}

// TestExtendedExampleConfigurationValidatesAgainstConfigContract keeps
// docs/examples/bootstrap-extended.yaml, which demonstrates the gh-infra
// feature review fields, in sync with the typed contract.
func TestExtendedExampleConfigurationValidatesAgainstConfigContract(t *testing.T) {
	t.Parallel()
	path := filepath.Join("..", "..", "docs", "examples", "bootstrap-extended.yaml")
	if _, err := Load(path); err != nil {
		t.Fatalf("Load(%q) error = %v", path, err)
	}
}
