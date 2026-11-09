package planner

import (
	"context"
	"net/http"
	"testing"

	"github.com/kkamegawa/gh-repospec/internal/config"
	"github.com/kkamegawa/gh-repospec/internal/githubapi"
)

type createCopilotRepositoryService struct {
	*repositorySettingsMemory
	created bool
}

func (s *createCopilotRepositoryService) GetRepository(_ context.Context, repository githubapi.Repository) (githubapi.RepositoryInfo, error) {
	if !s.created {
		return githubapi.RepositoryInfo{}, &githubapi.APIError{StatusCode: http.StatusNotFound}
	}
	return githubapi.RepositoryInfo{Name: repository.Name, Visibility: "public"}, nil
}

func (s *createCopilotRepositoryService) CreateRepository(_ context.Context, request githubapi.CreateRepositoryRequest) (githubapi.RepositoryInfo, error) {
	s.created = true
	return githubapi.RepositoryInfo{Name: request.Name, Visibility: request.Visibility}, nil
}

func (s *createCopilotRepositoryService) UpdateCopilotCodeReviewSettings(ctx context.Context, repository githubapi.Repository, request githubapi.CopilotCodeReviewSettingsRequest) error {
	if !s.created {
		return http.ErrServerClosed
	}
	return s.repositorySettingsMemory.UpdateCopilotCodeReviewSettings(ctx, repository, request)
}

func TestApplyCreatesRepositoryBeforeApplyingCopilotCodeReviewSettings(t *testing.T) {
	service := &createCopilotRepositoryService{repositorySettingsMemory: newRepositorySettingsMemory()}
	service.copilotCodeReview.AvailableReviewEfforts = []string{"lite", "balanced", "max"}
	engine, err := New(service)
	if err != nil {
		t.Fatal(err)
	}
	effort := "balanced"
	desired := &config.CopilotCodeReviewConfig{
		ReviewEffort: &effort,
		MCPEnabled:   boolPointer(false),
	}

	plan, err := engine.Apply(context.Background(), &config.Config{
		SchemaVersion: config.CurrentSchemaVersion,
		Repository: config.RepositoryConfig{
			Owner: "octo", Name: "created-repository", Create: true, Visibility: "public",
		},
		Settings: &config.RepositorySettings{CopilotCodeReview: desired},
	}, Options{})
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if !service.created {
		t.Fatal("Apply() did not create the missing repository")
	}
	if !hasChange(plan, "settings.copilotCodeReview.reviewEffort", ChangeCreate) || !hasChange(plan, "settings.copilotCodeReview.mcpEnabled", ChangeCreate) {
		t.Fatalf("initial plan = %#v, want Copilot settings deferred until creation", plan.Changes)
	}
	if service.copilotCodeReviewReads != 1 {
		t.Fatalf("Copilot settings reads = %d, want one read after creation during apply", service.copilotCodeReviewReads)
	}
	if len(service.copilotCodeReviewWrites) != 1 {
		t.Fatalf("Copilot settings PATCH count = %d, want one after creation", len(service.copilotCodeReviewWrites))
	}
	request := service.copilotCodeReviewWrites[0]
	if request.ReviewEffort == nil || *request.ReviewEffort != effort || request.MCPEnabled == nil || *request.MCPEnabled {
		t.Fatalf("Copilot settings PATCH = %#v, want declared settings", request)
	}
}
