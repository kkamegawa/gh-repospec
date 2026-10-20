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
	SchemaVersion int                 `json:"schemaVersion" yaml:"schemaVersion"`
	Repository    RepositoryConfig    `json:"repository" yaml:"repository"`
	Seed          *SeedConfig         `json:"seed,omitempty" yaml:"seed,omitempty"`
	Actions       *ActionsConfig      `json:"actions,omitempty" yaml:"actions,omitempty"`
	Rulesets      []RulesetConfig     `json:"rulesets,omitempty" yaml:"rulesets,omitempty"`
	Security      *SecurityConfig     `json:"security,omitempty" yaml:"security,omitempty"`
	Agents        *AgentsConfig       `json:"agents,omitempty" yaml:"agents,omitempty"`
	Labels        []LabelConfig       `json:"labels,omitempty" yaml:"labels,omitempty"`
	Milestones    []MilestoneConfig   `json:"milestones,omitempty" yaml:"milestones,omitempty"`
	Commands      []CommandConfig     `json:"commands,omitempty" yaml:"commands,omitempty"`
	Settings      *RepositorySettings `json:"settings,omitempty" yaml:"settings,omitempty"`
}

// RepositorySettings contains REST settings that are not part of the core
// repository update endpoint. Collections are declarative snapshots imported
// from the source repository; the planner creates or updates matching entries.
type RepositorySettings struct {
	Pages                  *PagesConfig                  `json:"pages,omitempty" yaml:"pages,omitempty"`
	Webhooks               []WebhookConfig               `json:"webhooks,omitempty" yaml:"webhooks,omitempty"`
	Autolinks              []AutolinkConfig              `json:"autolinks,omitempty" yaml:"autolinks,omitempty"`
	DeployKeys             []DeployKeyConfig             `json:"deployKeys,omitempty" yaml:"deployKeys,omitempty"`
	Collaborators          []CollaboratorConfig          `json:"collaborators,omitempty" yaml:"collaborators,omitempty"`
	Teams                  []TeamAccessConfig            `json:"teams,omitempty" yaml:"teams,omitempty"`
	CustomProperties       []CustomPropertyConfig        `json:"customProperties,omitempty" yaml:"customProperties,omitempty"`
	BranchProtections      []BranchProtectionConfig      `json:"branchProtections,omitempty" yaml:"branchProtections,omitempty"`
	InteractionLimit       *InteractionLimitConfig       `json:"interactionLimit,omitempty" yaml:"interactionLimit,omitempty"`
	PullRequestCreationCap *PullRequestCreationCapConfig `json:"pullRequestCreationCap,omitempty" yaml:"pullRequestCreationCap,omitempty"`
}

type InteractionLimitConfig struct {
	Enabled bool   `json:"enabled" yaml:"enabled"`
	Limit   string `json:"limit,omitempty" yaml:"limit,omitempty"`
	Expiry  string `json:"expiry,omitempty" yaml:"expiry,omitempty"`
}

type PullRequestCreationCapConfig struct {
	Enabled             bool      `json:"enabled" yaml:"enabled"`
	MaxOpenPullRequests int       `json:"maxOpenPullRequests,omitempty" yaml:"maxOpenPullRequests,omitempty"`
	IncludeDrafts       bool      `json:"includeDrafts,omitempty" yaml:"includeDrafts,omitempty"`
	BypassUsers         *[]string `json:"bypassUsers,omitempty" yaml:"bypassUsers,omitempty"`
}

type PagesConfig struct {
	Enabled       bool         `json:"enabled" yaml:"enabled"`
	BuildType     string       `json:"buildType,omitempty" yaml:"buildType,omitempty"`
	Source        *PagesSource `json:"source,omitempty" yaml:"source,omitempty"`
	CNAME         *string      `json:"cname,omitempty" yaml:"cname,omitempty"`
	HTTPSRequired *bool        `json:"httpsRequired,omitempty" yaml:"httpsRequired,omitempty"`
}

type PagesSource struct {
	Branch string `json:"branch" yaml:"branch"`
	Path   string `json:"path" yaml:"path"`
}

type WebhookConfig struct {
	URL         string   `json:"url" yaml:"url"`
	ContentType string   `json:"contentType" yaml:"contentType"`
	InsecureSSL string   `json:"insecureSsl,omitempty" yaml:"insecureSsl,omitempty"`
	Events      []string `json:"events" yaml:"events"`
	Active      bool     `json:"active" yaml:"active"`
}

type AutolinkConfig struct {
	KeyPrefix      string `json:"keyPrefix" yaml:"keyPrefix"`
	URLTemplate    string `json:"urlTemplate" yaml:"urlTemplate"`
	IsAlphanumeric bool   `json:"isAlphanumeric" yaml:"isAlphanumeric"`
}

type DeployKeyConfig struct {
	Title    string `json:"title" yaml:"title"`
	Key      string `json:"key" yaml:"key"`
	ReadOnly bool   `json:"readOnly" yaml:"readOnly"`
}

type CollaboratorConfig struct {
	Login      string `json:"login" yaml:"login"`
	Permission string `json:"permission" yaml:"permission"`
}

type TeamAccessConfig struct {
	Slug       string `json:"slug" yaml:"slug"`
	Permission string `json:"permission" yaml:"permission"`
}

type CustomPropertyConfig struct {
	Name  string `json:"name" yaml:"name"`
	Value any    `json:"value" yaml:"value"`
}

type BranchProtectionConfig struct {
	Branch               string         `json:"branch" yaml:"branch"`
	Settings             map[string]any `json:"settings" yaml:"settings"`
	RequireSignedCommits *bool          `json:"requireSignedCommits,omitempty" yaml:"requireSignedCommits,omitempty"`
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
	Archived                  *bool               `json:"archived,omitempty" yaml:"archived,omitempty"`
	Topics                    []string            `json:"topics,omitempty" yaml:"topics,omitempty"`
	Features                  *RepositoryFeatures `json:"features,omitempty" yaml:"features,omitempty"`
	AllowMergeCommit          *bool               `json:"allowMergeCommit,omitempty" yaml:"allowMergeCommit,omitempty"`
	AllowSquashMerge          *bool               `json:"allowSquashMerge,omitempty" yaml:"allowSquashMerge,omitempty"`
	AllowRebaseMerge          *bool               `json:"allowRebaseMerge,omitempty" yaml:"allowRebaseMerge,omitempty"`
	AllowAutoMerge            *bool               `json:"allowAutoMerge,omitempty" yaml:"allowAutoMerge,omitempty"`
	DeleteBranchOnMerge       *bool               `json:"deleteBranchOnMerge,omitempty" yaml:"deleteBranchOnMerge,omitempty"`
	HasDownloads              *bool               `json:"hasDownloads,omitempty" yaml:"hasDownloads,omitempty"`
	AllowForking              *bool               `json:"allowForking,omitempty" yaml:"allowForking,omitempty"`
	HasPullRequests           *bool               `json:"hasPullRequests,omitempty" yaml:"hasPullRequests,omitempty"`
	PullRequestCreationPolicy string              `json:"pullRequestCreationPolicy,omitempty" yaml:"pullRequestCreationPolicy,omitempty"`
	IsTemplate                *bool               `json:"isTemplate,omitempty" yaml:"isTemplate,omitempty"`
	AllowUpdateBranch         *bool               `json:"allowUpdateBranch,omitempty" yaml:"allowUpdateBranch,omitempty"`
	UseSquashPRTitleAsDefault *bool               `json:"useSquashPRTitleAsDefault,omitempty" yaml:"useSquashPRTitleAsDefault,omitempty"`
	WebCommitSignoffRequired  *bool               `json:"webCommitSignoffRequired,omitempty" yaml:"webCommitSignoffRequired,omitempty"`
	MergeCommitTitle          string              `json:"mergeCommitTitle,omitempty" yaml:"mergeCommitTitle,omitempty"`
	MergeCommitMessage        string              `json:"mergeCommitMessage,omitempty" yaml:"mergeCommitMessage,omitempty"`
	SquashMergeCommitTitle    string              `json:"squashMergeCommitTitle,omitempty" yaml:"squashMergeCommitTitle,omitempty"`
	SquashMergeCommitMessage  string              `json:"squashMergeCommitMessage,omitempty" yaml:"squashMergeCommitMessage,omitempty"`
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
	Template   *TemplateSeed   `json:"template,omitempty" yaml:"template,omitempty"`
	Directory  *DirectorySeed  `json:"directory,omitempty" yaml:"directory,omitempty"`
	ZIP        *ZIPSeed        `json:"zip,omitempty" yaml:"zip,omitempty"`
	Initialize *InitializeSeed `json:"initialize,omitempty" yaml:"initialize,omitempty"`
	Overwrite  []string        `json:"overwrite,omitempty" yaml:"overwrite,omitempty"`
}

// InitializeSeed describes standard files to create when bootstrapping an
// otherwise empty repository. A blank gitignoreTemplate disables .gitignore;
// README.md is generated by default unless Readme is explicitly false.
type InitializeSeed struct {
	GitignoreTemplate string `json:"gitignoreTemplate,omitempty" yaml:"gitignoreTemplate,omitempty"`
	Readme            *bool  `json:"readme,omitempty" yaml:"readme,omitempty"`
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
	Enabled                        *bool                           `json:"enabled,omitempty" yaml:"enabled,omitempty"`
	AllowedActions                 string                          `json:"allowedActions,omitempty" yaml:"allowedActions,omitempty"`
	SelectedActions                *SelectedActionsConfig          `json:"selectedActions,omitempty" yaml:"selectedActions,omitempty"`
	OIDCSubjectCustomization       *OIDCSubjectCustomizationConfig `json:"oidcSubjectCustomization,omitempty" yaml:"oidcSubjectCustomization,omitempty"`
	RequireSHAForGitHubActions     *bool                           `json:"requireSHAForGitHubActions,omitempty" yaml:"requireSHAForGitHubActions,omitempty"`
	DefaultWorkflowPermissions     string                          `json:"defaultWorkflowPermissions,omitempty" yaml:"defaultWorkflowPermissions,omitempty"`
	CanApprovePullRequestWorkflows *bool                           `json:"canApprovePullRequestWorkflows,omitempty" yaml:"canApprovePullRequestWorkflows,omitempty"`
	// ForkPullRequestApproval selects when a fork pull request workflow run
	// requires manual approval. GitHub rejects this on private repositories.
	ForkPullRequestApproval string        `json:"forkPullRequestApproval,omitempty" yaml:"forkPullRequestApproval,omitempty"`
	WorkflowAccessLevel     string        `json:"workflowAccessLevel,omitempty" yaml:"workflowAccessLevel,omitempty"`
	Variables               []VariableRef `json:"variables,omitempty" yaml:"variables,omitempty"`
	Secrets                 []SecretRef   `json:"secrets,omitempty" yaml:"secrets,omitempty"`
	Environments            []Environment `json:"environments,omitempty" yaml:"environments,omitempty"`
}

// OIDCSubjectCustomizationConfig describes the repository-level Actions OIDC
// subject claim template, independently of organization and enterprise policy.
type OIDCSubjectCustomizationConfig struct {
	UseDefault          bool     `json:"useDefault" yaml:"useDefault"`
	IncludeClaimKeys    []string `json:"includeClaimKeys,omitempty" yaml:"includeClaimKeys,omitempty"`
	UseImmutableSubject *bool    `json:"useImmutableSubject,omitempty" yaml:"useImmutableSubject,omitempty"`
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
	Name                   string                     `json:"name" yaml:"name"`
	WaitTimer              *int                       `json:"waitTimer,omitempty" yaml:"waitTimer,omitempty"`
	PreventSelfReview      *bool                      `json:"preventSelfReview,omitempty" yaml:"preventSelfReview,omitempty"`
	Reviewers              *[]EnvironmentReviewer     `json:"reviewers,omitempty" yaml:"reviewers,omitempty"`
	DeploymentBranchPolicy *DeploymentBranchPolicy    `json:"deploymentBranchPolicy,omitempty" yaml:"deploymentBranchPolicy,omitempty"`
	BranchPolicies         *[]EnvironmentBranchPolicy `json:"branchPolicies,omitempty" yaml:"branchPolicies,omitempty"`
	CustomProtectionRules  *[]string                  `json:"customProtectionRules,omitempty" yaml:"customProtectionRules,omitempty"`
}

type EnvironmentReviewer struct {
	Type string `json:"type" yaml:"type"`
	ID   int64  `json:"id" yaml:"id"`
}

type DeploymentBranchPolicy struct {
	AllowAllBranches     bool `json:"allowAllBranches,omitempty" yaml:"allowAllBranches,omitempty"`
	ProtectedBranches    bool `json:"protectedBranches" yaml:"protectedBranches"`
	CustomBranchPolicies bool `json:"customBranchPolicies" yaml:"customBranchPolicies"`
}

type EnvironmentBranchPolicy struct {
	Name string `json:"name" yaml:"name"`
	Type string `json:"type" yaml:"type"`
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
// one of Role, Team, App, User, OrgAdmin, or CustomRole must be set. Team, App,
// and CustomRole accept the resolved "id:<number>" form; User requires it so
// import output can be applied again without looking up an account by name.
type BypassActorConfig struct {
	Role       string `json:"role,omitempty" yaml:"role,omitempty"`
	Team       string `json:"team,omitempty" yaml:"team,omitempty"`
	App        string `json:"app,omitempty" yaml:"app,omitempty"`
	User       string `json:"user,omitempty" yaml:"user,omitempty"`
	OrgAdmin   bool   `json:"orgAdmin,omitempty" yaml:"orgAdmin,omitempty"`
	CustomRole string `json:"customRole,omitempty" yaml:"customRole,omitempty"`
	BypassMode string `json:"bypassMode,omitempty" yaml:"bypassMode,omitempty"`
}

// repositoryRoleActorIDs are GitHub's built-in repository role IDs for
// ruleset bypass actors. They are stable but undocumented by GitHub, and
// distinct from an organization custom repository role, whose ID is
// assigned when the role is created and is always resolved through the API
// instead. The planner resolves a declared role selector to its ID before a
// write; import reverses a fetched actor ID to recognize one of these fixed
// roles rather than a custom role. The map itself is unexported so neither
// caller can mutate a value the other depends on; RepositoryRoleActorID and
// RoleNameForActorID are the only way in or out.
var repositoryRoleActorIDs = map[string]int64{
	"maintain": 2,
	"write":    4,
	"admin":    5,
}

// RepositoryRoleActorID resolves a declared role selector (admin, write, or
// maintain) to the fixed numeric ID the rulesets API requires. ok is false
// for any other name.
func RepositoryRoleActorID(role string) (id int64, ok bool) {
	id, ok = repositoryRoleActorIDs[role]
	return id, ok
}

// RoleNameForActorID reverses RepositoryRoleActorID: it recognizes a fetched
// RepositoryRole actor ID as one of the fixed roles. ok is false for any
// other ID, including a custom repository role's server-assigned one, which
// shares the same RepositoryRole actor type on the wire.
func RoleNameForActorID(id int64) (role string, ok bool) {
	for name, roleID := range repositoryRoleActorIDs {
		if roleID == id {
			return name, true
		}
	}
	return "", false
}

// OrganizationAdminActorID is the ruleset bypass actor ID GitHub expects for
// the OrganizationAdmin actor type. GitHub ignores the value but rejects a
// request that omits it.
const OrganizationAdminActorID = 1

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
	DependencyGraph                       *FeatureSetting            `json:"dependencyGraph,omitempty" yaml:"dependencyGraph,omitempty"`
	DependabotAlerts                      *FeatureSetting            `json:"dependabotAlerts,omitempty" yaml:"dependabotAlerts,omitempty"`
	DependabotSecurityUpdates             *FeatureSetting            `json:"dependabotSecurityUpdates,omitempty" yaml:"dependabotSecurityUpdates,omitempty"`
	AdvancedSecurity                      *FeatureSetting            `json:"advancedSecurity,omitempty" yaml:"advancedSecurity,omitempty"`
	CodeSecurity                          *FeatureSetting            `json:"codeSecurity,omitempty" yaml:"codeSecurity,omitempty"`
	SecretScanning                        *FeatureSetting            `json:"secretScanning,omitempty" yaml:"secretScanning,omitempty"`
	PushProtection                        *FeatureSetting            `json:"pushProtection,omitempty" yaml:"pushProtection,omitempty"`
	SecretScanningAIDetection             *FeatureSetting            `json:"secretScanningAIDetection,omitempty" yaml:"secretScanningAIDetection,omitempty"`
	SecretScanningNonProviderPatterns     *FeatureSetting            `json:"secretScanningNonProviderPatterns,omitempty" yaml:"secretScanningNonProviderPatterns,omitempty"`
	SecretScanningDelegatedAlertDismissal *FeatureSetting            `json:"secretScanningDelegatedAlertDismissal,omitempty" yaml:"secretScanningDelegatedAlertDismissal,omitempty"`
	SecretScanningDelegatedBypass         *FeatureSetting            `json:"secretScanningDelegatedBypass,omitempty" yaml:"secretScanningDelegatedBypass,omitempty"`
	DelegatedBypassReviewers              *[]DelegatedBypassReviewer `json:"delegatedBypassReviewers,omitempty" yaml:"delegatedBypassReviewers,omitempty"`
	CodeQLDefaultSetup                    *FeatureSetting            `json:"codeqlDefaultSetup,omitempty" yaml:"codeqlDefaultSetup,omitempty"`
	AIPullRequestScan                     *FeatureSetting            `json:"aiPullRequestScan,omitempty" yaml:"aiPullRequestScan,omitempty"`
	PrivateVulnerabilityReporting         *FeatureSetting            `json:"privateVulnerabilityReporting,omitempty" yaml:"privateVulnerabilityReporting,omitempty"`
}

type DelegatedBypassReviewer struct {
	ReviewerID   int64  `json:"reviewerId" yaml:"reviewerId"`
	ReviewerType string `json:"reviewerType" yaml:"reviewerType"`
	Mode         string `json:"mode,omitempty" yaml:"mode,omitempty"`
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
