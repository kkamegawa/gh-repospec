package planner

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/kkamegawa/gh-repospec/internal/config"
	"github.com/kkamegawa/gh-repospec/internal/githubapi"
)

func TestBuildReportsOnlyDeclaredDifferences(t *testing.T) {
	t.Parallel()
	service := &fakeService{
		repository: githubapi.RepositoryInfo{
			Description: "old description",
			Visibility:  "public",
			Topics:      []string{"old"},
		},
		actions: githubapi.ActionsPermissions{Enabled: false},
	}
	engine, err := New(service)
	if err != nil {
		t.Fatal(err)
	}
	enabled := true
	description := "new description"
	plan, err := engine.Build(context.Background(), &config.Config{
		SchemaVersion: config.CurrentSchemaVersion,
		Repository: config.RepositoryConfig{
			Owner:       "octo",
			Name:        "demo",
			Visibility:  "public",
			Description: &description,
			Topics:      []string{"new"},
		},
		Actions: &config.ActionsConfig{Enabled: &enabled},
	}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !plan.Exists || plan.HasBlocked() {
		t.Fatalf("unexpected plan: %#v", plan)
	}
	if !hasChange(plan, "repository.description", ChangeUpdate) || !hasChange(plan, "repository.topics", ChangeUpdate) || !hasChange(plan, "actions.permissions", ChangeUpdate) {
		t.Fatalf("declared differences were not planned: %#v", plan.Changes)
	}
}

// TestBuildOmitsDescriptionChangeWhenUnmanaged guards the round-trip hazard
// that motivated making repository.description a *string: import output that
// does not manage the description must not report a change against the
// repository it was read from, matching how homepage already behaves.
func TestBuildOmitsDescriptionChangeWhenUnmanaged(t *testing.T) {
	t.Parallel()
	service := &fakeService{
		repository: githubapi.RepositoryInfo{Description: "existing description", Visibility: "public"},
	}
	engine, err := New(service)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := engine.Build(context.Background(), &config.Config{
		SchemaVersion: config.CurrentSchemaVersion,
		Repository:    config.RepositoryConfig{Owner: "octo", Name: "demo", Visibility: "public"},
	}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range plan.Changes {
		if change.Resource == "repository.description" {
			t.Fatalf("unmanaged description reported a change: %#v", change)
		}
	}
}

// TestBuildReportsExplicitEmptyDescriptionAsClear guards the other half of the
// tri-state contract: an explicit empty string is a declared value, not an
// absence, and must still converge against a repository that has one set.
func TestBuildReportsExplicitEmptyDescriptionAsClear(t *testing.T) {
	t.Parallel()
	service := &fakeService{
		repository: githubapi.RepositoryInfo{Description: "existing description", Visibility: "public"},
	}
	engine, err := New(service)
	if err != nil {
		t.Fatal(err)
	}
	empty := ""
	plan, err := engine.Build(context.Background(), &config.Config{
		SchemaVersion: config.CurrentSchemaVersion,
		Repository:    config.RepositoryConfig{Owner: "octo", Name: "demo", Visibility: "public", Description: &empty},
	}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !hasChange(plan, "repository.description", ChangeUpdate) {
		t.Fatalf("explicit empty description did not report a clearing update: %#v", plan.Changes)
	}
}

func TestBuildReportsSelectedActionsChange(t *testing.T) {
	t.Parallel()
	service := &fakeService{repository: githubapi.RepositoryInfo{Visibility: "public"}}
	engine, err := New(service)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := engine.Build(context.Background(), &config.Config{
		SchemaVersion: config.CurrentSchemaVersion,
		Repository:    config.RepositoryConfig{Owner: "octo", Name: "demo", Visibility: "public"},
		Actions: &config.ActionsConfig{
			AllowedActions:  "selected",
			SelectedActions: &config.SelectedActionsConfig{GitHubOwnedAllowed: true, VerifiedAllowed: true},
		},
	}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !hasChange(plan, "actions.selectedActions", ChangeUpdate) {
		t.Fatalf("plan did not report the selected Actions allowlist: %#v", plan.Changes)
	}
}

// TestBuildActionsSettingsConvergeAgainstLiveState guards the same
// round-trip hazard for actions.selectedActions, actions.workflowPermissions,
// and actions.environment.<name>: each was previously reported as an update
// whenever declared, regardless of whether GitHub already matched.
func TestBuildActionsSettingsConvergeAgainstLiveState(t *testing.T) {
	t.Parallel()
	service := &fakeService{
		repository:                 githubapi.RepositoryInfo{Visibility: "public"},
		selectedActions:            githubapi.SelectedActions{GitHubOwnedAllowed: true, VerifiedAllowed: true, PatternsAllowed: []string{"actions/checkout@*"}},
		defaultWorkflowPermissions: githubapi.DefaultWorkflowPermissions{DefaultWorkflowPermissions: "read", CanApprovePullRequest: true},
		environments:               []githubapi.Environment{{Name: "production"}},
	}
	engine, err := New(service)
	if err != nil {
		t.Fatal(err)
	}
	canApprove := true
	plan, err := engine.Build(context.Background(), &config.Config{
		SchemaVersion: config.CurrentSchemaVersion,
		Repository:    config.RepositoryConfig{Owner: "octo", Name: "demo", Visibility: "public"},
		Actions: &config.ActionsConfig{
			AllowedActions:                 "selected",
			SelectedActions:                &config.SelectedActionsConfig{GitHubOwnedAllowed: true, VerifiedAllowed: true, PatternsAllowed: []string{"actions/checkout@*"}},
			DefaultWorkflowPermissions:     "read",
			CanApprovePullRequestWorkflows: &canApprove,
			Environments:                   []config.Environment{{Name: "production"}},
		},
	}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !hasChange(plan, "actions.selectedActions", ChangeNoop) {
		t.Fatalf("plan.Changes = %#v, want actions.selectedActions as noop when it already matches", plan.Changes)
	}
	if !hasChange(plan, "actions.workflowPermissions", ChangeNoop) {
		t.Fatalf("plan.Changes = %#v, want actions.workflowPermissions as noop when it already matches", plan.Changes)
	}
	if !hasChange(plan, "actions.environment.production", ChangeNoop) {
		t.Fatalf("plan.Changes = %#v, want actions.environment.production as noop when it already exists", plan.Changes)
	}
}

// TestBuildActionsEnvironmentReportsCreateWhenAbsent is the mirror case: an
// environment GitHub does not have yet is a create, not a noop or a blind
// update.
func TestBuildActionsEnvironmentReportsCreateWhenAbsent(t *testing.T) {
	t.Parallel()
	service := &fakeService{repository: githubapi.RepositoryInfo{Visibility: "public"}}
	engine, err := New(service)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := engine.Build(context.Background(), &config.Config{
		SchemaVersion: config.CurrentSchemaVersion,
		Repository:    config.RepositoryConfig{Owner: "octo", Name: "demo", Visibility: "public"},
		Actions:       &config.ActionsConfig{Environments: []config.Environment{{Name: "staging"}}},
	}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !hasChange(plan, "actions.environment.staging", ChangeCreate) {
		t.Fatalf("plan.Changes = %#v, want actions.environment.staging as a create", plan.Changes)
	}
}

func TestApplyActionsConvergesVariablesAndEnvironmentsOnce(t *testing.T) {
	t.Parallel()
	service := &fakeService{repository: githubapi.RepositoryInfo{Visibility: "public"}}
	engine, err := New(service)
	if err != nil {
		t.Fatal(err)
	}
	_, err = engine.Apply(context.Background(), &config.Config{
		SchemaVersion: config.CurrentSchemaVersion,
		Repository:    config.RepositoryConfig{Owner: "octo", Name: "demo", Visibility: "public"},
		Actions: &config.ActionsConfig{
			Variables:    []config.VariableRef{{Name: "STAGE", Value: "prod"}},
			Environments: []config.Environment{{Name: "production"}},
		},
	}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	// Apply() recalculates the plan (one read-only ListActionVariables call for
	// the report) before converging it (one more call to apply changes); a
	// third call would mean applyActions still double-converges internally.
	if service.listActionVariablesCalls != 2 {
		t.Fatalf("ListActionVariables calls = %d, want 2 (one plan read, one apply convergence)", service.listActionVariablesCalls)
	}
	if service.createOrUpdateEnvironmentCalls != 1 {
		t.Fatalf("CreateOrUpdateEnvironment calls = %d, want 1 (environments must converge exactly once)", service.createOrUpdateEnvironmentCalls)
	}
}

func TestBuildRequiresVisibilityApproval(t *testing.T) {
	t.Parallel()
	service := &fakeService{repository: githubapi.RepositoryInfo{Visibility: "public"}}
	engine, err := New(service)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := engine.Build(context.Background(), &config.Config{
		SchemaVersion: config.CurrentSchemaVersion,
		Repository:    config.RepositoryConfig{Owner: "octo", Name: "demo", Visibility: "private"},
	}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !plan.HasBlocked() || !hasChange(plan, "repository.visibility", ChangeBlocked) {
		t.Fatalf("visibility plan = %#v", plan.Changes)
	}
}

func TestApplyConvergesSupportedSettings(t *testing.T) {
	t.Parallel()
	service := &fakeService{repository: githubapi.RepositoryInfo{Visibility: "public"}}
	engine, err := New(service)
	if err != nil {
		t.Fatal(err)
	}
	description := "configured"
	_, err = engine.Apply(context.Background(), &config.Config{
		SchemaVersion: config.CurrentSchemaVersion,
		Repository:    config.RepositoryConfig{Owner: "octo", Name: "demo", Visibility: "public", Description: &description},
	}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if service.updateRepositoryCalls != 1 {
		t.Fatalf("repository update calls = %d, want 1", service.updateRepositoryCalls)
	}
}

// TestApplyNeverSendsDescriptionWhenUnmanaged guards the apply-side half of
// the tri-state contract: a configuration that does not manage the
// description must not issue a repository update at all when nothing else
// changed, and must never send an empty description it never declared.
func TestApplyNeverSendsDescriptionWhenUnmanaged(t *testing.T) {
	t.Parallel()
	service := &fakeService{repository: githubapi.RepositoryInfo{Description: "existing description", Visibility: "public"}}
	engine, err := New(service)
	if err != nil {
		t.Fatal(err)
	}
	_, err = engine.Apply(context.Background(), &config.Config{
		SchemaVersion: config.CurrentSchemaVersion,
		Repository:    config.RepositoryConfig{Owner: "octo", Name: "demo", Visibility: "public"},
	}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if service.updateRepositoryCalls != 0 {
		t.Fatalf("repository update calls = %d, want 0 for an unmanaged description", service.updateRepositoryCalls)
	}
	if service.repository.Description != "existing description" {
		t.Fatalf("description = %q, want the original value left untouched", service.repository.Description)
	}
}

// TestApplyClearsExplicitEmptyDescription guards the other half: an explicit
// empty string is a declared value and must still reach GitHub as a clear.
func TestApplyClearsExplicitEmptyDescription(t *testing.T) {
	t.Parallel()
	service := &fakeService{repository: githubapi.RepositoryInfo{Description: "existing description", Visibility: "public"}}
	engine, err := New(service)
	if err != nil {
		t.Fatal(err)
	}
	empty := ""
	_, err = engine.Apply(context.Background(), &config.Config{
		SchemaVersion: config.CurrentSchemaVersion,
		Repository:    config.RepositoryConfig{Owner: "octo", Name: "demo", Visibility: "public", Description: &empty},
	}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if service.updateRepositoryCalls != 1 {
		t.Fatalf("repository update calls = %d, want 1 for an explicit clear", service.updateRepositoryCalls)
	}
	if service.repository.Description != "" {
		t.Fatalf("description = %q, want cleared", service.repository.Description)
	}
}

// TestStagedFilesDetectsExecutableBit guards against reading the executable
// bit with fs.DirEntry.Type().Perm(), which always yields zero because
// DirEntry.Type() carries only file-type bits, never permission bits. That
// bug silently commits every seeded script as mode 100644, dropping its
// executable bit. Not applicable on Windows, which does not have a POSIX
// executable permission bit to preserve.
func TestStagedFilesDetectsExecutableBit(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("POSIX executable permission bits are not meaningful on Windows")
	}
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "run.sh"), []byte("#!/bin/sh\n"), 0o700); err != nil { //nolint:gosec // the executable bit is the behavior under test; root is a t.TempDir path
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	files, err := stagedFiles(root)
	if err != nil {
		t.Fatal(err)
	}
	byPath := make(map[string]string, len(files))
	for _, file := range files {
		byPath[file.Path] = file.Mode
	}
	if byPath["run.sh"] != "100755" {
		t.Fatalf("run.sh mode = %q, want 100755", byPath["run.sh"])
	}
	if byPath["README.md"] != "100644" {
		t.Fatalf("README.md mode = %q, want 100644", byPath["README.md"])
	}
}

// TestLabelsAreAdditive guards the additive contract from docs/design.md:
// a label present on the repository but absent from configuration must never
// be created, updated, or even mentioned in the plan.
func TestLabelsAreAdditive(t *testing.T) {
	t.Parallel()
	service := &fakeService{
		repository: githubapi.RepositoryInfo{Visibility: "public"},
		labels: []githubapi.Label{
			{Name: "kind/bug", Color: "d73a4a", Description: "A defect in existing behavior"},
			{Name: "stale", Color: "cccccc", Description: "undeclared"},
		},
	}
	engine, err := New(service)
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{
		SchemaVersion: config.CurrentSchemaVersion,
		Repository:    config.RepositoryConfig{Owner: "octo", Name: "demo", Visibility: "public"},
		Labels: []config.LabelConfig{
			{Name: "kind/bug", Color: "d73a4a", Description: "A defect in existing behavior"},
			{Name: "kind/feature", Color: "425df5", Description: "A request for new behavior"},
		},
	}

	plan, err := engine.Build(context.Background(), cfg, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !hasChange(plan, "label.kind/bug", ChangeNoop) {
		t.Fatalf("plan did not report the matching label as noop: %#v", plan.Changes)
	}
	if !hasChange(plan, "label.kind/feature", ChangeCreate) {
		t.Fatalf("plan did not report the missing label as create: %#v", plan.Changes)
	}
	for _, change := range plan.Changes {
		if strings.Contains(change.Resource, "stale") {
			t.Fatalf("plan mentioned an undeclared label: %#v", change)
		}
	}

	if _, err := engine.Apply(context.Background(), cfg, Options{}); err != nil {
		t.Fatal(err)
	}
	if service.createLabelCalls != 1 || service.updateLabelCalls != 0 {
		t.Fatalf("createLabelCalls = %d, updateLabelCalls = %d, want 1 and 0", service.createLabelCalls, service.updateLabelCalls)
	}
	byName := make(map[string]githubapi.Label, len(service.labels))
	for _, label := range service.labels {
		byName[label.Name] = label
	}
	if stale := byName["stale"]; stale.Color != "cccccc" || stale.Description != "undeclared" {
		t.Fatalf("apply modified the undeclared label: %#v", stale)
	}
	if _, ok := byName["kind/feature"]; !ok {
		t.Fatal("apply did not create the declared label")
	}
}

// TestMilestoneStateTransitionAndDueOnConvergence guards two behaviors: a
// state change (open -> closed) must trigger an update, and GitHub's RFC 3339
// due_on timestamp must not be mistaken for a difference against a plain
// YYYY-MM-DD dueOn when the dates match.
func TestMilestoneStateTransitionAndDueOnConvergence(t *testing.T) {
	t.Parallel()
	service := &fakeService{
		repository: githubapi.RepositoryInfo{Visibility: "public"},
		milestones: []githubapi.Milestone{
			{Number: 1, Title: "v1.0", Description: "First stable release", State: "open", DueOn: "2026-12-01T00:00:00Z"},
		},
	}
	engine, err := New(service)
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{
		SchemaVersion: config.CurrentSchemaVersion,
		Repository:    config.RepositoryConfig{Owner: "octo", Name: "demo", Visibility: "public"},
		Milestones: []config.MilestoneConfig{
			{Title: "v1.0", Description: "First stable release", State: "closed", DueOn: "2026-12-01"},
		},
	}

	plan, err := engine.Build(context.Background(), cfg, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !hasChange(plan, "milestone.v1.0", ChangeUpdate) {
		t.Fatalf("plan did not report the state transition as update: %#v", plan.Changes)
	}

	if _, err := engine.Apply(context.Background(), cfg, Options{}); err != nil {
		t.Fatal(err)
	}
	if service.updateMilestoneCalls != 1 || service.createMilestoneCalls != 0 {
		t.Fatalf("updateMilestoneCalls = %d, createMilestoneCalls = %d, want 1 and 0", service.updateMilestoneCalls, service.createMilestoneCalls)
	}
	if service.milestones[0].State != "closed" {
		t.Fatalf("milestone state = %q, want closed", service.milestones[0].State)
	}

	// A second apply against the now-converged state must be a pure no-op:
	// the RFC 3339 due_on GitHub returns must not be misread as a difference
	// against the plain YYYY-MM-DD dueOn declared in configuration.
	plan, err = engine.Build(context.Background(), cfg, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !hasChange(plan, "milestone.v1.0", ChangeNoop) {
		t.Fatalf("converged milestone was not reported as noop: %#v", plan.Changes)
	}
	if _, err := engine.Apply(context.Background(), cfg, Options{}); err != nil {
		t.Fatal(err)
	}
	if service.updateMilestoneCalls != 1 {
		t.Fatalf("updateMilestoneCalls after convergence = %d, want still 1", service.updateMilestoneCalls)
	}
}

// TestApplyConvergesRepositoryFeatureAndMergeCommitSettings guards the wiring
// from config through the planner to the GitHub update request for the
// gh-infra repository fields: homepage, feature toggles, and merge commit
// title/message formats.
func TestApplyConvergesRepositoryFeatureAndMergeCommitSettings(t *testing.T) {
	t.Parallel()
	service := &fakeService{repository: githubapi.RepositoryInfo{Visibility: "public", HasWiki: true}}
	engine, err := New(service)
	if err != nil {
		t.Fatal(err)
	}
	homepage := "https://example.com/demo"
	issues := true
	wiki := false
	cfg := &config.Config{
		SchemaVersion: config.CurrentSchemaVersion,
		Repository: config.RepositoryConfig{
			Owner: "octo", Name: "demo", Visibility: "public",
			Homepage:                 &homepage,
			Features:                 &config.RepositoryFeatures{Issues: &issues, Wiki: &wiki},
			MergeCommitTitle:         "PR_TITLE",
			SquashMergeCommitMessage: "COMMIT_MESSAGES",
		},
	}

	plan, err := engine.Build(context.Background(), cfg, Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, resource := range []string{"repository.homepage", "repository.features.issues", "repository.features.wiki", "repository.mergeCommitTitle", "repository.squashMergeCommitMessage"} {
		if !hasChange(plan, resource, ChangeUpdate) {
			t.Fatalf("plan did not report %s as update: %#v", resource, plan.Changes)
		}
	}

	if _, err := engine.Apply(context.Background(), cfg, Options{}); err != nil {
		t.Fatal(err)
	}
	if service.repository.Homepage != homepage {
		t.Fatalf("repository homepage = %q, want %q", service.repository.Homepage, homepage)
	}
	if !service.repository.HasIssues || service.repository.HasWiki {
		t.Fatalf("repository features = issues:%t wiki:%t, want issues:true wiki:false", service.repository.HasIssues, service.repository.HasWiki)
	}
	if service.repository.MergeCommitTitle != "PR_TITLE" || service.repository.SquashMergeCommitMessage != "COMMIT_MESSAGES" {
		t.Fatalf("merge commit settings = %q/%q, want PR_TITLE/COMMIT_MESSAGES", service.repository.MergeCommitTitle, service.repository.SquashMergeCommitMessage)
	}
}

// TestBuildPlansBypassActorsAndForkPullRequestApproval guards visibility for
// issue #10: both fields must be actually planned for write, never reported
// as a manual no-op, and must never block the parts of the plan that are
// otherwise supported.
func TestBuildPlansBypassActorsAndForkPullRequestApproval(t *testing.T) {
	t.Parallel()
	enabled := true
	service := &fakeService{repository: githubapi.RepositoryInfo{Visibility: "public"}}
	engine, err := New(service)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := engine.Build(context.Background(), &config.Config{
		SchemaVersion: config.CurrentSchemaVersion,
		Repository:    config.RepositoryConfig{Owner: "octo", Name: "demo", Visibility: "public"},
		Actions:       &config.ActionsConfig{Enabled: &enabled, ForkPullRequestApproval: "all_external_contributors"},
		Rulesets: []config.RulesetConfig{{
			Name: "protect-main", Enforcement: "active", Target: "branch", Include: []string{"~DEFAULT_BRANCH"},
			Rules:        []config.RulesetRule{{Type: "deletion"}},
			BypassActors: []config.BypassActorConfig{{Role: "admin", BypassMode: "pull_request"}},
		}},
	}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !hasChange(plan, "actions.forkPullRequestApproval", ChangeUpdate) {
		t.Fatalf("plan did not plan forkPullRequestApproval for write: %#v", plan.Changes)
	}
	// The ruleset itself does not exist yet on the fake, so its bypassActors
	// entry is created alongside it rather than updated.
	if !hasChange(plan, "ruleset.protect-main.bypassActors", ChangeCreate) {
		t.Fatalf("plan did not plan bypassActors for write: %#v", plan.Changes)
	}
	if plan.HasBlocked() {
		t.Fatalf("resolvable fields unexpectedly blocked the plan: %#v", plan.Changes)
	}
}

// TestBuildBlocksUnresolvableBypassActor guards docs/design.md's requirement
// that an actor slug which cannot be resolved blocks the plan instead of
// silently dropping the bypass or failing midway through apply.
func TestBuildBlocksUnresolvableBypassActor(t *testing.T) {
	t.Parallel()
	service := &fakeService{repository: githubapi.RepositoryInfo{Owner: githubapi.Owner{Type: "Organization"}, Visibility: "public"}}
	engine, err := New(service)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := engine.Build(context.Background(), &config.Config{
		SchemaVersion: config.CurrentSchemaVersion,
		Repository:    config.RepositoryConfig{Owner: "octo-org", Name: "demo", Visibility: "public"},
		Rulesets: []config.RulesetConfig{{
			Name: "protect-main", Enforcement: "active", Target: "branch", Include: []string{"~DEFAULT_BRANCH"},
			Rules:        []config.RulesetRule{{Type: "deletion"}},
			BypassActors: []config.BypassActorConfig{{Team: "unknown-team", BypassMode: "always"}},
		}},
	}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !plan.HasBlocked() {
		t.Fatalf("unresolvable bypass actor did not block the plan: %#v", plan.Changes)
	}
	if !hasChange(plan, "ruleset.protect-main.bypassActors[0]", ChangeBlocked) {
		t.Fatalf("plan did not name the unresolvable entry by index: %#v", plan.Changes)
	}
}

// TestBuildBlocksOrganizationActorOnPersonalRepository guards the personal
// repository short-circuit: GitHub's organization endpoints 404 for a User
// account, so an org-only actor type is reported with its own message
// instead of a confusing "not found" lookup failure. role and app are
// unaffected, since both exist on personal repositories too.
func TestBuildBlocksOrganizationActorOnPersonalRepository(t *testing.T) {
	t.Parallel()
	service := &fakeService{
		repository: githubapi.RepositoryInfo{Owner: githubapi.Owner{Type: "User"}, Visibility: "public"},
		apps:       map[string]githubapi.App{"renovate": {ID: 7, Slug: "renovate"}},
	}
	engine, err := New(service)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := engine.Build(context.Background(), &config.Config{
		SchemaVersion: config.CurrentSchemaVersion,
		Repository:    config.RepositoryConfig{Owner: "octocat", Name: "demo", Visibility: "public"},
		Rulesets: []config.RulesetConfig{{
			Name: "protect-main", Enforcement: "active", Target: "branch", Include: []string{"~DEFAULT_BRANCH"},
			Rules: []config.RulesetRule{{Type: "deletion"}},
			BypassActors: []config.BypassActorConfig{
				{OrgAdmin: true, BypassMode: "always"},
				{App: "renovate", BypassMode: "always"},
			},
		}},
	}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !hasChange(plan, "ruleset.protect-main.bypassActors[0]", ChangeBlocked) {
		t.Fatalf("orgAdmin was not blocked on a personal repository: %#v", plan.Changes)
	}
	if service.getTeamCalls != 0 {
		t.Fatalf("personal repository short-circuit still queried the Teams API: %d calls", service.getTeamCalls)
	}
	if hasChange(plan, "ruleset.protect-main.bypassActors[1]", ChangeBlocked) {
		t.Fatalf("app bypass actor was unexpectedly blocked on a personal repository: %#v", plan.Changes)
	}
}

// TestBuildAcceptsResolvedBypassActorIDsWithoutLookups guards the
// "id:<number>" form docs/design.md requires so import output applies again
// unchanged, without spending a lookup on an identity already resolved.
func TestBuildAcceptsResolvedBypassActorIDsWithoutLookups(t *testing.T) {
	t.Parallel()
	service := &fakeService{repository: githubapi.RepositoryInfo{Owner: githubapi.Owner{Type: "Organization"}, Visibility: "public"}}
	engine, err := New(service)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := engine.Build(context.Background(), &config.Config{
		SchemaVersion: config.CurrentSchemaVersion,
		Repository:    config.RepositoryConfig{Owner: "octo-org", Name: "demo", Visibility: "public"},
		Rulesets: []config.RulesetConfig{{
			Name: "protect-main", Enforcement: "active", Target: "branch", Include: []string{"~DEFAULT_BRANCH"},
			Rules:        []config.RulesetRule{{Type: "deletion"}},
			BypassActors: []config.BypassActorConfig{{Team: "id:42", BypassMode: "always"}},
		}},
	}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if plan.HasBlocked() {
		t.Fatalf("resolved id:<number> form unexpectedly blocked the plan: %#v", plan.Changes)
	}
	if service.getTeamCalls != 0 {
		t.Fatalf("id:<number> form should not query the Teams API: %d calls", service.getTeamCalls)
	}
}

// TestBuildCachesBypassActorLookupsAcrossRulesets guards the resolver's
// cache: naming the same team from two rulesets in one plan must cost one
// Teams API request, not two.
func TestBuildCachesBypassActorLookupsAcrossRulesets(t *testing.T) {
	t.Parallel()
	service := &fakeService{
		repository: githubapi.RepositoryInfo{Owner: githubapi.Owner{Type: "Organization"}, Visibility: "public"},
		teams:      map[string]githubapi.Team{"octo-org/platform": {ID: 9, Slug: "platform"}},
	}
	engine, err := New(service)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := engine.Build(context.Background(), &config.Config{
		SchemaVersion: config.CurrentSchemaVersion,
		Repository:    config.RepositoryConfig{Owner: "octo-org", Name: "demo", Visibility: "public"},
		Rulesets: []config.RulesetConfig{
			{
				Name: "protect-main", Enforcement: "active", Target: "branch", Include: []string{"~DEFAULT_BRANCH"},
				Rules:        []config.RulesetRule{{Type: "deletion"}},
				BypassActors: []config.BypassActorConfig{{Team: "platform", BypassMode: "always"}},
			},
			{
				Name: "protect-release", Enforcement: "active", Target: "branch", Include: []string{"release/*"},
				Rules:        []config.RulesetRule{{Type: "deletion"}},
				BypassActors: []config.BypassActorConfig{{Team: "platform", BypassMode: "always"}},
			},
		},
	}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if plan.HasBlocked() {
		t.Fatalf("known team unexpectedly blocked the plan: %#v", plan.Changes)
	}
	if service.getTeamCalls != 1 {
		t.Fatalf("getTeamCalls = %d, want 1 (cached across rulesets)", service.getTeamCalls)
	}
}

// TestBuildSkipsPersonalRepositoryCheckWithoutBypassActors guards
// newRulesetBypassResolver: a configuration that declares rulesets but no
// ruleset declares a bypass actor must never resolve the owner, since
// resolution is never used. This matters on the create path, where the
// check would otherwise cost a GetAuthenticatedUser call for every
// configuration with rulesets, regardless of whether any of them declare a
// bypass actor.
func TestBuildSkipsPersonalRepositoryCheckWithoutBypassActors(t *testing.T) {
	t.Parallel()
	service := &fakeService{getRepositoryErr: &githubapi.APIError{StatusCode: 404}}
	engine, err := New(service)
	if err != nil {
		t.Fatal(err)
	}
	_, err = engine.Build(context.Background(), &config.Config{
		SchemaVersion: config.CurrentSchemaVersion,
		Repository:    config.RepositoryConfig{Owner: "octo", Name: "demo", Visibility: "public", Create: true},
		Rulesets: []config.RulesetConfig{{
			Name: "protect-main", Enforcement: "active", Target: "branch", Include: []string{"~DEFAULT_BRANCH"},
			Rules: []config.RulesetRule{{Type: "deletion"}},
		}},
	}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if service.getAuthenticatedUserCalls != 0 {
		t.Fatalf("getAuthenticatedUserCalls = %d, want 0: a ruleset without bypass actors must not trigger the personal-repository check", service.getAuthenticatedUserCalls)
	}
}

// TestBuildBlocksForkPullRequestApprovalOnPrivateRepository guards a GitHub
// constraint not stated in docs/design.md until this issue: the fork PR
// contributor approval endpoint rejects private repositories outright
// (HTTP 422), so that combination must never reach apply. It also guards
// against reading current visibility to decide this: a repository that is
// currently private but whose configuration is changing it to public must
// plan an update, not a live GET that would still 422 against the
// not-yet-changed repository.
func TestBuildBlocksForkPullRequestApprovalOnPrivateRepository(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name              string
		liveVisibility    string
		desiredVisibility string
		wantState         ChangeState
	}{
		{name: "private is blocked", liveVisibility: "private", desiredVisibility: "private", wantState: ChangeBlocked},
		{name: "public is planned", liveVisibility: "public", desiredVisibility: "public", wantState: ChangeUpdate},
		{name: "becoming public is planned, not read from the still-private repository", liveVisibility: "private", desiredVisibility: "public", wantState: ChangeUpdate},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			service := &fakeService{
				repository: githubapi.RepositoryInfo{Visibility: tc.liveVisibility},
				// Simulates the 422 GitHub still returns for a repository
				// that has not been made public yet. If Build ever reads
				// this, the test below fails instead of merely blocking.
				forkPullRequestApprovalErr: &githubapi.APIError{StatusCode: 422},
			}
			engine, err := New(service)
			if err != nil {
				t.Fatal(err)
			}
			plan, err := engine.Build(context.Background(), &config.Config{
				SchemaVersion: config.CurrentSchemaVersion,
				Repository:    config.RepositoryConfig{Owner: "octo", Name: "demo", Visibility: tc.desiredVisibility, AllowVisibilityChange: true},
				Actions:       &config.ActionsConfig{ForkPullRequestApproval: "all_external_contributors"},
			}, Options{})
			if err != nil {
				t.Fatal(err)
			}
			if !hasChange(plan, "actions.forkPullRequestApproval", tc.wantState) {
				t.Fatalf("live=%q desired=%q: plan.Changes = %#v, want actions.forkPullRequestApproval as %s", tc.liveVisibility, tc.desiredVisibility, plan.Changes, tc.wantState)
			}
		})
	}
}

// TestBuildSecurityFeaturesConvergeAgainstLiveState guards the round-trip
// hazard every security feature shared until now: reporting an update
// whenever a feature is declared, never comparing it against the value
// GitHub already has. An imported document that already matches the
// repository it was read from must plan no changes for these fields.
func TestBuildSecurityFeaturesConvergeAgainstLiveState(t *testing.T) {
	t.Parallel()
	service := &fakeService{
		repository: githubapi.RepositoryInfo{
			Visibility: "public",
			SecurityAndAnalysis: &githubapi.SecurityAndAnalysisStatus{
				SecretScanning:            &githubapi.SecurityFeature{Status: "enabled"},
				SecretScanningPushProtect: &githubapi.SecurityFeature{Status: "disabled"},
			},
		},
		vulnerabilityAlertsEnabled:    true,
		automatedSecurityFixesEnabled: true,
		codeQLDefaultSetup:            githubapi.CodeQLDefaultSetupStatus{State: "configured"},
		privateVulnerabilityReporting: true,
	}
	engine, err := New(service)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := engine.Build(context.Background(), &config.Config{
		SchemaVersion: config.CurrentSchemaVersion,
		Repository:    config.RepositoryConfig{Owner: "octo", Name: "demo", Visibility: "public"},
		Security: &config.SecurityConfig{
			SecretScanning:                &config.FeatureSetting{Enabled: true},
			PushProtection:                &config.FeatureSetting{Enabled: false},
			DependabotAlerts:              &config.FeatureSetting{Enabled: true},
			DependabotSecurityUpdates:     &config.FeatureSetting{Enabled: true},
			CodeQLDefaultSetup:            &config.FeatureSetting{Enabled: true},
			PrivateVulnerabilityReporting: &config.FeatureSetting{Enabled: true},
		},
	}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, resource := range []string{
		"security.secretScanning", "security.pushProtection", "security.dependabotAlerts",
		"security.dependabotSecurityUpdates", "security.codeqlDefaultSetup", "security.privateVulnerabilityReporting",
	} {
		if !hasChange(plan, resource, ChangeNoop) {
			t.Fatalf("plan.Changes = %#v, want %s as noop when it already matches the live value", plan.Changes, resource)
		}
	}
	if plan.HasChanges() {
		t.Fatalf("plan.HasChanges() = true, want false when every declared security feature already matches: %#v", plan.Changes)
	}
}

// TestBuildSecurityFeaturesReportUpdateWhenLiveValueDiffers is the mirror of
// the convergence test: a feature that genuinely differs from the live
// value must still be reported as an update, not silently treated as a noop.
func TestBuildSecurityFeaturesReportUpdateWhenLiveValueDiffers(t *testing.T) {
	t.Parallel()
	service := &fakeService{
		repository: githubapi.RepositoryInfo{
			Visibility: "public",
			SecurityAndAnalysis: &githubapi.SecurityAndAnalysisStatus{
				SecretScanning: &githubapi.SecurityFeature{Status: "disabled"},
			},
		},
	}
	engine, err := New(service)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := engine.Build(context.Background(), &config.Config{
		SchemaVersion: config.CurrentSchemaVersion,
		Repository:    config.RepositoryConfig{Owner: "octo", Name: "demo", Visibility: "public"},
		Security:      &config.SecurityConfig{SecretScanning: &config.FeatureSetting{Enabled: true}},
	}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !hasChange(plan, "security.secretScanning", ChangeUpdate) {
		t.Fatalf("plan.Changes = %#v, want security.secretScanning as an update", plan.Changes)
	}
}

// TestBuildDependabotAlertsComparesAgainstLiveValueNotDeclaredValue guards
// against a regression where security.dependabotAlerts compared the declared
// value to itself instead of to GitHub's live state, which always reported a
// noop whenever alerts were declared at all, even when the repository's
// actual setting disagreed.
func TestBuildDependabotAlertsComparesAgainstLiveValueNotDeclaredValue(t *testing.T) {
	t.Parallel()
	service := &fakeService{
		repository:                 githubapi.RepositoryInfo{Visibility: "public"},
		vulnerabilityAlertsEnabled: false,
	}
	engine, err := New(service)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := engine.Build(context.Background(), &config.Config{
		SchemaVersion: config.CurrentSchemaVersion,
		Repository:    config.RepositoryConfig{Owner: "octo", Name: "demo", Visibility: "public"},
		Security:      &config.SecurityConfig{DependabotAlerts: &config.FeatureSetting{Enabled: true}},
	}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !hasChange(plan, "security.dependabotAlerts", ChangeUpdate) {
		t.Fatalf("plan.Changes = %#v, want security.dependabotAlerts as an update when the live value (false) disagrees with the declared value (true)", plan.Changes)
	}
}

// TestBuildDependabotDependency guards docs/design.md's three-state
// dependency: security.dependabotSecurityUpdates requires Dependabot alerts
// to be effectively enabled, evaluated against configuration first and the
// live repository state as the fallback.
func TestBuildDependabotDependency(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name        string
		alerts      *config.FeatureSetting
		liveEnabled bool
		wantState   ChangeState
	}{
		{name: "alerts declared true", alerts: &config.FeatureSetting{Enabled: true}, wantState: ChangeUpdate},
		{name: "alerts omitted, live enabled", alerts: nil, liveEnabled: true, wantState: ChangeUpdate},
		{name: "alerts omitted, live disabled", alerts: nil, liveEnabled: false, wantState: ChangeBlocked},
		{name: "alerts declared false", alerts: &config.FeatureSetting{Enabled: false}, wantState: ChangeBlocked},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			service := &fakeService{
				repository:                 githubapi.RepositoryInfo{Visibility: "public"},
				vulnerabilityAlertsEnabled: tc.liveEnabled,
			}
			engine, err := New(service)
			if err != nil {
				t.Fatal(err)
			}
			plan, err := engine.Build(context.Background(), &config.Config{
				SchemaVersion: config.CurrentSchemaVersion,
				Repository:    config.RepositoryConfig{Owner: "octo", Name: "demo", Visibility: "public"},
				Security: &config.SecurityConfig{
					DependabotAlerts:          tc.alerts,
					DependabotSecurityUpdates: &config.FeatureSetting{Enabled: true},
				},
			}, Options{})
			if err != nil {
				t.Fatal(err)
			}
			if !hasChange(plan, "security.dependabotSecurityUpdates", tc.wantState) {
				t.Fatalf("plan.Changes = %#v, want security.dependabotSecurityUpdates as %s", plan.Changes, tc.wantState)
			}
		})
	}
}

// TestBuildDependabotSecurityUpdatesDegradesToManualWhenOptional guards the
// existing optional escape hatch: a blocked dependency must still degrade to
// a manual change, not stop the run, when the declaration opts in.
func TestBuildDependabotSecurityUpdatesDegradesToManualWhenOptional(t *testing.T) {
	t.Parallel()
	service := &fakeService{repository: githubapi.RepositoryInfo{Visibility: "public"}}
	engine, err := New(service)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := engine.Build(context.Background(), &config.Config{
		SchemaVersion: config.CurrentSchemaVersion,
		Repository:    config.RepositoryConfig{Owner: "octo", Name: "demo", Visibility: "public"},
		Security: &config.SecurityConfig{
			DependabotAlerts:          &config.FeatureSetting{Enabled: false},
			DependabotSecurityUpdates: &config.FeatureSetting{Enabled: true, Optional: true},
		},
	}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !hasChange(plan, "security.dependabotSecurityUpdates", ChangeManual) {
		t.Fatalf("optional dependency violation did not degrade to manual: %#v", plan.Changes)
	}
	if plan.HasBlocked() {
		t.Fatalf("optional dependency violation unexpectedly blocked the plan: %#v", plan.Changes)
	}
}

// TestBuildBlocksDependabotSecurityUpdatesOnNewRepository guards the create
// path: the live repository state cannot be consulted before the repository
// exists, so an omitted dependabotAlerts declaration is treated as
// unsatisfied rather than assumed enabled.
func TestBuildBlocksDependabotSecurityUpdatesOnNewRepository(t *testing.T) {
	t.Parallel()
	service := &fakeService{getRepositoryErr: &githubapi.APIError{StatusCode: 404}}
	engine, err := New(service)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := engine.Build(context.Background(), &config.Config{
		SchemaVersion: config.CurrentSchemaVersion,
		Repository:    config.RepositoryConfig{Owner: "octo", Name: "demo", Visibility: "public", Create: true},
		Security: &config.SecurityConfig{
			DependabotSecurityUpdates: &config.FeatureSetting{Enabled: true},
		},
	}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !hasChange(plan, "security.dependabotSecurityUpdates", ChangeBlocked) {
		t.Fatalf("plan.Changes = %#v, want security.dependabotSecurityUpdates blocked on the create path", plan.Changes)
	}
}

// TestBuildAIPullRequestScanConvergesAgainstLiveState guards convergence for
// security.aiPullRequestScan: a declaration that already matches the live
// AI Scan setting plans a noop, and a failed read (GitHub answers 403 when
// Advanced Security is not enabled) is reported as an update rather than
// guessed to be a noop.
func TestBuildAIPullRequestScanConvergesAgainstLiveState(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		declared  bool
		live      string
		liveErr   error
		wantState ChangeState
	}{
		{name: "enabled matches live", declared: true, live: "enabled", wantState: ChangeNoop},
		{name: "disabled matches live", declared: false, live: "disabled", wantState: ChangeNoop},
		{name: "enabled differs from live", declared: true, live: "disabled", wantState: ChangeUpdate},
		{name: "disabled differs from live", declared: false, live: "enabled", wantState: ChangeUpdate},
		{name: "live read fails", declared: true, liveErr: &githubapi.APIError{StatusCode: 403}, wantState: ChangeUpdate},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			service := &fakeService{
				repository:         githubapi.RepositoryInfo{Visibility: "public"},
				codeQLDefaultSetup: githubapi.CodeQLDefaultSetupStatus{State: "configured"},
				aiScan:             githubapi.AIScanSetting{PRScan: tc.live},
				aiScanErr:          tc.liveErr,
			}
			engine, err := New(service)
			if err != nil {
				t.Fatal(err)
			}
			plan, err := engine.Build(context.Background(), &config.Config{
				SchemaVersion: config.CurrentSchemaVersion,
				Repository:    config.RepositoryConfig{Owner: "octo", Name: "demo", Visibility: "public"},
				Security: &config.SecurityConfig{
					CodeQLDefaultSetup: &config.FeatureSetting{Enabled: true},
					AIPullRequestScan:  &config.FeatureSetting{Enabled: tc.declared},
				},
			}, Options{})
			if err != nil {
				t.Fatal(err)
			}
			if !hasChange(plan, "security.aiPullRequestScan", tc.wantState) {
				t.Fatalf("plan.Changes = %#v, want security.aiPullRequestScan as %s", plan.Changes, tc.wantState)
			}
			if tc.wantState == ChangeNoop && plan.HasChanges() {
				t.Fatalf("plan.HasChanges() = true, want false when AI Scan and CodeQL already match: %#v", plan.Changes)
			}
		})
	}
}

// TestBuildAIPullRequestScanCodeQLDependency guards docs/design.md's
// dependency: enabling AI Scan requires CodeQL default setup to be
// effectively enabled, with a declared codeqlDefaultSetup taking precedence
// over the live state, and disabling AI Scan having no dependency at all.
func TestBuildAIPullRequestScanCodeQLDependency(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		codeQL     *config.FeatureSetting
		liveCodeQL string
		liveErr    error
		aiScan     config.FeatureSetting
		wantState  ChangeState
	}{
		{name: "codeql declared true, live not configured", codeQL: &config.FeatureSetting{Enabled: true}, liveCodeQL: "not-configured", aiScan: config.FeatureSetting{Enabled: true}, wantState: ChangeUpdate},
		{name: "codeql declared false", codeQL: &config.FeatureSetting{Enabled: false}, liveCodeQL: "configured", aiScan: config.FeatureSetting{Enabled: true}, wantState: ChangeBlocked},
		{name: "codeql declared false, optional", codeQL: &config.FeatureSetting{Enabled: false}, liveCodeQL: "configured", aiScan: config.FeatureSetting{Enabled: true, Optional: true}, wantState: ChangeManual},
		{name: "codeql omitted, live not configured", liveCodeQL: "not-configured", aiScan: config.FeatureSetting{Enabled: true}, wantState: ChangeBlocked},
		{name: "codeql omitted, live not configured, optional", liveCodeQL: "not-configured", aiScan: config.FeatureSetting{Enabled: true, Optional: true}, wantState: ChangeManual},
		{name: "codeql omitted, live read fails", liveErr: &githubapi.APIError{StatusCode: 403}, aiScan: config.FeatureSetting{Enabled: true}, wantState: ChangeBlocked},
		{name: "codeql omitted, live configured", liveCodeQL: "configured", aiScan: config.FeatureSetting{Enabled: true}, wantState: ChangeUpdate},
		{name: "disabled with codeql declared false", codeQL: &config.FeatureSetting{Enabled: false}, liveCodeQL: "not-configured", aiScan: config.FeatureSetting{Enabled: false}, wantState: ChangeNoop},
		{name: "disabled with codeql omitted and not configured", liveCodeQL: "not-configured", aiScan: config.FeatureSetting{Enabled: false}, wantState: ChangeNoop},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			service := &fakeService{
				repository:            githubapi.RepositoryInfo{Visibility: "public"},
				codeQLDefaultSetup:    githubapi.CodeQLDefaultSetupStatus{State: tc.liveCodeQL},
				codeQLDefaultSetupErr: tc.liveErr,
				aiScan:                githubapi.AIScanSetting{PRScan: "disabled"},
			}
			engine, err := New(service)
			if err != nil {
				t.Fatal(err)
			}
			aiScan := tc.aiScan
			plan, err := engine.Build(context.Background(), &config.Config{
				SchemaVersion: config.CurrentSchemaVersion,
				Repository:    config.RepositoryConfig{Owner: "octo", Name: "demo", Visibility: "public"},
				Security: &config.SecurityConfig{
					CodeQLDefaultSetup: tc.codeQL,
					AIPullRequestScan:  &aiScan,
				},
			}, Options{})
			if err != nil {
				t.Fatal(err)
			}
			if !hasChange(plan, "security.aiPullRequestScan", tc.wantState) {
				t.Fatalf("plan.Changes = %#v, want security.aiPullRequestScan as %s", plan.Changes, tc.wantState)
			}
			if tc.wantState == ChangeManual && plan.HasBlocked() {
				t.Fatalf("optional dependency violation unexpectedly blocked the plan: %#v", plan.Changes)
			}
		})
	}
}

// TestBuildAIPullRequestScanOnNewRepository guards the create path: before the
// repository exists only a declared codeqlDefaultSetup can satisfy the
// dependency, and an omitted one is treated as unsatisfied.
func TestBuildAIPullRequestScanOnNewRepository(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		codeQL    *config.FeatureSetting
		aiScan    config.FeatureSetting
		wantState ChangeState
	}{
		{name: "codeql declared true", codeQL: &config.FeatureSetting{Enabled: true}, aiScan: config.FeatureSetting{Enabled: true}, wantState: ChangeUpdate},
		{name: "codeql omitted", aiScan: config.FeatureSetting{Enabled: true}, wantState: ChangeBlocked},
		{name: "codeql omitted, optional", aiScan: config.FeatureSetting{Enabled: true, Optional: true}, wantState: ChangeManual},
		{name: "disabled", aiScan: config.FeatureSetting{Enabled: false}, wantState: ChangeUpdate},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			service := &fakeService{getRepositoryErr: &githubapi.APIError{StatusCode: 404}}
			engine, err := New(service)
			if err != nil {
				t.Fatal(err)
			}
			aiScan := tc.aiScan
			plan, err := engine.Build(context.Background(), &config.Config{
				SchemaVersion: config.CurrentSchemaVersion,
				Repository:    config.RepositoryConfig{Owner: "octo", Name: "demo", Visibility: "public", Create: true},
				Security: &config.SecurityConfig{
					CodeQLDefaultSetup: tc.codeQL,
					AIPullRequestScan:  &aiScan,
				},
			}, Options{})
			if err != nil {
				t.Fatal(err)
			}
			if !hasChange(plan, "security.aiPullRequestScan", tc.wantState) {
				t.Fatalf("plan.Changes = %#v, want security.aiPullRequestScan as %s on the create path", plan.Changes, tc.wantState)
			}
		})
	}
}

func hasChange(plan Plan, resource string, state ChangeState) bool {
	for _, change := range plan.Changes {
		if change.Resource == resource && change.State == state {
			return true
		}
	}
	return false
}

type fakeService struct {
	repository                     githubapi.RepositoryInfo
	getRepositoryErr               error
	actions                        githubapi.ActionsPermissions
	actionVariables                githubapi.ActionVariableList
	agentVariables                 githubapi.AgentVariableList
	rulesets                       []githubapi.Ruleset
	labels                         []githubapi.Label
	milestones                     []githubapi.Milestone
	immutableReleases              githubapi.ImmutableReleases
	forkPullRequestApproval        githubapi.ForkPullRequestApproval
	forkPullRequestApprovalErr     error
	vulnerabilityAlertsEnabled     bool
	vulnerabilityAlertsErr         error
	automatedSecurityFixesEnabled  bool
	automatedSecurityFixesErr      error
	privateVulnerabilityReporting  bool
	privateVulnerabilityReportErr  error
	codeQLDefaultSetup             githubapi.CodeQLDefaultSetupStatus
	codeQLDefaultSetupErr          error
	aiScan                         githubapi.AIScanSetting
	aiScanErr                      error
	selectedActions                githubapi.SelectedActions
	defaultWorkflowPermissions     githubapi.DefaultWorkflowPermissions
	environments                   []githubapi.Environment
	teams                          map[string]githubapi.Team
	apps                           map[string]githubapi.App
	customRoles                    []githubapi.CustomRepositoryRole
	updateRepositoryCalls          int
	listActionVariablesCalls       int
	createOrUpdateEnvironmentCalls int
	createLabelCalls               int
	updateLabelCalls               int
	createMilestoneCalls           int
	updateMilestoneCalls           int
	getTeamCalls                   int
	getAppCalls                    int
	listCustomRepositoryRolesCalls int
	getAuthenticatedUserCalls      int
}

func (f *fakeService) GetRepository(context.Context, githubapi.Repository) (githubapi.RepositoryInfo, error) {
	return f.repository, f.getRepositoryErr
}

func (f *fakeService) CreateRepository(_ context.Context, request githubapi.CreateRepositoryRequest) (githubapi.RepositoryInfo, error) {
	return githubapi.RepositoryInfo{Name: request.Name, Visibility: request.Visibility}, nil
}

func (f *fakeService) UpdateRepository(_ context.Context, _ githubapi.Repository, request githubapi.UpdateRepositoryRequest) (githubapi.RepositoryInfo, error) {
	f.updateRepositoryCalls++
	if request.Description != nil {
		f.repository.Description = *request.Description
	}
	if request.Homepage != nil {
		f.repository.Homepage = *request.Homepage
	}
	if request.Archived != nil {
		f.repository.Archived = *request.Archived
	}
	if request.HasIssues != nil {
		f.repository.HasIssues = *request.HasIssues
	}
	if request.HasProjects != nil {
		f.repository.HasProjects = *request.HasProjects
	}
	if request.HasWiki != nil {
		f.repository.HasWiki = *request.HasWiki
	}
	if request.HasDiscussions != nil {
		f.repository.HasDiscussions = *request.HasDiscussions
	}
	if request.MergeCommitTitle != "" {
		f.repository.MergeCommitTitle = request.MergeCommitTitle
	}
	if request.MergeCommitMessage != "" {
		f.repository.MergeCommitMessage = request.MergeCommitMessage
	}
	if request.SquashMergeCommitTitle != "" {
		f.repository.SquashMergeCommitTitle = request.SquashMergeCommitTitle
	}
	if request.SquashMergeCommitMessage != "" {
		f.repository.SquashMergeCommitMessage = request.SquashMergeCommitMessage
	}
	return f.repository, nil
}

func (f *fakeService) ReplaceTopics(_ context.Context, _ githubapi.Repository, topics []string) ([]string, error) {
	f.repository.Topics = topics
	return topics, nil
}

func (f *fakeService) GetActionsPermissions(context.Context, githubapi.Repository) (githubapi.ActionsPermissions, error) {
	return f.actions, nil
}

func (f *fakeService) UpdateActionsPermissions(_ context.Context, _ githubapi.Repository, permissions githubapi.ActionsPermissions) error {
	f.actions = permissions
	return nil
}

func (f *fakeService) GetDefaultWorkflowPermissions(context.Context, githubapi.Repository) (githubapi.DefaultWorkflowPermissions, error) {
	return f.defaultWorkflowPermissions, nil
}

func (*fakeService) UpdateDefaultWorkflowPermissions(context.Context, githubapi.Repository, githubapi.DefaultWorkflowPermissions) error {
	return nil
}

func (f *fakeService) GetSelectedActions(context.Context, githubapi.Repository) (githubapi.SelectedActions, error) {
	return f.selectedActions, nil
}

func (*fakeService) UpdateSelectedActions(context.Context, githubapi.Repository, githubapi.SelectedActionsRequest) error {
	return nil
}

func (f *fakeService) ListEnvironments(context.Context, githubapi.Repository) ([]githubapi.Environment, error) {
	return f.environments, nil
}

func (f *fakeService) GetForkPullRequestApproval(context.Context, githubapi.Repository) (githubapi.ForkPullRequestApproval, error) {
	return f.forkPullRequestApproval, f.forkPullRequestApprovalErr
}

func (*fakeService) SetForkPullRequestApproval(context.Context, githubapi.Repository, githubapi.ForkPullRequestApproval) error {
	return nil
}

func (f *fakeService) GetTeam(_ context.Context, org, slug string) (githubapi.Team, error) {
	f.getTeamCalls++
	if team, ok := f.teams[org+"/"+slug]; ok {
		return team, nil
	}
	return githubapi.Team{}, &githubapi.APIError{StatusCode: 404, Method: "GET", Path: "/orgs/" + org + "/teams/" + slug}
}

func (f *fakeService) GetApp(_ context.Context, slug string) (githubapi.App, error) {
	f.getAppCalls++
	if app, ok := f.apps[slug]; ok {
		return app, nil
	}
	return githubapi.App{}, &githubapi.APIError{StatusCode: 404, Method: "GET", Path: "/apps/" + slug}
}

func (f *fakeService) ListCustomRepositoryRoles(context.Context, string) ([]githubapi.CustomRepositoryRole, error) {
	f.listCustomRepositoryRolesCalls++
	return f.customRoles, nil
}

func (f *fakeService) GetVulnerabilityAlertsEnabled(context.Context, githubapi.Repository) (bool, error) {
	return f.vulnerabilityAlertsEnabled, f.vulnerabilityAlertsErr
}

func (f *fakeService) GetAuthenticatedUser(context.Context) (githubapi.Owner, error) {
	f.getAuthenticatedUserCalls++
	return githubapi.Owner{}, nil
}

func (*fakeService) SetVulnerabilityAlerts(context.Context, githubapi.Repository, bool) error {
	return nil
}

func (f *fakeService) GetAutomatedSecurityFixesEnabled(context.Context, githubapi.Repository) (bool, error) {
	return f.automatedSecurityFixesEnabled, f.automatedSecurityFixesErr
}

func (*fakeService) SetAutomatedSecurityFixes(context.Context, githubapi.Repository, bool) error {
	return nil
}

func (f *fakeService) ListActionVariables(context.Context, githubapi.Repository) (githubapi.ActionVariableList, error) {
	f.listActionVariablesCalls++
	return f.actionVariables, nil
}

func (*fakeService) CreateActionVariable(context.Context, githubapi.Repository, githubapi.ActionVariable) error {
	return nil
}

func (*fakeService) UpdateActionVariable(context.Context, githubapi.Repository, githubapi.ActionVariable) error {
	return nil
}

func (f *fakeService) CreateOrUpdateEnvironment(context.Context, githubapi.Repository, string, githubapi.EnvironmentRequest) (githubapi.Environment, error) {
	f.createOrUpdateEnvironmentCalls++
	return githubapi.Environment{}, nil
}

func (f *fakeService) ListRulesets(context.Context, githubapi.Repository) ([]githubapi.Ruleset, error) {
	return f.rulesets, nil
}

func (*fakeService) CreateRuleset(context.Context, githubapi.Repository, githubapi.Ruleset) (githubapi.Ruleset, error) {
	return githubapi.Ruleset{}, nil
}

func (*fakeService) UpdateRuleset(context.Context, githubapi.Repository, int64, githubapi.Ruleset) (githubapi.Ruleset, error) {
	return githubapi.Ruleset{}, nil
}

func (*fakeService) UpdateSecurityAndAnalysis(context.Context, githubapi.Repository, githubapi.SecurityAndAnalysisRequest) (githubapi.RepositoryInfo, error) {
	return githubapi.RepositoryInfo{}, nil
}

func (f *fakeService) GetCodeQLDefaultSetup(context.Context, githubapi.Repository) (githubapi.CodeQLDefaultSetupStatus, error) {
	return f.codeQLDefaultSetup, f.codeQLDefaultSetupErr
}

func (*fakeService) ConfigureCodeQLDefaultSetup(context.Context, githubapi.Repository, githubapi.CodeQLDefaultSetupRequest) error {
	return nil
}

func (f *fakeService) GetAIScan(context.Context, githubapi.Repository) (githubapi.AIScanSetting, error) {
	return f.aiScan, f.aiScanErr
}

func (*fakeService) UpdateAIScan(context.Context, githubapi.Repository, githubapi.AIScanSetting) error {
	return nil
}

func (f *fakeService) GetPrivateVulnerabilityReportingEnabled(context.Context, githubapi.Repository) (bool, error) {
	return f.privateVulnerabilityReporting, f.privateVulnerabilityReportErr
}

func (*fakeService) SetPrivateVulnerabilityReporting(context.Context, githubapi.Repository, bool) error {
	return nil
}

func (f *fakeService) ListLabels(context.Context, githubapi.Repository) ([]githubapi.Label, error) {
	return f.labels, nil
}

func (f *fakeService) CreateLabel(_ context.Context, _ githubapi.Repository, label githubapi.Label) (githubapi.Label, error) {
	f.createLabelCalls++
	f.labels = append(f.labels, label)
	return label, nil
}

func (f *fakeService) UpdateLabel(_ context.Context, _ githubapi.Repository, name string, label githubapi.Label) (githubapi.Label, error) {
	f.updateLabelCalls++
	for i, existing := range f.labels {
		if strings.EqualFold(existing.Name, name) {
			f.labels[i] = label
			return label, nil
		}
	}
	return githubapi.Label{}, &githubapi.APIError{StatusCode: 404, Method: "PATCH", Path: "/labels/" + name}
}

func (f *fakeService) ListMilestones(context.Context, githubapi.Repository) ([]githubapi.Milestone, error) {
	return f.milestones, nil
}

func (f *fakeService) CreateMilestone(_ context.Context, _ githubapi.Repository, milestone githubapi.Milestone) (githubapi.Milestone, error) {
	f.createMilestoneCalls++
	milestone.Number = len(f.milestones) + 1
	f.milestones = append(f.milestones, milestone)
	return milestone, nil
}

func (f *fakeService) UpdateMilestone(_ context.Context, _ githubapi.Repository, number int, milestone githubapi.Milestone) (githubapi.Milestone, error) {
	f.updateMilestoneCalls++
	for i, existing := range f.milestones {
		if existing.Number == number {
			milestone.Number = number
			f.milestones[i] = milestone
			return milestone, nil
		}
	}
	return githubapi.Milestone{}, &githubapi.APIError{StatusCode: 404, Method: "PATCH", Path: "/milestones"}
}

func (f *fakeService) GetImmutableReleases(context.Context, githubapi.Repository) (githubapi.ImmutableReleases, error) {
	return f.immutableReleases, nil
}

func (f *fakeService) SetImmutableReleases(_ context.Context, _ githubapi.Repository, enabled bool) error {
	f.immutableReleases.Enabled = enabled
	return nil
}

func (f *fakeService) ListAgentVariables(context.Context, githubapi.Repository) (githubapi.AgentVariableList, error) {
	return f.agentVariables, nil
}

func (*fakeService) CreateAgentVariable(context.Context, githubapi.Repository, githubapi.AgentVariable) error {
	return nil
}

func (*fakeService) UpdateAgentVariable(context.Context, githubapi.Repository, githubapi.AgentVariable) error {
	return nil
}
