package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"strings"
	"testing"

	"github.com/kkamegawa/gh-repospec/internal/personal"
	"github.com/kkamegawa/gh-repospec/internal/planner"
	"github.com/kkamegawa/gh-repospec/internal/refresh"
)

func TestParsePersonalPlanFlags(t *testing.T) {
	t.Parallel()
	options, err := parsePersonalPlanFlags([]string{"-f", "personal.yaml", "--format", "json", "--ci"}, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	if options.file != "personal.yaml" || options.format != "json" || !options.ci {
		t.Fatalf("options = %#v", options)
	}
}

func TestParsePersonalPlanFlagsRejectsUnsupportedFlagsAndFormats(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{
		{"--file", "personal.yaml", "--yes"},
		{"--file", "personal.yaml", "--format", "yaml"},
		{"--file", "personal.yaml", "unexpected"},
	} {
		if _, err := parsePersonalPlanFlags(args, &bytes.Buffer{}); err == nil {
			t.Errorf("parsePersonalPlanFlags(%v) unexpectedly succeeded", args)
		}
	}
}

func TestParsePersonalApplyFlags(t *testing.T) {
	t.Parallel()
	options, err := parsePersonalApplyFlags([]string{"--file=personal.yaml", "--yes", "--format", "json"}, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	if options.file != "personal.yaml" || !options.yes || options.format != "json" {
		t.Fatalf("options = %#v", options)
	}
	if _, err := parsePersonalApplyFlags([]string{"--file", "personal.yaml", "--ci"}, &bytes.Buffer{}); err == nil {
		t.Fatal("personal apply unexpectedly accepted --ci")
	}
}

func TestParsePersonalImportFlags(t *testing.T) {
	t.Parallel()
	options, err := parsePersonalImportFlags([]string{"--format=json", "--into", "personal.json", "--yes"}, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	if options.format != "json" || !options.formatSet || options.into != "personal.json" || !options.yes {
		t.Fatalf("options = %#v", options)
	}
	for _, args := range [][]string{{"--yes"}, {"--format", "text"}, {"unexpected"}} {
		if _, err := parsePersonalImportFlags(args, &bytes.Buffer{}); err == nil {
			t.Errorf("parsePersonalImportFlags(%v) unexpectedly succeeded", args)
		}
	}
}

func TestPersonalCommandHelpAndUnknownSubcommand(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{{"--help"}, {"plan", "--help"}, {"apply", "-h"}, {"import", "--help"}} {
		var stdout, stderr bytes.Buffer
		if err := runPersonal(t.Context(), args, strings.NewReader(""), &stdout, &stderr); err != nil {
			t.Errorf("runPersonal(%v) error = %v", args, err)
		}
		if !strings.Contains(stdout.String(), "Usage: gh repospec personal") {
			t.Errorf("runPersonal(%v) stdout = %q", args, stdout.String())
		}
	}
	if err := runPersonal(t.Context(), []string{"unknown"}, strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{}); err == nil {
		t.Fatal("runPersonal unexpectedly accepted an unknown subcommand")
	}
}

func TestParsePersonalCommandHelpReturnsFlagErrHelp(t *testing.T) {
	t.Parallel()
	_, err := parsePersonalPlanFlags([]string{"--help"}, &bytes.Buffer{})
	if !errors.Is(err, flag.ErrHelp) {
		t.Fatalf("error = %v, want flag.ErrHelp", err)
	}
}

func TestWritePersonalPlanSupportsTextAndJSON(t *testing.T) {
	t.Parallel()
	plan := personal.Plan{Account: "octo", Changes: []planner.Change{{
		Stage: planner.StageSettings, Resource: "issueType.Bug", State: planner.ChangeCreate, Summary: "create issue type",
	}}}
	var output bytes.Buffer
	if err := writePersonalPlan(&output, plan, "text"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "Personal account: octo") || !strings.Contains(output.String(), "CREATE issueType.Bug") {
		t.Fatalf("text output = %q", output.String())
	}
	output.Reset()
	if err := writePersonalPlan(&output, plan, "json"); err != nil {
		t.Fatal(err)
	}
	var decoded personal.Plan
	if err := json.Unmarshal(output.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Account != "octo" || len(decoded.Changes) != 1 {
		t.Fatalf("decoded plan = %#v", decoded)
	}
}

func TestPersonalIntoFormatPreservesDestinationFormat(t *testing.T) {
	t.Parallel()
	if format, err := personalIntoFormat("yaml", false, []byte("schemaVersion: 2\nscope: personal\n"), "personal.yaml"); err != nil || format != refresh.FormatYAML {
		t.Fatalf("YAML format = %v, error = %v", format, err)
	}
	if format, err := personalIntoFormat("yaml", false, []byte("{\"schemaVersion\":2}"), "personal.json"); err != nil || format != refresh.FormatJSON {
		t.Fatalf("JSON format = %v, error = %v", format, err)
	}
	if _, err := personalIntoFormat("json", true, []byte("schemaVersion: 2\n"), "personal.yaml"); err == nil {
		t.Fatal("personalIntoFormat unexpectedly accepted a format mismatch")
	}
}
