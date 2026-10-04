package refresh

import (
	"strings"
	"testing"

	"github.com/kkamegawa/gh-repospec/internal/config"
)

func TestApplyYAMLUpdatesScalarPreservingCommentsAndKeyOrder(t *testing.T) {
	t.Parallel()
	src := "schemaVersion: 1\n" +
		"repository: # settings\n" +
		"  visibility: public\n" +
		"  owner: octo\n" +
		"  name: demo\n" +
		"  description: old\n"
	plan := Plan{Scalars: []ScalarChange{{Path: "repository.description", Value: stringValue("new")}}}

	out, err := ApplyYAML([]byte(src), plan)
	if err != nil {
		t.Fatal(err)
	}
	want := "schemaVersion: 1\n" +
		"repository: # settings\n" +
		"  visibility: public\n" +
		"  owner: octo\n" +
		"  name: demo\n" +
		"  description: new\n"
	if string(out) != want {
		t.Fatalf("out = %q, want %q", out, want)
	}
}

func TestApplyYAMLPreservesQuoteStyleOnUpdate(t *testing.T) {
	t.Parallel()
	src := "repository:\n  description: 'old value'\n"
	out, err := ApplyYAML([]byte(src), Plan{Scalars: []ScalarChange{
		{Path: "repository.description", Value: stringValue("it's new")},
	}})
	if err != nil {
		t.Fatal(err)
	}
	want := "repository:\n  description: 'it''s new'\n"
	if string(out) != want {
		t.Fatalf("out = %q, want %q (single-quote style preserved, embedded quote escaped)", out, want)
	}
}

func TestApplyYAMLUpdatesNestedActionsSelectedActionsField(t *testing.T) {
	t.Parallel()
	src := "actions:\n  allowedActions: selected\n  selectedActions:\n    githubOwnedAllowed: false\n    verifiedAllowed: true\n"
	out, err := ApplyYAML([]byte(src), Plan{Scalars: []ScalarChange{
		{Path: "actions.selectedActions.githubOwnedAllowed", Value: boolValue(true)},
	}})
	if err != nil {
		t.Fatal(err)
	}
	want := "actions:\n  allowedActions: selected\n  selectedActions:\n    githubOwnedAllowed: true\n    verifiedAllowed: true\n"
	if string(out) != want {
		t.Fatalf("out = %q, want %q", out, want)
	}
}

// A full-line comment above a mapping key combined with a trailing comment
// on the sibling line just before the edited field skews go-yaml's value
// token offset backwards onto the key's own ": ". The edit must still land
// on the value alone.
func TestApplyYAMLValueStartSurvivesCommentOffsetSkew(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		src  string
		plan Plan
		want string
	}{
		{
			name: "nested security field",
			src: "security:\n" +
				"  # note\n" +
				"  aiPullRequestScan:\n" +
				"    optional: true # x\n" +
				"    enabled: false\n",
			plan: Plan{Scalars: []ScalarChange{{Path: "security.aiPullRequestScan.enabled", Value: boolValue(true)}}},
			want: "security:\n" +
				"  # note\n" +
				"  aiPullRequestScan:\n" +
				"    optional: true # x\n" +
				"    enabled: true\n",
		},
		{
			name: "top-level repository field",
			src: "# note\n" +
				"repository:\n" +
				"  visibility: public # x\n" +
				"  description: old\n",
			plan: Plan{Scalars: []ScalarChange{{Path: "repository.description", Value: stringValue("new")}}},
			want: "# note\n" +
				"repository:\n" +
				"  visibility: public # x\n" +
				"  description: new\n",
		},
		{
			name: "label entry field",
			src: "labels:\n" +
				"  # note\n" +
				"  - name: bug # x\n" +
				"    color: d73a4a\n",
			plan: Plan{Collections: []CollectionChange{{
				Path:    "labels",
				Updated: []EntryUpdate{{Key: "bug", Scalars: []ScalarChange{{Path: "color", Value: stringValue("ff0000")}}}},
			}}},
			want: "labels:\n" +
				"  # note\n" +
				"  - name: bug # x\n" +
				"    color: ff0000\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assertApplyYAMLBothLineEndings(t, tt.src, tt.plan, tt.want)
		})
	}
}

// A quoted key never matches at its own token offset, so its start falls
// back to a literal search for the bare name. That search must stay inside
// the target mapping, and a same-named key it can still reach there (in a
// deeper child mapping) must never become the edit location.
func TestApplyYAMLQuotedKeyNeverRedirectsTheEdit(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		src  string
		plan Plan
		want string
	}{
		{
			name: "same-named key in an earlier sibling mapping",
			src: "security:\n" +
				"  codeqlDefaultSetup:\n" +
				"    enabled: false\n" +
				"  aiPullRequestScan:\n" +
				"    \"enabled\": false\n",
			plan: Plan{Scalars: []ScalarChange{{Path: "security.aiPullRequestScan.enabled", Value: boolValue(true)}}},
			want: "security:\n" +
				"  codeqlDefaultSetup:\n" +
				"    enabled: false\n" +
				"  aiPullRequestScan:\n" +
				"    \"enabled\": true\n",
		},
		{
			name: "same-named field in an earlier sequence entry",
			src: "labels:\n" +
				"  - name: bug\n" +
				"    color: d73a4a\n" +
				"  - name: stale\n" +
				"    \"color\": cccccc\n",
			plan: Plan{Collections: []CollectionChange{{
				Path:    "labels",
				Updated: []EntryUpdate{{Key: "stale", Scalars: []ScalarChange{{Path: "color", Value: stringValue("ff0000")}}}},
			}}},
			want: "labels:\n" +
				"  - name: bug\n" +
				"    color: d73a4a\n" +
				"  - name: stale\n" +
				"    \"color\": ff0000\n",
		},
		{
			name: "same-named key in a deeper child mapping",
			src: "repository:\n" +
				"  nested:\n" +
				"    description: inner\n" +
				"  \"description\": old\n",
			plan: Plan{Scalars: []ScalarChange{{Path: "repository.description", Value: stringValue("new")}}},
			want: "repository:\n" +
				"  nested:\n" +
				"    description: inner\n" +
				"  \"description\": new\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assertApplyYAMLBothLineEndings(t, tt.src, tt.plan, tt.want)
		})
	}
}

// assertApplyYAMLBothLineEndings applies plan to src and checks the result
// against want byte for byte, once with LF and once with CRLF line endings.
func assertApplyYAMLBothLineEndings(t *testing.T, src string, plan Plan, want string) {
	t.Helper()
	for _, ending := range []string{"\n", "\r\n"} {
		src := strings.ReplaceAll(src, "\n", ending)
		want := strings.ReplaceAll(want, "\n", ending)
		out, err := ApplyYAML([]byte(src), plan)
		if err != nil {
			t.Fatal(err)
		}
		if string(out) != want {
			t.Fatalf("out = %q, want %q", out, want)
		}
	}
}

func TestApplyYAMLUpdatesSecurityAIPullRequestScanPreservingComments(t *testing.T) {
	t.Parallel()
	src := "security:\n" +
		"  codeqlDefaultSetup:\n" +
		"    enabled: true\n" +
		"  # Public preview; needs Advanced Security.\n" +
		"  aiPullRequestScan:\n" +
		"    optional: true\n" +
		"    enabled: false # flip once licensed\n"
	out, err := ApplyYAML([]byte(src), Plan{Scalars: []ScalarChange{
		{Path: "security.aiPullRequestScan.enabled", Value: boolValue(true)},
	}})
	if err != nil {
		t.Fatal(err)
	}
	want := "security:\n" +
		"  codeqlDefaultSetup:\n" +
		"    enabled: true\n" +
		"  # Public preview; needs Advanced Security.\n" +
		"  aiPullRequestScan:\n" +
		"    optional: true\n" +
		"    enabled: true # flip once licensed\n"
	if string(out) != want {
		t.Fatalf("out = %q, want %q", out, want)
	}
}

func TestApplyYAMLUpdatesLabelEntryLeavesOthersUntouched(t *testing.T) {
	t.Parallel()
	src := "labels:\n" +
		"  - name: bug # important\n" +
		"    color: d73a4a\n" +
		"  - name: stale\n" +
		"    color: cccccc\n"
	plan := Plan{Collections: []CollectionChange{{
		Path: "labels",
		Updated: []EntryUpdate{
			{Key: "bug", Scalars: []ScalarChange{{Path: "color", Value: stringValue("ff0000")}}},
		},
		NotApplied: []string{"stale"},
	}}}
	out, err := ApplyYAML([]byte(src), plan)
	if err != nil {
		t.Fatal(err)
	}
	want := "labels:\n" +
		"  - name: bug # important\n" +
		"    color: ff0000\n" +
		"  - name: stale\n" +
		"    color: cccccc\n"
	if string(out) != want {
		t.Fatalf("out = %q, want %q (bug's color updated, its comment and the untouched stale entry preserved)", out, want)
	}
}

func TestApplyYAMLAppendsNewLabelEntryAfterExistingOnes(t *testing.T) {
	t.Parallel()
	src := "labels:\n" +
		"  - name: bug\n" +
		"    color: d73a4a\n" +
		"milestones: []\n"
	plan := Plan{Collections: []CollectionChange{{
		Path: "labels",
		Added: []EntryAddition{
			{Key: "enhancement", Fields: []FieldValue{{Name: "name", Value: stringValue("enhancement")}, {Name: "color", Value: stringValue("a2eeef")}}},
		},
	}}}
	out, err := ApplyYAML([]byte(src), plan)
	if err != nil {
		t.Fatal(err)
	}
	want := "labels:\n" +
		"  - name: bug\n" +
		"    color: d73a4a\n" +
		"  - name: enhancement\n" +
		"    color: a2eeef\n" +
		"milestones: []\n"
	if string(out) != want {
		t.Fatalf("out = %q, want %q", out, want)
	}
}

func TestApplyYAMLPopulatesEmptyDeclaredCollection(t *testing.T) {
	t.Parallel()
	src := "labels: []\nmilestones: []\n"
	plan := Plan{Collections: []CollectionChange{{
		Path: "labels",
		Added: []EntryAddition{
			{Key: "bug", Fields: []FieldValue{{Name: "name", Value: stringValue("bug")}, {Name: "color", Value: stringValue("d73a4a")}}},
		},
	}}}
	out, err := ApplyYAML([]byte(src), plan)
	if err != nil {
		t.Fatal(err)
	}
	want := "labels:\n" +
		"  - name: bug\n" +
		"    color: d73a4a\n" +
		"milestones: []\n"
	if string(out) != want {
		t.Fatalf("out = %q, want %q", out, want)
	}
}

func TestApplyYAMLReplacesWholeRulesetOnUpdateAndAppendsNewOne(t *testing.T) {
	t.Parallel()
	src := "rulesets:\n" +
		"  - name: protect-main\n" +
		"    enforcement: active\n" +
		"    target: branch\n" +
		"    include:\n" +
		"      - \"~DEFAULT_BRANCH\"\n" +
		"    rules: []\n"
	updated := config.RulesetConfig{Name: "protect-main", Enforcement: "evaluate", Target: "branch", Include: []string{"~DEFAULT_BRANCH"}, Rules: []config.RulesetRule{}}
	added := config.RulesetConfig{Name: "protect-tags", Enforcement: "active", Target: "tag", Include: []string{"v*"}, Rules: []config.RulesetRule{}}
	plan := Plan{Rulesets: &RulesetChange{Updated: []config.RulesetConfig{updated}, Added: []config.RulesetConfig{added}}}

	out, err := ApplyYAML([]byte(src), plan)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "enforcement: evaluate") {
		t.Fatalf("out = %s, want enforcement updated to evaluate", out)
	}
	if !strings.Contains(string(out), "protect-tags") {
		t.Fatalf("out = %s, want the new ruleset appended", out)
	}
	// The rewritten document must still be exactly one YAML document that
	// decodes cleanly; a byte-level sanity check that the sequence has both
	// entries as siblings.
	if strings.Count(string(out), "- name:") != 2 {
		t.Fatalf("out = %s, want exactly two ruleset entries", out)
	}
}

func TestApplyYAMLPreservesCRLFLineEndings(t *testing.T) {
	t.Parallel()
	src := "repository:\r\n  description: old\r\n"
	out, err := ApplyYAML([]byte(src), Plan{Scalars: []ScalarChange{
		{Path: "repository.description", Value: stringValue("new")},
	}})
	if err != nil {
		t.Fatal(err)
	}
	want := "repository:\r\n  description: new\r\n"
	if string(out) != want {
		t.Fatalf("out = %q, want %q", out, want)
	}
}

func TestApplyYAMLNoopPlanLeavesDocumentByteIdentical(t *testing.T) {
	t.Parallel()
	src := "schemaVersion: 1\nrepository:\n  # a comment\n  owner: octo\n  name: demo\n  visibility: public\n"
	out, err := ApplyYAML([]byte(src), Plan{})
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != src {
		t.Fatalf("out = %q, want the original bytes unchanged for an empty plan", out)
	}
}

func TestApplyYAMLRejectsUndeclaredField(t *testing.T) {
	t.Parallel()
	src := "repository:\n  owner: octo\n"
	_, err := ApplyYAML([]byte(src), Plan{Scalars: []ScalarChange{
		{Path: "repository.description", Value: stringValue("new")},
	}})
	if err == nil {
		t.Fatal("ApplyYAML() unexpectedly succeeded for a field the file never declared")
	}
}
