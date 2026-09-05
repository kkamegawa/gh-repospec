package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kkamegawa/gh-repospec/internal/config"
	"github.com/kkamegawa/gh-repospec/internal/importer"
	"github.com/kkamegawa/gh-repospec/internal/refresh"
)

func TestParseImportFlagsAcceptsPositionalRepository(t *testing.T) {
	t.Parallel()
	options, err := parseImportFlags([]string{"octo/demo"}, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	if options.repository != "octo/demo" || options.format != "yaml" {
		t.Fatalf("options = %#v", options)
	}
}

func TestParseImportFlagsAcceptsFlagsOnEitherSideOfPositional(t *testing.T) {
	t.Parallel()
	before, err := parseImportFlags([]string{"--format", "json", "octo/demo"}, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	after, err := parseImportFlags([]string{"octo/demo", "--format", "json"}, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	if before != after {
		t.Fatalf("before = %#v, after = %#v, want identical regardless of flag position", before, after)
	}
	if before.format != "json" {
		t.Fatalf("format = %q, want json", before.format)
	}
}

func TestParseImportFlagsRejectsPositionalAndDashRTogether(t *testing.T) {
	t.Parallel()
	_, err := parseImportFlags([]string{"octo/demo", "-R", "other/repo"}, &bytes.Buffer{})
	if err == nil {
		t.Fatal("parseImportFlags() unexpectedly accepted both a positional repository and -R")
	}
}

func TestParseImportFlagsRequiresARepository(t *testing.T) {
	t.Parallel()
	_, err := parseImportFlags([]string{"--format", "json"}, &bytes.Buffer{})
	if err == nil {
		t.Fatal("parseImportFlags() unexpectedly accepted no repository at all")
	}
}

func TestParseImportFlagsAcceptsDashRSubstitute(t *testing.T) {
	t.Parallel()
	options, err := parseImportFlags([]string{"-R", "octo/demo"}, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	if options.repository != "octo/demo" {
		t.Fatalf("repository = %q, want octo/demo", options.repository)
	}
}

func TestParseImportFlagsRejectsInvalidFormat(t *testing.T) {
	t.Parallel()
	_, err := parseImportFlags([]string{"octo/demo", "--format", "text"}, &bytes.Buffer{})
	if err == nil {
		t.Fatal("parseImportFlags() unexpectedly accepted --format text")
	}
}

func TestParseImportFlagsRejectsYesWithoutInto(t *testing.T) {
	t.Parallel()
	_, err := parseImportFlags([]string{"octo/demo", "--yes"}, &bytes.Buffer{})
	if err == nil {
		t.Fatal("parseImportFlags() unexpectedly accepted --yes without --into")
	}
}

func TestParseImportFlagsAcceptsIntoAndYes(t *testing.T) {
	t.Parallel()
	options, err := parseImportFlags([]string{"octo/demo", "--into", "bootstrap.yaml", "--yes"}, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	if options.into != "bootstrap.yaml" || !options.yes {
		t.Fatalf("options = %#v", options)
	}
}

func TestParseImportFlagsRejectsUnknownFlag(t *testing.T) {
	t.Parallel()
	_, err := parseImportFlags([]string{"octo/demo", "--bogus"}, &bytes.Buffer{})
	if err == nil {
		t.Fatal("parseImportFlags() unexpectedly accepted an unknown flag")
	}
}

func TestParseOwnerRepoValidatesShape(t *testing.T) {
	t.Parallel()
	if _, err := parseOwnerRepo("octo/demo"); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"octo", "octo/", "/demo", "octo/demo/extra", "oc to/demo"} {
		if _, err := parseOwnerRepo(bad); err == nil {
			t.Fatalf("parseOwnerRepo(%q) unexpectedly succeeded", bad)
		}
	}
}

func TestWriteImportDocumentYAMLAndJSON(t *testing.T) {
	t.Parallel()
	cfg := config.Config{SchemaVersion: 1, Repository: config.RepositoryConfig{Owner: "octo", Name: "demo", Visibility: "public"}}

	var yamlOut bytes.Buffer
	if err := writeImportDocument(&yamlOut, cfg, "yaml"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(yamlOut.String(), "owner: octo") {
		t.Fatalf("YAML output = %s, want owner field", yamlOut.String())
	}

	var jsonOut bytes.Buffer
	if err := writeImportDocument(&jsonOut, cfg, "json"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(jsonOut.String(), `"owner": "octo"`) {
		t.Fatalf("JSON output = %s, want owner field", jsonOut.String())
	}
}

func TestResolveIntoFormatDetectsAndValidatesAgreement(t *testing.T) {
	t.Parallel()
	yamlBytes := []byte("schemaVersion: 1\n")
	jsonBytes := []byte(`{"schemaVersion":1}`)

	format, err := resolveIntoFormat(importOptions{format: "yaml"}, yamlBytes)
	if err != nil || format != refresh.FormatYAML {
		t.Fatalf("format = %v, err = %v, want FormatYAML", format, err)
	}
	format, err = resolveIntoFormat(importOptions{format: "yaml"}, jsonBytes)
	if err != nil || format != refresh.FormatJSON {
		t.Fatalf("format = %v, err = %v, want FormatJSON detected even though --format defaulted to yaml", format, err)
	}
	if _, err := resolveIntoFormat(importOptions{format: "json"}, yamlBytes); err == nil {
		t.Fatal("resolveIntoFormat() unexpectedly accepted --format json against a non-JSON file")
	}
}

func TestIsTerminalIsFalseForNonFileReaders(t *testing.T) {
	t.Parallel()
	if isTerminal(strings.NewReader("y\n")) {
		t.Fatal("isTerminal() = true for an in-memory reader, want false")
	}
}

func TestPrintRefreshPlanListsEveryChangeKind(t *testing.T) {
	t.Parallel()
	plan := refresh.Plan{
		Scalars: []refresh.ScalarChange{{Path: "repository.description"}},
		Collections: []refresh.CollectionChange{{
			Path:       "labels",
			Updated:    []refresh.EntryUpdate{{Key: "bug"}},
			Added:      []refresh.EntryAddition{{Key: "enhancement"}},
			NotApplied: []string{"stale"},
		}},
	}
	var out bytes.Buffer
	if err := printRefreshPlan(&out, plan); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	for _, want := range []string{"UPDATE repository.description", "UPDATE labels[bug]", "ADD labels[enhancement]", "NOT APPLIED labels[stale]"} {
		if !strings.Contains(got, want) {
			t.Fatalf("output = %q, want it to contain %q", got, want)
		}
	}
}

// TestExecuteImportIntoRefusesPromptOnNonInteractiveInputWithoutYes guards
// the case a script or CI job hits: stdin is not a terminal (isTerminal
// only ever recognizes *os.File, so any test double reader counts as
// non-interactive, matching a piped or redirected stdin in production).
// Without --yes, --into must fail with a message naming --yes rather than
// silently reading whatever bytes happen to be on the wire, or hanging.
func TestExecuteImportIntoRefusesPromptOnNonInteractiveInputWithoutYes(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "bootstrap.yaml")
	original := "schemaVersion: 1\n" +
		"repository:\n" +
		"  owner: octo\n" +
		"  name: demo\n" +
		"  visibility: public\n" +
		"  description: old\n"
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}

	newDescription := "new"
	result := importer.Result{Config: config.Config{
		SchemaVersion: 1,
		Repository:    config.RepositoryConfig{Owner: "octo", Name: "demo", Visibility: "public", Description: &newDescription},
	}}

	var stdout, stderr bytes.Buffer
	err := executeImportInto(result, importOptions{into: path}, strings.NewReader("y\n"), &stdout, &stderr)
	if err == nil {
		t.Fatal("executeImportInto() unexpectedly succeeded on non-interactive input without --yes")
	}
	if !strings.Contains(err.Error(), "--yes") {
		t.Fatalf("error = %q, want it to name --yes", err)
	}
	unchanged, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(unchanged) != original {
		t.Fatalf("file = %q, want unchanged when the prompt is refused", unchanged)
	}
}

func TestConfirmAcceptsYAndYesDeclinesEverythingElseIncludingEOF(t *testing.T) {
	t.Parallel()
	accept := []string{"y", "Y", "yes", "YES"}
	for _, input := range accept {
		ok, err := confirm(strings.NewReader(input+"\n"), &bytes.Buffer{}, "Proceed?")
		if err != nil || !ok {
			t.Fatalf("confirm(%q) = (%v, %v), want (true, nil)", input, ok, err)
		}
	}
	decline := []string{"n", "no", "", "maybe"}
	for _, input := range decline {
		ok, err := confirm(strings.NewReader(input+"\n"), &bytes.Buffer{}, "Proceed?")
		if err != nil || ok {
			t.Fatalf("confirm(%q) = (%v, %v), want (false, nil)", input, ok, err)
		}
	}
	// EOF (no input at all) must decline, not error or hang.
	ok, err := confirm(strings.NewReader(""), &bytes.Buffer{}, "Proceed?")
	if err != nil || ok {
		t.Fatalf("confirm(EOF) = (%v, %v), want (false, nil)", ok, err)
	}
}

func TestExecuteImportIntoYesSkipsPrompt(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "bootstrap.yaml")
	original := "schemaVersion: 1\nrepository:\n  owner: octo\n  name: demo\n  visibility: public\n  description: old\n"
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	newDescription := "new"
	result := importer.Result{Config: config.Config{
		SchemaVersion: 1,
		Repository:    config.RepositoryConfig{Owner: "octo", Name: "demo", Visibility: "public", Description: &newDescription},
	}}

	var stdout, stderr bytes.Buffer
	// No stdin input at all: --yes must skip the prompt entirely.
	if err := executeImportInto(result, importOptions{into: path, yes: true}, strings.NewReader(""), &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	refreshed, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(refreshed), "description: new") {
		t.Fatalf("file = %s, want description refreshed", refreshed)
	}
}

func TestExecuteImportIntoNoopReportsNoChanges(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "bootstrap.yaml")
	original := "schemaVersion: 1\nrepository:\n  owner: octo\n  name: demo\n  visibility: public\n  description: same\n"
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	same := "same"
	result := importer.Result{Config: config.Config{
		SchemaVersion: 1,
		Repository:    config.RepositoryConfig{Owner: "octo", Name: "demo", Visibility: "public", Description: &same},
	}}

	var stdout, stderr bytes.Buffer
	if err := executeImportInto(result, importOptions{into: path}, strings.NewReader(""), &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "no changes") {
		t.Fatalf("stdout = %q, want a no-changes report", stdout.String())
	}
}
