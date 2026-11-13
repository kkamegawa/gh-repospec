package refresh

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/kkamegawa/gh-repospec/internal/config"
)

func TestRefreshPersonalJSONRetainsDeclaredEmptyCollection(t *testing.T) {
	t.Parallel()
	original := []byte(`{"schemaVersion":2,"scope":"personal","account":"octocat","issueTypes":[],"issueFields":[{"name":"Target date","dataType":"date","description":"old"}]}`)
	remote := []byte(`{"schemaVersion":2,"scope":"personal","account":"OCTOCAT","issueFields":[{"name":"Target date","dataType":"date","description":"new"}]}`)
	current, err := config.DecodePersonal(original)
	if err != nil {
		t.Fatal(err)
	}
	desired, err := config.DecodePersonal(remote)
	if err != nil {
		t.Fatal(err)
	}
	result, err := RefreshPersonal(original, FormatJSON, current, desired)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Changed {
		t.Fatal("expected the description to change")
	}
	refreshed, err := config.DecodePersonal(result.Updated)
	if err != nil {
		t.Fatal(err)
	}
	if refreshed.IssueTypes == nil || len(refreshed.IssueTypes) != 0 || refreshed.Account != current.Account {
		t.Fatalf("refresh lost the destination declaration: %s", result.Updated)
	}
}

func TestRefreshPersonalYAMLPreservesLocalEntriesAndNestedComments(t *testing.T) {
	t.Parallel()
	enabled := true
	priority := 1
	source := "schemaVersion: 2\n" +
		"scope: personal\n" +
		"account: octocat\n" +
		"issueTypes:\n" +
		"  # keep the hand-written type\n" +
		"  - name: Local\n" +
		"    isEnabled: true\n" +
		"issueFields:\n" +
		"  - name: Priority\n" +
		"    dataType: single_select\n" +
		"    options:\n" +
		"      - name: High # keep the option note\n" +
		"        color: red\n" +
		"        priority: 1\n"
	current := &config.PersonalConfig{
		SchemaVersion: 2, Scope: "personal", Account: "octocat",
		IssueTypes: []config.IssueTypeConfig{{Name: "Local", IsEnabled: &enabled}},
		IssueFields: []config.IssueFieldConfig{{
			Name: "Priority", DataType: "single_select",
			Options: []config.IssueFieldOptionConfig{{Name: "High", Color: "red", Priority: &priority}},
		}},
	}
	updatedEnabled := false
	newPriority := 2
	description := "Important"
	desired := &config.PersonalConfig{
		SchemaVersion: 2, Scope: "personal", Account: "octocat",
		IssueTypes: []config.IssueTypeConfig{
			{Name: "Local", IsEnabled: &updatedEnabled},
			{Name: "Feature", IsEnabled: &enabled},
		},
		IssueFields: []config.IssueFieldConfig{{
			Name: "Priority", DataType: "single_select",
			Options: []config.IssueFieldOptionConfig{
				{Name: "High", Description: &description, Color: "red", Priority: &priority},
				{Name: "Low", Color: "green", Priority: &newPriority},
			},
		}},
	}
	result, err := RefreshPersonal([]byte(source), FormatYAML, current, desired)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Changed {
		t.Fatal("Changed = false, want true")
	}
	if !strings.Contains(string(result.Updated), "# keep the hand-written type") ||
		!strings.Contains(string(result.Updated), "# keep the option note") {
		t.Fatalf("refresh lost comments:\n%s", result.Updated)
	}
	got, err := config.DecodePersonal(result.Updated)
	if err != nil {
		t.Fatalf("DecodePersonal: %v\n%s", err, result.Updated)
	}
	if len(got.IssueTypes) != 2 || got.IssueTypes[0].Name != "Local" || *got.IssueTypes[0].IsEnabled ||
		got.IssueTypes[1].Name != "Feature" {
		t.Fatalf("issue types = %#v", got.IssueTypes)
	}
	options := got.IssueFields[0].Options
	if len(options) != 2 || options[0].Name != "High" || options[1].Name != "Low" {
		t.Fatalf("options = %#v", options)
	}
}

func TestRefreshPersonalJSONPreservesUndeclaredDefinitions(t *testing.T) {
	t.Parallel()
	enabled := true
	current := &config.PersonalConfig{
		SchemaVersion: 2, Scope: "personal", Account: "octocat",
		IssueTypes: []config.IssueTypeConfig{{Name: "Local", IsEnabled: &enabled}},
	}
	desired := &config.PersonalConfig{
		SchemaVersion: 2, Scope: "personal", Account: "octocat",
		IssueTypes: []config.IssueTypeConfig{{Name: "Remote", IsEnabled: &enabled}},
	}
	result, err := RefreshPersonal([]byte("{\"schemaVersion\":2}"), FormatJSON, current, desired)
	if err != nil {
		t.Fatal(err)
	}
	var got config.PersonalConfig
	if err := json.Unmarshal(result.Updated, &got); err != nil {
		t.Fatal(err)
	}
	if len(got.IssueTypes) != 2 || got.IssueTypes[0].Name != "Local" || got.IssueTypes[1].Name != "Remote" {
		t.Fatalf("issue types = %#v", got.IssueTypes)
	}
}

func TestRefreshPersonalLeavesUndeclaredCollectionsAlone(t *testing.T) {
	t.Parallel()
	enabled := true
	current := &config.PersonalConfig{SchemaVersion: 2, Scope: "personal", Account: "octocat"}
	desired := &config.PersonalConfig{
		SchemaVersion: 2, Scope: "personal", Account: "octocat",
		IssueTypes: []config.IssueTypeConfig{{Name: "Remote", IsEnabled: &enabled}},
	}
	original := []byte("schemaVersion: 2\nscope: personal\naccount: octocat\n")
	result, err := RefreshPersonal(original, FormatYAML, current, desired)
	if err != nil {
		t.Fatal(err)
	}
	if result.Changed || string(result.Updated) != string(original) {
		t.Fatalf("result = %#v, want unchanged original", result)
	}
}
