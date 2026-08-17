package githubapi

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestRepositoryOperationsUseVersionedGitHubRequests(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.Header.Get("X-GitHub-Api-Version"), APIVersion; got != want {
			t.Fatalf("API version = %q, want %q", got, want)
		}
		if got, want := r.Header.Get("Accept"), "application/vnd.github+json"; got != want {
			t.Fatalf("Accept = %q, want %q", got, want)
		}
		if got, want := r.URL.Path, "/repos/octo/demo"; got != want {
			t.Fatalf("path = %q, want %q", got, want)
		}
		if r.Method != http.MethodPatch {
			t.Fatalf("method = %q", r.Method)
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
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/octo/demo/agents/secrets/KEY":
			secretRequest = r.Method == http.MethodPut
			w.WriteHeader(http.StatusNoContent)
		case "/repos/octo/demo/agents/variables/REGION":
			variableRequest = r.Method == http.MethodPatch
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
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
	started := time.Now()
	settings, err := client.GetActionsPermissions(context.Background(), Repository{Owner: "octo", Name: "demo"})
	if err != nil {
		t.Fatal(err)
	}
	if !settings.Enabled || requests.Load() != 2 {
		t.Fatalf("settings=%+v requests=%d", settings, requests.Load())
	}
	if time.Since(started) < time.Second {
		t.Fatal("Retry-After was not honored")
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
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.RequestURI())
		switch r.Method + " " + r.URL.Path {
		case "POST /repos/octo/demo/git/blobs":
			var request struct {
				Content  string `json:"content"`
				Encoding string `json:"encoding"`
			}
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Fatal(err)
			}
			if request.Encoding != "base64" || request.Content == "" {
				t.Fatalf("blob request = %+v", request)
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
				t.Fatal(err)
			}
			if request.SHA != "new-commit" || request.Force {
				t.Fatalf("ref request = %+v", request)
			}
			_ = json.NewEncoder(w).Encode(GitReference{Object: GitObject{SHA: request.SHA}})
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
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
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.URL.Query().Get("recursive"), "1"; got != want {
			t.Fatalf("recursive = %q, want %q", got, want)
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
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.URL.Path, "/repos/octo/template/zipball/main"; got != want {
			t.Fatalf("path = %q, want %q", got, want)
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
	content, err := os.ReadFile(filepath.Join(destination, "README.md"))
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
