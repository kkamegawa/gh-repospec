package planner

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/kkamegawa/gh-repospec/internal/config"
	"github.com/kkamegawa/gh-repospec/internal/githubapi"
	"github.com/kkamegawa/gh-repospec/internal/seed"
)

func TestBuildIsReadOnlyOrdersChangesAndReportsBlockedState(t *testing.T) {
	t.Parallel()
	enabled := true
	service := &recordingService{fakeService: fakeService{
		repository: githubapi.RepositoryInfo{Description: "old", Visibility: "public"},
		actions:    githubapi.ActionsPermissions{Enabled: false},
	}}
	engine, err := New(service)
	if err != nil {
		t.Fatal(err)
	}

	archived := true
	description := "configured"
	plan, err := engine.Build(context.Background(), &config.Config{
		SchemaVersion: config.CurrentSchemaVersion,
		Repository: config.RepositoryConfig{
			Owner: "octo", Name: "demo", Visibility: "public", Description: &description, Archived: &archived,
		},
		Actions: &config.ActionsConfig{Enabled: &enabled},
		Security: &config.SecurityConfig{
			DependencyGraph: &config.FeatureSetting{Enabled: true},
		},
		Agents: &config.AgentsConfig{MCPConfigPath: ".github/mcp.json"},
		Rulesets: []config.RulesetConfig{{
			Name: "protect-main", Enforcement: "active", Target: "branch", Include: []string{"~DEFAULT_BRANCH"},
			Rules: []config.RulesetRule{{Type: "deletion"}},
		}},
		Commands: []config.CommandConfig{{ID: "initialize", Program: "bootstrap", Timeout: "1m"}},
	}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !plan.HasBlocked() || !hasChange(plan, "security.dependencyGraph", ChangeBlocked) {
		t.Fatalf("plan did not report the unsupported required feature: %#v", plan.Changes)
	}
	if !hasChange(plan, "command.initialize", ChangeManual) {
		t.Fatalf("plan did not report the command approval requirement: %#v", plan.Changes)
	}
	if !hasChange(plan, "repository.archived", ChangeUpdate) {
		t.Fatalf("plan did not report the archive request: %#v", plan.Changes)
	}
	if len(service.mutations) != 0 {
		t.Fatalf("Build() mutated GitHub state: %#v", service.mutations)
	}
	for index := 1; index < len(plan.Changes); index++ {
		if stageRank(plan.Changes[index-1].Stage) > stageRank(plan.Changes[index].Stage) {
			t.Fatalf("plan stages are not ordered: %#v", plan.Changes)
		}
	}
	if last := plan.Changes[len(plan.Changes)-1]; last.Stage != StageArchive {
		t.Fatalf("last change stage = %q, want %q: %#v", last.Stage, StageArchive, plan.Changes)
	}
}

func TestApplyUpdatesSettingsBeforeRulesets(t *testing.T) {
	t.Parallel()
	enabled := true
	service := &recordingService{fakeService: fakeService{
		repository: githubapi.RepositoryInfo{
			ID: 1, Description: "old", Visibility: "public", Topics: []string{"old"},
		},
		actions: githubapi.ActionsPermissions{Enabled: false},
	}}
	engine, err := New(service)
	if err != nil {
		t.Fatal(err)
	}

	description := "configured"
	_, err = engine.Apply(context.Background(), &config.Config{
		SchemaVersion: config.CurrentSchemaVersion,
		Repository: config.RepositoryConfig{
			Owner: "octo", Name: "demo", Visibility: "public", Description: &description, Topics: []string{"configured"},
		},
		Actions:  &config.ActionsConfig{Enabled: &enabled},
		Security: &config.SecurityConfig{SecretScanning: &config.FeatureSetting{Enabled: true}},
		Rulesets: []config.RulesetConfig{{
			Name: "protect-main", Enforcement: "active", Target: "branch", Include: []string{"~DEFAULT_BRANCH"},
			Rules: []config.RulesetRule{{Type: "deletion"}},
		}},
	}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"repository", "topics", "security", "actions", "ruleset"}; !reflect.DeepEqual(service.mutations, want) {
		t.Fatalf("apply order = %#v, want %#v", service.mutations, want)
	}
	if service.lastRuleset.Conditions["ref_name"] == nil {
		t.Fatalf("ruleset conditions were not submitted: %#v", service.lastRuleset)
	}
}

func TestApplyOrdersSecurityAroundContentAndAutomation(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "README.md"), []byte("seed"), 0o600); err != nil {
		t.Fatal(err)
	}
	service := &recordingService{fakeService: fakeService{
		repository: githubapi.RepositoryInfo{ID: 1, Visibility: "public", DefaultBranch: "main"},
		actions:    githubapi.ActionsPermissions{Enabled: false},
	}}
	engine, err := New(service)
	if err != nil {
		t.Fatal(err)
	}

	_, err = engine.Apply(context.Background(), &config.Config{
		SchemaVersion: config.CurrentSchemaVersion,
		Repository:    config.RepositoryConfig{Owner: "octo", Name: "demo", Visibility: "public"},
		Seed:          &config.SeedConfig{Directory: &config.DirectorySeed{Path: directory}},
		Actions: &config.ActionsConfig{
			AllowedActions:  "selected",
			SelectedActions: &config.SelectedActionsConfig{GitHubOwnedAllowed: true, VerifiedAllowed: true},
		},
		Security: &config.SecurityConfig{
			SecretScanning:     &config.FeatureSetting{Enabled: true},
			CodeQLDefaultSetup: &config.FeatureSetting{Enabled: true},
		},
		Rulesets: []config.RulesetConfig{{
			Name: "protect-main", Enforcement: "active", Target: "branch", Include: []string{"~DEFAULT_BRANCH"},
			Rules: []config.RulesetRule{{Type: "deletion"}},
		}},
	}, Options{})
	if err != nil {
		t.Fatal(err)
	}

	preContentSecurity := slices.Index(service.mutations, "security")
	files := slices.Index(service.mutations, "files")
	selectedActions := slices.Index(service.mutations, "selected-actions")
	codeql := slices.Index(service.mutations, "codeql")
	ruleset := slices.Index(service.mutations, "ruleset")
	if preContentSecurity == -1 || files == -1 || preContentSecurity > files {
		t.Fatalf("pre-content security must apply before files: %#v", service.mutations)
	}
	if selectedActions == -1 || codeql == -1 || ruleset == -1 {
		t.Fatalf("expected selected-actions, codeql, and ruleset mutations: %#v", service.mutations)
	}
	if codeql < selectedActions {
		t.Fatalf("remaining security must apply after Actions automation: %#v", service.mutations)
	}
	if codeql > ruleset {
		t.Fatalf("remaining security must apply before rulesets: %#v", service.mutations)
	}
}

// TestApplyUnarchivesBeforeEveryOtherChange guards the apply order required
// because GitHub rejects writes to an archived repository: unarchiving must
// happen before any other mutation, including the repository settings PATCH
// that would otherwise race it.
func TestApplyUnarchivesBeforeEveryOtherChange(t *testing.T) {
	t.Parallel()
	archived := false
	service := &recordingService{fakeService: fakeService{
		repository: githubapi.RepositoryInfo{ID: 1, Visibility: "public", Archived: true},
	}}
	engine, err := New(service)
	if err != nil {
		t.Fatal(err)
	}

	description := "configured"
	_, err = engine.Apply(context.Background(), &config.Config{
		SchemaVersion: config.CurrentSchemaVersion,
		Repository: config.RepositoryConfig{
			Owner: "octo", Name: "demo", Visibility: "public", Description: &description, Archived: &archived,
		},
	}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(service.mutations) == 0 || service.mutations[0] != "unarchive" {
		t.Fatalf("apply order = %#v, want unarchive first", service.mutations)
	}
	if service.repository.Archived {
		t.Fatalf("repository archived = %t, want false after apply", service.repository.Archived)
	}
}

// TestApplyArchivesAfterEveryOtherChange guards the opposite order: archiving
// makes the repository read-only, so it must run last, after settings,
// labels, and rulesets.
func TestApplyArchivesAfterEveryOtherChange(t *testing.T) {
	t.Parallel()
	archived := true
	service := &recordingService{fakeService: fakeService{
		repository: githubapi.RepositoryInfo{ID: 1, Visibility: "public"},
	}}
	engine, err := New(service)
	if err != nil {
		t.Fatal(err)
	}

	_, err = engine.Apply(context.Background(), &config.Config{
		SchemaVersion: config.CurrentSchemaVersion,
		Repository:    config.RepositoryConfig{Owner: "octo", Name: "demo", Visibility: "public", Archived: &archived},
		Labels:        []config.LabelConfig{{Name: "kind/bug", Color: "d73a4a"}},
		Rulesets: []config.RulesetConfig{{
			Name: "protect-main", Enforcement: "active", Target: "branch", Include: []string{"~DEFAULT_BRANCH"},
			Rules: []config.RulesetRule{{Type: "deletion"}},
		}},
	}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(service.mutations) == 0 || service.mutations[len(service.mutations)-1] != "archive" {
		t.Fatalf("apply order = %#v, want archive last", service.mutations)
	}
	label := slices.Index(service.mutations, "label")
	ruleset := slices.Index(service.mutations, "ruleset")
	archiveIndex := slices.Index(service.mutations, "archive")
	if label == -1 || ruleset == -1 || archiveIndex < label || archiveIndex < ruleset {
		t.Fatalf("archive did not run after labels and rulesets: %#v", service.mutations)
	}
}

// TestApplyReachesAgentSecretsWithoutAgentVariables guards against
// applyAgentVariables returning early whenever agents.variables is empty,
// which used to skip agents.secrets entirely for configs that only declare
// secrets. The fake service does not implement secretService, so reaching
// applySecrets surfaces its "unavailable" error; before the fix, Apply()
// returned nil instead because the function returned before ever looking at
// desired.Secrets.
func TestApplyReachesAgentSecretsWithoutAgentVariables(t *testing.T) {
	// Not t.Parallel(): t.Setenv forbids parallel execution.
	t.Setenv("AGENT_TOKEN_SOURCE", "value")
	service := &fakeService{repository: githubapi.RepositoryInfo{ID: 1, Visibility: "public"}}
	engine, err := New(service)
	if err != nil {
		t.Fatal(err)
	}

	_, err = engine.Apply(context.Background(), &config.Config{
		SchemaVersion: config.CurrentSchemaVersion,
		Repository:    config.RepositoryConfig{Owner: "octo", Name: "demo", Visibility: "public"},
		Agents: &config.AgentsConfig{
			Secrets: []config.SecretRef{{Name: "TOKEN", SourceEnv: "AGENT_TOKEN_SOURCE"}},
		},
	}, Options{})
	if err == nil || !strings.Contains(err.Error(), "secret operations are unavailable") {
		t.Fatalf("Apply() error = %v, want the agents.secrets path to be reached", err)
	}
}

func TestApplyRequiresCommandApprovalBeforeExecution(t *testing.T) {
	t.Parallel()
	service := &recordingService{fakeService: fakeService{
		repository: githubapi.RepositoryInfo{ID: 1, Visibility: "public", DefaultBranch: "main"},
	}}
	engine, err := New(service)
	if err != nil {
		t.Fatal(err)
	}

	_, err = engine.Apply(context.Background(), &config.Config{
		SchemaVersion: config.CurrentSchemaVersion,
		Repository:    config.RepositoryConfig{Owner: "octo", Name: "demo", Visibility: "public"},
		Commands:      []config.CommandConfig{{ID: "initialize", Program: "not-run", Timeout: "1m"}},
	}, Options{})
	if !errors.Is(err, seed.ErrCommandNotApproved) {
		t.Fatalf("Apply() error = %v, want an unapproved command error", err)
	}
	if len(service.committed) != 0 {
		t.Fatalf("Apply() committed files after an unapproved command: %#v", service.committed)
	}
}

func TestApplyRejectsSeedConflictWithoutCommitting(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "README.md"), []byte("configured"), 0o600); err != nil {
		t.Fatal(err)
	}
	service := &recordingService{fakeService: fakeService{
		repository: githubapi.RepositoryInfo{ID: 1, Visibility: "public", DefaultBranch: "main"},
	}, tree: githubapi.GitTree{Tree: []githubapi.GitTreeEntry{{Path: "README.md", Type: "blob", SHA: "existing"}}}, blob: githubapi.GitBlob{
		Content: base64.StdEncoding.EncodeToString([]byte("existing")),
	}}
	engine, err := New(service)
	if err != nil {
		t.Fatal(err)
	}

	_, err = engine.Apply(context.Background(), &config.Config{
		SchemaVersion: config.CurrentSchemaVersion,
		Repository:    config.RepositoryConfig{Owner: "octo", Name: "demo", Visibility: "public"},
		Seed:          &config.SeedConfig{Directory: &config.DirectorySeed{Path: directory}},
	}, Options{})
	if err == nil || !strings.Contains(err.Error(), "seed conflicts with existing file") {
		t.Fatalf("Apply() error = %v, want a seed conflict", err)
	}
	if len(service.committed) != 0 {
		t.Fatalf("Apply() committed files despite a conflict: %#v", service.committed)
	}
}

type recordingService struct {
	fakeService
	mutations   []string
	lastRuleset githubapi.Ruleset
	committed   []githubapi.File
	tree        githubapi.GitTree
	blob        githubapi.GitBlob
}

func (s *recordingService) UpdateRepository(ctx context.Context, repository githubapi.Repository, request githubapi.UpdateRepositoryRequest) (githubapi.RepositoryInfo, error) {
	switch {
	case request.Archived != nil && *request.Archived:
		s.mutations = append(s.mutations, "archive")
	case request.Archived != nil && !*request.Archived:
		s.mutations = append(s.mutations, "unarchive")
	default:
		s.mutations = append(s.mutations, "repository")
	}
	return s.fakeService.UpdateRepository(ctx, repository, request)
}

func (s *recordingService) ReplaceTopics(ctx context.Context, repository githubapi.Repository, topics []string) ([]string, error) {
	s.mutations = append(s.mutations, "topics")
	return s.fakeService.ReplaceTopics(ctx, repository, topics)
}

func (s *recordingService) UpdateActionsPermissions(ctx context.Context, repository githubapi.Repository, permissions githubapi.ActionsPermissions) error {
	s.mutations = append(s.mutations, "actions")
	return s.fakeService.UpdateActionsPermissions(ctx, repository, permissions)
}

func (s *recordingService) UpdateSecurityAndAnalysis(context.Context, githubapi.Repository, githubapi.SecurityAndAnalysisRequest) (githubapi.RepositoryInfo, error) {
	s.mutations = append(s.mutations, "security")
	return s.repository, nil
}

func (s *recordingService) ConfigureCodeQLDefaultSetup(context.Context, githubapi.Repository, githubapi.CodeQLDefaultSetupRequest) error {
	s.mutations = append(s.mutations, "codeql")
	return nil
}

func (s *recordingService) UpdateSelectedActions(context.Context, githubapi.Repository, githubapi.SelectedActionsRequest) error {
	s.mutations = append(s.mutations, "selected-actions")
	return nil
}

func (s *recordingService) CreateRuleset(_ context.Context, _ githubapi.Repository, ruleset githubapi.Ruleset) (githubapi.Ruleset, error) {
	s.mutations = append(s.mutations, "ruleset")
	s.lastRuleset = ruleset
	return ruleset, nil
}

func (s *recordingService) UpdateRuleset(_ context.Context, _ githubapi.Repository, _ int64, ruleset githubapi.Ruleset) (githubapi.Ruleset, error) {
	s.mutations = append(s.mutations, "ruleset")
	s.lastRuleset = ruleset
	return ruleset, nil
}

func (s *recordingService) CommitFiles(_ context.Context, _ githubapi.Repository, _ string, _ string, files []githubapi.File) (githubapi.CommitResult, error) {
	s.mutations = append(s.mutations, "files")
	s.committed = append([]githubapi.File(nil), files...)
	return githubapi.CommitResult{CommitSHA: "commit"}, nil
}

func (*recordingService) MaterializeTemplateArchive(context.Context, githubapi.Repository, string, string) error {
	return errors.New("template archive was not expected in this test")
}

func (*recordingService) GetBranchHead(context.Context, githubapi.Repository, string) (githubapi.GitReference, error) {
	return githubapi.GitReference{Object: githubapi.GitObject{SHA: "head"}}, nil
}

func (*recordingService) GetCommit(context.Context, githubapi.Repository, string) (githubapi.GitCommit, error) {
	return githubapi.GitCommit{Tree: githubapi.GitObject{SHA: "tree"}}, nil
}

func (s *recordingService) GetTree(context.Context, githubapi.Repository, string, bool) (githubapi.GitTree, error) {
	return s.tree, nil
}

func (s *recordingService) GetBlob(context.Context, githubapi.Repository, string) (githubapi.GitBlob, error) {
	return s.blob, nil
}

func (s *recordingService) CreateLabel(ctx context.Context, repository githubapi.Repository, label githubapi.Label) (githubapi.Label, error) {
	s.mutations = append(s.mutations, "label")
	return s.fakeService.CreateLabel(ctx, repository, label)
}

func (s *recordingService) UpdateLabel(ctx context.Context, repository githubapi.Repository, name string, label githubapi.Label) (githubapi.Label, error) {
	s.mutations = append(s.mutations, "label")
	return s.fakeService.UpdateLabel(ctx, repository, name, label)
}

func (s *recordingService) CreateMilestone(ctx context.Context, repository githubapi.Repository, milestone githubapi.Milestone) (githubapi.Milestone, error) {
	s.mutations = append(s.mutations, "milestone")
	return s.fakeService.CreateMilestone(ctx, repository, milestone)
}

func (s *recordingService) UpdateMilestone(ctx context.Context, repository githubapi.Repository, number int, milestone githubapi.Milestone) (githubapi.Milestone, error) {
	s.mutations = append(s.mutations, "milestone")
	return s.fakeService.UpdateMilestone(ctx, repository, number, milestone)
}

func (s *recordingService) SetImmutableReleases(ctx context.Context, repository githubapi.Repository, enabled bool) error {
	s.mutations = append(s.mutations, "releaseImmutability")
	return s.fakeService.SetImmutableReleases(ctx, repository, enabled)
}

func (s *recordingService) SetForkPullRequestApproval(ctx context.Context, repository githubapi.Repository, request githubapi.ForkPullRequestApproval) error {
	s.mutations = append(s.mutations, "fork-pr-approval")
	return s.fakeService.SetForkPullRequestApproval(ctx, repository, request)
}

func (s *recordingService) SetVulnerabilityAlerts(ctx context.Context, repository githubapi.Repository, enabled bool) error {
	s.mutations = append(s.mutations, "dependabot-alerts")
	s.vulnerabilityAlertsEnabled = enabled
	return s.fakeService.SetVulnerabilityAlerts(ctx, repository, enabled)
}

func (s *recordingService) SetAutomatedSecurityFixes(ctx context.Context, repository githubapi.Repository, enabled bool) error {
	s.mutations = append(s.mutations, "dependabot-security-updates")
	return s.fakeService.SetAutomatedSecurityFixes(ctx, repository, enabled)
}

// TestApplySendsResolvedBypassActorsAndEvaluateEnforcement guards the wire
// shape apply actually sends: role and the resolved "id:<number>" form both
// resolve to the numeric actor IDs the rulesets API requires, and
// enforcement: evaluate passes through unchanged.
func TestApplySendsResolvedBypassActorsAndEvaluateEnforcement(t *testing.T) {
	t.Parallel()
	service := &recordingService{fakeService: fakeService{
		repository: githubapi.RepositoryInfo{ID: 1, Owner: githubapi.Owner{Type: "Organization"}, Visibility: "public"},
	}}
	engine, err := New(service)
	if err != nil {
		t.Fatal(err)
	}
	_, err = engine.Apply(context.Background(), &config.Config{
		SchemaVersion: config.CurrentSchemaVersion,
		Repository:    config.RepositoryConfig{Owner: "octo-org", Name: "demo", Visibility: "public"},
		Rulesets: []config.RulesetConfig{{
			Name: "protect-main", Enforcement: "evaluate", Target: "branch", Include: []string{"~DEFAULT_BRANCH"},
			Rules: []config.RulesetRule{{Type: "deletion"}},
			BypassActors: []config.BypassActorConfig{
				{Role: "admin", BypassMode: "pull_request"},
				{Team: "id:42", BypassMode: "always"},
			},
		}},
	}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if service.lastRuleset.Enforcement != "evaluate" {
		t.Fatalf("lastRuleset.Enforcement = %q, want %q", service.lastRuleset.Enforcement, "evaluate")
	}
	want := []githubapi.BypassActor{
		{ActorID: 5, ActorType: "RepositoryRole", BypassMode: "pull_request"},
		{ActorID: 42, ActorType: "Team", BypassMode: "always"},
	}
	if !reflect.DeepEqual(service.lastRuleset.BypassActors, want) {
		t.Fatalf("lastRuleset.BypassActors = %#v, want %#v", service.lastRuleset.BypassActors, want)
	}
}

// TestApplyResolvesBypassActorsOncePerRun guards resolver reuse between plan
// and apply: build resolves once during planning, and Apply must reuse that
// resolver's cache rather than looking the same team up a second time.
func TestApplyResolvesBypassActorsOncePerRun(t *testing.T) {
	t.Parallel()
	service := &recordingService{fakeService: fakeService{
		repository: githubapi.RepositoryInfo{ID: 1, Owner: githubapi.Owner{Type: "Organization"}, Visibility: "public"},
		teams:      map[string]githubapi.Team{"octo-org/platform": {ID: 9, Slug: "platform"}},
	}}
	engine, err := New(service)
	if err != nil {
		t.Fatal(err)
	}
	_, err = engine.Apply(context.Background(), &config.Config{
		SchemaVersion: config.CurrentSchemaVersion,
		Repository:    config.RepositoryConfig{Owner: "octo-org", Name: "demo", Visibility: "public"},
		Rulesets: []config.RulesetConfig{{
			Name: "protect-main", Enforcement: "active", Target: "branch", Include: []string{"~DEFAULT_BRANCH"},
			Rules:        []config.RulesetRule{{Type: "deletion"}},
			BypassActors: []config.BypassActorConfig{{Team: "platform", BypassMode: "always"}},
		}},
	}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if service.getTeamCalls != 1 {
		t.Fatalf("getTeamCalls = %d, want 1 (resolved once across plan and apply)", service.getTeamCalls)
	}
}

// TestApplySetsForkPullRequestApprovalAfterActionsPermissions guards apply
// order: the fork PR approval policy is a separate endpoint from Actions
// permissions and must be set after them, matching the plan's settings stage.
func TestApplySetsForkPullRequestApprovalAfterActionsPermissions(t *testing.T) {
	t.Parallel()
	enabled := true
	service := &recordingService{fakeService: fakeService{
		repository: githubapi.RepositoryInfo{ID: 1, Visibility: "public"},
		actions:    githubapi.ActionsPermissions{Enabled: false},
	}}
	engine, err := New(service)
	if err != nil {
		t.Fatal(err)
	}
	_, err = engine.Apply(context.Background(), &config.Config{
		SchemaVersion: config.CurrentSchemaVersion,
		Repository:    config.RepositoryConfig{Owner: "octo", Name: "demo", Visibility: "public"},
		Actions:       &config.ActionsConfig{Enabled: &enabled, ForkPullRequestApproval: "all_external_contributors"},
	}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	actions := slices.Index(service.mutations, "actions")
	forkApproval := slices.Index(service.mutations, "fork-pr-approval")
	if actions == -1 || forkApproval == -1 || actions > forkApproval {
		t.Fatalf("fork pull request approval must apply after Actions permissions: %#v", service.mutations)
	}
}

// TestApplyEnablesDependabotAlertsBeforeSecurityUpdates guards
// docs/design.md's ordering requirement: enabling both in the same file
// applies alerts first, because GitHub rejects security updates otherwise.
func TestApplyEnablesDependabotAlertsBeforeSecurityUpdates(t *testing.T) {
	t.Parallel()
	service := &recordingService{fakeService: fakeService{
		repository: githubapi.RepositoryInfo{ID: 1, Visibility: "public"},
	}}
	engine, err := New(service)
	if err != nil {
		t.Fatal(err)
	}
	_, err = engine.Apply(context.Background(), &config.Config{
		SchemaVersion: config.CurrentSchemaVersion,
		Repository:    config.RepositoryConfig{Owner: "octo", Name: "demo", Visibility: "public"},
		Security: &config.SecurityConfig{
			DependabotAlerts:          &config.FeatureSetting{Enabled: true},
			DependabotSecurityUpdates: &config.FeatureSetting{Enabled: true},
		},
	}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	alerts := slices.Index(service.mutations, "dependabot-alerts")
	securityUpdates := slices.Index(service.mutations, "dependabot-security-updates")
	if alerts == -1 || securityUpdates == -1 || alerts > securityUpdates {
		t.Fatalf("Dependabot alerts must apply before security updates: %#v", service.mutations)
	}
}

// TestApplySkipsSecurityUpdatesWhenAlertsAreNotEffectivelyEnabled guards the
// belt-and-suspenders check in applyRemainingSecurity: even if a caller
// somehow bypasses the blocked plan, apply itself never sends a security
// updates write GitHub is known to reject.
func TestApplySkipsSecurityUpdatesWhenAlertsAreNotEffectivelyEnabled(t *testing.T) {
	t.Parallel()
	service := &recordingService{fakeService: fakeService{
		repository:                 githubapi.RepositoryInfo{ID: 1, Visibility: "public"},
		vulnerabilityAlertsEnabled: false,
	}}
	engine, err := New(service)
	if err != nil {
		t.Fatal(err)
	}
	err = engine.applyRemainingSecurity(context.Background(), githubapi.Repository{Owner: "octo", Name: "demo"}, &config.SecurityConfig{
		DependabotSecurityUpdates: &config.FeatureSetting{Enabled: true},
	})
	if err == nil {
		t.Fatal("applyRemainingSecurity unexpectedly succeeded with alerts not effectively enabled")
	}
	if slices.Contains(service.mutations, "dependabot-security-updates") {
		t.Fatalf("SetAutomatedSecurityFixes was called despite alerts not being effectively enabled: %#v", service.mutations)
	}
}

// secretRecordingService adds an in-memory secret store to recordingService.
// No fake in this package implements secretService without it, so
// apply --force-secrets could not be exercised before this type existed.
type secretRecordingService struct {
	recordingService
	actionsSecrets map[string]bool
	agentSecrets   map[string]bool
	puts           []string // "actions:<name>" or "agents:<name>"
}

func (s *secretRecordingService) ListActionsSecrets(context.Context, githubapi.Repository) (githubapi.SecretList, error) {
	return secretListFor(s.actionsSecrets), nil
}

func (s *secretRecordingService) ListAgentSecrets(context.Context, githubapi.Repository) (githubapi.SecretList, error) {
	return secretListFor(s.agentSecrets), nil
}

func (s *secretRecordingService) GetActionsSecretPublicKey(context.Context, githubapi.Repository) (githubapi.PublicKey, error) {
	return fakeSecretPublicKey(), nil
}

func (s *secretRecordingService) GetAgentSecretPublicKey(context.Context, githubapi.Repository) (githubapi.PublicKey, error) {
	return fakeSecretPublicKey(), nil
}

func (s *secretRecordingService) PutActionsSecret(_ context.Context, _ githubapi.Repository, name string, _ githubapi.EncryptedSecret) error {
	s.puts = append(s.puts, "actions:"+name)
	if s.actionsSecrets == nil {
		s.actionsSecrets = map[string]bool{}
	}
	s.actionsSecrets[name] = true
	return nil
}

func (s *secretRecordingService) PutAgentSecret(_ context.Context, _ githubapi.Repository, name string, _ githubapi.EncryptedSecret) error {
	s.puts = append(s.puts, "agents:"+name)
	if s.agentSecrets == nil {
		s.agentSecrets = map[string]bool{}
	}
	s.agentSecrets[name] = true
	return nil
}

func secretListFor(names map[string]bool) githubapi.SecretList {
	list := githubapi.SecretList{}
	for name := range names {
		list.Secrets = append(list.Secrets, githubapi.SecretMetadata{Name: name})
	}
	list.TotalCount = len(list.Secrets)
	return list
}

// fakeSecretPublicKey is a syntactically valid (32-byte, base64-encoded)
// NaCl box public key. Tests never decrypt the resulting ciphertext, so its
// cryptographic weight does not matter.
func fakeSecretPublicKey() githubapi.PublicKey {
	return githubapi.PublicKey{KeyID: "test-key", Key: base64.StdEncoding.EncodeToString(make([]byte, 32))}
}

// TestBuildIgnoresForceSecrets guards that ForceSecrets has no effect on
// Build: appendSecretChange never consults Options, so a secret is already
// always reported as an update (or blocked), whether or not ForceSecrets is
// later passed to Apply.
func TestBuildIgnoresForceSecrets(t *testing.T) {
	t.Setenv("ACTIONS_TOKEN_SOURCE", "value")
	cfg := &config.Config{
		SchemaVersion: config.CurrentSchemaVersion,
		Repository:    config.RepositoryConfig{Owner: "octo", Name: "demo", Visibility: "public"},
		Actions: &config.ActionsConfig{
			Secrets: []config.SecretRef{{Name: "TOKEN", SourceEnv: "ACTIONS_TOKEN_SOURCE"}},
		},
	}
	without, err := New(&fakeService{repository: githubapi.RepositoryInfo{Visibility: "public"}})
	if err != nil {
		t.Fatal(err)
	}
	withoutPlan, err := without.Build(context.Background(), cfg, Options{})
	if err != nil {
		t.Fatal(err)
	}
	with, err := New(&fakeService{repository: githubapi.RepositoryInfo{Visibility: "public"}})
	if err != nil {
		t.Fatal(err)
	}
	withPlan, err := with.Build(context.Background(), cfg, Options{ForceSecrets: true})
	if err != nil {
		t.Fatal(err)
	}
	withoutJSON, err := json.Marshal(withoutPlan)
	if err != nil {
		t.Fatal(err)
	}
	withJSON, err := json.Marshal(withPlan)
	if err != nil {
		t.Fatal(err)
	}
	if string(withoutJSON) != string(withJSON) {
		t.Fatalf("plan with ForceSecrets = %s, want identical to plan without it: %s", withJSON, withoutJSON)
	}
}

// TestApplySkipsExistingSecretUnderCreateOnlyPolicy is the baseline for the
// createOnly gate that --force-secrets overrides.
func TestApplySkipsExistingSecretUnderCreateOnlyPolicy(t *testing.T) {
	// Not t.Parallel(): t.Setenv forbids parallel execution.
	t.Setenv("ACTIONS_TOKEN_SOURCE", "value")
	service := &secretRecordingService{
		recordingService: recordingService{fakeService: fakeService{repository: githubapi.RepositoryInfo{ID: 1, Visibility: "public"}}},
		actionsSecrets:   map[string]bool{"TOKEN": true},
	}
	engine, err := New(service)
	if err != nil {
		t.Fatal(err)
	}
	_, err = engine.Apply(context.Background(), &config.Config{
		SchemaVersion: config.CurrentSchemaVersion,
		Repository:    config.RepositoryConfig{Owner: "octo", Name: "demo", Visibility: "public"},
		Actions: &config.ActionsConfig{
			Secrets: []config.SecretRef{{Name: "TOKEN", SourceEnv: "ACTIONS_TOKEN_SOURCE"}},
		},
	}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(service.puts) != 0 {
		t.Fatalf("Apply() wrote %#v, want the existing createOnly secret left alone", service.puts)
	}
}

// TestApplyForceSecretsRewritesExistingCreateOnlySecrets covers apply
// --force-secrets against Actions secrets: an existing createOnly secret is
// re-written when ForceSecrets is set.
func TestApplyForceSecretsRewritesExistingCreateOnlySecrets(t *testing.T) {
	// Not t.Parallel(): t.Setenv forbids parallel execution.
	t.Setenv("ACTIONS_TOKEN_SOURCE", "value")
	service := &secretRecordingService{
		recordingService: recordingService{fakeService: fakeService{repository: githubapi.RepositoryInfo{ID: 1, Visibility: "public"}}},
		actionsSecrets:   map[string]bool{"TOKEN": true},
	}
	engine, err := New(service)
	if err != nil {
		t.Fatal(err)
	}
	_, err = engine.Apply(context.Background(), &config.Config{
		SchemaVersion: config.CurrentSchemaVersion,
		Repository:    config.RepositoryConfig{Owner: "octo", Name: "demo", Visibility: "public"},
		Actions: &config.ActionsConfig{
			Secrets: []config.SecretRef{{Name: "TOKEN", SourceEnv: "ACTIONS_TOKEN_SOURCE"}},
		},
	}, Options{ForceSecrets: true})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(service.puts, "actions:TOKEN") {
		t.Fatalf("Apply(ForceSecrets: true) puts = %#v, want actions:TOKEN rewritten", service.puts)
	}
}

// TestApplyForceSecretsCoversAgentSecrets guards that secretTarget is
// threaded through applyAgentVariables, not just applyActions.
func TestApplyForceSecretsCoversAgentSecrets(t *testing.T) {
	// Not t.Parallel(): t.Setenv forbids parallel execution.
	t.Setenv("AGENT_TOKEN_SOURCE", "value")
	service := &secretRecordingService{
		recordingService: recordingService{fakeService: fakeService{repository: githubapi.RepositoryInfo{ID: 1, Visibility: "public"}}},
		agentSecrets:     map[string]bool{"TOKEN": true},
	}
	engine, err := New(service)
	if err != nil {
		t.Fatal(err)
	}
	_, err = engine.Apply(context.Background(), &config.Config{
		SchemaVersion: config.CurrentSchemaVersion,
		Repository:    config.RepositoryConfig{Owner: "octo", Name: "demo", Visibility: "public"},
		Agents: &config.AgentsConfig{
			Secrets: []config.SecretRef{{Name: "TOKEN", SourceEnv: "AGENT_TOKEN_SOURCE"}},
		},
	}, Options{ForceSecrets: true})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(service.puts, "agents:TOKEN") {
		t.Fatalf("Apply(ForceSecrets: true) puts = %#v, want agents:TOKEN rewritten", service.puts)
	}
}

// TestApplyForceSecretsStillRequiresSourceEnvironmentVariable guards that
// ForceSecrets does not relax the sourceEnv requirement: a missing variable
// still blocks the plan, and Apply refuses a blocked plan before writing.
func TestApplyForceSecretsStillRequiresSourceEnvironmentVariable(t *testing.T) {
	t.Parallel()
	service := &secretRecordingService{
		recordingService: recordingService{fakeService: fakeService{repository: githubapi.RepositoryInfo{ID: 1, Visibility: "public"}}},
		actionsSecrets:   map[string]bool{"TOKEN": true},
	}
	engine, err := New(service)
	if err != nil {
		t.Fatal(err)
	}
	_, err = engine.Apply(context.Background(), &config.Config{
		SchemaVersion: config.CurrentSchemaVersion,
		Repository:    config.RepositoryConfig{Owner: "octo", Name: "demo", Visibility: "public"},
		Actions: &config.ActionsConfig{
			Secrets: []config.SecretRef{{Name: "TOKEN", SourceEnv: "ACTIONS_TOKEN_SOURCE_UNSET"}},
		},
	}, Options{ForceSecrets: true})
	if err == nil {
		t.Fatal("Apply(ForceSecrets: true) unexpectedly succeeded with an unset source environment variable")
	}
	if len(service.puts) != 0 {
		t.Fatalf("Apply() wrote %#v despite a blocked plan", service.puts)
	}
}
