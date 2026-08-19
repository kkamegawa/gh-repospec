package planner

import (
	"context"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/kkamegawa/gh-repobootstrapper/internal/config"
	"github.com/kkamegawa/gh-repobootstrapper/internal/githubapi"
	"github.com/kkamegawa/gh-repobootstrapper/internal/seed"
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

	plan, err := engine.Build(context.Background(), &config.Config{
		SchemaVersion: config.CurrentSchemaVersion,
		Repository: config.RepositoryConfig{
			Owner: "octo", Name: "demo", Visibility: "public", Description: "configured",
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
	if len(service.mutations) != 0 {
		t.Fatalf("Build() mutated GitHub state: %#v", service.mutations)
	}
	for index := 1; index < len(plan.Changes); index++ {
		if stageRank(plan.Changes[index-1].Stage) > stageRank(plan.Changes[index].Stage) {
			t.Fatalf("plan stages are not ordered: %#v", plan.Changes)
		}
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

	_, err = engine.Apply(context.Background(), &config.Config{
		SchemaVersion: config.CurrentSchemaVersion,
		Repository: config.RepositoryConfig{
			Owner: "octo", Name: "demo", Visibility: "public", Description: "configured", Topics: []string{"configured"},
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
	s.mutations = append(s.mutations, "repository")
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
