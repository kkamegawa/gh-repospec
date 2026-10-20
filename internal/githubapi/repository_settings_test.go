package githubapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestRepositorySettingsEndpoints(t *testing.T) {
	t.Parallel()
	assertions := newHandlerAssertions(t)
	requests := []struct{ method, path string }{
		{http.MethodGet, "/repos/octo/demo/pages"},
		{http.MethodPost, "/repos/octo/demo/pages"},
		{http.MethodPut, "/repos/octo/demo/pages"},
		{http.MethodDelete, "/repos/octo/demo/pages"},
		{http.MethodGet, "/repos/octo/demo/hooks"},
		{http.MethodPost, "/repos/octo/demo/hooks"},
		{http.MethodPatch, "/repos/octo/demo/hooks/7"},
		{http.MethodGet, "/repos/octo/demo/autolinks"},
		{http.MethodPost, "/repos/octo/demo/autolinks"},
		{http.MethodDelete, "/repos/octo/demo/autolinks/4"},
		{http.MethodGet, "/repos/octo/demo/keys"},
		{http.MethodPost, "/repos/octo/demo/keys"},
		{http.MethodDelete, "/repos/octo/demo/keys/8"},
		{http.MethodGet, "/repos/octo/demo/collaborators"},
		{http.MethodPut, "/repos/octo/demo/collaborators/alice"},
		{http.MethodGet, "/repos/octo/demo/teams"},
		{http.MethodPut, "/orgs/octo/teams/reviewers/repos/octo/demo"},
		{http.MethodGet, "/repos/octo/demo/properties/values"},
		{http.MethodPatch, "/repos/octo/demo/properties/values"},
		{http.MethodGet, "/repos/octo/demo/branches"},
		{http.MethodGet, "/repos/octo/demo/branches/main/protection"},
		{http.MethodPut, "/repos/octo/demo/branches/main/protection"},
		{http.MethodGet, "/repos/octo/demo/branches/main/protection/required_signatures"},
		{http.MethodPost, "/repos/octo/demo/branches/main/protection/required_signatures"},
		{http.MethodGet, "/repos/octo/demo/actions/oidc/customization/sub"},
		{http.MethodPut, "/repos/octo/demo/actions/oidc/customization/sub"},
		{http.MethodGet, "/repos/octo/demo/environments/production/deployment_protection_rules"},
		{http.MethodGet, "/repos/octo/demo/environments/production/deployment_protection_rules/apps"},
		{http.MethodPost, "/repos/octo/demo/environments/production/deployment_protection_rules"},
		{http.MethodDelete, "/repos/octo/demo/environments/production/deployment_protection_rules/9"},
	}
	index := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if index >= len(requests) {
			assertions.Errorf("unexpected extra request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		want := requests[index]
		index++
		if r.Method != want.method || r.URL.Path != want.path {
			assertions.Errorf("request = %s %s, want %s %s", r.Method, r.URL.Path, want.method, want.path)
		}
		if r.Method == http.MethodDelete || r.Method == http.MethodPost && r.URL.Path == "/repos/octo/demo/branches/main/protection/required_signatures" || r.Method == http.MethodPut && r.URL.Path == "/repos/octo/demo/actions/oidc/customization/sub" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		switch r.URL.Path {
		case "/repos/octo/demo/pages":
			_ = json.NewEncoder(w).Encode(PagesSite{BuildType: "legacy"})
		case "/repos/octo/demo/hooks":
			if r.Method == http.MethodGet {
				_ = json.NewEncoder(w).Encode([]any{})
			} else {
				_ = json.NewEncoder(w).Encode(RepositoryWebhook{})
			}
		case "/repos/octo/demo/autolinks":
			if r.Method == http.MethodGet {
				_ = json.NewEncoder(w).Encode([]any{})
			} else {
				_ = json.NewEncoder(w).Encode(Autolink{})
			}
		case "/repos/octo/demo/keys":
			if r.Method == http.MethodGet {
				_ = json.NewEncoder(w).Encode([]any{})
			} else {
				_ = json.NewEncoder(w).Encode(DeployKey{})
			}
		case "/repos/octo/demo/collaborators", "/repos/octo/demo/teams", "/repos/octo/demo/properties/values", "/repos/octo/demo/branches":
			_ = json.NewEncoder(w).Encode([]any{})
		case "/repos/octo/demo/branches/main/protection":
			_ = json.NewEncoder(w).Encode(map[string]any{"enforce_admins": map[string]any{"enabled": true}})
		case "/repos/octo/demo/branches/main/protection/required_signatures":
			_ = json.NewEncoder(w).Encode(map[string]any{"enabled": true})
		case "/repos/octo/demo/environments/production/deployment_protection_rules":
			if r.Method == http.MethodGet {
				_ = json.NewEncoder(w).Encode(map[string]any{"total_count": 1, "custom_deployment_protection_rules": []any{map[string]any{"id": 9, "enabled": true, "app": map[string]any{"id": 12, "slug": "approval-app"}}}})
			} else {
				_ = json.NewEncoder(w).Encode(map[string]any{"id": 9, "enabled": true, "app": map[string]any{"id": 12, "slug": "approval-app"}})
			}
		case "/repos/octo/demo/environments/production/deployment_protection_rules/apps":
			_ = json.NewEncoder(w).Encode(map[string]any{"total_count": 1, "available_custom_deployment_protection_rule_integrations": []any{map[string]any{"id": 12, "slug": "approval-app"}}})
		default:
			_ = json.NewEncoder(w).Encode(map[string]any{})
		}
	}))
	defer server.Close()
	client, err := NewClient(server.Client(), server.URL)
	if err != nil {
		t.Fatal(err)
	}
	repo := Repository{Owner: "octo", Name: "demo"}
	if _, err := client.GetPages(context.Background(), repo); err != nil {
		t.Fatal(err)
	}
	if _, err := client.CreatePages(context.Background(), repo, PagesRequest{}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.UpdatePages(context.Background(), repo, PagesRequest{}); err != nil {
		t.Fatal(err)
	}
	if err := client.DeletePages(context.Background(), repo); err != nil {
		t.Fatal(err)
	}
	if _, err := client.ListWebhooks(context.Background(), repo); err != nil {
		t.Fatal(err)
	}
	if _, err := client.CreateWebhook(context.Background(), repo, RepositoryWebhook{}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.UpdateWebhook(context.Background(), repo, 7, RepositoryWebhook{}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.ListAutolinks(context.Background(), repo); err != nil {
		t.Fatal(err)
	}
	if _, err := client.CreateAutolink(context.Background(), repo, Autolink{}); err != nil {
		t.Fatal(err)
	}
	if err := client.DeleteAutolink(context.Background(), repo, 4); err != nil {
		t.Fatal(err)
	}
	if _, err := client.ListDeployKeys(context.Background(), repo); err != nil {
		t.Fatal(err)
	}
	if _, err := client.CreateDeployKey(context.Background(), repo, DeployKey{}); err != nil {
		t.Fatal(err)
	}
	if err := client.DeleteDeployKey(context.Background(), repo, 8); err != nil {
		t.Fatal(err)
	}
	if _, err := client.ListCollaborators(context.Background(), repo); err != nil {
		t.Fatal(err)
	}
	if err := client.SetCollaboratorPermission(context.Background(), repo, "alice", "push"); err != nil {
		t.Fatal(err)
	}
	if _, err := client.ListRepositoryTeams(context.Background(), repo); err != nil {
		t.Fatal(err)
	}
	if err := client.SetTeamRepositoryPermission(context.Background(), repo, "reviewers", "maintain"); err != nil {
		t.Fatal(err)
	}
	if _, err := client.GetCustomPropertyValues(context.Background(), repo); err != nil {
		t.Fatal(err)
	}
	if err := client.SetCustomPropertyValues(context.Background(), repo, []CustomPropertyValue{}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.ListProtectedBranches(context.Background(), repo); err != nil {
		t.Fatal(err)
	}
	if _, err := client.GetBranchProtection(context.Background(), repo, "main"); err != nil {
		t.Fatal(err)
	}
	if err := client.SetBranchProtection(context.Background(), repo, "main", map[string]any{}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.GetRequiredSignatures(context.Background(), repo, "main"); err != nil {
		t.Fatal(err)
	}
	if err := client.SetRequiredSignatures(context.Background(), repo, "main", true); err != nil {
		t.Fatal(err)
	}
	if _, err := client.GetOIDCSubjectCustomization(context.Background(), repo); err != nil {
		t.Fatal(err)
	}
	if err := client.SetOIDCSubjectCustomization(context.Background(), repo, OIDCSubjectCustomization{UseDefault: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.ListDeploymentProtectionRules(context.Background(), repo, "production"); err != nil {
		t.Fatal(err)
	}
	if _, err := client.ListAvailableDeploymentProtectionIntegrations(context.Background(), repo, "production"); err != nil {
		t.Fatal(err)
	}
	if _, err := client.EnableDeploymentProtectionIntegration(context.Background(), repo, "production", 12); err != nil {
		t.Fatal(err)
	}
	if err := client.DisableDeploymentProtectionRule(context.Background(), repo, "production", 9); err != nil {
		t.Fatal(err)
	}
	if index != len(requests) {
		t.Fatalf("received %d API requests, want %d", index, len(requests))
	}
}

func TestRepositoryInteractionSettingsEndpoints(t *testing.T) {
	t.Parallel()
	requests := []struct{ method, path string }{
		{http.MethodGet, "/repos/octo/demo/interaction-limits"},
		{http.MethodPut, "/repos/octo/demo/interaction-limits"},
		{http.MethodDelete, "/repos/octo/demo/interaction-limits"},
		{http.MethodGet, "/repos/octo/demo/interaction-limits/pulls/creation-cap"},
		{http.MethodPatch, "/repos/octo/demo/interaction-limits/pulls/creation-cap"},
		{http.MethodGet, "/repos/octo/demo/interaction-limits/pulls/bypass-list"},
		{http.MethodPut, "/repos/octo/demo/interaction-limits/pulls/bypass-list"},
		{http.MethodDelete, "/repos/octo/demo/interaction-limits/pulls/bypass-list"},
	}
	index := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if index >= len(requests) {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		want := requests[index]
		index++
		if r.Method != want.method || r.URL.Path != want.path {
			t.Errorf("request = %s %s, want %s %s", r.Method, r.URL.Path, want.method, want.path)
		}
		switch r.URL.Path {
		case "/repos/octo/demo/interaction-limits":
			switch r.Method {
			case http.MethodDelete:
				w.WriteHeader(http.StatusNoContent)
			case http.MethodPut:
				var body RepositoryInteractionLimitRequest
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Errorf("decode interaction limit request: %v", err)
				} else if body.Limit != "contributors_only" || body.Expiry != "one_week" {
					t.Errorf("interaction limit request = %#v", body)
				}
				_ = json.NewEncoder(w).Encode(RepositoryInteractionLimit{Limit: "contributors_only", Origin: "repository"})
			default:
				_ = json.NewEncoder(w).Encode(RepositoryInteractionLimit{Limit: "contributors_only", Origin: "repository", ExpiresAt: "2030-01-01T00:00:00Z"})
			}
		case "/repos/octo/demo/interaction-limits/pulls/creation-cap":
			if r.Method == http.MethodPatch {
				var body map[string]json.RawMessage
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Errorf("decode pull request cap request: %v", err)
				} else if string(body["include_drafts"]) != "false" {
					t.Errorf("include_drafts request value = %s, want explicit false", body["include_drafts"])
				}
			}
			_ = json.NewEncoder(w).Encode(PullRequestCreationCap{Enabled: true, MaxOpenPullRequests: 3, IncludeDrafts: false})
		case "/repos/octo/demo/interaction-limits/pulls/bypass-list":
			if r.Method == http.MethodGet {
				_ = json.NewEncoder(w).Encode([]InteractionBypassUser{{Login: "octocat"}})
			} else {
				var body struct {
					Users []string `json:"users"`
				}
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Errorf("decode bypass user request: %v", err)
				} else if !reflect.DeepEqual(body.Users, []string{"octocat"}) {
					t.Errorf("bypass user request = %v", body.Users)
				}
				w.WriteHeader(http.StatusNoContent)
			}
		}
	}))
	defer server.Close()
	client, err := NewClient(server.Client(), server.URL)
	if err != nil {
		t.Fatal(err)
	}
	repo := Repository{Owner: "octo", Name: "demo"}
	if _, err := client.GetRepositoryInteractionLimit(context.Background(), repo); err != nil {
		t.Fatal(err)
	}
	if _, err := client.SetRepositoryInteractionLimit(context.Background(), repo, RepositoryInteractionLimitRequest{Limit: "contributors_only", Expiry: "one_week"}); err != nil {
		t.Fatal(err)
	}
	if err := client.DeleteRepositoryInteractionLimit(context.Background(), repo); err != nil {
		t.Fatal(err)
	}
	if _, err := client.GetPullRequestCreationCap(context.Background(), repo); err != nil {
		t.Fatal(err)
	}
	maximum := 3
	includeDrafts := false
	if _, err := client.UpdatePullRequestCreationCap(context.Background(), repo, PullRequestCreationCapRequest{Enabled: true, MaxOpenPullRequests: &maximum, IncludeDrafts: &includeDrafts}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.ListPullRequestCreationCapBypassUsers(context.Background(), repo); err != nil {
		t.Fatal(err)
	}
	if err := client.AddPullRequestCreationCapBypassUsers(context.Background(), repo, []string{"octocat"}); err != nil {
		t.Fatal(err)
	}
	if err := client.RemovePullRequestCreationCapBypassUsers(context.Background(), repo, []string{"octocat"}); err != nil {
		t.Fatal(err)
	}
	if index != len(requests) {
		t.Fatalf("received %d API requests, want %d", index, len(requests))
	}
}

func TestBranchProtectionMatchesKeepsAnyAppDistinctFromPinnedApp(t *testing.T) {
	pinned := map[string]any{"required_status_checks": map[string]any{"strict": true, "checks": []any{map[string]any{"context": "build", "app_id": 15368}}}}
	anyApp := map[string]any{"required_status_checks": map[string]any{"strict": true, "checks": []any{map[string]any{"context": "build", "app_id": nil}}}}
	for name, tc := range map[string]struct {
		check  map[string]any
		actual map[string]any
		want   bool
	}{
		"null against pinned":       {check: map[string]any{"context": "build", "app_id": nil}, actual: pinned, want: false},
		"-1 against pinned":         {check: map[string]any{"context": "build", "app_id": -1}, actual: pinned, want: false},
		"null against any app":      {check: map[string]any{"context": "build", "app_id": nil}, actual: anyApp, want: true},
		"-1 against any app":        {check: map[string]any{"context": "build", "app_id": -1}, actual: anyApp, want: true},
		"pinned against any app":    {check: map[string]any{"context": "build", "app_id": 15368}, actual: anyApp, want: false},
		"undeclared against pinned": {check: map[string]any{"context": "build"}, actual: pinned, want: true},
	} {
		t.Run(name, func(t *testing.T) {
			desired := map[string]any{"required_status_checks": map[string]any{"strict": true, "checks": []any{tc.check}}}
			if got := BranchProtectionMatches(desired, tc.actual); got != tc.want {
				t.Fatalf("BranchProtectionMatches() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestNormalizeBranchProtectionForUpdate(t *testing.T) {
	input := map[string]any{
		"url": "response-only", "enforce_admins": map[string]any{"url": "response-only", "enabled": true},
		"required_status_checks": map[string]any{"url": "response-only", "contexts_url": "response-only", "enforcement_level": "non_admins", "strict": true, "contexts": []any{"ci"}},
		"required_pull_request_reviews": map[string]any{
			"dismissal_restrictions": map[string]any{"users_url": "response-only", "users": []any{map[string]any{"login": "alice"}}, "teams": []any{map[string]any{"slug": "reviewers"}}},
		},
		"restrictions":            map[string]any{"url": "response-only", "users": []any{map[string]any{"login": "alice"}}, "teams": []any{map[string]any{"slug": "reviewers"}}},
		"required_linear_history": map[string]any{"enabled": false}, "required_signatures": map[string]any{"enabled": true},
	}
	got := NormalizeBranchProtection(input)
	checks := got["required_status_checks"].(map[string]any)
	if checks["url"] != nil || checks["enforcement_level"] != nil || checks["strict"] != true {
		t.Fatalf("status check normalization = %#v", checks)
	}
	admins, ok := got["enforce_admins"].(bool)
	if !ok || !admins {
		t.Fatalf("enforce_admins = %#v", got["enforce_admins"])
	}
	linear, ok := got["required_linear_history"].(bool)
	if !ok || linear {
		t.Fatalf("required_linear_history = %#v", got["required_linear_history"])
	}
	if got["required_signatures"] != nil {
		t.Fatalf("required_signatures remained in update body: %#v", got)
	}
	restrictions := got["restrictions"].(map[string]any)
	if !reflect.DeepEqual(restrictions["users"], []any{"alice"}) || !reflect.DeepEqual(restrictions["teams"], []any{"reviewers"}) {
		t.Fatalf("restrictions actor normalization = %#v", restrictions)
	}
	if input["url"] != "response-only" {
		t.Fatal("normalization mutated input")
	}
}
