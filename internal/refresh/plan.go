package refresh

import (
	"reflect"
	"sort"

	"github.com/kkamegawa/gh-repospec/internal/config"
)

// Plan is what --into would change, computed purely from the file's own
// decoded configuration (current) and the imported state (desired). It has
// no file-format or byte-editing knowledge: yamlapply.go and jsonapply.go
// each turn it into an actual document independently.
type Plan struct {
	Scalars     []ScalarChange
	Collections []CollectionChange
	Rulesets    *RulesetChange
}

// Changed reports whether applying the plan would change anything at all.
func (p Plan) Changed() bool {
	if len(p.Scalars) > 0 {
		return true
	}
	for _, c := range p.Collections {
		if len(c.Updated) > 0 || len(c.Added) > 0 {
			return true
		}
	}
	if p.Rulesets != nil && (len(p.Rulesets.Updated) > 0 || len(p.Rulesets.Added) > 0) {
		return true
	}
	return false
}

// RulesetChange is rulesets' declared-collection change. Unlike the other
// collections, an updated ruleset is replaced as a whole unit rather than
// field by field: bypassActors and rules are already nested, order- and
// structure-sensitive data with no local-only sub-fields to preserve, so
// there is little a narrower per-field splice would buy, and treating the
// whole ruleset as the unit of change keeps the rewrite simple and
// unambiguous. Comments attached inside a ruleset that changes are not
// preserved through that change; comments on unchanged rulesets, and
// everything outside the rulesets collection, are unaffected.
type RulesetChange struct {
	Updated    []config.RulesetConfig
	Added      []config.RulesetConfig
	NotApplied []string
}

// ScalarChange is one declared scalar field whose value differs from the
// imported state. Path is a dotted, human-readable form such as
// "repository.description" or "actions.selectedActions.verifiedAllowed".
type ScalarChange struct {
	Path  string
	Value ScalarValue
}

// ScalarKind tags the Go type ScalarValue actually holds, since YAML
// rendering and JSON encoding both need to know it precisely (an unquoted
// "true" is not the string "true").
type ScalarKind int

const (
	ScalarString ScalarKind = iota
	ScalarBool
)

type ScalarValue struct {
	Kind ScalarKind
	Str  string
	Bool bool
}

func stringValue(s string) ScalarValue { return ScalarValue{Kind: ScalarString, Str: s} }
func boolValue(b bool) ScalarValue     { return ScalarValue{Kind: ScalarBool, Bool: b} }

// CollectionChange is one declared collection's additions and updates.
// NotApplied lists entries this file declares that GitHub does not have;
// those are never removed, only reported.
type CollectionChange struct {
	Path       string
	Updated    []EntryUpdate
	Added      []EntryAddition
	NotApplied []string
}

// EntryUpdate is one existing entry, matched by Key, whose declared fields
// differ from the imported state.
type EntryUpdate struct {
	Key     string
	Scalars []ScalarChange // paths are field names local to the entry, e.g. "color"
}

// EntryAddition is an entry GitHub has that the file does not declare yet.
// New entries are appended in Key order, never interleaved with retained
// entries, matching design.md's "new entries are appended in name order."
type EntryAddition struct {
	Key    string
	Fields []FieldValue
}

// FieldValue is one field of a brand-new entry, in the order it should be
// rendered.
type FieldValue struct {
	Name  string
	Value ScalarValue
}

// computePlan compares current (the file's own declared configuration) with
// desired (imported state) and returns every change --into would make.
// Nothing is included for a field or section current does not declare: an
// undeclared path stays undeclared, exactly as design.md requires.
func computePlan(current, desired *config.Config) Plan {
	var plan Plan
	plan.Scalars = append(plan.Scalars, repositoryScalarChanges(current.Repository, desired.Repository)...)
	if current.Actions != nil && desired.Actions != nil {
		plan.Scalars = append(plan.Scalars, actionsScalarChanges(current.Actions, desired.Actions)...)
	}
	if current.Security != nil && desired.Security != nil {
		plan.Scalars = append(plan.Scalars, securityScalarChanges(current.Security, desired.Security)...)
	}

	if current.Labels != nil {
		plan.Collections = append(plan.Collections, labelCollectionChange(current.Labels, desired.Labels))
	}
	if current.Milestones != nil {
		plan.Collections = append(plan.Collections, milestoneCollectionChange(current.Milestones, desired.Milestones))
	}
	if current.Rulesets != nil {
		plan.Rulesets = rulesetChange(current.Rulesets, desired.Rulesets)
	}
	if current.Actions != nil && desired.Actions != nil {
		if current.Actions.Variables != nil {
			plan.Collections = append(plan.Collections, variableCollectionChange("actions.variables", current.Actions.Variables, desired.Actions.Variables))
		}
		if current.Actions.Environments != nil {
			plan.Collections = append(plan.Collections, environmentCollectionChange(current.Actions.Environments, desired.Actions.Environments))
		}
	}
	if current.Agents != nil && desired.Agents != nil {
		if current.Agents.Variables != nil {
			plan.Collections = append(plan.Collections, variableCollectionChange("agents.variables", current.Agents.Variables, desired.Agents.Variables))
		}
	}
	return plan
}

func repositoryScalarChanges(current, desired config.RepositoryConfig) []ScalarChange {
	var changes []ScalarChange
	add := func(path string, value ScalarValue) { changes = append(changes, ScalarChange{Path: path, Value: value}) }

	if current.Description != nil && (desired.Description == nil || *current.Description != *desired.Description) && desired.Description != nil {
		add("repository.description", stringValue(*desired.Description))
	}
	if current.Homepage != nil && desired.Homepage != nil && *current.Homepage != *desired.Homepage {
		add("repository.homepage", stringValue(*desired.Homepage))
	}
	if current.Visibility != "" && current.Visibility != desired.Visibility && desired.Visibility != "" {
		add("repository.visibility", stringValue(desired.Visibility))
	}
	if current.DefaultBranch != "" && current.DefaultBranch != desired.DefaultBranch && desired.DefaultBranch != "" {
		add("repository.defaultBranch", stringValue(desired.DefaultBranch))
	}
	if current.Archived != nil && desired.Archived != nil && *current.Archived != *desired.Archived {
		add("repository.archived", boolValue(*desired.Archived))
	}
	if current.MergeCommitTitle != "" && current.MergeCommitTitle != desired.MergeCommitTitle && desired.MergeCommitTitle != "" {
		add("repository.mergeCommitTitle", stringValue(desired.MergeCommitTitle))
	}
	if current.MergeCommitMessage != "" && current.MergeCommitMessage != desired.MergeCommitMessage && desired.MergeCommitMessage != "" {
		add("repository.mergeCommitMessage", stringValue(desired.MergeCommitMessage))
	}
	if current.SquashMergeCommitTitle != "" && current.SquashMergeCommitTitle != desired.SquashMergeCommitTitle && desired.SquashMergeCommitTitle != "" {
		add("repository.squashMergeCommitTitle", stringValue(desired.SquashMergeCommitTitle))
	}
	if current.SquashMergeCommitMessage != "" && current.SquashMergeCommitMessage != desired.SquashMergeCommitMessage && desired.SquashMergeCommitMessage != "" {
		add("repository.squashMergeCommitMessage", stringValue(desired.SquashMergeCommitMessage))
	}
	if current.ReleaseImmutability != nil && desired.ReleaseImmutability != nil && *current.ReleaseImmutability != *desired.ReleaseImmutability {
		add("repository.releaseImmutability", boolValue(*desired.ReleaseImmutability))
	}
	if current.AllowMergeCommit != nil && desired.AllowMergeCommit != nil && *current.AllowMergeCommit != *desired.AllowMergeCommit {
		add("repository.allowMergeCommit", boolValue(*desired.AllowMergeCommit))
	}
	if current.AllowSquashMerge != nil && desired.AllowSquashMerge != nil && *current.AllowSquashMerge != *desired.AllowSquashMerge {
		add("repository.allowSquashMerge", boolValue(*desired.AllowSquashMerge))
	}
	if current.AllowRebaseMerge != nil && desired.AllowRebaseMerge != nil && *current.AllowRebaseMerge != *desired.AllowRebaseMerge {
		add("repository.allowRebaseMerge", boolValue(*desired.AllowRebaseMerge))
	}
	if current.AllowAutoMerge != nil && desired.AllowAutoMerge != nil && *current.AllowAutoMerge != *desired.AllowAutoMerge {
		add("repository.allowAutoMerge", boolValue(*desired.AllowAutoMerge))
	}
	if current.DeleteBranchOnMerge != nil && desired.DeleteBranchOnMerge != nil && *current.DeleteBranchOnMerge != *desired.DeleteBranchOnMerge {
		add("repository.deleteBranchOnMerge", boolValue(*desired.DeleteBranchOnMerge))
	}

	if current.Features != nil && desired.Features != nil {
		cf, df := current.Features, desired.Features
		if cf.Issues != nil && df.Issues != nil && *cf.Issues != *df.Issues {
			add("repository.features.issues", boolValue(*df.Issues))
		}
		if cf.Projects != nil && df.Projects != nil && *cf.Projects != *df.Projects {
			add("repository.features.projects", boolValue(*df.Projects))
		}
		if cf.Wiki != nil && df.Wiki != nil && *cf.Wiki != *df.Wiki {
			add("repository.features.wiki", boolValue(*df.Wiki))
		}
		if cf.Discussions != nil && df.Discussions != nil && *cf.Discussions != *df.Discussions {
			add("repository.features.discussions", boolValue(*df.Discussions))
		}
	}
	return changes
}

func actionsScalarChanges(current, desired *config.ActionsConfig) []ScalarChange {
	var changes []ScalarChange
	add := func(path string, value ScalarValue) { changes = append(changes, ScalarChange{Path: path, Value: value}) }

	if current.Enabled != nil && desired.Enabled != nil && *current.Enabled != *desired.Enabled {
		add("actions.enabled", boolValue(*desired.Enabled))
	}
	if current.AllowedActions != "" && current.AllowedActions != desired.AllowedActions && desired.AllowedActions != "" {
		add("actions.allowedActions", stringValue(desired.AllowedActions))
	}
	if current.RequireSHAForGitHubActions != nil && desired.RequireSHAForGitHubActions != nil && *current.RequireSHAForGitHubActions != *desired.RequireSHAForGitHubActions {
		add("actions.requireSHAForGitHubActions", boolValue(*desired.RequireSHAForGitHubActions))
	}
	if current.DefaultWorkflowPermissions != "" && current.DefaultWorkflowPermissions != desired.DefaultWorkflowPermissions && desired.DefaultWorkflowPermissions != "" {
		add("actions.defaultWorkflowPermissions", stringValue(desired.DefaultWorkflowPermissions))
	}
	if current.CanApprovePullRequestWorkflows != nil && desired.CanApprovePullRequestWorkflows != nil && *current.CanApprovePullRequestWorkflows != *desired.CanApprovePullRequestWorkflows {
		add("actions.canApprovePullRequestWorkflows", boolValue(*desired.CanApprovePullRequestWorkflows))
	}
	if current.ForkPullRequestApproval != "" && current.ForkPullRequestApproval != desired.ForkPullRequestApproval && desired.ForkPullRequestApproval != "" {
		add("actions.forkPullRequestApproval", stringValue(desired.ForkPullRequestApproval))
	}
	if current.SelectedActions != nil && desired.SelectedActions != nil {
		cs, ds := current.SelectedActions, desired.SelectedActions
		if cs.GitHubOwnedAllowed != ds.GitHubOwnedAllowed {
			add("actions.selectedActions.githubOwnedAllowed", boolValue(ds.GitHubOwnedAllowed))
		}
		if cs.VerifiedAllowed != ds.VerifiedAllowed {
			add("actions.selectedActions.verifiedAllowed", boolValue(ds.VerifiedAllowed))
		}
	}
	return changes
}

func securityScalarChanges(current, desired *config.SecurityConfig) []ScalarChange {
	var changes []ScalarChange
	feature := func(path string, c, d *config.FeatureSetting) {
		if c != nil && d != nil && c.Enabled != d.Enabled {
			changes = append(changes, ScalarChange{Path: path, Value: boolValue(d.Enabled)})
		}
	}
	feature("security.dependencyGraph.enabled", current.DependencyGraph, desired.DependencyGraph)
	feature("security.dependabotAlerts.enabled", current.DependabotAlerts, desired.DependabotAlerts)
	feature("security.dependabotSecurityUpdates.enabled", current.DependabotSecurityUpdates, desired.DependabotSecurityUpdates)
	feature("security.secretScanning.enabled", current.SecretScanning, desired.SecretScanning)
	feature("security.pushProtection.enabled", current.PushProtection, desired.PushProtection)
	feature("security.codeqlDefaultSetup.enabled", current.CodeQLDefaultSetup, desired.CodeQLDefaultSetup)
	feature("security.privateVulnerabilityReporting.enabled", current.PrivateVulnerabilityReporting, desired.PrivateVulnerabilityReporting)
	return changes
}

func labelCollectionChange(current, desired []config.LabelConfig) CollectionChange {
	change := CollectionChange{Path: "labels"}
	byName := make(map[string]config.LabelConfig, len(desired))
	for _, l := range desired {
		byName[l.Name] = l
	}
	seen := make(map[string]bool, len(current))
	for _, c := range current {
		seen[c.Name] = true
		d, ok := byName[c.Name]
		if !ok {
			change.NotApplied = append(change.NotApplied, c.Name)
			continue
		}
		var scalars []ScalarChange
		if c.Color != d.Color {
			scalars = append(scalars, ScalarChange{Path: "color", Value: stringValue(d.Color)})
		}
		if c.Description != d.Description {
			scalars = append(scalars, ScalarChange{Path: "description", Value: stringValue(d.Description)})
		}
		if len(scalars) > 0 {
			change.Updated = append(change.Updated, EntryUpdate{Key: c.Name, Scalars: scalars})
		}
	}
	var toAdd []config.LabelConfig
	for _, d := range desired {
		if !seen[d.Name] {
			toAdd = append(toAdd, d)
		}
	}
	sort.Slice(toAdd, func(i, j int) bool { return toAdd[i].Name < toAdd[j].Name })
	for _, d := range toAdd {
		fields := []FieldValue{{Name: "name", Value: stringValue(d.Name)}, {Name: "color", Value: stringValue(d.Color)}}
		if d.Description != "" {
			fields = append(fields, FieldValue{Name: "description", Value: stringValue(d.Description)})
		}
		change.Added = append(change.Added, EntryAddition{Key: d.Name, Fields: fields})
	}
	return change
}

func milestoneCollectionChange(current, desired []config.MilestoneConfig) CollectionChange {
	change := CollectionChange{Path: "milestones"}
	byTitle := make(map[string]config.MilestoneConfig, len(desired))
	for _, m := range desired {
		byTitle[m.Title] = m
	}
	seen := make(map[string]bool, len(current))
	for _, c := range current {
		seen[c.Title] = true
		d, ok := byTitle[c.Title]
		if !ok {
			change.NotApplied = append(change.NotApplied, c.Title)
			continue
		}
		var scalars []ScalarChange
		if c.Description != d.Description {
			scalars = append(scalars, ScalarChange{Path: "description", Value: stringValue(d.Description)})
		}
		if c.State != "" && d.State != "" && c.State != d.State {
			scalars = append(scalars, ScalarChange{Path: "state", Value: stringValue(d.State)})
		}
		if c.DueOn != "" && d.DueOn != "" && c.DueOn != d.DueOn {
			scalars = append(scalars, ScalarChange{Path: "dueOn", Value: stringValue(d.DueOn)})
		}
		if len(scalars) > 0 {
			change.Updated = append(change.Updated, EntryUpdate{Key: c.Title, Scalars: scalars})
		}
	}
	var toAdd []config.MilestoneConfig
	for _, d := range desired {
		if !seen[d.Title] {
			toAdd = append(toAdd, d)
		}
	}
	sort.Slice(toAdd, func(i, j int) bool { return toAdd[i].Title < toAdd[j].Title })
	for _, d := range toAdd {
		fields := []FieldValue{{Name: "title", Value: stringValue(d.Title)}}
		if d.Description != "" {
			fields = append(fields, FieldValue{Name: "description", Value: stringValue(d.Description)})
		}
		if d.State != "" {
			fields = append(fields, FieldValue{Name: "state", Value: stringValue(d.State)})
		}
		if d.DueOn != "" {
			fields = append(fields, FieldValue{Name: "dueOn", Value: stringValue(d.DueOn)})
		}
		change.Added = append(change.Added, EntryAddition{Key: d.Title, Fields: fields})
	}
	return change
}

func variableCollectionChange(path string, current, desired []config.VariableRef) CollectionChange {
	change := CollectionChange{Path: path}
	byName := make(map[string]config.VariableRef, len(desired))
	for _, v := range desired {
		byName[v.Name] = v
	}
	seen := make(map[string]bool, len(current))
	for _, c := range current {
		seen[c.Name] = true
		d, ok := byName[c.Name]
		if !ok {
			change.NotApplied = append(change.NotApplied, c.Name)
			continue
		}
		if c.Value != d.Value {
			change.Updated = append(change.Updated, EntryUpdate{
				Key:     c.Name,
				Scalars: []ScalarChange{{Path: "value", Value: stringValue(d.Value)}},
			})
		}
	}
	var toAdd []config.VariableRef
	for _, d := range desired {
		if !seen[d.Name] {
			toAdd = append(toAdd, d)
		}
	}
	sort.Slice(toAdd, func(i, j int) bool { return toAdd[i].Name < toAdd[j].Name })
	for _, d := range toAdd {
		change.Added = append(change.Added, EntryAddition{
			Key:    d.Name,
			Fields: []FieldValue{{Name: "name", Value: stringValue(d.Name)}, {Name: "value", Value: stringValue(d.Value)}},
		})
	}
	return change
}

func rulesetChange(current, desired []config.RulesetConfig) *RulesetChange {
	change := &RulesetChange{}
	byName := make(map[string]config.RulesetConfig, len(desired))
	for _, r := range desired {
		byName[r.Name] = r
	}
	seen := make(map[string]bool, len(current))
	for _, c := range current {
		seen[c.Name] = true
		d, ok := byName[c.Name]
		if !ok {
			change.NotApplied = append(change.NotApplied, c.Name)
			continue
		}
		if !reflect.DeepEqual(c, d) {
			change.Updated = append(change.Updated, d)
		}
	}
	var toAdd []config.RulesetConfig
	for _, d := range desired {
		if !seen[d.Name] {
			toAdd = append(toAdd, d)
		}
	}
	sort.Slice(toAdd, func(i, j int) bool { return toAdd[i].Name < toAdd[j].Name })
	change.Added = toAdd
	return change
}

func environmentCollectionChange(current, desired []config.Environment) CollectionChange {
	change := CollectionChange{Path: "actions.environments"}
	have := make(map[string]bool, len(desired))
	for _, e := range desired {
		have[e.Name] = true
	}
	seen := make(map[string]bool, len(current))
	for _, c := range current {
		seen[c.Name] = true
		if !have[c.Name] {
			change.NotApplied = append(change.NotApplied, c.Name)
		}
	}
	var toAdd []config.Environment
	for _, d := range desired {
		if !seen[d.Name] {
			toAdd = append(toAdd, d)
		}
	}
	sort.Slice(toAdd, func(i, j int) bool { return toAdd[i].Name < toAdd[j].Name })
	for _, d := range toAdd {
		change.Added = append(change.Added, EntryAddition{Key: d.Name, Fields: []FieldValue{{Name: "name", Value: stringValue(d.Name)}}})
	}
	return change
}
