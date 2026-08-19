package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadAcceptsStrictYAMLAndResolvesSeedRelativeToConfiguration(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	seedDirectory := filepath.Join(directory, "boilerplate")
	if err := os.Mkdir(seedDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "bootstrap.yaml")
	content := `schemaVersion: 1
repository:
  owner: octo-org
  name: example
  visibility: private
seed:
  directory:
    path: boilerplate
commands:
  - id: apm-init
    program: apm
    args: [init, -y]
    cwd: .
    timeout: 5m
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got, want := cfg.Seed.Directory.Path, seedDirectory; got != want {
		t.Fatalf("seed directory = %q, want %q", got, want)
	}
}

func TestLoadRejectsUnknownAndDuplicateFields(t *testing.T) {
	t.Parallel()
	for name, content := range map[string]string{
		"unknown": `schemaVersion: 1
repository:
  owner: octo-org
  name: example
  visibility: public
  unknown: value
`,
		"duplicate": `schemaVersion: 1
repository:
  owner: octo-org
  owner: another-org
  name: example
  visibility: public
`,
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "bootstrap.yaml")
			if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := Load(path); err == nil {
				t.Fatal("Load() unexpectedly accepted invalid configuration")
			}
		})
	}
}

func TestValidateRequiresSelectedActionsOnlyWithSelectedAllowedActions(t *testing.T) {
	t.Parallel()
	base := Config{
		SchemaVersion: CurrentSchemaVersion,
		Repository:    RepositoryConfig{Owner: "octo-org", Name: "example", Visibility: "public"},
	}

	missing := base
	missing.Actions = &ActionsConfig{AllowedActions: "selected"}
	if err := Validate(&missing); err == nil {
		t.Fatal("Validate() unexpectedly accepted allowedActions=selected without selectedActions")
	}

	misplaced := base
	misplaced.Actions = &ActionsConfig{
		AllowedActions:  "all",
		SelectedActions: &SelectedActionsConfig{GitHubOwnedAllowed: true},
	}
	if err := Validate(&misplaced); err == nil {
		t.Fatal("Validate() unexpectedly accepted selectedActions without allowedActions=selected")
	}

	valid := base
	valid.Actions = &ActionsConfig{
		AllowedActions: "selected",
		SelectedActions: &SelectedActionsConfig{
			GitHubOwnedAllowed: true,
			VerifiedAllowed:    true,
			PatternsAllowed:    []string{"actions/*"},
		},
	}
	if err := Validate(&valid); err != nil {
		t.Fatalf("Validate() error = %v, want a valid configuration accepted", err)
	}
}

func TestValidateRejectsShellCommands(t *testing.T) {
	t.Parallel()
	err := Validate(&Config{
		SchemaVersion: CurrentSchemaVersion,
		Repository:    RepositoryConfig{Owner: "octo-org", Name: "example", Visibility: "public"},
		Commands: []CommandConfig{{
			ID: "unsafe", Program: "pwsh", Timeout: "1m",
		}},
	})
	if err == nil {
		t.Fatal("Validate() unexpectedly accepted a shell command")
	}
}
