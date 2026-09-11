package refresh

import (
	"testing"

	"github.com/kkamegawa/gh-repospec/internal/config"
)

func TestComputePlanOnlyTouchesDeclaredScalars(t *testing.T) {
	t.Parallel()
	current := &config.Config{
		Repository: config.RepositoryConfig{
			Owner: "octo", Name: "demo", Visibility: "public",
			Description: strPtr("old"),
			// Homepage is undeclared (nil).
		},
	}
	desired := &config.Config{
		Repository: config.RepositoryConfig{
			Owner: "octo", Name: "demo", Visibility: "public",
			Description: strPtr("new"),
			Homepage:    strPtr("https://example.com"),
		},
	}
	plan := computePlan(current, desired)
	if len(plan.Scalars) != 1 || plan.Scalars[0].Path != "repository.description" || plan.Scalars[0].Value.Str != "new" {
		t.Fatalf("Scalars = %#v, want only repository.description updated", plan.Scalars)
	}
}

func TestComputePlanNoopWhenValuesAlreadyMatch(t *testing.T) {
	t.Parallel()
	cfg := func() config.RepositoryConfig {
		return config.RepositoryConfig{Owner: "octo", Name: "demo", Visibility: "public", Description: strPtr("same")}
	}
	current := &config.Config{Repository: cfg()}
	desired := &config.Config{Repository: cfg()}
	plan := computePlan(current, desired)
	if plan.Changed() {
		t.Fatalf("plan.Changed() = true, want false when nothing differs: %#v", plan)
	}
}

func TestComputePlanFeaturesAndSecurity(t *testing.T) {
	t.Parallel()
	current := &config.Config{
		Repository: config.RepositoryConfig{
			Owner: "o", Name: "n", Visibility: "public",
			Features: &config.RepositoryFeatures{Issues: boolPtr(true), Wiki: boolPtr(true)},
		},
		Security: &config.SecurityConfig{
			SecretScanning: &config.FeatureSetting{Enabled: false},
		},
	}
	desired := &config.Config{
		Repository: config.RepositoryConfig{
			Owner: "o", Name: "n", Visibility: "public",
			Features: &config.RepositoryFeatures{Issues: boolPtr(true), Wiki: boolPtr(false)},
		},
		Security: &config.SecurityConfig{
			SecretScanning: &config.FeatureSetting{Enabled: true},
		},
	}
	plan := computePlan(current, desired)
	if !hasScalar(plan.Scalars, "repository.features.wiki", boolValue(false)) {
		t.Fatalf("Scalars = %#v, want repository.features.wiki=false", plan.Scalars)
	}
	if hasScalarPath(plan.Scalars, "repository.features.issues") {
		t.Fatalf("Scalars = %#v, want issues unchanged (already true both sides) not reported", plan.Scalars)
	}
	if !hasScalar(plan.Scalars, "security.secretScanning.enabled", boolValue(true)) {
		t.Fatalf("Scalars = %#v, want security.secretScanning.enabled=true", plan.Scalars)
	}
}

func TestComputePlanRefreshesDeclaredAIPullRequestScan(t *testing.T) {
	t.Parallel()
	current := &config.Config{Security: &config.SecurityConfig{
		AIPullRequestScan: &config.FeatureSetting{Enabled: false, Optional: true},
	}}
	desired := &config.Config{Security: &config.SecurityConfig{
		AIPullRequestScan: &config.FeatureSetting{Enabled: true},
	}}
	plan := computePlan(current, desired)
	if len(plan.Scalars) != 1 || !hasScalar(plan.Scalars, "security.aiPullRequestScan.enabled", boolValue(true)) {
		t.Fatalf("Scalars = %#v, want only security.aiPullRequestScan.enabled=true", plan.Scalars)
	}
}

// TestComputePlanKeepsAIPullRequestScanWhenLiveStateUnreadable guards the
// design rule that a declared security feature whose live state could not
// be read (import leaves it nil after a 403 or 404) keeps its declaration:
// no change is planned, so the file is left untouched.
func TestComputePlanKeepsAIPullRequestScanWhenLiveStateUnreadable(t *testing.T) {
	t.Parallel()
	current := &config.Config{Security: &config.SecurityConfig{
		AIPullRequestScan: &config.FeatureSetting{Enabled: true},
	}}
	desired := &config.Config{Security: &config.SecurityConfig{}}
	plan := computePlan(current, desired)
	if plan.Changed() {
		t.Fatalf("plan = %#v, want no change for an unreadable live AI Scan state", plan)
	}
}

func TestComputePlanNeverIntroducesUndeclaredAIPullRequestScan(t *testing.T) {
	t.Parallel()
	current := &config.Config{Security: &config.SecurityConfig{}}
	desired := &config.Config{Security: &config.SecurityConfig{
		AIPullRequestScan: &config.FeatureSetting{Enabled: true},
	}}
	plan := computePlan(current, desired)
	if hasScalarPath(plan.Scalars, "security.aiPullRequestScan.enabled") {
		t.Fatalf("Scalars = %#v, want an undeclared aiPullRequestScan left undeclared", plan.Scalars)
	}
}

func TestComputePlanLabelsAddsUpdatesAndKeepsNotApplied(t *testing.T) {
	t.Parallel()
	current := &config.Config{
		Labels: []config.LabelConfig{
			{Name: "bug", Color: "d73a4a"},
			{Name: "stale", Color: "cccccc"}, // not on GitHub
		},
	}
	desired := &config.Config{
		Labels: []config.LabelConfig{
			{Name: "bug", Color: "ff0000"}, // color changed
			{Name: "enhancement", Color: "a2eeef"},
		},
	}
	plan := computePlan(current, desired)
	if len(plan.Collections) != 1 {
		t.Fatalf("Collections = %#v, want one (labels)", plan.Collections)
	}
	labels := plan.Collections[0]
	if labels.Path != "labels" {
		t.Fatalf("Path = %q", labels.Path)
	}
	if len(labels.Updated) != 1 || labels.Updated[0].Key != "bug" {
		t.Fatalf("Updated = %#v, want bug updated", labels.Updated)
	}
	if len(labels.Added) != 1 || labels.Added[0].Key != "enhancement" {
		t.Fatalf("Added = %#v, want enhancement added", labels.Added)
	}
	if len(labels.NotApplied) != 1 || labels.NotApplied[0] != "stale" {
		t.Fatalf("NotApplied = %#v, want stale kept and reported, never deleted", labels.NotApplied)
	}
}

// TestComputePlanNeverIntroducesAnUndeclaredLabelDescription guards against
// planning a description update purely because the imported label has one:
// an omitted description decodes as "", indistinguishable from "declared
// empty", and planning an update there would ask the YAML writer to add a
// sub-field that was never declared, which it refuses.
func TestComputePlanNeverIntroducesAnUndeclaredLabelDescription(t *testing.T) {
	t.Parallel()
	current := &config.Config{Labels: []config.LabelConfig{{Name: "bug", Color: "d73a4a"}}}
	desired := &config.Config{Labels: []config.LabelConfig{{Name: "bug", Color: "d73a4a", Description: "a bug"}}}
	plan := computePlan(current, desired)
	if len(plan.Collections) != 1 || len(plan.Collections[0].Updated) != 0 {
		t.Fatalf("Collections = %#v, want no update planned for an undeclared description", plan.Collections)
	}
}

// TestComputePlanRefreshesAlreadyDeclaredLabelDescriptionToEmpty guards the
// other direction: once a description is already declared, GitHub reporting
// it removed is a value change to an existing field, not an add, and must
// still refresh through.
func TestComputePlanRefreshesAlreadyDeclaredLabelDescriptionToEmpty(t *testing.T) {
	t.Parallel()
	current := &config.Config{Labels: []config.LabelConfig{{Name: "bug", Color: "d73a4a", Description: "a bug"}}}
	desired := &config.Config{Labels: []config.LabelConfig{{Name: "bug", Color: "d73a4a"}}}
	plan := computePlan(current, desired)
	if len(plan.Collections) != 1 || len(plan.Collections[0].Updated) != 1 {
		t.Fatalf("Collections = %#v, want the declared description cleared", plan.Collections)
	}
}

// TestComputePlanRefreshesDeclaredMilestoneStateAndDueOnEvenWhenGitHubClearsThem
// guards against the mirror-image bug: requiring the desired value to be
// non-empty too (in addition to the declared one) left a stale declared
// state or dueOn in place whenever GitHub reported the field absent,
// breaking import -> refresh -> plan convergence.
func TestComputePlanRefreshesDeclaredMilestoneStateAndDueOnEvenWhenGitHubClearsThem(t *testing.T) {
	t.Parallel()
	current := &config.Config{Milestones: []config.MilestoneConfig{{Title: "v1", State: "closed", DueOn: "2026-01-01"}}}
	desired := &config.Config{Milestones: []config.MilestoneConfig{{Title: "v1"}}}
	plan := computePlan(current, desired)
	if len(plan.Collections) != 1 || len(plan.Collections[0].Updated) != 1 {
		t.Fatalf("Collections = %#v, want state and dueOn refreshed to reflect GitHub clearing them", plan.Collections)
	}
	scalars := plan.Collections[0].Updated[0].Scalars
	var sawState, sawDueOn bool
	for _, sc := range scalars {
		if sc.Path == "state" {
			sawState = true
		}
		if sc.Path == "dueOn" {
			sawDueOn = true
		}
	}
	if !sawState || !sawDueOn {
		t.Fatalf("Scalars = %#v, want both state and dueOn updates", scalars)
	}
}

func TestComputePlanUndeclaredCollectionIsNeverTouched(t *testing.T) {
	t.Parallel()
	current := &config.Config{} // Labels is nil: undeclared
	desired := &config.Config{Labels: []config.LabelConfig{{Name: "bug", Color: "d73a4a"}}}
	plan := computePlan(current, desired)
	if len(plan.Collections) != 0 {
		t.Fatalf("Collections = %#v, want none for an undeclared labels key", plan.Collections)
	}
}

func TestComputePlanEmptyDeclaredCollectionStillGetsAdditions(t *testing.T) {
	t.Parallel()
	current := &config.Config{Labels: []config.LabelConfig{}} // declared empty
	desired := &config.Config{Labels: []config.LabelConfig{{Name: "bug", Color: "d73a4a"}}}
	plan := computePlan(current, desired)
	if len(plan.Collections) != 1 || len(plan.Collections[0].Added) != 1 {
		t.Fatalf("Collections = %#v, want the empty declared collection refreshed with the new label", plan.Collections)
	}
}

func TestComputePlanRulesetsReplaceWholeEntryOnAnyChange(t *testing.T) {
	t.Parallel()
	base := config.RulesetConfig{Name: "protect-main", Enforcement: "active", Target: "branch", Include: []string{"~DEFAULT_BRANCH"}}
	changed := base
	changed.Enforcement = "evaluate"
	current := &config.Config{Rulesets: []config.RulesetConfig{base, {Name: "legacy", Enforcement: "active", Target: "branch", Include: []string{"main"}}}}
	desired := &config.Config{Rulesets: []config.RulesetConfig{changed}}

	plan := computePlan(current, desired)
	if plan.Rulesets == nil {
		t.Fatal("Rulesets plan is nil")
	}
	if len(plan.Rulesets.Updated) != 1 || plan.Rulesets.Updated[0].Enforcement != "evaluate" {
		t.Fatalf("Updated = %#v, want protect-main replaced with the evaluate enforcement version", plan.Rulesets.Updated)
	}
	if len(plan.Rulesets.NotApplied) != 1 || plan.Rulesets.NotApplied[0] != "legacy" {
		t.Fatalf("NotApplied = %#v, want legacy kept, never deleted", plan.Rulesets.NotApplied)
	}
}

func hasScalar(scalars []ScalarChange, path string, value ScalarValue) bool {
	for _, s := range scalars {
		if s.Path == path && s.Value == value {
			return true
		}
	}
	return false
}

func hasScalarPath(scalars []ScalarChange, path string) bool {
	for _, s := range scalars {
		if s.Path == path {
			return true
		}
	}
	return false
}
