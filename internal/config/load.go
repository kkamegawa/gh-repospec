package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Load reads a YAML or JSON configuration document and validates it fully,
// including that a declared seed.directory or seed.zip exists on disk.
// yaml.v3 accepts JSON as a YAML subset, detects duplicate mapping keys, and
// its KnownFields option rejects fields that do not belong to the typed
// contract.
func Load(path string) (*Config, error) {
	return load(path, Validate)
}

// LoadForRefresh reads and decodes a configuration exactly the way Load
// does, but validates it with ValidateDeclaredSurface instead: a --into
// refresh candidate must not fail merely because the machine running it
// lacks the file's declared seed content locally.
func LoadForRefresh(path string) (*Config, error) {
	return load(path, ValidateDeclaredSurface)
}

func load(path string, validate func(*Config) error) (*Config, error) {
	data, err := os.ReadFile(path) //nolint:gosec // the path is explicitly supplied by the user
	if err != nil {
		return nil, fmt.Errorf("read configuration %q: %w", path, err)
	}

	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	var cfg Config
	if err := decoder.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("decode configuration %q: %w", path, err)
	}
	var extra yaml.Node
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return nil, fmt.Errorf("decode configuration %q: multiple documents are not allowed", path)
		}
		return nil, fmt.Errorf("decode configuration %q: %w", path, err)
	}
	resolveLocalSeedPath(&cfg, filepath.Dir(path))
	if err := validate(&cfg); err != nil {
		return nil, fmt.Errorf("validate configuration %q: %w", path, err)
	}
	return &cfg, nil
}

// resolveLocalSeedPath makes local seed paths independent of the caller's
// current working directory. Template references intentionally remain opaque.
func resolveLocalSeedPath(cfg *Config, base string) {
	if cfg.Seed == nil {
		return
	}
	if cfg.Seed.Directory != nil && !filepath.IsAbs(cfg.Seed.Directory.Path) {
		cfg.Seed.Directory.Path = filepath.Join(base, cfg.Seed.Directory.Path)
	}
	if cfg.Seed.ZIP != nil && !filepath.IsAbs(cfg.Seed.ZIP.Path) {
		cfg.Seed.ZIP.Path = filepath.Join(base, cfg.Seed.ZIP.Path)
	}
}

// Validate checks semantic constraints that cannot be expressed by Go tags.
func Validate(cfg *Config) error {
	return validate(cfg, true)
}

// ValidateDeclaredSurface validates a configuration the same way Validate
// does, except it does not stat a seed.directory or seed.zip path on disk.
// A refresh candidate can be valid on the repository it describes even when
// the machine running the refresh does not have the seed content locally;
// that is a concern for apply, not for accepting the refreshed document.
func ValidateDeclaredSurface(cfg *Config) error {
	return validate(cfg, false)
}

func validate(cfg *Config, checkSeedFilesystem bool) error {
	if cfg == nil {
		return errors.New("configuration is required")
	}
	if cfg.SchemaVersion != CurrentSchemaVersion {
		return fmt.Errorf("schemaVersion must be %d", CurrentSchemaVersion)
	}
	if err := validateRepository(cfg.Repository); err != nil {
		return err
	}
	if err := validateSeed(cfg.Seed, checkSeedFilesystem); err != nil {
		return err
	}
	if err := validateActions(cfg.Actions); err != nil {
		return err
	}
	if err := validateRulesets(cfg.Rulesets); err != nil {
		return err
	}
	if err := validateSecurity(cfg.Security); err != nil {
		return err
	}
	if err := validateAgents(cfg.Agents); err != nil {
		return err
	}
	if err := validateLabels(cfg.Labels); err != nil {
		return err
	}
	if err := validateMilestones(cfg.Milestones); err != nil {
		return err
	}
	if err := validateRepositorySettings(cfg.Settings); err != nil {
		return err
	}
	return validateCommands(cfg.Commands)
}

func validateRepositorySettings(settings *RepositorySettings) error {
	if settings == nil {
		return nil
	}
	if interaction := settings.InteractionLimit; interaction != nil && interaction.Enabled {
		if !oneOf(interaction.Limit, "existing_users", "contributors_only", "collaborators_only") {
			return errors.New("settings.interactionLimit.limit must be existing_users, contributors_only, or collaborators_only")
		}
		if !oneOf(interaction.Expiry, "one_day", "three_days", "one_week", "one_month", "six_months") {
			return errors.New("settings.interactionLimit.expiry must be one_day, three_days, one_week, one_month, or six_months")
		}
	}
	if cap := settings.PullRequestCreationCap; cap != nil {
		if cap.Enabled && cap.MaxOpenPullRequests < 1 {
			return errors.New("settings.pullRequestCreationCap.maxOpenPullRequests must be positive when enabled")
		}
		if cap.BypassUsers != nil {
			if !cap.Enabled && len(*cap.BypassUsers) > 0 {
				return errors.New("settings.pullRequestCreationCap bypassUsers require the cap to be enabled")
			}
			for _, login := range *cap.BypassUsers {
				if login == "" {
					return errors.New("settings.pullRequestCreationCap.bypassUsers cannot contain empty logins")
				}
			}
			if err := validateUnique("settings.pullRequestCreationCap.bypassUsers", *cap.BypassUsers); err != nil {
				return err
			}
			if len(*cap.BypassUsers) > 100 {
				return errors.New("settings.pullRequestCreationCap.bypassUsers cannot exceed 100 entries")
			}
		}
	}
	if settings.Pages != nil {
		pages := settings.Pages
		if pages.Enabled && pages.BuildType != "workflow" && pages.Source == nil {
			return errors.New("settings.pages.source is required when Pages is enabled")
		}
		if pages.Source != nil && (pages.Source.Branch == "" || !oneOf(pages.Source.Path, "/", "/docs")) {
			return errors.New("settings.pages.source requires a branch and path / or /docs")
		}
		if pages.BuildType != "" && !oneOf(pages.BuildType, "legacy", "workflow") {
			return errors.New("settings.pages.buildType must be legacy or workflow")
		}
	}
	for _, webhook := range settings.Webhooks {
		if webhook.URL == "" || len(webhook.Events) == 0 {
			return errors.New("settings.webhooks requires url and at least one event")
		}
		events := append([]string(nil), webhook.Events...)
		sort.Strings(events)
		if err := validateUnique("settings.webhooks events", events); err != nil {
			return err
		}
	}
	autolinkKeys := make([]string, 0, len(settings.Autolinks))
	for _, autolink := range settings.Autolinks {
		if autolink.KeyPrefix == "" || autolink.URLTemplate == "" {
			return errors.New("settings.autolinks requires keyPrefix and urlTemplate")
		}
		autolinkKeys = append(autolinkKeys, autolink.KeyPrefix)
	}
	if err := validateUnique("settings.autolinks", autolinkKeys); err != nil {
		return err
	}
	deployKeyTitles := make([]string, 0, len(settings.DeployKeys))
	for _, key := range settings.DeployKeys {
		if key.Title == "" || key.Key == "" {
			return errors.New("settings.deployKeys requires title and key")
		}
		deployKeyTitles = append(deployKeyTitles, key.Title)
	}
	if err := validateUnique("settings.deployKeys", deployKeyTitles); err != nil {
		return err
	}
	collaborators := make([]string, 0, len(settings.Collaborators))
	for _, collaborator := range settings.Collaborators {
		if collaborator.Login == "" || collaborator.Permission == "" {
			return errors.New("settings.collaborators requires login and permission")
		}
		collaborators = append(collaborators, strings.ToLower(collaborator.Login))
	}
	if err := validateUnique("settings.collaborators", collaborators); err != nil {
		return err
	}
	teamSlugs := make([]string, 0, len(settings.Teams))
	for _, team := range settings.Teams {
		if team.Slug == "" || !oneOf(team.Permission, "pull", "triage", "push", "maintain", "admin") {
			return errors.New("settings.teams requires slug and a valid permission")
		}
		teamSlugs = append(teamSlugs, strings.ToLower(team.Slug))
	}
	if err := validateUnique("settings.teams", teamSlugs); err != nil {
		return err
	}
	propertyNames := make([]string, 0, len(settings.CustomProperties))
	for _, property := range settings.CustomProperties {
		if property.Name == "" || property.Value == nil {
			return errors.New("settings.customProperties requires name and value")
		}
		propertyNames = append(propertyNames, property.Name)
	}
	if err := validateUnique("settings.customProperties", propertyNames); err != nil {
		return err
	}
	branches := make([]string, 0, len(settings.BranchProtections))
	for _, protection := range settings.BranchProtections {
		if protection.Branch == "" || protection.Settings == nil {
			return errors.New("settings.branchProtections requires branch and settings")
		}
		branches = append(branches, protection.Branch)
	}
	return validateUnique("settings.branchProtections", branches)
}

func validateRepository(r RepositoryConfig) error {
	if r.Owner == "" || r.Name == "" {
		return errors.New("repository.owner and repository.name are required")
	}
	if !oneOf(r.Visibility, "public", "private") {
		return errors.New("repository.visibility must be public or private")
	}
	if r.MergeCommitTitle != "" && !oneOf(r.MergeCommitTitle, "MERGE_MESSAGE", "PR_TITLE") {
		return errors.New("repository.mergeCommitTitle must be MERGE_MESSAGE or PR_TITLE")
	}
	if r.MergeCommitMessage != "" && !oneOf(r.MergeCommitMessage, "PR_TITLE", "PR_BODY", "BLANK") {
		return errors.New("repository.mergeCommitMessage must be PR_TITLE, PR_BODY, or BLANK")
	}
	if r.SquashMergeCommitTitle != "" && !oneOf(r.SquashMergeCommitTitle, "PR_TITLE", "COMMIT_OR_PR_TITLE") {
		return errors.New("repository.squashMergeCommitTitle must be PR_TITLE or COMMIT_OR_PR_TITLE")
	}
	if r.SquashMergeCommitMessage != "" && !oneOf(r.SquashMergeCommitMessage, "COMMIT_MESSAGES", "PR_BODY", "BLANK") {
		return errors.New("repository.squashMergeCommitMessage must be COMMIT_MESSAGES, PR_BODY, or BLANK")
	}
	if r.PullRequestCreationPolicy != "" && !oneOf(r.PullRequestCreationPolicy, "all", "collaborators_only") {
		return errors.New("repository.pullRequestCreationPolicy must be all or collaborators_only")
	}
	return validateUnique("repository.topics", r.Topics)
}

func validateLabels(labels []LabelConfig) error {
	// GitHub treats label names case-insensitively, so "Bug" and "bug" would
	// collide at apply time even though they are distinct strings here.
	seen := make(map[string]struct{}, len(labels))
	for _, label := range labels {
		if label.Name == "" {
			return errors.New("labels contains a label without name")
		}
		if !isHexColor(label.Color) {
			return fmt.Errorf("label %q color must be six hexadecimal digits without a leading #", label.Name)
		}
		key := strings.ToLower(label.Name)
		if _, ok := seen[key]; ok {
			return fmt.Errorf("labels contains duplicate name %q (label names are case-insensitive)", label.Name)
		}
		seen[key] = struct{}{}
	}
	return nil
}

func isHexColor(value string) bool {
	if len(value) != 6 {
		return false
	}
	for _, r := range value {
		switch {
		case r >= '0' && r <= '9':
		case r >= 'a' && r <= 'f':
		case r >= 'A' && r <= 'F':
		default:
			return false
		}
	}
	return true
}

func validateMilestones(milestones []MilestoneConfig) error {
	titles := make([]string, 0, len(milestones))
	for _, milestone := range milestones {
		if milestone.Title == "" {
			return errors.New("milestones contains a milestone without title")
		}
		if milestone.State != "" && !oneOf(milestone.State, "open", "closed") {
			return fmt.Errorf("milestone %q state must be open or closed", milestone.Title)
		}
		if milestone.DueOn != "" {
			if _, err := time.Parse("2006-01-02", milestone.DueOn); err != nil {
				return fmt.Errorf("milestone %q dueOn must be a YYYY-MM-DD date", milestone.Title)
			}
		}
		titles = append(titles, milestone.Title)
	}
	return validateUnique("milestones", titles)
}

func validateSeed(seed *SeedConfig, checkSeedFilesystem bool) error {
	if seed == nil {
		return nil
	}
	count := 0
	if seed.Template != nil {
		count++
		if seed.Template.Repository == "" {
			return errors.New("seed.template.repository is required")
		}
	}
	if seed.Directory != nil {
		count++
		if seed.Directory.Path == "" {
			return errors.New("seed.directory.path is required")
		}
		if checkSeedFilesystem {
			if err := validateLocalSeedPath("seed.directory.path", seed.Directory.Path, false); err != nil {
				return err
			}
		}
	}
	if seed.ZIP != nil {
		count++
		if seed.ZIP.Path == "" {
			return errors.New("seed.zip.path is required")
		}
		if checkSeedFilesystem {
			if err := validateLocalSeedPath("seed.zip.path", seed.ZIP.Path, true); err != nil {
				return err
			}
		}
	}
	if seed.Initialize != nil {
		if seed.Initialize.GitignoreTemplate != "" && strings.TrimSpace(seed.Initialize.GitignoreTemplate) != seed.Initialize.GitignoreTemplate {
			return errors.New("seed.initialize.gitignoreTemplate must not contain leading or trailing whitespace")
		}
	}
	if count != 1 && (count != 0 || seed.Initialize == nil) {
		return errors.New("seed must define exactly one of template, directory, or zip")
	}
	for _, path := range seed.Overwrite {
		if err := validateRepositoryRelativePath("seed.overwrite", path); err != nil {
			return err
		}
	}
	if err := validateUnique("seed.overwrite", seed.Overwrite); err != nil {
		return err
	}
	return nil
}

func validateLocalSeedPath(field, path string, zip bool) error {
	if path == "" {
		return fmt.Errorf("%s is required", field)
	}
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("%s: %w", field, err)
	}
	if zip {
		if info.IsDir() || !strings.EqualFold(filepath.Ext(path), ".zip") {
			return fmt.Errorf("%s must name a .zip file", field)
		}
		return nil
	}
	if !info.IsDir() {
		return fmt.Errorf("%s must name a directory", field)
	}
	return nil
}

func validateActions(actions *ActionsConfig) error {
	if actions == nil {
		return nil
	}
	if actions.AllowedActions != "" && !oneOf(actions.AllowedActions, "all", "local_only", "selected") {
		return errors.New("actions.allowedActions must be all, local_only, or selected")
	}
	if actions.AllowedActions == "selected" && actions.SelectedActions == nil {
		return errors.New("actions.selectedActions is required when actions.allowedActions is selected")
	}
	if actions.AllowedActions != "selected" && actions.SelectedActions != nil {
		return errors.New("actions.selectedActions is only valid when actions.allowedActions is selected")
	}
	if actions.SelectedActions != nil {
		if err := validateUnique("actions.selectedActions.patternsAllowed", actions.SelectedActions.PatternsAllowed); err != nil {
			return err
		}
	}
	if customization := actions.OIDCSubjectCustomization; customization != nil {
		if err := validateUnique("actions.oidcSubjectCustomization.includeClaimKeys", customization.IncludeClaimKeys); err != nil {
			return err
		}
		for _, claim := range customization.IncludeClaimKeys {
			if claim == "" {
				return errors.New("actions.oidcSubjectCustomization.includeClaimKeys must not contain empty values")
			}
			for _, character := range claim {
				if (character < 'a' || character > 'z') && (character < 'A' || character > 'Z') && (character < '0' || character > '9') && character != '_' {
					return fmt.Errorf("actions.oidcSubjectCustomization.includeClaimKeys contains invalid key %q", claim)
				}
			}
		}
	}
	if actions.DefaultWorkflowPermissions != "" && !oneOf(actions.DefaultWorkflowPermissions, "read", "write") {
		return errors.New("actions.defaultWorkflowPermissions must be read or write")
	}
	if actions.ForkPullRequestApproval != "" && !oneOf(actions.ForkPullRequestApproval, "first_time_contributors_new_to_github", "first_time_contributors", "all_external_contributors") {
		return errors.New("actions.forkPullRequestApproval must be first_time_contributors_new_to_github, first_time_contributors, or all_external_contributors")
	}
	if actions.WorkflowAccessLevel != "" && !oneOf(actions.WorkflowAccessLevel, "none", "user", "organization") {
		return errors.New("actions.workflowAccessLevel must be none, user, or organization")
	}
	if err := validateVariables("actions.variables", actions.Variables); err != nil {
		return err
	}
	if err := validateSecrets("actions.secrets", actions.Secrets); err != nil {
		return err
	}
	return validateEnvironmentNames(actions.Environments)
}

func validateRulesets(rulesets []RulesetConfig) error {
	names := make([]string, 0, len(rulesets))
	for _, ruleset := range rulesets {
		if ruleset.Name == "" || ruleset.Target == "" || len(ruleset.Include) == 0 {
			return errors.New("rulesets require name, target, and at least one include pattern")
		}
		if !oneOf(ruleset.Enforcement, "active", "evaluate", "disabled") {
			return fmt.Errorf("ruleset %q enforcement must be active, evaluate, or disabled", ruleset.Name)
		}
		for _, rule := range ruleset.Rules {
			if rule.Type == "" {
				return fmt.Errorf("ruleset %q has a rule without type", ruleset.Name)
			}
		}
		if err := validateBypassActors(ruleset.Name, ruleset.BypassActors); err != nil {
			return err
		}
		names = append(names, ruleset.Name)
	}
	return validateUnique("rulesets", names)
}

func validateBypassActors(rulesetName string, actors []BypassActorConfig) error {
	for index, actor := range actors {
		set := 0
		if actor.Role != "" {
			set++
			if !oneOf(actor.Role, "admin", "write", "maintain") {
				return fmt.Errorf("ruleset %q bypassActors[%d] role must be admin, write, or maintain", rulesetName, index)
			}
		}
		if actor.Team != "" {
			set++
			if err := validateBypassActorSelector(rulesetName, index, "team", actor.Team); err != nil {
				return err
			}
		}
		if actor.App != "" {
			set++
			if err := validateBypassActorSelector(rulesetName, index, "app", actor.App); err != nil {
				return err
			}
		}
		if actor.User != "" {
			set++
			if _, found, err := ParseBypassActorID(actor.User); !found || err != nil {
				return fmt.Errorf("ruleset %q bypassActors[%d] user must use id:<number>", rulesetName, index)
			}
		}
		if actor.OrgAdmin {
			set++
		}
		if actor.CustomRole != "" {
			set++
			if err := validateBypassActorSelector(rulesetName, index, "customRole", actor.CustomRole); err != nil {
				return err
			}
		}
		if set != 1 {
			return fmt.Errorf("ruleset %q bypassActors[%d] must set exactly one of role, team, app, user, orgAdmin, or customRole", rulesetName, index)
		}
		// docs/design.md requires every entry to carry a bypassMode alongside
		// its actor selector.
		if !oneOf(actor.BypassMode, "always", "pull_request") {
			return fmt.Errorf("ruleset %q bypassActors[%d] bypassMode must be always or pull_request", rulesetName, index)
		}
	}
	return nil
}

// validateBypassActorSelector accepts a slug or the resolved "id:<number>"
// form ParseBypassActorID recognises. Role and orgAdmin are not eligible
// here: their actor IDs are the fixed GitHub base role IDs, so import always
// writes their symbolic form instead.
func validateBypassActorSelector(rulesetName string, index int, field, value string) error {
	if _, found, err := ParseBypassActorID(value); found && err != nil {
		return fmt.Errorf("ruleset %q bypassActors[%d] %s must be a slug or id:<number>: %w", rulesetName, index, field, err)
	}
	return nil
}

func validateAgents(agents *AgentsConfig) error {
	if agents == nil {
		return nil
	}
	if err := validateVariables("agents.variables", agents.Variables); err != nil {
		return err
	}
	if err := validateSecrets("agents.secrets", agents.Secrets); err != nil {
		return err
	}
	for field, path := range map[string]string{
		"agents.mcpConfigPath":    agents.MCPConfigPath,
		"agents.instructionsPath": agents.InstructionsPath,
		"agents.customAgentsPath": agents.CustomAgentsPath,
		"agents.skillsPath":       agents.SkillsPath,
	} {
		if path != "" {
			if err := validateRepositoryRelativePath(field, path); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateVariables(field string, variables []VariableRef) error {
	names := make([]string, 0, len(variables))
	for _, variable := range variables {
		if variable.Name == "" {
			return fmt.Errorf("%s contains a variable without name", field)
		}
		names = append(names, variable.Name)
	}
	return validateUnique(field, names)
}

func validateSecrets(field string, secrets []SecretRef) error {
	names := make([]string, 0, len(secrets))
	for _, secret := range secrets {
		if secret.Name == "" || secret.SourceEnv == "" {
			return fmt.Errorf("%s requires name and sourceEnv", field)
		}
		if !oneOf(defaultWhenEmpty(secret.UpdatePolicy, "createOnly"), "createOnly", "always") {
			return fmt.Errorf("%s secret %q has an invalid updatePolicy", field, secret.Name)
		}
		names = append(names, secret.Name)
	}
	return validateUnique(field, names)
}

func validateEnvironmentNames(environments []Environment) error {
	names := make([]string, 0, len(environments))
	for _, environment := range environments {
		if environment.Name == "" {
			return errors.New("actions.environments contains an environment without name")
		}
		names = append(names, environment.Name)
		if environment.WaitTimer != nil && (*environment.WaitTimer < 0 || *environment.WaitTimer > 43200) {
			return fmt.Errorf("environment %q waitTimer must be between 0 and 43200 minutes", environment.Name)
		}
		if environment.DeploymentBranchPolicy != nil {
			policy := environment.DeploymentBranchPolicy
			if policy.AllowAllBranches {
				if policy.ProtectedBranches || policy.CustomBranchPolicies {
					return fmt.Errorf("environment %q allowAllBranches cannot be combined with a deployment branch policy", environment.Name)
				}
			} else if policy.ProtectedBranches == policy.CustomBranchPolicies {
				return fmt.Errorf("environment %q deploymentBranchPolicy must enable exactly one policy or allowAllBranches", environment.Name)
			}
			if policy.CustomBranchPolicies && environment.BranchPolicies == nil {
				return fmt.Errorf("environment %q custom deployment branch policies require branchPolicies", environment.Name)
			}
		}
		if environment.Reviewers != nil {
			for _, reviewer := range *environment.Reviewers {
				if !oneOf(reviewer.Type, "User", "Team") || reviewer.ID <= 0 {
					return fmt.Errorf("environment %q reviewers require type User or Team and a positive id", environment.Name)
				}
			}
		}
		if environment.BranchPolicies != nil {
			policies := make([]string, 0, len(*environment.BranchPolicies))
			for _, policy := range *environment.BranchPolicies {
				if policy.Name == "" || !oneOf(policy.Type, "branch", "tag") {
					return fmt.Errorf("environment %q branchPolicies require name and type branch or tag", environment.Name)
				}
				policies = append(policies, policy.Type+":"+policy.Name)
			}
			if err := validateUnique("environment "+environment.Name+" branchPolicies", policies); err != nil {
				return err
			}
		}
		if environment.CustomProtectionRules != nil {
			for _, slug := range *environment.CustomProtectionRules {
				if slug == "" {
					return fmt.Errorf("environment %q customProtectionRules require non-empty app slugs", environment.Name)
				}
			}
			if err := validateUnique("environment "+environment.Name+" customProtectionRules", *environment.CustomProtectionRules); err != nil {
				return err
			}
		}
	}
	return validateUnique("actions.environments", names)
}

func validateSecurity(security *SecurityConfig) error {
	if security == nil {
		return nil
	}
	if security.DelegatedBypassReviewers == nil {
		return nil
	}
	keys := make([]string, 0, len(*security.DelegatedBypassReviewers))
	for index, reviewer := range *security.DelegatedBypassReviewers {
		if reviewer.ReviewerID <= 0 || !oneOf(reviewer.ReviewerType, "TEAM", "ROLE") {
			return fmt.Errorf("security.delegatedBypassReviewers[%d] requires positive reviewerId and reviewerType TEAM or ROLE", index)
		}
		if reviewer.Mode != "" && !oneOf(reviewer.Mode, "ALWAYS", "EXEMPT") {
			return fmt.Errorf("security.delegatedBypassReviewers[%d] mode must be ALWAYS or EXEMPT", index)
		}
		keys = append(keys, fmt.Sprintf("%d:%s", reviewer.ReviewerID, reviewer.ReviewerType))
	}
	return validateUnique("security.delegatedBypassReviewers", keys)
}

func validateCommands(commands []CommandConfig) error {
	ids := make([]string, 0, len(commands))
	for _, command := range commands {
		if command.ID == "" || command.Program == "" || command.Timeout == "" {
			return errors.New("commands require id, program, and timeout")
		}
		if isShell(command.Program) {
			return fmt.Errorf("command %q uses forbidden shell program %q", command.ID, command.Program)
		}
		if _, err := command.TimeoutDuration(); err != nil {
			return fmt.Errorf("command %q timeout: %w", command.ID, err)
		}
		if command.CWD != "" && command.CWD != "." {
			if err := validateRepositoryRelativePath("command "+command.ID+" cwd", command.CWD); err != nil {
				return err
			}
		}
		ids = append(ids, command.ID)
	}
	return validateUnique("commands", ids)
}

func validateRepositoryRelativePath(field, path string) error {
	cleaned := filepath.Clean(path)
	if path == "" || filepath.IsAbs(path) || cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return fmt.Errorf("%s must be a non-empty repository-relative path", field)
	}
	return nil
}

func validateUnique(field string, values []string) error {
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if value == "" {
			return fmt.Errorf("%s cannot contain an empty value", field)
		}
		if _, ok := seen[value]; ok {
			return fmt.Errorf("%s contains duplicate value %q", field, value)
		}
		seen[value] = struct{}{}
	}
	return nil
}

func oneOf(value string, permitted ...string) bool {
	for _, candidate := range permitted {
		if value == candidate {
			return true
		}
	}
	return false
}

func defaultWhenEmpty(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func isShell(program string) bool {
	switch strings.ToLower(filepath.Base(program)) {
	case "sh", "bash", "zsh", "fish", "cmd", "cmd.exe", "powershell", "powershell.exe", "pwsh", "pwsh.exe":
		return true
	default:
		return false
	}
}
