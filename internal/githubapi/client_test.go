package githubapi

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestRepositoryOperationsUseVersionedGitHubRequests(t *testing.T) {
	t.Parallel()
	assertions := newHandlerAssertions(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.Header.Get("X-GitHub-Api-Version"), APIVersion; got != want {
			assertions.Errorf("API version = %q, want %q", got, want)
		}
		if got, want := r.Header.Get("Accept"), "application/vnd.github+json"; got != want {
			assertions.Errorf("Accept = %q, want %q", got, want)
		}
		if got, want := r.URL.Path, "/repos/octo/demo"; got != want {
			assertions.Errorf("path = %q, want %q", got, want)
		}
		if r.Method != http.MethodPatch {
			assertions.Errorf("method = %q, want %q", r.Method, http.MethodPatch)
		}
		_ = json.NewEncoder(w).Encode(RepositoryInfo{Name: "demo", FullName: "octo/demo", DefaultBranch: "main"})
	}))
	defer server.Close()

	client, err := NewClient(server.Client(), server.URL)
	if err != nil {
		t.Fatal(err)
	}
	info, err := client.UpdateRepository(context.Background(), Repository{Owner: "octo", Name: "demo"}, UpdateRepositoryRequest{DefaultBranch: "main"})
	if err != nil {
		t.Fatal(err)
	}
	if info.FullName != "octo/demo" {
		t.Fatalf("full name = %q", info.FullName)
	}
}

func TestAgentSecretAndVariableEndpoints(t *testing.T) {
	t.Parallel()
	var secretRequest, variableRequest bool
	assertions := newHandlerAssertions(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/octo/demo/agents/secrets/KEY":
			secretRequest = r.Method == http.MethodPut
			w.WriteHeader(http.StatusNoContent)
		case "/repos/octo/demo/agents/variables/REGION":
			variableRequest = r.Method == http.MethodPatch
			w.WriteHeader(http.StatusNoContent)
		default:
			assertions.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()
	client, _ := NewClient(server.Client(), server.URL)
	repo := Repository{Owner: "octo", Name: "demo"}
	if err := client.PutAgentSecret(context.Background(), repo, "KEY", EncryptedSecret{KeyID: "1", EncryptedValue: "ciphertext"}); err != nil {
		t.Fatal(err)
	}
	if err := client.UpdateAgentVariable(context.Background(), repo, AgentVariable{Name: "REGION", Value: "japan"}); err != nil {
		t.Fatal(err)
	}
	if !secretRequest || !variableRequest {
		t.Fatalf("agent calls were not made: secret=%t variable=%t", secretRequest, variableRequest)
	}
}

func TestNewAuthenticatedClientRejectsEnterpriseServerHost(t *testing.T) {
	// Not t.Parallel(): t.Setenv forbids parallel execution.
	t.Setenv("GH_HOST", "github.example.com")
	_, err := NewAuthenticatedClient()
	if err == nil || !strings.Contains(err.Error(), "unsupported GitHub host") {
		t.Fatalf("NewAuthenticatedClient() error = %v, want an unsupported host error", err)
	}
}

func TestRetriesIdempotentRateLimitedRequest(t *testing.T) {
	t.Parallel()
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requests.Add(1) == 1 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"message":"rate limited"}`))
			return
		}
		_ = json.NewEncoder(w).Encode(ActionsPermissions{Enabled: true})
	}))
	defer server.Close()
	client, _ := NewClient(server.Client(), server.URL)
	client.SetRetryPolicy(RetryPolicy{MaxAttempts: 2, BaseDelay: time.Millisecond})
	var waits []time.Duration
	client.setRetryWait(func(_ context.Context, delay time.Duration) error {
		waits = append(waits, delay)
		return nil
	})
	settings, err := client.GetActionsPermissions(context.Background(), Repository{Owner: "octo", Name: "demo"})
	if err != nil {
		t.Fatal(err)
	}
	if !settings.Enabled || requests.Load() != 2 {
		t.Fatalf("settings=%+v requests=%d", settings, requests.Load())
	}
	if !slices.Equal(waits, []time.Duration{time.Second}) {
		t.Fatalf("retry waits = %v, want [%v]", waits, time.Second)
	}
}

func TestPostIsNotRetried(t *testing.T) {
	t.Parallel()
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"message":"temporary failure"}`))
	}))
	defer server.Close()
	client, _ := NewClient(server.Client(), server.URL)
	client.SetRetryPolicy(RetryPolicy{MaxAttempts: 3, BaseDelay: time.Millisecond})
	_, err := client.CreateRepository(context.Background(), CreateRepositoryRequest{Name: "demo"})
	if err == nil || requests.Load() != 1 {
		t.Fatalf("err=%v requests=%d", err, requests.Load())
	}
}

func TestTypedNotFoundError(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-GitHub-Request-Id", "request-id")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"message":"Not Found"}`))
	}))
	defer server.Close()
	client, _ := NewClient(server.Client(), server.URL)
	_, err := client.GetRepository(context.Background(), Repository{Owner: "octo", Name: "missing"})
	if !IsNotFound(err) {
		t.Fatalf("expected typed not found, got %v", err)
	}
	apiErr := err.(*APIError)
	if apiErr.RequestID != "request-id" {
		t.Fatalf("request ID = %q", apiErr.RequestID)
	}
}

func TestCommitFilesCreatesOneNonForcedCommit(t *testing.T) {
	t.Parallel()
	var requests []string
	assertions := newHandlerAssertions(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.RequestURI())
		switch r.Method + " " + r.URL.Path {
		case "POST /repos/octo/demo/git/blobs":
			var request struct {
				Content  string `json:"content"`
				Encoding string `json:"encoding"`
			}
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				assertions.Errorf("decode blob request: %v", err)
			}
			if request.Encoding != "base64" || request.Content == "" {
				assertions.Errorf("blob request = %+v", request)
			}
			_ = json.NewEncoder(w).Encode(GitBlob{SHA: "blob-" + request.Content})
		case "GET /repos/octo/demo/git/ref/heads/main":
			_ = json.NewEncoder(w).Encode(GitReference{Ref: "refs/heads/main", Object: GitObject{SHA: "old"}})
		case "GET /repos/octo/demo/git/commits/old":
			_ = json.NewEncoder(w).Encode(GitCommit{SHA: "old", Tree: GitObject{SHA: "base-tree"}})
		case "POST /repos/octo/demo/git/trees":
			_ = json.NewEncoder(w).Encode(GitTree{SHA: "new-tree"})
		case "POST /repos/octo/demo/git/commits":
			_ = json.NewEncoder(w).Encode(GitCommit{SHA: "new-commit"})
		case "PATCH /repos/octo/demo/git/refs/heads/main":
			var request struct {
				SHA   string `json:"sha"`
				Force bool   `json:"force"`
			}
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				assertions.Errorf("decode ref request: %v", err)
			}
			if request.SHA != "new-commit" || request.Force {
				assertions.Errorf("ref request = %+v", request)
			}
			_ = json.NewEncoder(w).Encode(GitReference{Object: GitObject{SHA: request.SHA}})
		default:
			assertions.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	client, err := NewClient(server.Client(), server.URL)
	if err != nil {
		t.Fatal(err)
	}
	result, err := client.CommitFiles(context.Background(), Repository{Owner: "octo", Name: "demo"}, "main", "bootstrap", []File{
		{Path: "z.txt", Content: []byte("z")},
		{Path: "a.txt", Content: []byte("a"), Mode: "100755"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result != (CommitResult{CommitSHA: "new-commit", TreeSHA: "new-tree", PreviousSHA: "old"}) {
		t.Fatalf("result = %+v", result)
	}
	want := []string{
		"POST /repos/octo/demo/git/blobs",
		"POST /repos/octo/demo/git/blobs",
		"GET /repos/octo/demo/git/ref/heads/main",
		"GET /repos/octo/demo/git/commits/old",
		"POST /repos/octo/demo/git/trees",
		"POST /repos/octo/demo/git/commits",
		"PATCH /repos/octo/demo/git/refs/heads/main",
	}
	if !slices.Equal(requests, want) {
		t.Fatalf("requests = %#v, want %#v", requests, want)
	}
}

func TestGetTreePreservesRecursiveQuery(t *testing.T) {
	t.Parallel()
	assertions := newHandlerAssertions(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.URL.Query().Get("recursive"), "1"; got != want {
			assertions.Errorf("recursive = %q, want %q", got, want)
		}
		_ = json.NewEncoder(w).Encode(GitTree{SHA: "tree"})
	}))
	defer server.Close()
	client, _ := NewClient(server.Client(), server.URL)
	if _, err := client.GetTree(context.Background(), Repository{Owner: "octo", Name: "demo"}, "tree", true); err != nil {
		t.Fatal(err)
	}
}

func TestMaterializeTemplateArchiveStripsGitHubRootDirectory(t *testing.T) {
	t.Parallel()
	archive := templateArchive(t, map[string]string{
		"template-sha/":             "",
		"template-sha/README.md":    "hello",
		"template-sha/.github/x.md": "settings",
	})
	assertions := newHandlerAssertions(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.URL.Path, "/repos/octo/template/zipball/main"; got != want {
			assertions.Errorf("path = %q, want %q", got, want)
		}
		w.Header().Set("Content-Type", "application/zip")
		_, _ = w.Write(archive)
	}))
	defer server.Close()
	client, _ := NewClient(server.Client(), server.URL)
	destination := filepath.Join(t.TempDir(), "materialized")
	if err := client.MaterializeTemplateArchive(context.Background(), Repository{Owner: "octo", Name: "template"}, "main", destination); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(filepath.Join(destination, "README.md")) //nolint:gosec // destination is a t.TempDir path
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(content), "hello"; got != want {
		t.Fatalf("README = %q, want %q", got, want)
	}
	if _, err := os.Stat(filepath.Join(destination, "template-sha")); !os.IsNotExist(err) {
		t.Fatalf("archive root was not removed: %v", err)
	}
}

func TestMaterializeTemplateArchiveRejectsProhibitedGitMetadata(t *testing.T) {
	t.Parallel()
	archive := templateArchive(t, map[string]string{
		"template-sha/.git/config": "metadata",
	})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/zip")
		_, _ = w.Write(archive)
	}))
	defer server.Close()

	client, err := NewClient(server.Client(), server.URL)
	if err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(t.TempDir(), "materialized")
	err = client.MaterializeTemplateArchive(context.Background(), Repository{Owner: "octo", Name: "template"}, "main", destination)
	if err == nil || !strings.Contains(err.Error(), ".git") {
		t.Fatalf("MaterializeTemplateArchive() error = %v, want prohibited .git path", err)
	}
	if _, err := os.Stat(destination); !os.IsNotExist(err) {
		t.Fatalf("unsafe archive created destination: %v", err)
	}
}

// TestListLabelsWalksEveryPage guards listAll: the client exposes no response
// headers, so it must stop paging on the first short page rather than relying
// on a Link header, and it must request pages in order.
func TestListLabelsWalksEveryPage(t *testing.T) {
	t.Parallel()
	assertions := newHandlerAssertions(t)
	var requests []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.URL.RawQuery)
		if got, want := r.URL.Path, "/repos/octo/demo/labels"; got != want {
			assertions.Errorf("path = %q, want %q", got, want)
		}
		page := r.URL.Query().Get("page")
		if got, want := r.URL.Query().Get("per_page"), "100"; got != want {
			assertions.Errorf("per_page = %q, want %q", got, want)
		}
		var labels []Label
		switch page {
		case "1":
			for i := 0; i < 100; i++ {
				labels = append(labels, Label{Name: fmt.Sprintf("label-%d", i), Color: "d73a4a"})
			}
		case "2":
			labels = []Label{{Name: "extra-1", Color: "d73a4a"}, {Name: "extra-2", Color: "d73a4a"}, {Name: "extra-3", Color: "d73a4a"}}
		default:
			assertions.Errorf("unexpected page %q", page)
		}
		_ = json.NewEncoder(w).Encode(labels)
	}))
	defer server.Close()

	client, err := NewClient(server.Client(), server.URL)
	if err != nil {
		t.Fatal(err)
	}
	labels, err := client.ListLabels(context.Background(), Repository{Owner: "octo", Name: "demo"})
	if err != nil {
		t.Fatal(err)
	}
	if len(labels) != 103 {
		t.Fatalf("len(labels) = %d, want 103", len(labels))
	}
	want := []string{"per_page=100&page=1", "per_page=100&page=2"}
	if !slices.Equal(requests, want) {
		t.Fatalf("requests = %#v, want %#v", requests, want)
	}
}

func TestLabelAndMilestoneEndpoints(t *testing.T) {
	t.Parallel()
	assertions := newHandlerAssertions(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "POST /repos/octo/demo/labels":
			var label Label
			if err := json.NewDecoder(r.Body).Decode(&label); err != nil {
				assertions.Errorf("decode label request: %v", err)
			}
			if label.Name != "kind/bug" || label.Color != "d73a4a" {
				assertions.Errorf("label request = %+v", label)
			}
			_ = json.NewEncoder(w).Encode(label)
		case "PATCH /repos/octo/demo/labels/kind/bug":
			_ = json.NewEncoder(w).Encode(Label{Name: "kind/bug", Color: "425df5"})
		case "POST /repos/octo/demo/milestones":
			var milestone Milestone
			if err := json.NewDecoder(r.Body).Decode(&milestone); err != nil {
				assertions.Errorf("decode milestone request: %v", err)
			}
			if milestone.Title != "v1.0" || milestone.DueOn != "2026-12-01T00:00:00Z" {
				assertions.Errorf("milestone request = %+v", milestone)
			}
			_ = json.NewEncoder(w).Encode(Milestone{Number: 1, Title: milestone.Title})
		case "PATCH /repos/octo/demo/milestones/1":
			var milestone Milestone
			if err := json.NewDecoder(r.Body).Decode(&milestone); err != nil {
				assertions.Errorf("decode milestone update request: %v", err)
			}
			if milestone.State != "closed" {
				assertions.Errorf("milestone update request = %+v", milestone)
			}
			_ = json.NewEncoder(w).Encode(Milestone{Number: 1, Title: "v1.0", State: "closed"})
		default:
			assertions.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	client, err := NewClient(server.Client(), server.URL)
	if err != nil {
		t.Fatal(err)
	}
	repo := Repository{Owner: "octo", Name: "demo"}
	if _, err := client.CreateLabel(context.Background(), repo, Label{Name: "kind/bug", Color: "d73a4a"}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.UpdateLabel(context.Background(), repo, "kind/bug", Label{Name: "kind/bug", Color: "425df5"}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.CreateMilestone(context.Background(), repo, Milestone{Title: "v1.0", DueOn: "2026-12-01T00:00:00Z"}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.UpdateMilestone(context.Background(), repo, 1, Milestone{Title: "v1.0", State: "closed"}); err != nil {
		t.Fatal(err)
	}
}

// TestImmutableReleasesTogglesWithPutAndDelete mirrors the PUT/DELETE toggle
// idiom used for the other repository security feature switches.
func TestImmutableReleasesTogglesWithPutAndDelete(t *testing.T) {
	t.Parallel()
	assertions := newHandlerAssertions(t)
	var methods []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.URL.Path, "/repos/octo/demo/immutable-releases"; got != want {
			assertions.Errorf("path = %q, want %q", got, want)
		}
		methods = append(methods, r.Method)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client, err := NewClient(server.Client(), server.URL)
	if err != nil {
		t.Fatal(err)
	}
	repo := Repository{Owner: "octo", Name: "demo"}
	if err := client.SetImmutableReleases(context.Background(), repo, true); err != nil {
		t.Fatal(err)
	}
	if err := client.SetImmutableReleases(context.Background(), repo, false); err != nil {
		t.Fatal(err)
	}
	want := []string{http.MethodPut, http.MethodDelete}
	if !slices.Equal(methods, want) {
		t.Fatalf("methods = %#v, want %#v", methods, want)
	}
}

func TestGetTeamAndAppResolveIDs(t *testing.T) {
	t.Parallel()
	assertions := newHandlerAssertions(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/orgs/octo-org/teams/platform":
			_ = json.NewEncoder(w).Encode(Team{ID: 9, Slug: "platform"})
		case "/apps/renovate":
			_ = json.NewEncoder(w).Encode(App{ID: 7, Slug: "renovate"})
		default:
			assertions.Errorf("unexpected path %q", r.URL.Path)
		}
	}))
	defer server.Close()

	client, err := NewClient(server.Client(), server.URL)
	if err != nil {
		t.Fatal(err)
	}
	team, err := client.GetTeam(context.Background(), "octo-org", "platform")
	if err != nil {
		t.Fatal(err)
	}
	if team.ID != 9 {
		t.Fatalf("team.ID = %d, want 9", team.ID)
	}
	app, err := client.GetApp(context.Background(), "renovate")
	if err != nil {
		t.Fatal(err)
	}
	if app.ID != 7 {
		t.Fatalf("app.ID = %d, want 7", app.ID)
	}
}

func TestListCustomRepositoryRolesUnwrapsResponse(t *testing.T) {
	t.Parallel()
	assertions := newHandlerAssertions(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.URL.Path, "/orgs/octo-org/custom-repository-roles"; got != want {
			assertions.Errorf("path = %q, want %q", got, want)
		}
		_, _ = w.Write([]byte(`{"total_count":1,"custom_roles":[{"id":501,"name":"release-manager"}]}`))
	}))
	defer server.Close()

	client, err := NewClient(server.Client(), server.URL)
	if err != nil {
		t.Fatal(err)
	}
	roles, err := client.ListCustomRepositoryRoles(context.Background(), "octo-org")
	if err != nil {
		t.Fatal(err)
	}
	if len(roles) != 1 || roles[0].ID != 501 || roles[0].Name != "release-manager" {
		t.Fatalf("roles = %#v, want one role {501 release-manager}", roles)
	}
}

func TestForkPullRequestApprovalEndpoints(t *testing.T) {
	t.Parallel()
	assertions := newHandlerAssertions(t)
	var putBody ForkPullRequestApproval
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.URL.Path, "/repos/octo/demo/actions/permissions/fork-pr-contributor-approval"; got != want {
			assertions.Errorf("path = %q, want %q", got, want)
		}
		switch r.Method {
		case http.MethodGet:
			_ = json.NewEncoder(w).Encode(ForkPullRequestApproval{ApprovalPolicy: "first_time_contributors"})
		case http.MethodPut:
			_ = json.NewDecoder(r.Body).Decode(&putBody)
			w.WriteHeader(http.StatusNoContent)
		default:
			assertions.Errorf("unexpected method %q", r.Method)
		}
	}))
	defer server.Close()

	client, err := NewClient(server.Client(), server.URL)
	if err != nil {
		t.Fatal(err)
	}
	repo := Repository{Owner: "octo", Name: "demo"}
	actual, err := client.GetForkPullRequestApproval(context.Background(), repo)
	if err != nil {
		t.Fatal(err)
	}
	if actual.ApprovalPolicy != "first_time_contributors" {
		t.Fatalf("ApprovalPolicy = %q, want %q", actual.ApprovalPolicy, "first_time_contributors")
	}
	if err := client.SetForkPullRequestApproval(context.Background(), repo, ForkPullRequestApproval{ApprovalPolicy: "all_external_contributors"}); err != nil {
		t.Fatal(err)
	}
	if putBody.ApprovalPolicy != "all_external_contributors" {
		t.Fatalf("PUT body ApprovalPolicy = %q, want %q", putBody.ApprovalPolicy, "all_external_contributors")
	}
}

func TestGetVulnerabilityAlertsEnabledTreats404AsDisabled(t *testing.T) {
	t.Parallel()
	assertions := newHandlerAssertions(t)
	var status int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.URL.Path, "/repos/octo/demo/vulnerability-alerts"; got != want {
			assertions.Errorf("path = %q, want %q", got, want)
		}
		w.WriteHeader(status)
	}))
	defer server.Close()

	client, err := NewClient(server.Client(), server.URL)
	if err != nil {
		t.Fatal(err)
	}
	repo := Repository{Owner: "octo", Name: "demo"}

	status = http.StatusNoContent
	enabled, err := client.GetVulnerabilityAlertsEnabled(context.Background(), repo)
	if err != nil {
		t.Fatal(err)
	}
	if !enabled {
		t.Fatal("204 response should report alerts enabled")
	}

	status = http.StatusNotFound
	enabled, err = client.GetVulnerabilityAlertsEnabled(context.Background(), repo)
	if err != nil {
		t.Fatalf("404 should not be an error, got %v", err)
	}
	if enabled {
		t.Fatal("404 response should report alerts disabled")
	}
}

func TestIsNotFoundRecognizesWrappedAPIError(t *testing.T) {
	t.Parallel()
	wrapped := fmt.Errorf("get repository: %w", &APIError{StatusCode: 404})
	if !IsNotFound(wrapped) {
		t.Fatal("IsNotFound did not recognize a wrapped *APIError")
	}
}

func TestListRulesetsWalksEveryPage(t *testing.T) {
	t.Parallel()
	assertions := newHandlerAssertions(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.URL.Path, "/repos/octo/demo/rulesets"; got != want {
			assertions.Errorf("path = %q, want %q", got, want)
		}
		var rulesets []Ruleset
		switch r.URL.Query().Get("page") {
		case "1":
			for i := 0; i < 100; i++ {
				rulesets = append(rulesets, Ruleset{ID: int64(i), Name: fmt.Sprintf("ruleset-%d", i), Enforcement: "active"})
			}
		case "2":
			rulesets = []Ruleset{{ID: 100, Name: "ruleset-100", Enforcement: "active"}}
		default:
			assertions.Errorf("unexpected page %q", r.URL.Query().Get("page"))
		}
		_ = json.NewEncoder(w).Encode(rulesets)
	}))
	defer server.Close()

	client, err := NewClient(server.Client(), server.URL)
	if err != nil {
		t.Fatal(err)
	}
	rulesets, err := client.ListRulesets(context.Background(), Repository{Owner: "octo", Name: "demo"})
	if err != nil {
		t.Fatal(err)
	}
	if len(rulesets) != 101 {
		t.Fatalf("len(rulesets) = %d, want 101", len(rulesets))
	}
}

func TestCreateRulesetSendsBypassActorsAndEvaluateEnforcement(t *testing.T) {
	t.Parallel()
	assertions := newHandlerAssertions(t)
	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.URL.Path, "/repos/octo/demo/rulesets"; got != want {
			assertions.Errorf("path = %q, want %q", got, want)
		}
		if r.Method != http.MethodPost {
			assertions.Errorf("method = %q, want POST", r.Method)
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		_ = json.NewEncoder(w).Encode(Ruleset{ID: 1, Name: "protect-main", Enforcement: "evaluate"})
	}))
	defer server.Close()

	client, err := NewClient(server.Client(), server.URL)
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.CreateRuleset(context.Background(), Repository{Owner: "octo", Name: "demo"}, Ruleset{
		Name:        "protect-main",
		Enforcement: "evaluate",
		BypassActors: []BypassActor{
			{ActorID: 5, ActorType: "RepositoryRole", BypassMode: "pull_request"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := body["enforcement"], "evaluate"; got != want {
		t.Fatalf("enforcement = %v, want %v", got, want)
	}
	actors, ok := body["bypass_actors"].([]any)
	if !ok || len(actors) != 1 {
		t.Fatalf("bypass_actors = %v, want one entry", body["bypass_actors"])
	}
	entry, ok := actors[0].(map[string]any)
	if !ok || entry["actor_id"] != float64(5) || entry["actor_type"] != "RepositoryRole" || entry["bypass_mode"] != "pull_request" {
		t.Fatalf("bypass_actors[0] = %v, want actor_id 5, actor_type RepositoryRole, bypass_mode pull_request", entry)
	}
}

func TestGetRulesetRetrievesFullDefinition(t *testing.T) {
	t.Parallel()
	assertions := newHandlerAssertions(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.URL.Path, "/repos/octo/demo/rulesets/42"; got != want {
			assertions.Errorf("path = %q, want %q", got, want)
		}
		if r.Method != http.MethodGet {
			assertions.Errorf("method = %q, want GET", r.Method)
		}
		_, _ = w.Write([]byte(`{
			"id": 42, "name": "protect-main", "target": "branch", "enforcement": "active",
			"source_type": "Repository",
			"rules": [{"type": "deletion"}],
			"bypass_actors": [{"actor_id": 5, "actor_type": "RepositoryRole", "bypass_mode": "always"}]
		}`))
	}))
	defer server.Close()

	client, err := NewClient(server.Client(), server.URL)
	if err != nil {
		t.Fatal(err)
	}
	ruleset, err := client.GetRuleset(context.Background(), Repository{Owner: "octo", Name: "demo"}, 42)
	if err != nil {
		t.Fatal(err)
	}
	if ruleset.SourceType != "Repository" {
		t.Fatalf("SourceType = %q, want %q", ruleset.SourceType, "Repository")
	}
	if len(ruleset.Rules) != 1 || len(ruleset.BypassActors) != 1 {
		t.Fatalf("GetRuleset() = %#v, want the full rules and bypass_actors the list endpoint omits", ruleset)
	}

	if _, err := client.GetRuleset(context.Background(), Repository{Owner: "octo", Name: "demo"}, 0); err == nil {
		t.Fatal("GetRuleset() with a non-positive ID unexpectedly succeeded")
	}
}

func TestListEnvironmentsWalksEveryPage(t *testing.T) {
	t.Parallel()
	assertions := newHandlerAssertions(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.URL.Path, "/repos/octo/demo/environments"; got != want {
			assertions.Errorf("path = %q, want %q", got, want)
		}
		if got, want := r.URL.Query().Get("per_page"), "100"; got != want {
			assertions.Errorf("per_page = %q, want %q", got, want)
		}
		var environments []Environment
		switch r.URL.Query().Get("page") {
		case "1":
			for i := 0; i < 100; i++ {
				environments = append(environments, Environment{Name: fmt.Sprintf("env-%d", i)})
			}
		case "2":
			environments = []Environment{{Name: "production"}}
		default:
			assertions.Errorf("unexpected page %q", r.URL.Query().Get("page"))
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"total_count": len(environments), "environments": environments})
	}))
	defer server.Close()

	client, err := NewClient(server.Client(), server.URL)
	if err != nil {
		t.Fatal(err)
	}
	environments, err := client.ListEnvironments(context.Background(), Repository{Owner: "octo", Name: "demo"})
	if err != nil {
		t.Fatal(err)
	}
	if len(environments) != 101 {
		t.Fatalf("len(environments) = %d, want 101", len(environments))
	}
	if environments[100].Name != "production" {
		t.Fatalf("environments[100].Name = %q, want %q", environments[100].Name, "production")
	}
}

func TestListActionVariablesAndSecretsWalkEveryPage(t *testing.T) {
	t.Parallel()
	assertions := newHandlerAssertions(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page := r.URL.Query().Get("page")
		switch r.URL.Path {
		case "/repos/octo/demo/actions/variables":
			var variables []ActionVariable
			if page == "1" {
				for i := 0; i < 100; i++ {
					variables = append(variables, ActionVariable{Name: fmt.Sprintf("VAR_%d", i), Value: "x"})
				}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"total_count": len(variables), "variables": variables})
		case "/repos/octo/demo/actions/secrets":
			var secrets []SecretMetadata
			if page == "1" {
				for i := 0; i < 100; i++ {
					secrets = append(secrets, SecretMetadata{Name: fmt.Sprintf("SECRET_%d", i)})
				}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"total_count": len(secrets), "secrets": secrets})
		default:
			assertions.Errorf("unexpected path %q", r.URL.Path)
		}
	}))
	defer server.Close()

	client, err := NewClient(server.Client(), server.URL)
	if err != nil {
		t.Fatal(err)
	}
	repo := Repository{Owner: "octo", Name: "demo"}
	variables, err := client.ListActionVariables(context.Background(), repo)
	if err != nil {
		t.Fatal(err)
	}
	if len(variables.Variables) != 100 {
		t.Fatalf("len(variables.Variables) = %d, want 100 (GitHub's default page size of 30 must not silently truncate this)", len(variables.Variables))
	}
	secrets, err := client.ListActionsSecrets(context.Background(), repo)
	if err != nil {
		t.Fatal(err)
	}
	if len(secrets.Secrets) != 100 {
		t.Fatalf("len(secrets.Secrets) = %d, want 100", len(secrets.Secrets))
	}
}

func TestGetSelectedActionsAndDefaultWorkflowPermissions(t *testing.T) {
	t.Parallel()
	assertions := newHandlerAssertions(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/octo/demo/actions/permissions/selected-actions":
			_, _ = w.Write([]byte(`{"github_owned_allowed":true,"verified_allowed":false,"patterns_allowed":["actions/checkout@*"]}`))
		case "/repos/octo/demo/actions/permissions/workflow":
			_, _ = w.Write([]byte(`{"default_workflow_permissions":"read","can_approve_pull_request_reviews":false}`))
		default:
			assertions.Errorf("unexpected path %q", r.URL.Path)
		}
	}))
	defer server.Close()

	client, err := NewClient(server.Client(), server.URL)
	if err != nil {
		t.Fatal(err)
	}
	repo := Repository{Owner: "octo", Name: "demo"}
	selected, err := client.GetSelectedActions(context.Background(), repo)
	if err != nil {
		t.Fatal(err)
	}
	if !selected.GitHubOwnedAllowed || selected.VerifiedAllowed || len(selected.PatternsAllowed) != 1 {
		t.Fatalf("GetSelectedActions() = %#v", selected)
	}
	permissions, err := client.GetDefaultWorkflowPermissions(context.Background(), repo)
	if err != nil {
		t.Fatal(err)
	}
	if permissions.DefaultWorkflowPermissions != "read" || permissions.CanApprovePullRequest {
		t.Fatalf("GetDefaultWorkflowPermissions() = %#v", permissions)
	}
}

func TestGetAutomatedSecurityFixesEnabledTreats404AsDisabled(t *testing.T) {
	t.Parallel()
	assertions := newHandlerAssertions(t)
	var status int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.URL.Path, "/repos/octo/demo/automated-security-fixes"; got != want {
			assertions.Errorf("path = %q, want %q", got, want)
		}
		if status == http.StatusOK {
			_ = json.NewEncoder(w).Encode(map[string]any{"enabled": true, "paused": false})
			return
		}
		w.WriteHeader(status)
	}))
	defer server.Close()

	client, err := NewClient(server.Client(), server.URL)
	if err != nil {
		t.Fatal(err)
	}
	repo := Repository{Owner: "octo", Name: "demo"}

	status = http.StatusOK
	enabled, err := client.GetAutomatedSecurityFixesEnabled(context.Background(), repo)
	if err != nil {
		t.Fatal(err)
	}
	if !enabled {
		t.Fatal("200 response with enabled:true should report enabled")
	}

	status = http.StatusNotFound
	enabled, err = client.GetAutomatedSecurityFixesEnabled(context.Background(), repo)
	if err != nil {
		t.Fatalf("404 should not be an error, got %v", err)
	}
	if enabled {
		t.Fatal("404 response should report disabled")
	}
}

func TestGetPrivateVulnerabilityReportingEnabled(t *testing.T) {
	t.Parallel()
	assertions := newHandlerAssertions(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.URL.Path, "/repos/octo/demo/private-vulnerability-reporting"; got != want {
			assertions.Errorf("path = %q, want %q", got, want)
		}
		_, _ = w.Write([]byte(`{"enabled":true}`))
	}))
	defer server.Close()

	client, err := NewClient(server.Client(), server.URL)
	if err != nil {
		t.Fatal(err)
	}
	enabled, err := client.GetPrivateVulnerabilityReportingEnabled(context.Background(), Repository{Owner: "octo", Name: "demo"})
	if err != nil {
		t.Fatal(err)
	}
	if !enabled {
		t.Fatal("GetPrivateVulnerabilityReportingEnabled() = false, want true")
	}
}

func TestGetCodeQLDefaultSetup(t *testing.T) {
	t.Parallel()
	assertions := newHandlerAssertions(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.URL.Path, "/repos/octo/demo/code-scanning/default-setup"; got != want {
			assertions.Errorf("path = %q, want %q", got, want)
		}
		_, _ = w.Write([]byte(`{"state":"configured","languages":["go"],"query_suite":"default","updated_at":"2026-01-01T00:00:00Z"}`))
	}))
	defer server.Close()

	client, err := NewClient(server.Client(), server.URL)
	if err != nil {
		t.Fatal(err)
	}
	status, err := client.GetCodeQLDefaultSetup(context.Background(), Repository{Owner: "octo", Name: "demo"})
	if err != nil {
		t.Fatal(err)
	}
	if status.State != "configured" || len(status.Languages) != 1 || status.Languages[0] != "go" {
		t.Fatalf("GetCodeQLDefaultSetup() = %#v", status)
	}
}

func TestGetAIScan(t *testing.T) {
	t.Parallel()
	assertions := newHandlerAssertions(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			assertions.Errorf("method = %q, want %q", r.Method, http.MethodGet)
		}
		if got, want := r.URL.Path, "/repos/octo/demo/code-scanning/ai-scan"; got != want {
			assertions.Errorf("path = %q, want %q", got, want)
		}
		if got, want := r.Header.Get("X-GitHub-Api-Version"), APIVersion; got != want {
			assertions.Errorf("API version = %q, want %q", got, want)
		}
		_, _ = w.Write([]byte(`{"pr_scan":"enabled"}`))
	}))
	defer server.Close()

	client, err := NewClient(server.Client(), server.URL)
	if err != nil {
		t.Fatal(err)
	}
	setting, err := client.GetAIScan(context.Background(), Repository{Owner: "octo", Name: "demo"})
	if err != nil {
		t.Fatal(err)
	}
	if setting.PRScan != "enabled" {
		t.Fatalf("GetAIScan() = %#v, want pr_scan enabled", setting)
	}
}

func TestGetAIScanReturnsForbiddenAsError(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"message":"Advanced Security must be enabled for this repository"}`))
	}))
	defer server.Close()

	client, err := NewClient(server.Client(), server.URL)
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.GetAIScan(context.Background(), Repository{Owner: "octo", Name: "demo"})
	var apiError *APIError
	if !errors.As(err, &apiError) || apiError.StatusCode != http.StatusForbidden {
		t.Fatalf("GetAIScan() error = %v, want HTTP 403 APIError", err)
	}
}

func TestUpdateAIScan(t *testing.T) {
	t.Parallel()
	assertions := newHandlerAssertions(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			assertions.Errorf("method = %q, want %q", r.Method, http.MethodPatch)
		}
		if got, want := r.URL.Path, "/repos/octo/demo/code-scanning/ai-scan"; got != want {
			assertions.Errorf("path = %q, want %q", got, want)
		}
		if got, want := r.Header.Get("X-GitHub-Api-Version"), APIVersion; got != want {
			assertions.Errorf("API version = %q, want %q", got, want)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			assertions.Errorf("read body: %v", err)
		}
		if got, want := strings.TrimSpace(string(body)), `{"pr_scan":"enabled"}`; got != want {
			assertions.Errorf("body = %s, want %s", got, want)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client, err := NewClient(server.Client(), server.URL)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.UpdateAIScan(context.Background(), Repository{Owner: "octo", Name: "demo"}, AIScanSetting{PRScan: "enabled"}); err != nil {
		t.Fatal(err)
	}
}

func TestUpdateAIScanReturnsForbiddenAsError(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"message":"AI Scan is disabled by the organization"}`))
	}))
	defer server.Close()

	client, err := NewClient(server.Client(), server.URL)
	if err != nil {
		t.Fatal(err)
	}
	err = client.UpdateAIScan(context.Background(), Repository{Owner: "octo", Name: "demo"}, AIScanSetting{PRScan: "enabled"})
	var apiError *APIError
	if !errors.As(err, &apiError) || apiError.StatusCode != http.StatusForbidden {
		t.Fatalf("UpdateAIScan() error = %v, want HTTP 403 APIError", err)
	}
}

func TestRepositoryInfoDecodesSecurityAndAnalysis(t *testing.T) {
	t.Parallel()
	assertions := newHandlerAssertions(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.URL.Path, "/repos/octo/demo"; got != want {
			assertions.Errorf("path = %q, want %q", got, want)
		}
		_, _ = w.Write([]byte(`{
			"name": "demo", "visibility": "public",
			"security_and_analysis": {
				"secret_scanning": {"status": "enabled"},
				"secret_scanning_push_protection": {"status": "disabled"}
			}
		}`))
	}))
	defer server.Close()

	client, err := NewClient(server.Client(), server.URL)
	if err != nil {
		t.Fatal(err)
	}
	info, err := client.GetRepository(context.Background(), Repository{Owner: "octo", Name: "demo"})
	if err != nil {
		t.Fatal(err)
	}
	if info.SecurityAndAnalysis == nil {
		t.Fatal("SecurityAndAnalysis was not decoded")
	}
	if info.SecurityAndAnalysis.SecretScanning == nil || info.SecurityAndAnalysis.SecretScanning.Status != "enabled" {
		t.Fatalf("SecretScanning = %#v, want status enabled", info.SecurityAndAnalysis.SecretScanning)
	}
	if info.SecurityAndAnalysis.SecretScanningPushProtect == nil || info.SecurityAndAnalysis.SecretScanningPushProtect.Status != "disabled" {
		t.Fatalf("SecretScanningPushProtect = %#v, want status disabled", info.SecurityAndAnalysis.SecretScanningPushProtect)
	}
}

func TestListCustomRepositoryRolesWalksEveryPage(t *testing.T) {
	t.Parallel()
	assertions := newHandlerAssertions(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.URL.Path, "/orgs/octo-org/custom-repository-roles"; got != want {
			assertions.Errorf("path = %q, want %q", got, want)
		}
		var roles []CustomRepositoryRole
		switch r.URL.Query().Get("page") {
		case "1":
			for i := 0; i < 100; i++ {
				roles = append(roles, CustomRepositoryRole{ID: int64(i + 1), Name: fmt.Sprintf("role-%d", i)})
			}
		case "2":
			roles = []CustomRepositoryRole{{ID: 501, Name: "release-manager"}}
		default:
			assertions.Errorf("unexpected page %q", r.URL.Query().Get("page"))
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"total_count": len(roles), "custom_roles": roles})
	}))
	defer server.Close()

	client, err := NewClient(server.Client(), server.URL)
	if err != nil {
		t.Fatal(err)
	}
	roles, err := client.ListCustomRepositoryRoles(context.Background(), "octo-org")
	if err != nil {
		t.Fatal(err)
	}
	if len(roles) != 101 {
		t.Fatalf("len(roles) = %d, want 101 (an organization with more than one page of custom roles must not lose the rest)", len(roles))
	}
}

func templateArchive(t *testing.T, entries map[string]string) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	for name, content := range entries {
		var entry io.Writer
		var err error
		if strings.HasSuffix(name, "/") {
			header := &zip.FileHeader{Name: name}
			header.SetMode(os.ModeDir | 0o755)
			entry, err = writer.CreateHeader(header)
		} else {
			entry, err = writer.Create(name)
		}
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(entry, content); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

// handlerAssertions collects request validation failures in the test goroutine.
// net/http invokes handlers on a separate goroutine, where Fatal/Fatalf would
// call FailNow on the wrong goroutine.
type handlerAssertions struct {
	mu     sync.Mutex
	errors []string
}

func newHandlerAssertions(t *testing.T) *handlerAssertions {
	t.Helper()
	assertions := &handlerAssertions{}
	t.Cleanup(func() {
		assertions.mu.Lock()
		defer assertions.mu.Unlock()
		if len(assertions.errors) != 0 {
			t.Error(strings.Join(assertions.errors, "; "))
		}
	})
	return assertions
}

func (a *handlerAssertions) Errorf(format string, args ...any) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.errors = append(a.errors, fmt.Sprintf(format, args...))
}
