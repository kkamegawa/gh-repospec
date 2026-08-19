package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kkamegawa/gh-repobootstrapper/internal/githubapi"
	"github.com/kkamegawa/gh-repobootstrapper/internal/planner"
)

func TestRunValidateWritesSuccessToStdout(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "bootstrap.yaml")
	contents := `schemaVersion: 1
repository:
  owner: octo
  name: demo
  visibility: public
`
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if err := Run([]string{"validate", "--file", path}, strings.NewReader(""), &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	if got, want := stdout.String(), "configuration is valid\n"; got != want {
		t.Fatalf("stdout = %q, want %q", got, want)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
}

func TestWritePlanFormatsTextAndJSON(t *testing.T) {
	t.Parallel()
	plan := planner.Plan{
		Repository: githubapi.Repository{Owner: "octo", Name: "demo"},
		Exists:     true,
		Changes: []planner.Change{{
			Stage: planner.StageSettings, Resource: "repository.description", State: planner.ChangeUpdate, Summary: "update description",
		}},
	}
	var text bytes.Buffer
	if err := writePlan(&text, plan, "text"); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Repository: octo/demo", "Repository exists.", "[settings] UPDATE repository.description: update description"} {
		if !strings.Contains(text.String(), want) {
			t.Fatalf("text plan = %q, missing %q", text.String(), want)
		}
	}
	var output bytes.Buffer
	if err := writePlan(&output, plan, "json"); err != nil {
		t.Fatal(err)
	}
	var decoded planner.Plan
	if err := json.Unmarshal(output.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Repository != plan.Repository || len(decoded.Changes) != 1 || decoded.Changes[0].Resource != "repository.description" {
		t.Fatalf("JSON plan = %#v", decoded)
	}
}

func TestParseApplyFlagsCollectsEveryExplicitCommandApproval(t *testing.T) {
	t.Parallel()
	options, err := parseExecutionFlags("apply", []string{
		"-f", "bootstrap.yaml", "--yes", "--approve-command", "apm-init", "--approve-command", "configure-agent",
	}, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	if !options.yes || !options.approvedCommands["apm-init"] || !options.approvedCommands["configure-agent"] || len(options.approvedCommands) != 2 {
		t.Fatalf("apply options = %#v", options)
	}
}
