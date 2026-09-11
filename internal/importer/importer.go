// Package importer reads the live state of an existing GitHub repository and
// reverse-maps it into a schemaVersion 1 configuration. It performs no
// GitHub write of any kind.
//
// Two kinds of GitHub state cannot be represented in the returned
// [config.Config] and are reported instead, through [Result.Diagnostics]:
// secret values, which the GitHub API never returns, and repository state
// the schema has no field for (an inherited organization ruleset, for
// example). Local intent — seed sources, commands, agent file paths,
// secret sourceEnv and updatePolicy, and repository.create /
// allowVisibilityChange — is never inferred, because no repository state
// implies it; those fields are simply left unset on the returned
// configuration.
//
// A failure to reach GitHub at all (authorization, permission, or
// communication failure) is returned as an error from [Import], not folded
// into a diagnostic: only state that is structurally unavailable or
// unrepresentable is reported that way.
package importer

import (
	"context"
	"fmt"

	"github.com/kkamegawa/gh-repospec/internal/config"
	"github.com/kkamegawa/gh-repospec/internal/githubapi"
)

// Reader is the read-only GitHub surface Import needs. It is deliberately
// narrow and consumer-defined, so tests can supply a small fake instead of
// the much larger planner-facing githubapi.API; *githubapi.Client satisfies
// it structurally, with no adapter required.
type Reader interface {
	GetRepository(context.Context, githubapi.Repository) (githubapi.RepositoryInfo, error)
	GetImmutableReleases(context.Context, githubapi.Repository) (githubapi.ImmutableReleases, error)
	ListLabels(context.Context, githubapi.Repository) ([]githubapi.Label, error)
	ListMilestones(context.Context, githubapi.Repository) ([]githubapi.Milestone, error)
	ListRulesets(context.Context, githubapi.Repository) ([]githubapi.Ruleset, error)
	GetRuleset(context.Context, githubapi.Repository, int64) (githubapi.Ruleset, error)
	GetActionsPermissions(context.Context, githubapi.Repository) (githubapi.ActionsPermissions, error)
	GetSelectedActions(context.Context, githubapi.Repository) (githubapi.SelectedActions, error)
	GetDefaultWorkflowPermissions(context.Context, githubapi.Repository) (githubapi.DefaultWorkflowPermissions, error)
	GetForkPullRequestApproval(context.Context, githubapi.Repository) (githubapi.ForkPullRequestApproval, error)
	ListActionVariables(context.Context, githubapi.Repository) (githubapi.ActionVariableList, error)
	ListActionsSecrets(context.Context, githubapi.Repository) (githubapi.SecretList, error)
	ListEnvironments(context.Context, githubapi.Repository) ([]githubapi.Environment, error)
	GetVulnerabilityAlertsEnabled(context.Context, githubapi.Repository) (bool, error)
	GetAutomatedSecurityFixesEnabled(context.Context, githubapi.Repository) (bool, error)
	GetPrivateVulnerabilityReportingEnabled(context.Context, githubapi.Repository) (bool, error)
	GetCodeQLDefaultSetup(context.Context, githubapi.Repository) (githubapi.CodeQLDefaultSetupStatus, error)
	GetAIScan(context.Context, githubapi.Repository) (githubapi.AIScanSetting, error)
	ListAgentVariables(context.Context, githubapi.Repository) (githubapi.AgentVariableList, error)
	ListAgentSecrets(context.Context, githubapi.Repository) (githubapi.SecretList, error)
}

var _ Reader = (*githubapi.Client)(nil)

// Result is the outcome of one import: the reverse-mapped configuration and
// any diagnostics about repository state the configuration cannot represent.
type Result struct {
	Config      config.Config
	Diagnostics []Diagnostic
}

// Diagnostic reports one piece of repository state Import could not write
// into the configuration, and why. Resource follows the same dotted-path
// convention as planner.Change.Resource (for example "security.dependencyGraph",
// "actions.secret.RELEASE_TOKEN", "ruleset.legacy-protection").
type Diagnostic struct {
	Resource string
	Message  string
}

func (d Diagnostic) String() string { return d.Resource + ": " + d.Message }

// Import fetches repository, labels, milestones, rulesets, Actions,
// security, and agent variable state from GitHub and reverse-maps it into a
// schemaVersion 1 configuration. It never issues a GitHub write request.
func Import(ctx context.Context, reader Reader, repository githubapi.Repository) (Result, error) {
	info, err := reader.GetRepository(ctx, repository)
	if err != nil {
		return Result{}, fmt.Errorf("get repository %s: %w", repository, err)
	}
	immutableReleases, err := reader.GetImmutableReleases(ctx, repository)
	if err != nil {
		return Result{}, fmt.Errorf("get immutable releases setting for %s: %w", repository, err)
	}
	labels, err := reader.ListLabels(ctx, repository)
	if err != nil {
		return Result{}, fmt.Errorf("list labels for %s: %w", repository, err)
	}
	milestones, err := reader.ListMilestones(ctx, repository)
	if err != nil {
		return Result{}, fmt.Errorf("list milestones for %s: %w", repository, err)
	}
	rulesets, rulesetDiagnostics, err := importRulesets(ctx, reader, repository)
	if err != nil {
		return Result{}, err
	}
	actions, actionsDiagnostics, err := importActions(ctx, reader, repository, info)
	if err != nil {
		return Result{}, err
	}
	security, securityDiagnostics, err := importSecurity(ctx, reader, repository, info)
	if err != nil {
		return Result{}, err
	}
	agents, agentDiagnostics, err := importAgents(ctx, reader, repository)
	if err != nil {
		return Result{}, err
	}

	result := Result{
		Config: config.Config{
			SchemaVersion: config.CurrentSchemaVersion,
			Repository:    mapRepository(repository, info, immutableReleases),
			Labels:        mapLabels(labels),
			Milestones:    mapMilestones(milestones),
			Rulesets:      rulesets,
			Actions:       actions,
			Security:      security,
			Agents:        agents,
		},
	}
	result.Diagnostics = append(result.Diagnostics, rulesetDiagnostics...)
	result.Diagnostics = append(result.Diagnostics, actionsDiagnostics...)
	result.Diagnostics = append(result.Diagnostics, securityDiagnostics...)
	result.Diagnostics = append(result.Diagnostics, agentDiagnostics...)
	return result, nil
}

func boolPtr(v bool) *bool { return &v }

// stringSlice extracts a []string from a decoded JSON value of unknown
// shape, as found inside a ruleset's map[string]any conditions. Anything
// that is not a JSON array of strings yields nil rather than a partial or
// panicking conversion.
func stringSlice(value any) []string {
	raw, ok := value.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// asMap type-asserts a decoded JSON value to map[string]any, returning nil
// for anything else instead of panicking.
func asMap(value any) map[string]any {
	m, _ := value.(map[string]any)
	return m
}
