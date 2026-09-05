// Package config defines and validates versioned repository bootstrap files.
package config

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

const CurrentSchemaVersion = 1

// Config is the complete schemaVersion 1 bootstrap document.
type Config struct {
	SchemaVersion int               `json:"schemaVersion" yaml:"schemaVersion"`
	Repository    RepositoryConfig  `json:"repository" yaml:"repository"`
	Seed          *SeedConfig       `json:"seed,omitempty" yaml:"seed,omitempty"`
	Actions       *ActionsConfig    `json:"actions,omitempty" yaml:"actions,omitempty"`
	Rulesets      []RulesetConfig   `json:"rulesets,omitempty" yaml:"rulesets,omitempty"`
	Security      *SecurityConfig   `json:"security,omitempty" yaml:"security,omitempty"`
	Agents        *AgentsConfig     `json:"agents,omitempty" yaml:"agents,omitempty"`
	Labels        []LabelConfig     `json:"labels,omitempty" yaml:"labels,omitempty"`
	Milestones    []MilestoneConfig `json:"milestones,omitempty" yaml:"milestones,omitempty"`
	Commands      []CommandConfig   `json:"commands,omitempty" yaml:"commands,omitempty"`
}

type RepositoryConfig struct {
	Owner  string `json:"owner" yaml:"owner"`
	Name   string `json:"name" yaml:"name"`
	Create bool   `json:"create" yaml:"create"`
	// Description and Homepage are both nil when unmanaged. An undeclared value
	// must never be treated as "clear the field": import output that does not
	// manage one of these must not report a change against the repository it
	// was read from. An explicit empty string clears the live value.
	Description           *string `json:"description,omitempty" yaml:"description,omitempty"`
	Homepage              *string `json:"homepage,omitempty" yaml:"homepage,omitempty"`
	Visibility            string  `json:"visibility" yaml:"visibility"`
	AllowVisibilityChange bool    `json:"allowVisibilityChange,omitempty" yaml:"allowVisibilityChange,omitempty"`
	DefaultBranch         string  `json:"defaultBranch,omitempty" yaml:"defaultBranch,omitempty"`
	// Archived is reversible: false unarchives a previously archived repository.
	Archived                 *bool               `json:"archived,omitempty" yaml:"archived,omitempty"`
	Topics                   []string            `json:"topics,omitempty" yaml:"topics,omitempty"`
	Features                 *RepositoryFeatures `json:"features,omitempty" yaml:"features,omitempty"`
	AllowMergeCommit         *bool               `json:"allowMergeCommit,omitempty" yaml:"allowMergeCommit,omitempty"`
	AllowSquashMerge         *bool               `json:"allowSquashMerge,omitempty" yaml:"allowSquashMerge,omitempty"`
	AllowRebaseMerge         *bool               `json:"allowRebaseMerge,omitempty" yaml:"allowRebaseMerge,omitempty"`
	AllowAutoMerge           *bool               `json:"allowAutoMerge,omitempty" yaml:"allowAutoMerge,omitempty"`
	DeleteBranchOnMerge      *bool               `json:"deleteBranchOnMerge,omitempty" yaml:"deleteBranchOnMerge,omitempty"`
	MergeCommitTitle         string              `json:"mergeCommitTitle,omitempty" yaml:"mergeCommitTitle,omitempty"`
	MergeCommitMessage       string              `json:"mergeCommitMessage,omitempty" yaml:"mergeCommitMessage,omitempty"`
	SquashMergeCommitTitle   string              `json:"squashMergeCommitTitle,omitempty" yaml:"squashMergeCommitTitle,omitempty"`
	SquashMergeCommitMessage string              `json:"squashMergeCommitMessage,omitempty" yaml:"squashMergeCommitMessage,omitempty"`
	// ReleaseImmutability is applied through a dedicated endpoint, not the
	// repository update call, because GitHub does not accept it there.
	ReleaseImmutability *bool `json:"releaseImmutability,omitempty" yaml:"releaseImmutability,omitempty"`
}

// RepositoryFeatures toggles the repository tabs. A nil field is unmanaged.
type RepositoryFeatures struct {
	Issues      *bool `json:"issues,omitempty" yaml:"issues,omitempty"`
	Projects    *bool `json:"projects,omitempty" yaml:"projects,omitempty"`
	Wiki        *bool `json:"wiki,omitempty" yaml:"wiki,omitempty"`
	Discussions *bool `json:"discussions,omitempty" yaml:"discussions,omitempty"`
}

// LabelConfig is one declared label. Labels are additive: a label present on
// the repository but absent here is never modified or deleted.
type LabelConfig struct {
	Name        string `json:"name" yaml:"name"`
	Color       string `json:"color" yaml:"color"`
	Description string `json:"description,omitempty" yaml:"description,omitempty"`
}

// MilestoneConfig is one declared milestone. The title identifies it on
// GitHub. An empty State means open; retire a milestone with state: closed
// rather than removing it from configuration.
type MilestoneConfig struct {
	Title       string `json:"title" yaml:"title"`
	Description string `json:"description,omitempty" yaml:"description,omitempty"`
	State       string `json:"state,omitempty" yaml:"state,omitempty"`
	DueOn       string `json:"dueOn,omitempty" yaml:"dueOn,omitempty"`
}

// SeedConfig has exactly one source. Files explicitly listed in Overwrite may
// replace differing files in an existing repository.
type SeedConfig struct {
	Template  *TemplateSeed  `json:"template,omitempty" yaml:"template,omitempty"`
	Directory *DirectorySeed `json:"directory,omitempty" yaml:"directory,omitempty"`
	ZIP       *ZIPSeed       `json:"zip,omitempty" yaml:"zip,omitempty"`
	Overwrite []string       `json:"overwrite,omitempty" yaml:"overwrite,omitempty"`
}

type TemplateSeed struct {
	Repository string `json:"repository" yaml:"repository"`
}

type DirectorySeed struct {
	Path string `json:"path" yaml:"path"`
}

type ZIPSeed struct {
	Path string `json:"path" yaml:"path"`
}

type ActionsConfig struct {
	Enabled                        *bool                  `json:"enabled,omitempty" yaml:"enabled,omitempty"`
	AllowedActions                 string                 `json:"allowedActions,omitempty" yaml:"allowedActions,omitempty"`
	SelectedActions                *SelectedActionsConfig `json:"selectedActions,omitempty" yaml:"selectedActions,omitempty"`
	RequireSHAForGitHubActions     *bool                  `json:"requireSHAForGitHubActions,omitempty" yaml:"requireSHAForGitHubActions,omitempty"`
	DefaultWorkflowPermissions     string                 `json:"defaultWorkflowPermissions,omitempty" yaml:"defaultWorkflowPermissions,omitempty"`
	CanApprovePullRequestWorkflows *bool                  `json:"canApprovePullRequestWorkflows,omitempty" yaml:"canApprovePullRequestWorkflows,omitempty"`
	// ForkPullRequestApproval selects when a fork pull request workflow run
	// requires manual approval. GitHub rejects this on private repositories.
	ForkPullRequestApproval string        `json:"forkPullRequestApproval,omitempty" yaml:"forkPullRequestApproval,omitempty"`
	Variables               []VariableRef `json:"variables,omitempty" yaml:"variables,omitempty"`
	Secrets                 []SecretRef   `json:"secrets,omitempty" yaml:"secrets,omitempty"`
	Environments            []Environment `json:"environments,omitempty" yaml:"environments,omitempty"`
}

// SelectedActionsConfig is required when AllowedActions is "selected". Without it,
// GitHub's selected-actions allowlist defaults to empty and every workflow run using
// an Action is blocked.
type SelectedActionsConfig struct {
	GitHubOwnedAllowed bool     `json:"githubOwnedAllowed" yaml:"githubOwnedAllowed"`
	VerifiedAllowed    bool     `json:"verifiedAllowed" yaml:"verifiedAllowed"`
	PatternsAllowed    []string `json:"patternsAllowed,omitempty" yaml:"patternsAllowed,omitempty"`
}

type VariableRef struct {
	Name  string `json:"name" yaml:"name"`
	Value string `json:"value" yaml:"value"`
}

// SecretRef deliberately stores only a source environment variable name.
type SecretRef struct {
	Name         string `json:"name" yaml:"name"`
	SourceEnv    string `json:"sourceEnv" yaml:"sourceEnv"`
	UpdatePolicy string `json:"updatePolicy,omitempty" yaml:"updatePolicy,omitempty"`
}

type Environment struct {
	Name string `json:"name" yaml:"name"`
}

type RulesetConfig struct {
	Name        string   `json:"name" yaml:"name"`
	Enforcement string   `json:"enforcement" yaml:"enforcement"`
	Target      string   `json:"target" yaml:"target"`
	Include     []string `json:"include" yaml:"include"`
	Exclude     []string `json:"exclude,omitempty" yaml:"exclude,omitempty"`
	// BypassActors lists the actors allowed to bypass this ruleset. Actor
	// identities are resolved to the numeric IDs the rulesets API requires
	// before any write; see internal/planner's bypass actor resolver.
	BypassActors []BypassActorConfig `json:"bypassActors,omitempty" yaml:"bypassActors,omitempty"`
	Rules        []RulesetRule       `json:"rules" yaml:"rules"`
}

// BypassActorConfig identifies one actor allowed to bypass a ruleset. Exactly
// one of Role, Team, App, OrgAdmin, or CustomRole must be set. Team, App, and
// CustomRole also accept the resolved "id:<number>" form ParseBypassActorID
// recognises, so import output can be applied again unchanged even for a
// private GitHub App whose slug the authenticated user cannot resolve.
type BypassActorConfig struct {
	Role       string `json:"role,omitempty" yaml:"role,omitempty"`
	Team       string `json:"team,omitempty" yaml:"team,omitempty"`
	App        string `json:"app,omitempty" yaml:"app,omitempty"`
	OrgAdmin   bool   `json:"orgAdmin,omitempty" yaml:"orgAdmin,omitempty"`
	CustomRole string `json:"customRole,omitempty" yaml:"customRole,omitempty"`
	BypassMode string `json:"bypassMode,omitempty" yaml:"bypassMode,omitempty"`
}

// bypassActorIDPrefix marks a bypass actor selector as an already-resolved
// numeric ID rather than a name to look up.
const bypassActorIDPrefix = "id:"

// ParseBypassActorID interprets a Team, App, or CustomRole selector in the
// resolved "id:<number>" form. found reports whether the value carries the
// prefix at all; err reports a malformed number so validation rejects it
// outright instead of the planner treating it as a literal slug and later
// failing to resolve it against the GitHub API.
func ParseBypassActorID(value string) (id int64, found bool, err error) {
	if !strings.HasPrefix(value, bypassActorIDPrefix) {
		return 0, false, nil
	}
	digits := strings.TrimPrefix(value, bypassActorIDPrefix)
	parsed, parseErr := strconv.ParseInt(digits, 10, 64)
	if parseErr != nil || parsed <= 0 {
		return 0, true, fmt.Errorf("%q is not a positive id:<number>", value)
	}
	return parsed, true, nil
}

// RulesetRule retains endpoint-specific parameters while keeping the
// document's top-level contract typed and strict.
type RulesetRule struct {
	Type       string         `json:"type" yaml:"type"`
	Parameters map[string]any `json:"parameters,omitempty" yaml:"parameters,omitempty"`
}

type SecurityConfig struct {
	DependencyGraph               *FeatureSetting `json:"dependencyGraph,omitempty" yaml:"dependencyGraph,omitempty"`
	DependabotAlerts              *FeatureSetting `json:"dependabotAlerts,omitempty" yaml:"dependabotAlerts,omitempty"`
	DependabotSecurityUpdates     *FeatureSetting `json:"dependabotSecurityUpdates,omitempty" yaml:"dependabotSecurityUpdates,omitempty"`
	SecretScanning                *FeatureSetting `json:"secretScanning,omitempty" yaml:"secretScanning,omitempty"`
	PushProtection                *FeatureSetting `json:"pushProtection,omitempty" yaml:"pushProtection,omitempty"`
	CodeQLDefaultSetup            *FeatureSetting `json:"codeqlDefaultSetup,omitempty" yaml:"codeqlDefaultSetup,omitempty"`
	PrivateVulnerabilityReporting *FeatureSetting `json:"privateVulnerabilityReporting,omitempty" yaml:"privateVulnerabilityReporting,omitempty"`
}

type FeatureSetting struct {
	Enabled  bool `json:"enabled" yaml:"enabled"`
	Optional bool `json:"optional,omitempty" yaml:"optional,omitempty"`
}

type AgentsConfig struct {
	Variables        []VariableRef `json:"variables,omitempty" yaml:"variables,omitempty"`
	Secrets          []SecretRef   `json:"secrets,omitempty" yaml:"secrets,omitempty"`
	MCPConfigPath    string        `json:"mcpConfigPath,omitempty" yaml:"mcpConfigPath,omitempty"`
	InstructionsPath string        `json:"instructionsPath,omitempty" yaml:"instructionsPath,omitempty"`
	CustomAgentsPath string        `json:"customAgentsPath,omitempty" yaml:"customAgentsPath,omitempty"`
	SkillsPath       string        `json:"skillsPath,omitempty" yaml:"skillsPath,omitempty"`
}

type CommandConfig struct {
	ID          string   `json:"id" yaml:"id"`
	Program     string   `json:"program" yaml:"program"`
	Args        []string `json:"args,omitempty" yaml:"args,omitempty"`
	CWD         string   `json:"cwd,omitempty" yaml:"cwd,omitempty"`
	Timeout     string   `json:"timeout" yaml:"timeout"`
	Environment []string `json:"environment,omitempty" yaml:"environment,omitempty"`
}

// TimeoutDuration parses the command timeout after structural validation.
func (c CommandConfig) TimeoutDuration() (time.Duration, error) {
	return time.ParseDuration(c.Timeout)
}
