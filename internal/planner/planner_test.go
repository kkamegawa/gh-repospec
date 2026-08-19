package planner

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
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
	if err := os.WriteFile(filepath.Join(root, "run.sh"), []byte("#!/bin/sh\n"), 0o700); err != nil {
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

func hasChange(plan Plan, resource string, state ChangeState) bool {
	for _, change := range plan.Changes {
		if change.Resource == resource && change.State == state {
			return true
		}
	}
	return false
}

type fakeService struct {
	repository            githubapi.RepositoryInfo
	actions               githubapi.ActionsPermissions
	actionVariables       githubapi.ActionVariableList
	agentVariables        githubapi.AgentVariableList
	rulesets              []githubapi.Ruleset
	updateRepositoryCalls int
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
	return f.actionVariables, nil
}

func (*fakeService) CreateActionVariable(context.Context, githubapi.Repository, githubapi.ActionVariable) error {
	return nil
}

func (*fakeService) UpdateActionVariable(context.Context, githubapi.Repository, githubapi.ActionVariable) error {
	return nil
}

func (*fakeService) CreateOrUpdateEnvironment(context.Context, githubapi.Repository, string, githubapi.EnvironmentRequest) (githubapi.Environment, error) {
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

func (f *fakeService) ListAgentVariables(context.Context, githubapi.Repository) (githubapi.AgentVariableList, error) {
	return f.agentVariables, nil
}

func (*fakeService) CreateAgentVariable(context.Context, githubapi.Repository, githubapi.AgentVariable) error {
	return nil
}

func (*fakeService) UpdateAgentVariable(context.Context, githubapi.Repository, githubapi.AgentVariable) error {
	return nil
}
