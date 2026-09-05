package refresh

import (
	"encoding/json"
	"fmt"

	"github.com/kkamegawa/gh-repospec/internal/config"
)

// ApplyJSON returns current with every planned change applied, re-marshaled
// as indented JSON. JSON carries no comments and no formatting worth
// preserving, so unlike ApplyYAML this reformats the whole document rather
// than editing byte ranges; the input format is retained (JSON stays JSON).
func ApplyJSON(current *config.Config, plan Plan) ([]byte, error) {
	updated := *current
	if err := applyScalarsToConfig(&updated, plan.Scalars); err != nil {
		return nil, err
	}
	for _, cc := range plan.Collections {
		if err := applyCollectionToConfig(&updated, cc); err != nil {
			return nil, err
		}
	}
	if plan.Rulesets != nil {
		updated.Rulesets = applyRulesetChange(updated.Rulesets, plan.Rulesets)
	}
	data, err := json.MarshalIndent(&updated, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("render refreshed JSON: %w", err)
	}
	return append(data, '\n'), nil
}

// applyScalarsToConfig sets each planned scalar change on cfg. The path set
// here is exactly the one computePlan emits; an unrecognized path is a bug
// in this package, not a data problem, so it fails loudly rather than being
// silently skipped.
func applyScalarsToConfig(cfg *config.Config, scalars []ScalarChange) error {
	for _, sc := range scalars {
		switch sc.Path {
		case "repository.description":
			cfg.Repository.Description = strPtr(sc.Value.Str)
		case "repository.homepage":
			cfg.Repository.Homepage = strPtr(sc.Value.Str)
		case "repository.visibility":
			cfg.Repository.Visibility = sc.Value.Str
		case "repository.defaultBranch":
			cfg.Repository.DefaultBranch = sc.Value.Str
		case "repository.archived":
			cfg.Repository.Archived = boolPtr(sc.Value.Bool)
		case "repository.mergeCommitTitle":
			cfg.Repository.MergeCommitTitle = sc.Value.Str
		case "repository.mergeCommitMessage":
			cfg.Repository.MergeCommitMessage = sc.Value.Str
		case "repository.squashMergeCommitTitle":
			cfg.Repository.SquashMergeCommitTitle = sc.Value.Str
		case "repository.squashMergeCommitMessage":
			cfg.Repository.SquashMergeCommitMessage = sc.Value.Str
		case "repository.releaseImmutability":
			cfg.Repository.ReleaseImmutability = boolPtr(sc.Value.Bool)
		case "repository.allowMergeCommit":
			cfg.Repository.AllowMergeCommit = boolPtr(sc.Value.Bool)
		case "repository.allowSquashMerge":
			cfg.Repository.AllowSquashMerge = boolPtr(sc.Value.Bool)
		case "repository.allowRebaseMerge":
			cfg.Repository.AllowRebaseMerge = boolPtr(sc.Value.Bool)
		case "repository.allowAutoMerge":
			cfg.Repository.AllowAutoMerge = boolPtr(sc.Value.Bool)
		case "repository.deleteBranchOnMerge":
			cfg.Repository.DeleteBranchOnMerge = boolPtr(sc.Value.Bool)
		case "repository.features.issues":
			cfg.Repository.Features.Issues = boolPtr(sc.Value.Bool)
		case "repository.features.projects":
			cfg.Repository.Features.Projects = boolPtr(sc.Value.Bool)
		case "repository.features.wiki":
			cfg.Repository.Features.Wiki = boolPtr(sc.Value.Bool)
		case "repository.features.discussions":
			cfg.Repository.Features.Discussions = boolPtr(sc.Value.Bool)
		case "actions.enabled":
			cfg.Actions.Enabled = boolPtr(sc.Value.Bool)
		case "actions.allowedActions":
			cfg.Actions.AllowedActions = sc.Value.Str
		case "actions.requireSHAForGitHubActions":
			cfg.Actions.RequireSHAForGitHubActions = boolPtr(sc.Value.Bool)
		case "actions.defaultWorkflowPermissions":
			cfg.Actions.DefaultWorkflowPermissions = sc.Value.Str
		case "actions.canApprovePullRequestWorkflows":
			cfg.Actions.CanApprovePullRequestWorkflows = boolPtr(sc.Value.Bool)
		case "actions.forkPullRequestApproval":
			cfg.Actions.ForkPullRequestApproval = sc.Value.Str
		case "actions.selectedActions.githubOwnedAllowed":
			cfg.Actions.SelectedActions.GitHubOwnedAllowed = sc.Value.Bool
		case "actions.selectedActions.verifiedAllowed":
			cfg.Actions.SelectedActions.VerifiedAllowed = sc.Value.Bool
		case "security.dependencyGraph.enabled":
			cfg.Security.DependencyGraph.Enabled = sc.Value.Bool
		case "security.dependabotAlerts.enabled":
			cfg.Security.DependabotAlerts.Enabled = sc.Value.Bool
		case "security.dependabotSecurityUpdates.enabled":
			cfg.Security.DependabotSecurityUpdates.Enabled = sc.Value.Bool
		case "security.secretScanning.enabled":
			cfg.Security.SecretScanning.Enabled = sc.Value.Bool
		case "security.pushProtection.enabled":
			cfg.Security.PushProtection.Enabled = sc.Value.Bool
		case "security.codeqlDefaultSetup.enabled":
			cfg.Security.CodeQLDefaultSetup.Enabled = sc.Value.Bool
		case "security.privateVulnerabilityReporting.enabled":
			cfg.Security.PrivateVulnerabilityReporting.Enabled = sc.Value.Bool
		default:
			return fmt.Errorf("unrecognized scalar path %q", sc.Path)
		}
	}
	return nil
}

func strPtr(s string) *string { return &s }
func boolPtr(b bool) *bool    { return &b }

func applyCollectionToConfig(cfg *config.Config, change CollectionChange) error {
	switch change.Path {
	case "labels":
		cfg.Labels = applyLabelChange(cfg.Labels, change)
	case "milestones":
		cfg.Milestones = applyMilestoneChange(cfg.Milestones, change)
	case "actions.variables":
		cfg.Actions.Variables = applyVariableChange(cfg.Actions.Variables, change)
	case "actions.environments":
		cfg.Actions.Environments = applyEnvironmentChange(cfg.Actions.Environments, change)
	case "agents.variables":
		cfg.Agents.Variables = applyVariableChange(cfg.Agents.Variables, change)
	default:
		return fmt.Errorf("unrecognized collection path %q", change.Path)
	}
	return nil
}

func applyLabelChange(current []config.LabelConfig, change CollectionChange) []config.LabelConfig {
	updated := append([]config.LabelConfig{}, current...)
	updates := make(map[string][]ScalarChange, len(change.Updated))
	for _, u := range change.Updated {
		updates[u.Key] = u.Scalars
	}
	for i := range updated {
		for _, sc := range updates[updated[i].Name] {
			switch sc.Path {
			case "color":
				updated[i].Color = sc.Value.Str
			case "description":
				updated[i].Description = sc.Value.Str
			}
		}
	}
	for _, add := range change.Added {
		var label config.LabelConfig
		for _, f := range add.Fields {
			switch f.Name {
			case "name":
				label.Name = f.Value.Str
			case "color":
				label.Color = f.Value.Str
			case "description":
				label.Description = f.Value.Str
			}
		}
		updated = append(updated, label)
	}
	return updated
}

func applyMilestoneChange(current []config.MilestoneConfig, change CollectionChange) []config.MilestoneConfig {
	updated := append([]config.MilestoneConfig{}, current...)
	updates := make(map[string][]ScalarChange, len(change.Updated))
	for _, u := range change.Updated {
		updates[u.Key] = u.Scalars
	}
	for i := range updated {
		for _, sc := range updates[updated[i].Title] {
			switch sc.Path {
			case "description":
				updated[i].Description = sc.Value.Str
			case "state":
				updated[i].State = sc.Value.Str
			case "dueOn":
				updated[i].DueOn = sc.Value.Str
			}
		}
	}
	for _, add := range change.Added {
		var milestone config.MilestoneConfig
		for _, f := range add.Fields {
			switch f.Name {
			case "title":
				milestone.Title = f.Value.Str
			case "description":
				milestone.Description = f.Value.Str
			case "state":
				milestone.State = f.Value.Str
			case "dueOn":
				milestone.DueOn = f.Value.Str
			}
		}
		updated = append(updated, milestone)
	}
	return updated
}

func applyVariableChange(current []config.VariableRef, change CollectionChange) []config.VariableRef {
	updated := append([]config.VariableRef{}, current...)
	updates := make(map[string][]ScalarChange, len(change.Updated))
	for _, u := range change.Updated {
		updates[u.Key] = u.Scalars
	}
	for i := range updated {
		for _, sc := range updates[updated[i].Name] {
			if sc.Path == "value" {
				updated[i].Value = sc.Value.Str
			}
		}
	}
	for _, add := range change.Added {
		var variable config.VariableRef
		for _, f := range add.Fields {
			switch f.Name {
			case "name":
				variable.Name = f.Value.Str
			case "value":
				variable.Value = f.Value.Str
			}
		}
		updated = append(updated, variable)
	}
	return updated
}

func applyEnvironmentChange(current []config.Environment, change CollectionChange) []config.Environment {
	updated := append([]config.Environment{}, current...)
	for _, add := range change.Added {
		var env config.Environment
		for _, f := range add.Fields {
			if f.Name == "name" {
				env.Name = f.Value.Str
			}
		}
		updated = append(updated, env)
	}
	return updated
}

func applyRulesetChange(current []config.RulesetConfig, change *RulesetChange) []config.RulesetConfig {
	byName := make(map[string]config.RulesetConfig, len(change.Updated))
	for _, u := range change.Updated {
		byName[u.Name] = u
	}
	updated := make([]config.RulesetConfig, len(current))
	for i, c := range current {
		if replacement, ok := byName[c.Name]; ok {
			updated[i] = replacement
		} else {
			updated[i] = c
		}
	}
	return append(updated, change.Added...)
}
