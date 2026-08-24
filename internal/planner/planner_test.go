package planner

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/kkamegawa/gh-repobootstrapper/internal/config"
	"github.com/kkamegawa/gh-repobootstrapper/internal/githubapi"
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
	plan, err := engine.Build(context.Background(), &config.Config{
		SchemaVersion: config.CurrentSchemaVersion,
		Repository: config.RepositoryConfig{
			Owner:       "octo",
			Name:        "demo",
			Visibility:  "public",
			Description: "new description",
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
		Repository:    config.RepositoryConfig{Owner: "octo", Name: "demo", Visibility: "public", Description: description},
	}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if service.updateRepositoryCalls != 1 {
		t.Fatalf("repository update calls = %d, want 1", service.updateRepositoryCalls)
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

// TestBuildSurfacesPendingBypassActorsAndForkPullRequestApproval guards
// visibility for fields accepted by config validation but not yet applied
// (issue #10): they must appear as manual changes, never silently dropped,
// and must never block the parts of the plan that are supported.
func TestBuildSurfacesPendingBypassActorsAndForkPullRequestApproval(t *testing.T) {
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
	if !hasChange(plan, "actions.forkPullRequestApproval", ChangeManual) {
		t.Fatalf("plan did not surface forkPullRequestApproval as manual: %#v", plan.Changes)
	}
	if !hasChange(plan, "ruleset.protect-main.bypassActors", ChangeManual) {
		t.Fatalf("plan did not surface bypassActors as manual: %#v", plan.Changes)
	}
	if plan.HasBlocked() {
		t.Fatalf("pending fields unexpectedly blocked the plan: %#v", plan.Changes)
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
	actions                        githubapi.ActionsPermissions
	actionVariables                githubapi.ActionVariableList
	agentVariables                 githubapi.AgentVariableList
	rulesets                       []githubapi.Ruleset
	labels                         []githubapi.Label
	milestones                     []githubapi.Milestone
	immutableReleases              githubapi.ImmutableReleases
	updateRepositoryCalls          int
	listActionVariablesCalls       int
	createOrUpdateEnvironmentCalls int
	createLabelCalls               int
	updateLabelCalls               int
	createMilestoneCalls           int
	updateMilestoneCalls           int
}

func (f *fakeService) GetRepository(context.Context, githubapi.Repository) (githubapi.RepositoryInfo, error) {
	return f.repository, nil
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

func (*fakeService) UpdateDefaultWorkflowPermissions(context.Context, githubapi.Repository, githubapi.DefaultWorkflowPermissions) error {
	return nil
}

func (*fakeService) UpdateSelectedActions(context.Context, githubapi.Repository, githubapi.SelectedActionsRequest) error {
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

func (*fakeService) ConfigureCodeQLDefaultSetup(context.Context, githubapi.Repository, githubapi.CodeQLDefaultSetupRequest) error {
	return nil
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
