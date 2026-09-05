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
