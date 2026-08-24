package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Load reads a YAML or JSON configuration document and validates it. yaml.v3
// accepts JSON as a YAML subset, detects duplicate mapping keys, and its
// KnownFields option rejects fields that do not belong to the typed contract.
func Load(path string) (*Config, error) {
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
	if err := Validate(&cfg); err != nil {
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
	if cfg == nil {
		return errors.New("configuration is required")
	}
	if cfg.SchemaVersion != CurrentSchemaVersion {
		return fmt.Errorf("schemaVersion must be %d", CurrentSchemaVersion)
	}
	if err := validateRepository(cfg.Repository); err != nil {
		return err
	}
	if err := validateSeed(cfg.Seed); err != nil {
		return err
	}
	if err := validateActions(cfg.Actions); err != nil {
		return err
	}
	if err := validateRulesets(cfg.Rulesets); err != nil {
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
	return validateCommands(cfg.Commands)
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

func validateSeed(seed *SeedConfig) error {
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
		if err := validateLocalSeedPath("seed.directory.path", seed.Directory.Path, false); err != nil {
			return err
		}
	}
	if seed.ZIP != nil {
		count++
		if err := validateLocalSeedPath("seed.zip.path", seed.ZIP.Path, true); err != nil {
			return err
		}
	}
	if count != 1 {
		return errors.New("seed must define exactly one of template, directory, or zip")
	}
	for _, path := range seed.Overwrite {
		if err := validateRepositoryRelativePath("seed.overwrite", path); err != nil {
			return err
		}
	}
	return validateUnique("seed.overwrite", seed.Overwrite)
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
	if actions.DefaultWorkflowPermissions != "" && !oneOf(actions.DefaultWorkflowPermissions, "read", "write") {
		return errors.New("actions.defaultWorkflowPermissions must be read or write")
	}
	if actions.ForkPullRequestApproval != "" && !oneOf(actions.ForkPullRequestApproval, "first_time_contributors_new_to_github", "first_time_contributors", "all_external_contributors") {
		return errors.New("actions.forkPullRequestApproval must be first_time_contributors_new_to_github, first_time_contributors, or all_external_contributors")
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
		}
		if actor.App != "" {
			set++
		}
		if actor.OrgAdmin {
			set++
		}
		if actor.CustomRole != "" {
			set++
		}
		if set != 1 {
			return fmt.Errorf("ruleset %q bypassActors[%d] must set exactly one of role, team, app, orgAdmin, or customRole", rulesetName, index)
		}
		// docs/design.md requires every entry to carry a bypassMode alongside
		// its actor selector.
		if !oneOf(actor.BypassMode, "always", "pull_request") {
			return fmt.Errorf("ruleset %q bypassActors[%d] bypassMode must be always or pull_request", rulesetName, index)
		}
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
	}
	return validateUnique("actions.environments", names)
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
