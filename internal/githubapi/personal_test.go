package githubapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestListPersonalIssueSettingsJoinsRESTIdentityAndGraphQLState(t *testing.T) {
	t.Parallel()
	assertions := newHandlerAssertions(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("X-GitHub-Api-Version"); got != APIVersion {
			assertions.Errorf("API version = %q, want %q", got, APIVersion)
		}
		if r.URL.Path != "/graphql" && r.Header.Get("GraphQL-Features") != "" {
			assertions.Errorf("GraphQL feature header sent to REST endpoint %s", r.URL.Path)
		}
		switch r.Method + " " + r.URL.Path {
		case "GET /user/issue-types":
			_ = json.NewEncoder(w).Encode([]PersonalIssueType{{ID: 4, NodeID: "type-node", Name: "Bug", Description: personalStringPointer("REST description")}})
		case "GET /user/issue-fields":
			_ = json.NewEncoder(w).Encode([]PersonalIssueField{{ID: 7, NodeID: "field-node", Name: "Priority", DataType: "single_select", Options: []PersonalIssueFieldOption{{ID: 91, Name: "High", Color: "red", Priority: personalIntPointer(1)}}}})
		case "POST /graphql":
			var request struct {
				Query string `json:"query"`
			}
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				assertions.Errorf("decode GraphQL request: %v", err)
			}
			switch {
			case strings.Contains(request.Query, "PersonalIssueTypes"):
				if got := r.Header.Get("GraphQL-Features"); got != "issue_types" {
					assertions.Errorf("issue types feature header = %q, want issue_types", got)
				}
				_, _ = w.Write([]byte(`{"data":{"viewer":{"issueTypes":{"nodes":[{"id":"type-node","name":"Bug","description":"Bug","color":"BLUE","isEnabled":true}]}}}}`))
			case strings.Contains(request.Query, "PersonalIssueFields"):
				if got := r.Header.Get("GraphQL-Features"); got != "issue_fields" {
					assertions.Errorf("issue fields feature header = %q, want issue_fields", got)
				}
				_, _ = w.Write([]byte(`{"data":{"viewer":{"issueFields":{"nodes":[{"id":"field-node","name":"Priority","description":null,"dataType":"SINGLE_SELECT","visibility":"RESTRICTED","options":[{"id":"option-node","databaseId":999,"name":"High","description":null,"color":"GREEN","priority":4}]}]}}}}`))
			default:
				assertions.Errorf("unexpected GraphQL query %q", request.Query)
			}
		default:
			assertions.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client, _ := NewClient(server.Client(), server.URL)

	types, err := client.ListPersonalIssueTypes(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(types) != 1 || types[0].ID != 4 || types[0].Color == nil || *types[0].Color != "blue" || types[0].IsEnabled == nil || !*types[0].IsEnabled {
		t.Fatalf("personal issue types = %+v", types)
	}
	if types[0].Description == nil || *types[0].Description != "REST description" {
		t.Fatalf("description = %v, want REST description", types[0].Description)
	}

	fields, err := client.ListPersonalIssueFields(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(fields) != 1 || fields[0].ID != 7 || fields[0].Visibility == nil || *fields[0].Visibility != "restricted" {
		t.Fatalf("personal issue fields = %+v", fields)
	}
	if len(fields[0].Options) != 1 || fields[0].Options[0].ID != 91 || fields[0].Options[0].Color != "red" || fields[0].Options[0].Priority == nil {
		t.Fatalf("personal issue field options = %+v", fields[0].Options)
	}
}

func TestPersonalIssueWritesUseAccountEndpointsAndPreserveNullableValues(t *testing.T) {
	t.Parallel()
	assertions := newHandlerAssertions(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			assertions.Errorf("decode request body: %v", err)
		}
		if got := r.Header.Get("X-GitHub-Api-Version"); got != APIVersion {
			assertions.Errorf("API version = %q, want %q", got, APIVersion)
		}
		switch r.Method + " " + r.URL.Path {
		case "POST /user/issue-types":
			if string(body["description"]) != "null" || string(body["color"]) != "null" || string(body["is_enabled"]) != "true" {
				assertions.Errorf("create issue type body = %s", encodeTestJSON(body))
			}
		case "PUT /user/issue-types/4":
			if string(body["description"]) != "null" || string(body["color"]) != "null" || body["id"] != nil {
				assertions.Errorf("update issue type body = %s", encodeTestJSON(body))
			}
		case "POST /user/issue-fields":
			if body["data_type"] == nil || body["options"] == nil {
				assertions.Errorf("create field omitted required fields: %s", encodeTestJSON(body))
			}
			var options []map[string]json.RawMessage
			_ = json.Unmarshal(body["options"], &options)
			if len(options) != 1 || options[0]["id"] != nil {
				assertions.Errorf("new option unexpectedly includes an ID: %s", encodeTestJSON(body))
			}
		case "PATCH /user/issue-fields/7":
			if body["data_type"] != nil || body["id"] != nil {
				assertions.Errorf("patch field body = %s", encodeTestJSON(body))
			}
			if body["options"] != nil {
				var options []map[string]json.RawMessage
				_ = json.Unmarshal(body["options"], &options)
				if len(options) != 1 || string(options[0]["id"]) != "91" {
					assertions.Errorf("existing option ID was not preserved: %s", encodeTestJSON(body))
				}
			}
		default:
			assertions.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	client, _ := NewClient(server.Client(), server.URL)

	enabled := true
	if err := client.CreatePersonalIssueType(context.Background(), PersonalIssueType{Name: "Bug", IsEnabled: &enabled}); err != nil {
		t.Fatal(err)
	}
	if err := client.UpdatePersonalIssueType(context.Background(), 4, PersonalIssueType{Name: "Bug", IsEnabled: &enabled}); err != nil {
		t.Fatal(err)
	}
	priority := 1
	if err := client.CreatePersonalIssueField(context.Background(), PersonalIssueField{Name: "Priority", DataType: "single_select", Options: []PersonalIssueFieldOption{{Name: "High", Color: "red", Priority: &priority}}}); err != nil {
		t.Fatal(err)
	}
	if err := client.UpdatePersonalIssueField(context.Background(), 7, PersonalIssueField{Name: "Priority"}); err != nil {
		t.Fatal(err)
	}
	if err := client.UpdatePersonalIssueField(context.Background(), 7, PersonalIssueField{Name: "Priority", Options: []PersonalIssueFieldOption{{ID: 91, Name: "High", Color: "red", Priority: &priority}}}); err != nil {
		t.Fatal(err)
	}
}

func TestPersonalIssueListFailsWhenSupplementalStateIsIncomplete(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "GET /user/issue-types":
			_, _ = w.Write([]byte(`[ {"id":1,"node_id":"type-node","name":"Bug"} ]`))
		case "POST /graphql":
			_, _ = w.Write([]byte(`{"data":{"viewer":{"issueTypes":{"nodes":[]}}}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client, _ := NewClient(server.Client(), server.URL)
	if _, err := client.ListPersonalIssueTypes(context.Background()); err == nil {
		t.Fatal("ListPersonalIssueTypes() unexpectedly accepted missing state")
	}
}

func TestUpdatePersonalIssueFieldDoesNotRetryOptionReplacement(t *testing.T) {
	t.Parallel()
	var requests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"message":"temporary failure"}`))
	}))
	defer server.Close()
	client, _ := NewClient(server.Client(), server.URL)
	client.SetRetryPolicy(RetryPolicy{MaxAttempts: 3})
	err := client.UpdatePersonalIssueField(context.Background(), 7, PersonalIssueField{
		Name: "Priority", Options: []PersonalIssueFieldOption{{Name: "High", Color: "red", Priority: personalIntPointer(1)}},
	})
	if err == nil || requests != 1 {
		t.Fatalf("error = %v, requests = %d, want one failed request", err, requests)
	}
}

func encodeTestJSON(value any) string {
	encoded, _ := json.Marshal(value)
	return string(encoded)
}

func personalStringPointer(value string) *string { return &value }

func personalIntPointer(value int) *int { return &value }
