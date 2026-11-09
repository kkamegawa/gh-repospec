package githubapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync/atomic"
	"testing"
	"time"
)

func TestCopilotCodeReviewSettingsEndpoint(t *testing.T) {
	t.Parallel()
	assertions := newHandlerAssertions(t)
	var patchAttempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/octo/demo/copilot/code-review/settings" {
			assertions.Errorf("request path = %s", r.URL.Path)
		}
		if got := r.Header.Get("X-GitHub-Api-Version"); got != "2026-03-10" {
			assertions.Errorf("API version = %q", got)
		}
		switch r.Method {
		case http.MethodGet:
			_, _ = w.Write([]byte(`{"review_effort":{"configured":null,"effective":"lite"},"available_review_efforts":["lite","balanced"],"severity_filter":"no_filter","custom_instructions_enabled":false,"mcp_enabled":true}`))
		case http.MethodPatch:
			var got map[string]any
			want := map[string]any{"review_effort": "inherit", "mcp_enabled": false}
			if err := json.NewDecoder(r.Body).Decode(&got); err != nil || !reflect.DeepEqual(got, want) {
				assertions.Errorf("PATCH body = %#v, error = %v, want %#v", got, err, want)
			}
			if patchAttempts.Add(1) == 1 {
				w.WriteHeader(http.StatusServiceUnavailable)
				return
			}
			w.WriteHeader(http.StatusOK)
		default:
			assertions.Errorf("unexpected request method %s", r.Method)
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	defer server.Close()

	client, err := NewClient(server.Client(), server.URL)
	if err != nil {
		t.Fatal(err)
	}
	client.SetRetryPolicy(RetryPolicy{MaxAttempts: 2, BaseDelay: time.Millisecond})
	repo := Repository{Owner: "octo", Name: "demo"}
	settings, err := client.GetCopilotCodeReviewSettings(t.Context(), repo)
	if err != nil {
		t.Fatal(err)
	}
	if settings.ReviewEffort.Configured != nil || settings.ReviewEffort.Effective != "lite" ||
		!reflect.DeepEqual(settings.AvailableReviewEfforts, []string{"lite", "balanced"}) ||
		settings.SeverityFilter == nil || *settings.SeverityFilter != "no_filter" ||
		settings.CustomInstructionsEnabled == nil || *settings.CustomInstructionsEnabled ||
		settings.MCPEnabled == nil || !*settings.MCPEnabled {
		t.Fatalf("GET settings = %+v", settings)
	}
	var waits int
	client.setRetryWait(func(context.Context, time.Duration) error {
		waits++
		return nil
	})
	inherit, disabled := "inherit", false
	if err := client.UpdateCopilotCodeReviewSettings(t.Context(), repo, CopilotCodeReviewSettingsRequest{
		ReviewEffort: &inherit,
		MCPEnabled:   &disabled,
	}); err != nil {
		t.Fatal(err)
	}
	if got := patchAttempts.Load(); got != 2 || waits != 1 {
		t.Fatalf("PATCH attempts = %d, retry waits = %d, want 2 attempts and 1 wait", got, waits)
	}
}
