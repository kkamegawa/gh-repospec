package config

import (
	"os"
	"path/filepath"
	"strings"
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

func TestValidateLabelColor(t *testing.T) {
	t.Parallel()
	base := Config{
		SchemaVersion: CurrentSchemaVersion,
		Repository:    RepositoryConfig{Owner: "octo-org", Name: "example", Visibility: "public"},
	}

	invalid := map[string]string{
		"leading hash": "#d73a4a",
		"too short":    "d73a4",
		"not hex":      "gggggg",
	}
	for name, color := range invalid {
		t.Run(name, func(t *testing.T) {
			cfg := base
			cfg.Labels = []LabelConfig{{Name: "kind/bug", Color: color}}
			err := Validate(&cfg)
			if err == nil {
				t.Fatalf("Validate() unexpectedly accepted color %q", color)
			}
			if !strings.Contains(err.Error(), "kind/bug") {
				t.Fatalf("Validate() error = %v, want it to name the offending label", err)
			}
		})
	}

	upper := base
	upper.Labels = []LabelConfig{{Name: "kind/bug", Color: "D73A4A"}}
	if err := Validate(&upper); err != nil {
		t.Fatalf("Validate() error = %v, want uppercase hex accepted", err)
	}

	empty := base
	empty.Labels = []LabelConfig{{Color: "d73a4a"}}
	if err := Validate(&empty); err == nil {
		t.Fatal("Validate() unexpectedly accepted a label without name")
	}

	duplicate := base
	duplicate.Labels = []LabelConfig{{Name: "kind/bug", Color: "d73a4a"}, {Name: "kind/bug", Color: "425df5"}}
	if err := Validate(&duplicate); err == nil {
		t.Fatal("Validate() unexpectedly accepted duplicate label names")
	}

	// GitHub label names are case-insensitive, so "Bug" and "bug" would
	// collide at apply time even though they are distinct Go strings.
	caseInsensitiveDuplicate := base
	caseInsensitiveDuplicate.Labels = []LabelConfig{{Name: "Bug", Color: "d73a4a"}, {Name: "bug", Color: "425df5"}}
	if err := Validate(&caseInsensitiveDuplicate); err == nil {
		t.Fatal("Validate() unexpectedly accepted label names that differ only by case")
	}
}

func TestValidateMilestones(t *testing.T) {
	t.Parallel()
	base := Config{
		SchemaVersion: CurrentSchemaVersion,
		Repository:    RepositoryConfig{Owner: "octo-org", Name: "example", Visibility: "public"},
	}

	badState := base
	badState.Milestones = []MilestoneConfig{{Title: "v1.0", State: "archived"}}
	if err := Validate(&badState); err == nil {
		t.Fatal("Validate() unexpectedly accepted an invalid milestone state")
	} else if !strings.Contains(err.Error(), "v1.0") {
		t.Fatalf("Validate() error = %v, want it to name the offending milestone", err)
	}

	for name, dueOn := range map[string]string{
		"invalid month": "2026-13-01",
		"invalid day":   "2026-02-30",
	} {
		t.Run(name, func(t *testing.T) {
			cfg := base
			cfg.Milestones = []MilestoneConfig{{Title: "v1.0", DueOn: dueOn}}
			if err := Validate(&cfg); err == nil {
				t.Fatalf("Validate() unexpectedly accepted dueOn %q", dueOn)
			}
		})
	}

	valid := base
	valid.Milestones = []MilestoneConfig{{Title: "v1.0", State: "closed", DueOn: "2026-12-01"}}
	if err := Validate(&valid); err != nil {
		t.Fatalf("Validate() error = %v, want a valid milestone accepted", err)
	}

	noTitle := base
	noTitle.Milestones = []MilestoneConfig{{State: "open"}}
	if err := Validate(&noTitle); err == nil {
		t.Fatal("Validate() unexpectedly accepted a milestone without title")
	}

	duplicate := base
	duplicate.Milestones = []MilestoneConfig{{Title: "v1.0"}, {Title: "v1.0"}}
	if err := Validate(&duplicate); err == nil {
		t.Fatal("Validate() unexpectedly accepted duplicate milestone titles")
	}
}

func TestValidateRepositoryMergeCommitEnums(t *testing.T) {
	t.Parallel()
	base := Config{
		SchemaVersion: CurrentSchemaVersion,
		Repository:    RepositoryConfig{Owner: "octo-org", Name: "example", Visibility: "public"},
	}

	invalid := base
	invalid.Repository.MergeCommitTitle = "NOT_A_VALUE"
	if err := Validate(&invalid); err == nil {
		t.Fatal("Validate() unexpectedly accepted an invalid mergeCommitTitle")
	}

	valid := base
	valid.Repository.MergeCommitTitle = "PR_TITLE"
	valid.Repository.MergeCommitMessage = "PR_BODY"
	valid.Repository.SquashMergeCommitTitle = "COMMIT_OR_PR_TITLE"
	valid.Repository.SquashMergeCommitMessage = "COMMIT_MESSAGES"
	if err := Validate(&valid); err != nil {
		t.Fatalf("Validate() error = %v, want valid merge commit enums accepted", err)
	}
}

func TestValidateRulesetEnforcementAndBypassActors(t *testing.T) {
	t.Parallel()
	base := Config{
		SchemaVersion: CurrentSchemaVersion,
		Repository:    RepositoryConfig{Owner: "octo-org", Name: "example", Visibility: "public"},
	}

	evaluate := base
	evaluate.Rulesets = []RulesetConfig{{
		Name: "protect-main", Enforcement: "evaluate", Target: "branch", Include: []string{"~DEFAULT_BRANCH"},
		Rules: []RulesetRule{{Type: "deletion"}},
	}}
	if err := Validate(&evaluate); err != nil {
		t.Fatalf("Validate() error = %v, want evaluate enforcement accepted", err)
	}

	ambiguousActor := base
	ambiguousActor.Rulesets = []RulesetConfig{{
		Name: "protect-main", Enforcement: "active", Target: "branch", Include: []string{"~DEFAULT_BRANCH"},
		Rules: []RulesetRule{{Type: "deletion"}},
		BypassActors: []BypassActorConfig{
			{Role: "admin", BypassMode: "pull_request"},
			{Role: "admin", Team: "platform", BypassMode: "always"},
		},
	}}
	err := Validate(&ambiguousActor)
	if err == nil {
		t.Fatal("Validate() unexpectedly accepted a bypass actor with two selectors set")
	}
	if !strings.Contains(err.Error(), "bypassActors[1]") {
		t.Fatalf("Validate() error = %v, want it to name the offending entry index", err)
	}

	missingBypassMode := base
	missingBypassMode.Rulesets = []RulesetConfig{{
		Name: "protect-main", Enforcement: "active", Target: "branch", Include: []string{"~DEFAULT_BRANCH"},
		Rules:        []RulesetRule{{Type: "deletion"}},
		BypassActors: []BypassActorConfig{{Role: "admin"}},
	}}
	if err := Validate(&missingBypassMode); err == nil {
		t.Fatal("Validate() unexpectedly accepted a bypass actor without bypassMode")
	}

	validActor := base
	validActor.Rulesets = []RulesetConfig{{
		Name: "protect-main", Enforcement: "active", Target: "branch", Include: []string{"~DEFAULT_BRANCH"},
		Rules:        []RulesetRule{{Type: "deletion"}},
		BypassActors: []BypassActorConfig{{Role: "admin", BypassMode: "pull_request"}, {App: "dependabot", BypassMode: "always"}},
	}}
	if err := Validate(&validActor); err != nil {
		t.Fatalf("Validate() error = %v, want valid bypass actors accepted", err)
	}
}

// TestValidateBypassActorResolvedIDForm guards docs/design.md's requirement
// that team, app, and customRole accept the resolved "id:<number>" form so
// import output can be applied again unchanged, while role and orgAdmin
// reject it since their actor IDs are the fixed GitHub base role IDs.
func TestValidateBypassActorResolvedIDForm(t *testing.T) {
	t.Parallel()
	base := Config{
		SchemaVersion: CurrentSchemaVersion,
		Repository:    RepositoryConfig{Owner: "octo-org", Name: "example", Visibility: "public"},
	}

	resolved := base
	resolved.Rulesets = []RulesetConfig{{
		Name: "protect-main", Enforcement: "active", Target: "branch", Include: []string{"~DEFAULT_BRANCH"},
		Rules: []RulesetRule{{Type: "deletion"}},
		BypassActors: []BypassActorConfig{
			{Team: "id:42", BypassMode: "always"},
			{App: "id:7", BypassMode: "always"},
			{CustomRole: "id:19", BypassMode: "always"},
		},
	}}
	if err := Validate(&resolved); err != nil {
		t.Fatalf("Validate() error = %v, want the resolved id:<number> form accepted for team, app, and customRole", err)
	}

	for _, malformed := range []string{"id:", "id:abc", "id:-1", "id:0"} {
		invalid := base
		invalid.Rulesets = []RulesetConfig{{
			Name: "protect-main", Enforcement: "active", Target: "branch", Include: []string{"~DEFAULT_BRANCH"},
			Rules:        []RulesetRule{{Type: "deletion"}},
			BypassActors: []BypassActorConfig{{App: malformed, BypassMode: "always"}},
		}}
		err := Validate(&invalid)
		if err == nil {
			t.Fatalf("Validate() unexpectedly accepted malformed app selector %q", malformed)
		}
		if !strings.Contains(err.Error(), "bypassActors[0]") {
			t.Fatalf("Validate() error = %v, want it to name the offending entry index", err)
		}
	}

	roleWithID := base
	roleWithID.Rulesets = []RulesetConfig{{
		Name: "protect-main", Enforcement: "active", Target: "branch", Include: []string{"~DEFAULT_BRANCH"},
		Rules:        []RulesetRule{{Type: "deletion"}},
		BypassActors: []BypassActorConfig{{Role: "id:5", BypassMode: "always"}},
	}}
	if err := Validate(&roleWithID); err == nil {
		t.Fatal("Validate() unexpectedly accepted id:<number> for role: role IDs are fixed, not resolved")
	}
}

func TestValidateActionsForkPullRequestApproval(t *testing.T) {
	t.Parallel()
	base := Config{
		SchemaVersion: CurrentSchemaVersion,
		Repository:    RepositoryConfig{Owner: "octo-org", Name: "example", Visibility: "public"},
	}

	invalid := base
	invalid.Actions = &ActionsConfig{ForkPullRequestApproval: "everyone"}
	if err := Validate(&invalid); err == nil {
		t.Fatal("Validate() unexpectedly accepted an invalid forkPullRequestApproval")
	}

	valid := base
	valid.Actions = &ActionsConfig{ForkPullRequestApproval: "all_external_contributors"}
	if err := Validate(&valid); err != nil {
		t.Fatalf("Validate() error = %v, want a valid forkPullRequestApproval accepted", err)
	}
}
