package refresh

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kkamegawa/gh-repospec/internal/config"
	yamlv3 "gopkg.in/yaml.v3"
)

func decodeYAML(t *testing.T, src string) *config.Config {
	t.Helper()
	var cfg config.Config
	decoder := yamlv3.NewDecoder(strings.NewReader(src))
	decoder.KnownFields(true)
	if err := decoder.Decode(&cfg); err != nil {
		t.Fatalf("decode fixture: %v", err)
	}
	return &cfg
}

func TestRefreshYAMLEndToEndUpdatesLabelsAndValidatesResult(t *testing.T) {
	t.Parallel()
	original := "schemaVersion: 1\n" +
		"repository:\n" +
		"  owner: octo\n" +
		"  name: demo\n" +
		"  visibility: public\n" +
		"  description: old description\n" +
		"labels:\n" +
		"  - name: bug\n" +
		"    color: d73a4a\n" +
		"  - name: stale\n" +
		"    color: cccccc\n"
	current := decodeYAML(t, original)
	desired := decodeYAML(t, "schemaVersion: 1\n"+
		"repository:\n"+
		"  owner: octo\n"+
		"  name: demo\n"+
		"  visibility: public\n"+
		"  description: new description\n"+
		"labels:\n"+
		"  - name: bug\n"+
		"    color: ff0000\n"+
		"  - name: enhancement\n"+
		"    color: a2eeef\n")

	result, err := Refresh([]byte(original), FormatYAML, current, desired)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Changed {
		t.Fatal("Changed = false, want true")
	}
	got := string(result.Updated)
	if !strings.Contains(got, "description: new description") {
		t.Fatalf("Updated = %s, want description refreshed", got)
	}
	if !strings.Contains(got, "color: ff0000") {
		t.Fatalf("Updated = %s, want bug's color refreshed", got)
	}
	if !strings.Contains(got, "name: stale") {
		t.Fatalf("Updated = %s, want stale kept, never deleted", got)
	}
	if !strings.Contains(got, "name: enhancement") {
		t.Fatalf("Updated = %s, want enhancement added", got)
	}

	// The result must itself reload cleanly through the exact strict
	// decoding config.Load uses.
	if _, err := decodeStrict(got); err != nil {
		t.Fatalf("refreshed document did not decode: %v\n%s", err, got)
	}
}

func TestRefreshNoopReturnsOriginalBytesUnchanged(t *testing.T) {
	t.Parallel()
	original := "schemaVersion: 1\nrepository:\n  owner: octo\n  name: demo\n  visibility: public\n"
	cfg := decodeYAML(t, original)
	result, err := Refresh([]byte(original), FormatYAML, cfg, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if result.Changed {
		t.Fatal("Changed = true, want false for identical current and desired")
	}
	if string(result.Updated) != original {
		t.Fatalf("Updated = %q, want the original bytes returned unchanged", result.Updated)
	}
}

func TestRefreshDoesNotFailOnAbsentSeedDirectory(t *testing.T) {
	t.Parallel()
	missing := filepath.Join(t.TempDir(), "does-not-exist")
	original := "schemaVersion: 1\n" +
		"repository:\n  owner: octo\n  name: demo\n  visibility: public\n  description: old\n" +
		"seed:\n  directory:\n    path: " + missing + "\n"
	current := decodeYAML(t, original)
	desired := decodeYAML(t, "schemaVersion: 1\nrepository:\n  owner: octo\n  name: demo\n  visibility: public\n  description: new\n")

	if _, err := os.Stat(missing); err == nil {
		t.Fatal("test setup: the seed directory unexpectedly exists")
	}
	result, err := Refresh([]byte(original), FormatYAML, current, desired)
	if err != nil {
		t.Fatalf("Refresh() error = %v, want the absent seed directory ignored", err)
	}
	if !bytes.Contains(result.Updated, []byte("description: new")) {
		t.Fatalf("Updated = %s, want description refreshed despite the missing seed directory", result.Updated)
	}
	if !bytes.Contains(result.Updated, []byte(missing)) {
		t.Fatalf("Updated = %s, want the seed declaration preserved untouched", result.Updated)
	}
}

func TestRefreshJSONReformatsAndPreservesLocalIntent(t *testing.T) {
	t.Parallel()
	type doc struct {
		SchemaVersion int `json:"schemaVersion"`
		Repository    struct {
			Owner       string  `json:"owner"`
			Name        string  `json:"name"`
			Visibility  string  `json:"visibility"`
			Description *string `json:"description,omitempty"`
		} `json:"repository"`
		Seed struct {
			Directory struct {
				Path string `json:"path"`
			} `json:"directory"`
		} `json:"seed"`
	}
	var original doc
	original.SchemaVersion = 1
	original.Repository.Owner = "octo"
	original.Repository.Name = "demo"
	original.Repository.Visibility = "public"
	desc := "old"
	original.Repository.Description = &desc
	original.Seed.Directory.Path = filepath.Join(t.TempDir(), "missing")
	originalBytes, err := json.MarshalIndent(&original, "", "  ")
	if err != nil {
		t.Fatal(err)
	}

	var current config.Config
	if err := json.Unmarshal(originalBytes, &current); err != nil {
		t.Fatal(err)
	}
	desired := current
	newDesc := "new"
	desired.Repository.Description = &newDesc

	result, err := Refresh(originalBytes, FormatJSON, &current, &desired)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Changed {
		t.Fatal("Changed = false, want true")
	}
	var updated config.Config
	if err := json.Unmarshal(result.Updated, &updated); err != nil {
		t.Fatalf("refreshed JSON did not decode: %v\n%s", err, result.Updated)
	}
	if updated.Repository.Description == nil || *updated.Repository.Description != "new" {
		t.Fatalf("Description = %v, want \"new\"", updated.Repository.Description)
	}
	if updated.Seed == nil || updated.Seed.Directory == nil || updated.Seed.Directory.Path != original.Seed.Directory.Path {
		t.Fatalf("Seed = %#v, want the local seed declaration preserved", updated.Seed)
	}
}

func TestRefreshYAMLKeepsAIPullRequestScanWhenLiveStateUnreadable(t *testing.T) {
	t.Parallel()
	original := "schemaVersion: 1\n" +
		"repository:\n  owner: octo\n  name: demo\n  visibility: public\n" +
		"security:\n" +
		"  aiPullRequestScan: # needs Advanced Security\n" +
		"    enabled: true\n" +
		"    optional: true\n"
	current := decodeYAML(t, original)
	// Import leaves aiPullRequestScan nil when GitHub answers 403 or 404.
	desired := decodeYAML(t, "schemaVersion: 1\n"+
		"repository:\n  owner: octo\n  name: demo\n  visibility: public\n"+
		"security: {}\n")

	result, err := Refresh([]byte(original), FormatYAML, current, desired)
	if err != nil {
		t.Fatal(err)
	}
	if result.Changed {
		t.Fatalf("Changed = true, want the declaration kept: %#v", result.Plan)
	}
	if string(result.Updated) != original {
		t.Fatalf("Updated = %q, want the original bytes unchanged", result.Updated)
	}
}

func TestRefreshYAMLUpdatesDeclaredAIPullRequestScan(t *testing.T) {
	t.Parallel()
	original := "schemaVersion: 1\n" +
		"repository:\n  owner: octo\n  name: demo\n  visibility: public\n" +
		"security:\n" +
		"  codeqlDefaultSetup:\n    enabled: true\n" +
		"  aiPullRequestScan:\n    enabled: false # preview\n    optional: true\n"
	current := decodeYAML(t, original)
	desired := decodeYAML(t, "schemaVersion: 1\n"+
		"repository:\n  owner: octo\n  name: demo\n  visibility: public\n"+
		"security:\n"+
		"  codeqlDefaultSetup:\n    enabled: true\n"+
		"  aiPullRequestScan:\n    enabled: true\n")

	result, err := Refresh([]byte(original), FormatYAML, current, desired)
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Replace(original, "enabled: false # preview", "enabled: true # preview", 1)
	if string(result.Updated) != want {
		t.Fatalf("Updated = %q, want %q", result.Updated, want)
	}
	refreshed, err := decodeStrict(string(result.Updated))
	if err != nil {
		t.Fatalf("refreshed document did not decode: %v\n%s", err, result.Updated)
	}
	if got := refreshed.Security.AIPullRequestScan; got == nil || !got.Enabled || !got.Optional {
		t.Fatalf("AIPullRequestScan = %#v, want enabled with optional preserved", got)
	}
}

func decodeStrict(data string) (*config.Config, error) {
	decoder := yamlv3.NewDecoder(strings.NewReader(data))
	decoder.KnownFields(true)
	var cfg config.Config
	if err := decoder.Decode(&cfg); err != nil {
		return nil, err
	}
	return &cfg, config.ValidateDeclaredSurface(&cfg)
}
