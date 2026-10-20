package planner

import (
	"context"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kkamegawa/gh-repospec/internal/config"
	"github.com/kkamegawa/gh-repospec/internal/githubapi"
)

func TestEmptyRepositoryInitializationAndCommandsRunOnce(t *testing.T) {
	countPath := filepath.Join(t.TempDir(), "command-count.txt")
	t.Setenv("GO_WANT_BOOTSTRAP_HELPER", "1")
	t.Setenv("BOOTSTRAP_COUNT_FILE", countPath)
	service := &bootstrapRecordingService{
		recordingService: recordingService{fakeService: fakeService{repository: githubapi.RepositoryInfo{Name: "repotest1", Visibility: "public"}}},
		files:            map[string][]byte{},
		blobs:            map[string][]byte{},
	}
	engine, err := New(service)
	if err != nil {
		t.Fatal(err)
	}
	commandProgram, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{
		SchemaVersion: config.CurrentSchemaVersion,
		Repository: config.RepositoryConfig{
			Owner: "octo", Name: "repotest1", Visibility: "public", DefaultBranch: "main",
		},
		Seed: &config.SeedConfig{Initialize: &config.InitializeSeed{GitignoreTemplate: "VisualStudio"}},
		Commands: []config.CommandConfig{{
			ID: "apm-init", Program: commandProgram,
			Args: []string{"-test.run=TestOneTimeBootstrapCommandHelper"},
			CWD:  ".", Timeout: "30s",
			Environment: []string{"GO_WANT_BOOTSTRAP_HELPER", "BOOTSTRAP_COUNT_FILE"},
		}},
	}
	options := Options{ApprovedCommandIDs: map[string]bool{"apm-init": true}}
	firstPlan, err := engine.Apply(context.Background(), cfg, options)
	if err != nil {
		t.Fatalf("first Apply() error = %v; plan: %#v", err, firstPlan.Changes)
	}
	if service.templateName != "VisualStudio" {
		t.Fatalf("gitignore template = %q, want VisualStudio", service.templateName)
	}
	if len(service.firstContentsFile) == 0 || service.firstContentsFile[0].Path != "README.md" {
		t.Fatalf("first Contents API file = %#v, want README.md; mutations=%v, branchReady=%v, file count=%d", service.firstContentsFile, service.mutations, service.branchReady, len(service.files))
	}
	if got := string(service.files[".gitignore"]); got != "# generated from github/gitignore VisualStudio\n" {
		t.Fatalf(".gitignore = %q", got)
	}
	if got := string(service.files["apm.yml"]); !strings.Contains(got, "name: repotest1") || !strings.Contains(got, "- claude") || !strings.Contains(got, "tfsugjp/skills") {
		t.Fatalf("apm.yml did not contain the destination name and Relaypublisher dependencies: %s", got)
	}
	if _, exists := service.files[commandStatePath]; !exists {
		t.Fatalf("bootstrap did not commit %s", commandStatePath)
	}

	secondPlan, err := engine.Build(context.Background(), cfg, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !hasChange(secondPlan, "command.apm-init", ChangeNoop) {
		t.Fatalf("second plan = %#v, want apm-init already completed", secondPlan.Changes)
	}
	if _, err := engine.Apply(context.Background(), cfg, Options{}); err != nil {
		t.Fatalf("second Apply() without command approval error = %v", err)
	}
	count, err := os.ReadFile(countPath) //nolint:gosec // countPath is a t.TempDir path
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(string(count), "run\n"); got != 1 {
		t.Fatalf("command invocation count = %d, want exactly one; content %q", got, count)
	}
}

func TestOneTimeBootstrapCommandHelper(t *testing.T) {
	if os.Getenv("GO_WANT_BOOTSTRAP_HELPER") != "1" {
		return
	}
	count, err := os.OpenFile(os.Getenv("BOOTSTRAP_COUNT_FILE"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600) //nolint:gosec // the parent test sets this to a t.TempDir path

	if err != nil {
		t.Fatal(err)
	}
	if _, err := count.WriteString("run\n"); err != nil {
		_ = count.Close()
		t.Fatal(err)
	}
	if err := count.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("apm.yml", []byte(testRelayPublisherAPM), 0o600); err != nil {
		t.Fatal(err)
	}
}

const testRelayPublisherAPM = `name: repotest1
version: 1.0.0
description: APM project for Relaypublisher
author: Kazushi Kamegawa
targets:
  - claude
  - copilot
  - codex
  - agent-skills
dependencies:
  apm:
    - microsoft/azure-skills/.github/plugins/azure-skills
    - MicrosoftDocs/mcp
    - dotnet/skills/plugins/dotnet
    - dotnet/skills/plugins/dotnet-test
    - github/awesome-copilot/plugins/csharp-dotnet-development
    - github/awesome-copilot/plugins/security-best-practices
    - github/awesome-copilot/plugins/software-engineering-team
    - tfsugjp/skills
  mcp:
    - name: microsoft-learn
      registry: false
      transport: http
      url: https://learn.microsoft.com/api/mcp
    - name: azure
      registry: false
      transport: stdio
      command: npx
      args:
        - -y
        - '@azure/mcp@latest'
        - server
        - start
includes: auto
scripts: {}
`

type bootstrapRecordingService struct {
	recordingService
	files             map[string][]byte
	blobs             map[string][]byte
	templateName      string
	firstContentsFile []githubapi.File
	branchReady       bool
}

func (s *bootstrapRecordingService) UpdateRepository(ctx context.Context, repository githubapi.Repository, request githubapi.UpdateRepositoryRequest) (githubapi.RepositoryInfo, error) {
	updated, err := s.recordingService.UpdateRepository(ctx, repository, request)
	if err == nil && request.DefaultBranch != "" {
		s.repository.DefaultBranch = request.DefaultBranch
		s.branchReady = true
	}
	return updated, err
}

func (s *bootstrapRecordingService) GetGitignoreTemplate(_ context.Context, name string) (githubapi.GitignoreTemplate, error) {
	s.templateName = name
	return githubapi.GitignoreTemplate{Name: name, FileName: ".gitignore", Source: "# generated from github/gitignore VisualStudio\n"}, nil
}

func (s *bootstrapRecordingService) CreateContentsFile(_ context.Context, _ githubapi.Repository, file githubapi.File, _, _ string) error {
	s.branchReady = true
	s.files[file.Path] = append([]byte(nil), file.Content...)
	s.firstContentsFile = append(s.firstContentsFile, file)
	s.mutations = append(s.mutations, "contents")
	return nil
}

func (s *bootstrapRecordingService) CommitFiles(ctx context.Context, repository githubapi.Repository, branch, message string, files []githubapi.File) (githubapi.CommitResult, error) {
	result, err := s.recordingService.CommitFiles(ctx, repository, branch, message, files)
	if err != nil {
		return githubapi.CommitResult{}, err
	}
	for _, file := range files {
		s.files[file.Path] = append([]byte(nil), file.Content...)
	}
	return result, nil
}

func (s *bootstrapRecordingService) GetBranchHead(_ context.Context, _ githubapi.Repository, branch string) (githubapi.GitReference, error) {
	if !s.branchReady {
		return githubapi.GitReference{}, &githubapi.APIError{StatusCode: 409, Method: "GET", Path: "/git/ref/heads/" + branch, Message: "Git Repository is empty."}
	}
	return githubapi.GitReference{Object: githubapi.GitObject{SHA: "head"}}, nil
}

func (*bootstrapRecordingService) GetCommit(context.Context, githubapi.Repository, string) (githubapi.GitCommit, error) {
	return githubapi.GitCommit{Tree: githubapi.GitObject{SHA: "tree"}}, nil
}

func (s *bootstrapRecordingService) GetTree(context.Context, githubapi.Repository, string, bool) (githubapi.GitTree, error) {
	tree := githubapi.GitTree{SHA: "tree"}
	for path, content := range s.files {
		sha := "blob:" + path
		s.blobs[sha] = append([]byte(nil), content...)
		tree.Tree = append(tree.Tree, githubapi.GitTreeEntry{Path: path, Type: "blob", SHA: sha})
	}
	return tree, nil
}

func (s *bootstrapRecordingService) GetBlob(_ context.Context, _ githubapi.Repository, sha string) (githubapi.GitBlob, error) {
	content, ok := s.blobs[sha]
	if !ok {
		return githubapi.GitBlob{}, errors.New("unknown test blob")
	}
	return githubapi.GitBlob{Content: base64.StdEncoding.EncodeToString(content)}, nil
}
