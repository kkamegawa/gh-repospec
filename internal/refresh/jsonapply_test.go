package refresh

import (
	"testing"

	"github.com/kkamegawa/gh-repospec/internal/config"
)

// TestApplyJSONDoesNotMutateInputConfig guards against ApplyJSON's original
// shallow copy (`updated := *current`): config.Config nests several pointer
// fields (Actions, Security, Agents, Seed, Repository.Features), so a
// shallow struct copy still aliases them with current. Writing through
// updated.Actions.AllowedActions would silently mutate current.Actions too,
// surprising any caller that reuses current after Refresh returns.
func TestApplyJSONDoesNotMutateInputConfig(t *testing.T) {
	t.Parallel()
	current := &config.Config{
		SchemaVersion: config.CurrentSchemaVersion,
		Repository:    config.RepositoryConfig{Owner: "octo", Name: "demo", Visibility: "public"},
		Actions:       &config.ActionsConfig{AllowedActions: "all"},
	}
	plan := Plan{Scalars: []ScalarChange{{Path: "actions.allowedActions", Value: stringValue("selected")}}}

	data, err := ApplyJSON(current, plan)
	if err != nil {
		t.Fatal(err)
	}
	if current.Actions.AllowedActions != "all" {
		t.Fatalf("current.Actions.AllowedActions = %q, want unchanged %q", current.Actions.AllowedActions, "all")
	}
	if len(data) == 0 {
		t.Fatal("ApplyJSON returned no data")
	}
}
