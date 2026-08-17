// Package config defines and validates versioned repository bootstrap files.
package config

import "time"

const CurrentSchemaVersion = 1

// Config is the complete schemaVersion 1 bootstrap document.
type Config struct {
	SchemaVersion int              `json:"schemaVersion" yaml:"schemaVersion"`
	Repository    RepositoryConfig `json:"repository" yaml:"repository"`
	Seed          *SeedConfig      `json:"seed,omitempty" yaml:"seed,omitempty"`
	Actions       *ActionsConfig   `json:"actions,omitempty" yaml:"actions,omitempty"`
	Rulesets      []RulesetConfig  `json:"rulesets,omitempty" yaml:"rulesets,omitempty"`
	Security      *SecurityConfig  `json:"security,omitempty" yaml:"security,omitempty"`
	Agents        *AgentsConfig    `json:"agents,omitempty" yaml:"agents,omitempty"`
	Commands      []CommandConfig  `json:"commands,omitempty" yaml:"commands,omitempty"`
}

type RepositoryConfig struct {
	Owner                 string   `json:"owner" yaml:"owner"`
	Name                  string   `json:"name" yaml:"name"`
	Create                bool     `json:"create" yaml:"create"`
	Description           string   `json:"description,omitempty" yaml:"description,omitempty"`
	Visibility            string   `json:"visibility" yaml:"visibility"`
	AllowVisibilityChange bool     `json:"allowVisibilityChange,omitempty" yaml:"allowVisibilityChange,omitempty"`
	DefaultBranch         string   `json:"defaultBranch,omitempty" yaml:"defaultBranch,omitempty"`
	Topics                []string `json:"topics,omitempty" yaml:"topics,omitempty"`
	AllowMergeCommit      *bool    `json:"allowMergeCommit,omitempty" yaml:"allowMergeCommit,omitempty"`
	AllowSquashMerge      *bool    `json:"allowSquashMerge,omitempty" yaml:"allowSquashMerge,omitempty"`
	AllowRebaseMerge      *bool    `json:"allowRebaseMerge,omitempty" yaml:"allowRebaseMerge,omitempty"`
	AllowAutoMerge        *bool    `json:"allowAutoMerge,omitempty" yaml:"allowAutoMerge,omitempty"`
	DeleteBranchOnMerge   *bool    `json:"deleteBranchOnMerge,omitempty" yaml:"deleteBranchOnMerge,omitempty"`
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
	Enabled                        *bool         `json:"enabled,omitempty" yaml:"enabled,omitempty"`
	AllowedActions                 string        `json:"allowedActions,omitempty" yaml:"allowedActions,omitempty"`
	RequireSHAForGitHubActions     *bool         `json:"requireSHAForGitHubActions,omitempty" yaml:"requireSHAForGitHubActions,omitempty"`
	DefaultWorkflowPermissions     string        `json:"defaultWorkflowPermissions,omitempty" yaml:"defaultWorkflowPermissions,omitempty"`
	CanApprovePullRequestWorkflows *bool         `json:"canApprovePullRequestWorkflows,omitempty" yaml:"canApprovePullRequestWorkflows,omitempty"`
	Variables                      []VariableRef `json:"variables,omitempty" yaml:"variables,omitempty"`
	Secrets                        []SecretRef   `json:"secrets,omitempty" yaml:"secrets,omitempty"`
	Environments                   []Environment `json:"environments,omitempty" yaml:"environments,omitempty"`
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
	Name        string        `json:"name" yaml:"name"`
	Enforcement string        `json:"enforcement" yaml:"enforcement"`
	Target      string        `json:"target" yaml:"target"`
	Include     []string      `json:"include" yaml:"include"`
	Exclude     []string      `json:"exclude,omitempty" yaml:"exclude,omitempty"`
	Rules       []RulesetRule `json:"rules" yaml:"rules"`
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
