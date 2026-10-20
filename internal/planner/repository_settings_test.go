package planner

import (
	"context"
	"testing"
	"time"

	"github.com/kkamegawa/gh-repospec/internal/config"
	"github.com/kkamegawa/gh-repospec/internal/githubapi"
)

// repositorySettingsMemory implements the REST settings surface in memory so
// the apply-then-plan test exercises convergence without contacting GitHub.
type repositorySettingsMemory struct {
	*recordingService
	pages                 githubapi.PagesSite
	pagesEnabled          bool
	webhooks              []githubapi.RepositoryWebhook
	autolinks             []githubapi.Autolink
	deployKeys            []githubapi.DeployKey
	collaborators         []githubapi.Collaborator
	teams                 []githubapi.RepositoryTeam
	properties            []githubapi.CustomPropertyValue
	customRules           map[string][]githubapi.DeploymentProtectionRule
	availableIntegrations map[string][]githubapi.AvailableDeploymentProtectionIntegration
	interactionLimit      githubapi.RepositoryInteractionLimit
	creationCap           githubapi.PullRequestCreationCap
	creationCapBypasses   []githubapi.InteractionBypassUser
	protections           map[string]map[string]any
	signatures            map[string]bool
	oidc                  githubapi.OIDCSubjectCustomization
	oidcWrites            []githubapi.OIDCSubjectCustomization
	// stickyImmutableSubject models a repository created after 2026-07-15,
	// where GitHub keeps the immutable subject claim enabled.
	stickyImmutableSubject bool
	nextID                 int64
}

func (s *repositorySettingsMemory) GetRepositoryInteractionLimit(context.Context, githubapi.Repository) (githubapi.RepositoryInteractionLimit, error) {
	return s.interactionLimit, nil
}

func (s *repositorySettingsMemory) SetRepositoryInteractionLimit(_ context.Context, _ githubapi.Repository, request githubapi.RepositoryInteractionLimitRequest) (githubapi.RepositoryInteractionLimit, error) {
	durations := map[string]time.Duration{"one_day": 24 * time.Hour, "three_days": 72 * time.Hour, "one_week": 7 * 24 * time.Hour, "one_month": 30 * 24 * time.Hour, "six_months": 180 * 24 * time.Hour}
	s.interactionLimit = githubapi.RepositoryInteractionLimit{Limit: request.Limit, Origin: "repository", ExpiresAt: time.Now().UTC().Add(durations[request.Expiry]).Format(time.RFC3339)}
	return s.interactionLimit, nil
}

func (s *repositorySettingsMemory) DeleteRepositoryInteractionLimit(context.Context, githubapi.Repository) error {
	s.interactionLimit = githubapi.RepositoryInteractionLimit{}
	return nil
}

func (s *repositorySettingsMemory) GetPullRequestCreationCap(context.Context, githubapi.Repository) (githubapi.PullRequestCreationCap, error) {
	return s.creationCap, nil
}

func (s *repositorySettingsMemory) UpdatePullRequestCreationCap(_ context.Context, _ githubapi.Repository, request githubapi.PullRequestCreationCapRequest) (githubapi.PullRequestCreationCap, error) {
	s.creationCap.Enabled = request.Enabled
	if request.MaxOpenPullRequests != nil {
		s.creationCap.MaxOpenPullRequests = *request.MaxOpenPullRequests
	}
	if request.IncludeDrafts != nil {
		s.creationCap.IncludeDrafts = *request.IncludeDrafts
	}
	return s.creationCap, nil
}

func (s *repositorySettingsMemory) ListPullRequestCreationCapBypassUsers(context.Context, githubapi.Repository) ([]githubapi.InteractionBypassUser, error) {
	return s.creationCapBypasses, nil
}

func (s *repositorySettingsMemory) AddPullRequestCreationCapBypassUsers(_ context.Context, _ githubapi.Repository, users []string) error {
	for _, login := range users {
		s.creationCapBypasses = append(s.creationCapBypasses, githubapi.InteractionBypassUser{Login: login})
	}
	return nil
}

func (s *repositorySettingsMemory) RemovePullRequestCreationCapBypassUsers(_ context.Context, _ githubapi.Repository, users []string) error {
	remove := make(map[string]bool, len(users))
	for _, login := range users {
		remove[login] = true
	}
	kept := s.creationCapBypasses[:0]
	for _, user := range s.creationCapBypasses {
		if !remove[user.Login] {
			kept = append(kept, user)
		}
	}
	s.creationCapBypasses = kept
	return nil
}

func newRepositorySettingsMemory() *repositorySettingsMemory {
	return &repositorySettingsMemory{
		recordingService:      &recordingService{fakeService: fakeService{repository: githubapi.RepositoryInfo{ID: 1, Visibility: "private", DefaultBranch: "main"}}},
		protections:           make(map[string]map[string]any),
		signatures:            make(map[string]bool),
		customRules:           make(map[string][]githubapi.DeploymentProtectionRule),
		availableIntegrations: make(map[string][]githubapi.AvailableDeploymentProtectionIntegration),
		nextID:                1,
	}
}

func (s *repositorySettingsMemory) CreateOrUpdateEnvironment(_ context.Context, _ githubapi.Repository, name string, _ githubapi.EnvironmentRequest) (githubapi.Environment, error) {
	for _, environment := range s.environments {
		if environment.Name == name {
			return environment, nil
		}
	}
	environment := githubapi.Environment{Name: name}
	s.environments = append(s.environments, environment)
	return environment, nil
}

func (s *repositorySettingsMemory) ListDeploymentProtectionRules(_ context.Context, _ githubapi.Repository, environment string) ([]githubapi.DeploymentProtectionRule, error) {
	return s.customRules[environment], nil
}

func (s *repositorySettingsMemory) ListAvailableDeploymentProtectionIntegrations(_ context.Context, _ githubapi.Repository, environment string) ([]githubapi.AvailableDeploymentProtectionIntegration, error) {
	return s.availableIntegrations[environment], nil
}

func (s *repositorySettingsMemory) EnableDeploymentProtectionIntegration(_ context.Context, _ githubapi.Repository, environment string, integrationID int64) (githubapi.DeploymentProtectionRule, error) {
	for _, integration := range s.availableIntegrations[environment] {
		if integration.ID == integrationID {
			rule := githubapi.DeploymentProtectionRule{ID: s.nextID, Enabled: true, App: struct {
				ID   int64  `json:"id"`
				Slug string `json:"slug"`
			}{ID: integration.ID, Slug: integration.Slug}}
			s.nextID++
			s.customRules[environment] = append(s.customRules[environment], rule)
			return rule, nil
		}
	}
	return githubapi.DeploymentProtectionRule{}, &githubapi.APIError{StatusCode: 404}
}

func (s *repositorySettingsMemory) DisableDeploymentProtectionRule(_ context.Context, _ githubapi.Repository, environment string, ruleID int64) error {
	for index, rule := range s.customRules[environment] {
		if rule.ID == ruleID {
			s.customRules[environment] = append(s.customRules[environment][:index], s.customRules[environment][index+1:]...)
			return nil
		}
	}
	return &githubapi.APIError{StatusCode: 404}
}

func (s *repositorySettingsMemory) GetPages(context.Context, githubapi.Repository) (githubapi.PagesSite, error) {
	if !s.pagesEnabled {
		return githubapi.PagesSite{}, &githubapi.APIError{StatusCode: 404}
	}
	return s.pages, nil
}

func (s *repositorySettingsMemory) CreatePages(_ context.Context, _ githubapi.Repository, request githubapi.PagesRequest) (githubapi.PagesSite, error) {
	s.pagesEnabled = true
	s.pages = githubapi.PagesSite{BuildType: request.BuildType, Source: request.Source}
	return s.pages, nil
}

func (s *repositorySettingsMemory) UpdatePages(_ context.Context, _ githubapi.Repository, request githubapi.PagesRequest) (githubapi.PagesSite, error) {
	s.pages = githubapi.PagesSite{BuildType: request.BuildType, Source: request.Source}
	if request.CNAME != nil {
		s.pages.CNAME = *request.CNAME
	}
	if request.HTTPSEnforced != nil {
		s.pages.HTTPSEnforced = *request.HTTPSEnforced
	}
	return s.pages, nil
}

func (s *repositorySettingsMemory) DeletePages(context.Context, githubapi.Repository) error {
	s.pagesEnabled = false
	return nil
}

func (s *repositorySettingsMemory) ListWebhooks(context.Context, githubapi.Repository) ([]githubapi.RepositoryWebhook, error) {
	return s.webhooks, nil
}

func (s *repositorySettingsMemory) CreateWebhook(_ context.Context, _ githubapi.Repository, hook githubapi.RepositoryWebhook) (githubapi.RepositoryWebhook, error) {
	hook.ID = s.nextID
	s.nextID++
	s.webhooks = append(s.webhooks, hook)
	return hook, nil
}

func (s *repositorySettingsMemory) UpdateWebhook(_ context.Context, _ githubapi.Repository, id int64, hook githubapi.RepositoryWebhook) (githubapi.RepositoryWebhook, error) {
	for i := range s.webhooks {
		if s.webhooks[i].ID == id {
			hook.ID = id
			s.webhooks[i] = hook
			return hook, nil
		}
	}
	return githubapi.RepositoryWebhook{}, &githubapi.APIError{StatusCode: 404}
}

func (s *repositorySettingsMemory) ListAutolinks(context.Context, githubapi.Repository) ([]githubapi.Autolink, error) {
	return s.autolinks, nil
}

func (s *repositorySettingsMemory) CreateAutolink(_ context.Context, _ githubapi.Repository, item githubapi.Autolink) (githubapi.Autolink, error) {
	item.ID = s.nextID
	s.nextID++
	s.autolinks = append(s.autolinks, item)
	return item, nil
}

func (s *repositorySettingsMemory) DeleteAutolink(_ context.Context, _ githubapi.Repository, id int64) error {
	for i := range s.autolinks {
		if s.autolinks[i].ID == id {
			s.autolinks = append(s.autolinks[:i], s.autolinks[i+1:]...)
			return nil
		}
	}
	return &githubapi.APIError{StatusCode: 404}
}

func (s *repositorySettingsMemory) ListDeployKeys(context.Context, githubapi.Repository) ([]githubapi.DeployKey, error) {
	return s.deployKeys, nil
}

func (s *repositorySettingsMemory) CreateDeployKey(_ context.Context, _ githubapi.Repository, item githubapi.DeployKey) (githubapi.DeployKey, error) {
	item.ID = s.nextID
	s.nextID++
	s.deployKeys = append(s.deployKeys, item)
	return item, nil
}

func (s *repositorySettingsMemory) DeleteDeployKey(_ context.Context, _ githubapi.Repository, id int64) error {
	for i := range s.deployKeys {
		if s.deployKeys[i].ID == id {
			s.deployKeys = append(s.deployKeys[:i], s.deployKeys[i+1:]...)
			return nil
		}
	}
	return &githubapi.APIError{StatusCode: 404}
}

func (s *repositorySettingsMemory) ListCollaborators(context.Context, githubapi.Repository) ([]githubapi.Collaborator, error) {
	return s.collaborators, nil
}

func (s *repositorySettingsMemory) SetCollaboratorPermission(_ context.Context, _ githubapi.Repository, login, permission string) error {
	for i := range s.collaborators {
		if s.collaborators[i].Login == login {
			s.collaborators[i].Permission = permission
			return nil
		}
	}
	s.collaborators = append(s.collaborators, githubapi.Collaborator{Login: login, Permission: permission})
	return nil
}

func (s *repositorySettingsMemory) ListRepositoryTeams(context.Context, githubapi.Repository) ([]githubapi.RepositoryTeam, error) {
	return s.teams, nil
}

func (s *repositorySettingsMemory) SetTeamRepositoryPermission(_ context.Context, _ githubapi.Repository, slug, permission string) error {
	for i := range s.teams {
		if s.teams[i].Slug == slug {
			s.teams[i].Permission = permission
			return nil
		}
	}
	s.teams = append(s.teams, githubapi.RepositoryTeam{Slug: slug, Permission: permission})
	return nil
}

func (s *repositorySettingsMemory) GetCustomPropertyValues(context.Context, githubapi.Repository) ([]githubapi.CustomPropertyValue, error) {
	return s.properties, nil
}

func (s *repositorySettingsMemory) SetCustomPropertyValues(_ context.Context, _ githubapi.Repository, values []githubapi.CustomPropertyValue) error {
	s.properties = append([]githubapi.CustomPropertyValue(nil), values...)
	return nil
}

func (s *repositorySettingsMemory) ListProtectedBranches(context.Context, githubapi.Repository) ([]githubapi.Branch, error) {
	branches := make([]githubapi.Branch, 0, len(s.protections))
	for branch := range s.protections {
		branches = append(branches, githubapi.Branch{Name: branch, Protected: true})
	}
	return branches, nil
}

func (s *repositorySettingsMemory) GetBranchProtection(_ context.Context, _ githubapi.Repository, branch string) (map[string]any, error) {
	protection, ok := s.protections[branch]
	if !ok {
		return nil, &githubapi.APIError{StatusCode: 404}
	}
	return protection, nil
}

func (s *repositorySettingsMemory) SetBranchProtection(_ context.Context, _ githubapi.Repository, branch string, settings map[string]any) error {
	s.protections[branch] = settings
	return nil
}

func (s *repositorySettingsMemory) GetRequiredSignatures(_ context.Context, _ githubapi.Repository, branch string) (bool, error) {
	enabled, ok := s.signatures[branch]
	if !ok {
		return false, &githubapi.APIError{StatusCode: 404}
	}
	return enabled, nil
}

func (s *repositorySettingsMemory) SetRequiredSignatures(_ context.Context, _ githubapi.Repository, branch string, enabled bool) error {
	s.signatures[branch] = enabled
	return nil
}

func (s *repositorySettingsMemory) GetOIDCSubjectCustomization(context.Context, githubapi.Repository) (githubapi.OIDCSubjectCustomization, error) {
	return s.oidc, nil
}

// SetOIDCSubjectCustomization follows GitHub: an omitted
// use_immutable_subject keeps the current value, and with
// stickyImmutableSubject an enabled immutable subject stays enabled whatever
// the request says.
func (s *repositorySettingsMemory) SetOIDCSubjectCustomization(_ context.Context, _ githubapi.Repository, settings githubapi.OIDCSubjectCustomization) error {
	s.oidcWrites = append(s.oidcWrites, settings)
	current := s.oidc.UseImmutableSubject
	switch {
	case settings.UseImmutableSubject == nil:
		settings.UseImmutableSubject = current
	case s.stickyImmutableSubject && current != nil && *current:
		settings.UseImmutableSubject = current
	}
	s.oidc = settings
	return nil
}

func TestRepositorySettingsApplyThenPlanHasNoDrift(t *testing.T) {
	service := newRepositorySettingsMemory()
	engine, err := New(service)
	if err != nil {
		t.Fatal(err)
	}

	customDomain := "docs.example.test"
	httpsRequired := true
	requireSignedCommits := true
	bypassUsers := []string{"octocat"}
	desired := &config.RepositorySettings{
		InteractionLimit: &config.InteractionLimitConfig{Enabled: true, Limit: "contributors_only", Expiry: "one_week"},
		PullRequestCreationCap: &config.PullRequestCreationCapConfig{
			Enabled: true, MaxOpenPullRequests: 5, IncludeDrafts: false, BypassUsers: &bypassUsers,
		},
		Pages: &config.PagesConfig{
			Enabled: true, BuildType: "legacy", Source: &config.PagesSource{Branch: "main", Path: "/"},
			CNAME: &customDomain, HTTPSRequired: &httpsRequired,
		},
		Webhooks:         []config.WebhookConfig{{URL: "https://hooks.example.test/repo", ContentType: "json", Events: []string{"pull_request", "push"}, Active: true}},
		Autolinks:        []config.AutolinkConfig{{KeyPrefix: "ISSUE-", URLTemplate: "https://tracker.example.test/<num>", IsAlphanumeric: false}},
		DeployKeys:       []config.DeployKeyConfig{{Title: "deploy", Key: "ssh-ed25519 AAAATEST", ReadOnly: true}},
		Collaborators:    []config.CollaboratorConfig{{Login: "octocat", Permission: "push"}},
		Teams:            []config.TeamAccessConfig{{Slug: "engineering", Permission: "maintain"}},
		CustomProperties: []config.CustomPropertyConfig{{Name: "tier", Value: "gold"}},
		BranchProtections: []config.BranchProtectionConfig{{
			Branch: "main", Settings: map[string]any{"required_linear_history": true}, RequireSignedCommits: &requireSignedCommits,
		}},
	}
	repository := githubapi.Repository{Owner: "octo", Name: "demo"}
	initialPlan := Plan{}
	appendRepositorySettingsChanges(context.Background(), service, &initialPlan, desired, repository, true, false)
	if !hasChange(initialPlan, "settings.branchProtection.main", ChangeCreate) {
		t.Fatalf("initial settings plan = %#v, want protection created on the existing unprotected branch", initialPlan.Changes)
	}
	if err := engine.applyRepositorySettings(context.Background(), repository, desired); err != nil {
		t.Fatalf("applyRepositorySettings() error = %v", err)
	}

	plan := Plan{}
	appendRepositorySettingsChanges(context.Background(), service, &plan, desired, repository, true, false)
	if plan.HasDrift() || plan.HasBlocked() {
		t.Fatalf("plan after applying settings still has drift or blocked entries: %#v", plan.Changes)
	}
	if len(plan.Changes) < 8 {
		t.Fatalf("post-apply plan has %d entries, want explicit no-op checks for every settings category: %#v", len(plan.Changes), plan.Changes)
	}
	for _, change := range plan.Changes {
		if change.State != ChangeNoop {
			t.Errorf("post-apply change %q = %s, want no-op", change.Resource, change.State)
		}
	}
}

func TestActionsOIDCSubjectCustomizationApplyThenPlanHasNoDrift(t *testing.T) {
	service := newRepositorySettingsMemory()
	engine, err := New(service)
	if err != nil {
		t.Fatal(err)
	}
	useImmutableSubject := true
	desired := &config.ActionsConfig{OIDCSubjectCustomization: &config.OIDCSubjectCustomizationConfig{
		UseDefault: false, IncludeClaimKeys: []string{"repo", "context"}, UseImmutableSubject: &useImmutableSubject,
	}}
	repository := githubapi.Repository{Owner: "octo", Name: "demo"}
	if err := engine.applyActions(context.Background(), repository, desired, Options{}); err != nil {
		t.Fatalf("applyActions() error = %v", err)
	}
	plan := Plan{}
	appendActionsChanges(context.Background(), service, &plan, desired, repository, config.RepositoryConfig{}, githubapi.RepositoryInfo{})
	if !hasChange(plan, "actions.oidcSubjectCustomization", ChangeNoop) {
		t.Fatalf("post-apply actions plan = %#v, want OIDC customization no-op", plan.Changes)
	}
}

func TestActionsOIDCImmutableSubjectThatGitHubKeepsIsManualAndNeverWrittenOff(t *testing.T) {
	enabled := true
	disabled := false
	repository := githubapi.Repository{Owner: "octo", Name: "demo"}
	for _, tc := range []struct {
		name       string
		desired    config.OIDCSubjectCustomizationConfig
		wantWrites int
	}{
		// The configuration imported from an older repository matches apart
		// from the immutable subject, so nothing needs to be written.
		{name: "only the immutable subject differs", desired: config.OIDCSubjectCustomizationConfig{UseDefault: true, UseImmutableSubject: &disabled}, wantWrites: 0},
		// The claim keys still converge; only the field GitHub keeps is dropped.
		{name: "claim keys differ too", desired: config.OIDCSubjectCustomizationConfig{UseDefault: false, IncludeClaimKeys: []string{"repo", "context"}, UseImmutableSubject: &disabled}, wantWrites: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			service := newRepositorySettingsMemory()
			service.stickyImmutableSubject = true
			service.oidc = githubapi.OIDCSubjectCustomization{UseDefault: true, UseImmutableSubject: &enabled}
			engine, err := New(service)
			if err != nil {
				t.Fatal(err)
			}
			desired := &config.ActionsConfig{OIDCSubjectCustomization: &tc.desired}

			before := Plan{}
			appendActionsChanges(context.Background(), service, &before, desired, repository, config.RepositoryConfig{}, githubapi.RepositoryInfo{})
			if !hasChange(before, "actions.oidcSubjectCustomization.useImmutableSubject", ChangeManual) {
				t.Fatalf("plan = %#v, want the kept immutable subject reported as manual", before.Changes)
			}
			if err := engine.applyActions(context.Background(), repository, desired, Options{}); err != nil {
				t.Fatalf("applyActions() error = %v", err)
			}
			if len(service.oidcWrites) != tc.wantWrites {
				t.Fatalf("OIDC writes = %#v, want %d", service.oidcWrites, tc.wantWrites)
			}
			for _, write := range service.oidcWrites {
				if write.UseImmutableSubject != nil {
					t.Fatalf("OIDC write = %#v, want use_immutable_subject omitted", write)
				}
			}
			after := Plan{}
			appendActionsChanges(context.Background(), service, &after, desired, repository, config.RepositoryConfig{}, githubapi.RepositoryInfo{})
			if after.HasDrift() || after.HasBlocked() || !hasChange(after, "actions.oidcSubjectCustomization", ChangeNoop) {
				t.Fatalf("post-apply plan = %#v, want no drift and the customization converged", after.Changes)
			}
		})
	}
}

func TestActionsOIDCImmutableSubjectCanBeTurnedOn(t *testing.T) {
	enabled := true
	disabled := false
	service := newRepositorySettingsMemory()
	service.oidc = githubapi.OIDCSubjectCustomization{UseDefault: true, UseImmutableSubject: &disabled}
	desired := &config.ActionsConfig{OIDCSubjectCustomization: &config.OIDCSubjectCustomizationConfig{UseDefault: true, UseImmutableSubject: &enabled}}
	repository := githubapi.Repository{Owner: "octo", Name: "demo"}

	plan := Plan{}
	appendActionsChanges(context.Background(), service, &plan, desired, repository, config.RepositoryConfig{}, githubapi.RepositoryInfo{})
	if !hasChange(plan, "actions.oidcSubjectCustomization", ChangeUpdate) {
		t.Fatalf("plan = %#v, want an update turning the immutable subject on", plan.Changes)
	}
	if _, ok := findChange(plan, "actions.oidcSubjectCustomization.useImmutableSubject"); ok {
		t.Fatalf("plan = %#v, want no manual entry when the subject can be enabled", plan.Changes)
	}
}

func TestCustomEnvironmentProtectionRulesApplyThenPlanHasNoDrift(t *testing.T) {
	service := newRepositorySettingsMemory()
	service.availableIntegrations["production"] = []githubapi.AvailableDeploymentProtectionIntegration{{ID: 22, Slug: "release-approval"}}
	engine, err := New(service)
	if err != nil {
		t.Fatal(err)
	}
	rules := []string{"release-approval"}
	desired := &config.ActionsConfig{Environments: []config.Environment{{Name: "production", CustomProtectionRules: &rules}}}
	repository := githubapi.Repository{Owner: "octo", Name: "demo"}

	if err := engine.applyActions(context.Background(), repository, desired, Options{}); err != nil {
		t.Fatalf("applyActions() error = %v", err)
	}
	plan := Plan{}
	appendActionsChanges(context.Background(), service, &plan, desired, repository, config.RepositoryConfig{}, githubapi.RepositoryInfo{})
	if !hasChange(plan, "actions.environment.production.customProtectionRules", ChangeNoop) {
		t.Fatalf("post-apply actions plan = %#v, want custom protection rules no-op", plan.Changes)
	}
}

func TestCustomEnvironmentProtectionPlanBlocksUnavailableApp(t *testing.T) {
	service := newRepositorySettingsMemory()
	service.environments = []githubapi.Environment{{Name: "production"}}
	rules := []string{"approval-app"}
	desired := &config.ActionsConfig{Environments: []config.Environment{{Name: "production", CustomProtectionRules: &rules}}}
	plan := Plan{}
	appendActionsChanges(context.Background(), service, &plan, desired, githubapi.Repository{Owner: "octo", Name: "demo"}, config.RepositoryConfig{}, githubapi.RepositoryInfo{})
	change, ok := findChange(plan, "actions.environment.production.customProtectionRules")
	if !ok || change.State != ChangeBlocked {
		t.Fatalf("custom protection change = %#v, want blocked when app is unavailable", change)
	}
}
