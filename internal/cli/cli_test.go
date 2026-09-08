package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kkamegawa/gh-repospec/internal/githubapi"
	"github.com/kkamegawa/gh-repospec/internal/planner"
)

func TestRunVersionWritesVersionToStdout(t *testing.T) {
	t.Parallel()
	previous := Version
	Version = "v1.2.3"
	defer func() { Version = previous }()

	for _, arg := range []string{"version", "--version", "-v"} {
		var stdout, stderr bytes.Buffer
		if err := Run([]string{arg}, strings.NewReader(""), &stdout, &stderr); err != nil {
			t.Fatalf("Run(%q) error = %v", arg, err)
		}
		if got, want := stdout.String(), "gh-repospec v1.2.3\n"; got != want {
			t.Fatalf("Run(%q) stdout = %q, want %q", arg, got, want)
		}
		if stderr.Len() != 0 {
			t.Fatalf("Run(%q) stderr = %q, want empty", arg, stderr.String())
		}
	}
}

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

func TestParsePlanFlagsAcceptsCI(t *testing.T) {
	t.Parallel()
	options, err := parseExecutionFlags("plan", []string{"-f", "bootstrap.yaml", "--ci"}, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	if !options.ci {
		t.Fatalf("plan options = %#v, want ci = true", options)
	}
}

func TestParseApplyFlagsRejectsCI(t *testing.T) {
	t.Parallel()
	if _, err := parseExecutionFlags("apply", []string{"-f", "bootstrap.yaml", "--ci"}, &bytes.Buffer{}); err == nil {
		t.Fatal("parseExecutionFlags(apply, --ci) = nil error, want a flag-parsing error")
	}
}

func TestDriftErrorReportsExitCodeTwo(t *testing.T) {
	t.Parallel()
	err := &DriftError{Changes: 3}
	if got := err.ExitCode(); got != 2 {
		t.Fatalf("ExitCode() = %d, want 2", got)
	}
	if err.Error() == "" {
		t.Fatal("Error() = empty string, want a non-empty message")
	}
}

func TestPlanOutcomeSelectsExitCode(t *testing.T) {
	t.Parallel()
	noChange := planner.Plan{Changes: []planner.Change{{State: planner.ChangeNoop}}}
	opaqueOnly := planner.Plan{Changes: []planner.Change{{State: planner.ChangeUpdate, Opaque: true}}}
	manualOnly := planner.Plan{Changes: []planner.Change{{State: planner.ChangeManual}}}
	withDrift := planner.Plan{Changes: []planner.Change{{State: planner.ChangeCreate}}}
	blocked := planner.Plan{Changes: []planner.Change{{State: planner.ChangeBlocked}}}
	blockedAndDrift := planner.Plan{Changes: []planner.Change{{State: planner.ChangeBlocked}, {State: planner.ChangeCreate}}}

	tests := []struct {
		name      string
		plan      planner.Plan
		wantDrift bool
		wantErr   bool
	}{
		{"no changes", noChange, false, false},
		{"secret updates only", opaqueOnly, false, false},
		{"manual entries only", manualOnly, false, false},
		{"create or update present", withDrift, true, false},
		{"blocked present", blocked, false, true},
		{"blocked and drift present", blockedAndDrift, false, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var stderr bytes.Buffer
			err := planOutcome(&stderr, tt.plan, true)
			var drift *DriftError
			switch {
			case tt.wantDrift:
				if !errors.As(err, &drift) {
					t.Fatalf("planOutcome() error = %v, want a *DriftError", err)
				}
				if stderr.Len() == 0 {
					t.Fatal("planOutcome() wrote nothing to stderr, want a drift notice")
				}
			case tt.wantErr:
				if err == nil || errors.As(err, &drift) {
					t.Fatalf("planOutcome() error = %v, want a non-drift failure", err)
				}
			default:
				if err != nil {
					t.Fatalf("planOutcome() error = %v, want nil", err)
				}
				if stderr.Len() != 0 {
					t.Fatalf("planOutcome() stderr = %q, want empty", stderr.String())
				}
			}
		})
	}
}

func TestPlanOutcomeWithoutCIAlwaysSucceeds(t *testing.T) {
	t.Parallel()
	blockedAndDrift := planner.Plan{Changes: []planner.Change{{State: planner.ChangeBlocked}, {State: planner.ChangeCreate}}}
	var stderr bytes.Buffer
	if err := planOutcome(&stderr, blockedAndDrift, false); err != nil {
		t.Fatalf("planOutcome(ci=false) error = %v, want nil", err)
	}
	if stderr.Len() != 0 {
		t.Fatalf("planOutcome(ci=false) stderr = %q, want empty", stderr.String())
	}
}

func TestPrintUsageDocumentsCI(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	printUsage(&buf)
	if !strings.Contains(buf.String(), "--ci") {
		t.Fatalf("usage text = %q, missing --ci", buf.String())
	}
}
