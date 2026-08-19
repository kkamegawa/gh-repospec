// Package planner calculates and applies declarative repository bootstrap changes.
package planner

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/kkamegawa/gh-repobootstrapper/internal/config"
	"github.com/kkamegawa/gh-repobootstrapper/internal/githubapi"
	"github.com/kkamegawa/gh-repobootstrapper/internal/seed"
)

// ChangeState describes the action a plan can take for one resource.
type ChangeState string

const (
	ChangeCreate  ChangeState = "create"
	ChangeUpdate  ChangeState = "update"
	ChangeNoop    ChangeState = "noop"
	ChangeManual  ChangeState = "manual"
	ChangeBlocked ChangeState = "blocked"
)

// Stage fixes the application order. Rulesets remain last so they cannot
// prevent repository initialization. Security settings are split in two:
// StageSecurityPre (secret scanning, push protection) runs before files so the
// initial commit is protected; StageSecurity (dependency graph, Dependabot,
// CodeQL default setup, private vulnerability reporting) runs after settings
// and automation, once there is repository content for those features to act on.
type Stage string

const (
	StageRepository  Stage = "repository"
	StageSecurityPre Stage = "security-pre"
	StageFiles       Stage = "files"
	StageSettings    Stage = "settings"
	StageAutomation  Stage = "automation"
	StageSecurity    Stage = "security"
	StageRulesets    Stage = "rulesets"
)

// Change is one visible capability or resource difference.
type Change struct {
	Stage    Stage       `json:"stage"`
	Resource string      `json:"resource"`
	State    ChangeState `json:"state"`
	Summary  string      `json:"summary"`
}

// Plan is a complete, read-only report. Blocked changes prevent apply;
// manual changes are reported but do not prevent the supported work.
type Plan struct {
	Repository githubapi.Repository `json:"repository"`
	Exists     bool                 `json:"exists"`
	Changes    []Change             `json:"changes"`
}

// HasBlocked reports whether an apply would be unsafe or unsupported.
func (p Plan) HasBlocked() bool {
	return slices.ContainsFunc(p.Changes, func(change Change) bool { return change.State == ChangeBlocked })
}

// HasChanges reports whether any supported remote mutation is required.
func (p Plan) HasChanges() bool {
	return slices.ContainsFunc(p.Changes, func(change Change) bool {
		return change.State == ChangeCreate || change.State == ChangeUpdate
	})
}

// Service is the deliberately narrow GitHub dependency required by planner.
// githubapi.Client implements it and tests may use small fakes.
type Service interface {
	GetRepository(context.Context, githubapi.Repository) (githubapi.RepositoryInfo, error)
	CreateRepository(context.Context, githubapi.CreateRepositoryRequest) (githubapi.RepositoryInfo, error)
	UpdateRepository(context.Context, githubapi.Repository, githubapi.UpdateRepositoryRequest) (githubapi.RepositoryInfo, error)
	ReplaceTopics(context.Context, githubapi.Repository, []string) ([]string, error)
	GetActionsPermissions(context.Context, githubapi.Repository) (githubapi.ActionsPermissions, error)
	UpdateActionsPermissions(context.Context, githubapi.Repository, githubapi.ActionsPermissions) error
	UpdateSelectedActions(context.Context, githubapi.Repository, githubapi.SelectedActionsRequest) error
	UpdateDefaultWorkflowPermissions(context.Context, githubapi.Repository, githubapi.DefaultWorkflowPermissions) error
	ListActionVariables(context.Context, githubapi.Repository) (githubapi.ActionVariableList, error)
	CreateActionVariable(context.Context, githubapi.Repository, githubapi.ActionVariable) error
	UpdateActionVariable(context.Context, githubapi.Repository, githubapi.ActionVariable) error
	CreateOrUpdateEnvironment(context.Context, githubapi.Repository, string, githubapi.EnvironmentRequest) (githubapi.Environment, error)
	ListRulesets(context.Context, githubapi.Repository) ([]githubapi.Ruleset, error)
	CreateRuleset(context.Context, githubapi.Repository, githubapi.Ruleset) (githubapi.Ruleset, error)
	UpdateRuleset(context.Context, githubapi.Repository, int64, githubapi.Ruleset) (githubapi.Ruleset, error)
	UpdateSecurityAndAnalysis(context.Context, githubapi.Repository, githubapi.SecurityAndAnalysisRequest) (githubapi.RepositoryInfo, error)
	ConfigureCodeQLDefaultSetup(context.Context, githubapi.Repository, githubapi.CodeQLDefaultSetupRequest) error
	SetPrivateVulnerabilityReporting(context.Context, githubapi.Repository, bool) error
	ListAgentVariables(context.Context, githubapi.Repository) (githubapi.AgentVariableList, error)
	CreateAgentVariable(context.Context, githubapi.Repository, githubapi.AgentVariable) error
	UpdateAgentVariable(context.Context, githubapi.Repository, githubapi.AgentVariable) error
}

// Options controls planning and application without embedding CLI concerns.
type Options struct {
	RepositoryOverride string
	ApprovedCommandIDs map[string]bool
}

// Engine owns no mutable configuration and may be reused for repeated plans.
type Engine struct {
	service Service
}

// ContentService is implemented by the concrete GitHub client for file commits
// and template archive downloads. It remains optional so settings-only planner
// tests can use small fakes.
type ContentService interface {
	CommitFiles(context.Context, githubapi.Repository, string, string, []githubapi.File) (githubapi.CommitResult, error)
	MaterializeTemplateArchive(context.Context, githubapi.Repository, string, string) error
}

type treeService interface {
	GetBranchHead(context.Context, githubapi.Repository, string) (githubapi.GitReference, error)
	GetCommit(context.Context, githubapi.Repository, string) (githubapi.GitCommit, error)
	GetTree(context.Context, githubapi.Repository, string, bool) (githubapi.GitTree, error)
	GetBlob(context.Context, githubapi.Repository, string) (githubapi.GitBlob, error)
}

type secretService interface {
	GetActionsSecretPublicKey(context.Context, githubapi.Repository) (githubapi.PublicKey, error)
	PutActionsSecret(context.Context, githubapi.Repository, string, githubapi.EncryptedSecret) error
	ListActionsSecrets(context.Context, githubapi.Repository) (githubapi.SecretList, error)
	GetAgentSecretPublicKey(context.Context, githubapi.Repository) (githubapi.PublicKey, error)
	PutAgentSecret(context.Context, githubapi.Repository, string, githubapi.EncryptedSecret) error
	ListAgentSecrets(context.Context, githubapi.Repository) (githubapi.SecretList, error)
}

type dependabotService interface {
	SetVulnerabilityAlerts(context.Context, githubapi.Repository, bool) error
	SetAutomatedSecurityFixes(context.Context, githubapi.Repository, bool) error
}

type userService interface {
	GetAuthenticatedUser(context.Context) (githubapi.Owner, error)
}

type branchService interface {
	GetBranchHead(context.Context, githubapi.Repository, string) (githubapi.GitReference, error)
	CreateBranch(context.Context, githubapi.Repository, string, string) (githubapi.GitReference, error)
}

// New creates an Engine with the supplied GitHub service.
func New(service Service) (*Engine, error) {
	if service == nil {
		return nil, errors.New("GitHub service is required")
	}
	return &Engine{service: service}, nil
}

// Build calculates a complete change report without modifying GitHub.
func (e *Engine) Build(ctx context.Context, cfg *config.Config, options Options) (Plan, error) {
	if e == nil || e.service == nil {
		return Plan{}, errors.New("planner is not initialized")
	}
	if err := config.Validate(cfg); err != nil {
		return Plan{}, fmt.Errorf("validate configuration: %w", err)
	}
	repository, err := resolveRepository(cfg.Repository, options.RepositoryOverride)
	if err != nil {
		return Plan{}, err
	}
	plan := Plan{Repository: repository}
	info, err := e.service.GetRepository(ctx, repository)
	if err != nil {
		if !githubapi.IsNotFound(err) {
			return Plan{}, fmt.Errorf("get repository %s: %w", repository, err)
		}
		if !cfg.Repository.Create {
			return Plan{}, fmt.Errorf("repository %s does not exist and repository.create is false", repository)
		}
		plan.Changes = append(plan.Changes, Change{Stage: StageRepository, Resource: "repository", State: ChangeCreate, Summary: "create repository"})
		appendDeferredChanges(&plan, cfg)
		return finalize(plan), nil
	}
	plan.Exists = true
	appendRepositoryChanges(&plan, cfg.Repository, info)
	appendActionsChanges(ctx, e.service, &plan, cfg.Actions, repository)
	appendRulesetChanges(ctx, e.service, &plan, cfg.Rulesets, repository)
	appendSecurityChanges(&plan, cfg.Security)
	appendAgentChanges(ctx, e.service, &plan, cfg.Agents, repository)
	appendSeedChanges(&plan, cfg)
	return finalize(plan), nil
}

// Apply recalculates the plan before making changes, then performs supported
// changes in stage order. Unsupported manual work remains visible in the
// returned plan. A partial result can be safely reconciled by running apply
// again after resolving the reported error.
func (e *Engine) Apply(ctx context.Context, cfg *config.Config, options Options) (Plan, error) {
	plan, err := e.Build(ctx, cfg, options)
	if err != nil {
		return Plan{}, err
	}
	if plan.HasBlocked() {
		return plan, errors.New("apply is blocked; inspect the plan for unsupported capabilities or missing inputs")
	}

	repository := plan.Repository
	info, err := e.service.GetRepository(ctx, repository)
	created := false
	if err != nil && !githubapi.IsNotFound(err) {
		return plan, fmt.Errorf("get repository %s before apply: %w", repository, err)
	}
	if githubapi.IsNotFound(err) {
		request := createRequest(cfg.Repository, repository)
		if users, ok := e.service.(userService); ok {
			if user, userErr := users.GetAuthenticatedUser(ctx); userErr == nil && strings.EqualFold(user.Login, repository.Owner) {
				request.Owner = ""
			}
		}
		info, err = e.service.CreateRepository(ctx, request)
		if err != nil {
			return plan, fmt.Errorf("create repository %s: %w", repository, err)
		}
		created = true
	}
	if cfg.Repository.DefaultBranch != "" && info.DefaultBranch != cfg.Repository.DefaultBranch {
		if branches, ok := e.service.(branchService); ok {
			if _, branchErr := branches.GetBranchHead(ctx, repository, cfg.Repository.DefaultBranch); githubapi.IsNotFound(branchErr) {
				base, baseErr := branches.GetBranchHead(ctx, repository, info.DefaultBranch)
				if baseErr != nil {
					return plan, fmt.Errorf("get current default branch %s: %w", info.DefaultBranch, baseErr)
				}
				if _, createErr := branches.CreateBranch(ctx, repository, cfg.Repository.DefaultBranch, base.Object.SHA); createErr != nil {
					return plan, fmt.Errorf("create default branch %s: %w", cfg.Repository.DefaultBranch, createErr)
				}
			} else if branchErr != nil {
				return plan, fmt.Errorf("check default branch %s: %w", cfg.Repository.DefaultBranch, branchErr)
			}
			info.DefaultBranch = cfg.Repository.DefaultBranch
		} else {
			return plan, errors.New("default branch creation is unavailable from the configured GitHub service")
		}
	}

	if err := e.applyRepository(ctx, repository, cfg.Repository, info); err != nil {
		return plan, err
	}
	if err := e.applyPreContentSecurity(ctx, repository, cfg.Security); err != nil {
		return plan, err
	}
	if err := e.applySeed(ctx, repository, info, cfg, options, !created); err != nil {
		return plan, err
	}
	if err := e.applyActions(ctx, repository, cfg.Actions); err != nil {
		return plan, err
	}
	if err := e.applyAgentVariables(ctx, repository, cfg.Agents); err != nil {
		return plan, err
	}
	if err := e.applyRemainingSecurity(ctx, repository, cfg.Security); err != nil {
		return plan, err
	}
	if err := e.applyRulesets(ctx, repository, cfg.Rulesets); err != nil {
		return plan, err
	}
	return plan, nil
}

func createRequest(desired config.RepositoryConfig, repository githubapi.Repository) githubapi.CreateRepositoryRequest {
	private := desired.Visibility == "private"
	return githubapi.CreateRepositoryRequest{
		Owner:               repository.Owner,
		Name:                repository.Name,
		Description:         desired.Description,
		Private:             &private,
		Visibility:          desired.Visibility,
		AutoInit:            true,
		AllowMergeCommit:    desired.AllowMergeCommit,
		AllowSquashMerge:    desired.AllowSquashMerge,
		AllowRebaseMerge:    desired.AllowRebaseMerge,
		AllowAutoMerge:      desired.AllowAutoMerge,
		DeleteBranchOnMerge: desired.DeleteBranchOnMerge,
	}
}

func (e *Engine) applyRepository(ctx context.Context, repository githubapi.Repository, desired config.RepositoryConfig, actual githubapi.RepositoryInfo) error {
	request := githubapi.UpdateRepositoryRequest{}
	changed := false
	if desired.AllowMergeCommit != nil && *desired.AllowMergeCommit != actual.AllowMergeCommit {
		request.AllowMergeCommit = desired.AllowMergeCommit
		changed = true
	}
	if desired.AllowSquashMerge != nil && *desired.AllowSquashMerge != actual.AllowSquashMerge {
		request.AllowSquashMerge = desired.AllowSquashMerge
		changed = true
	}
	if desired.AllowRebaseMerge != nil && *desired.AllowRebaseMerge != actual.AllowRebaseMerge {
		request.AllowRebaseMerge = desired.AllowRebaseMerge
		changed = true
	}
	if desired.AllowAutoMerge != nil && *desired.AllowAutoMerge != actual.AllowAutoMerge {
		request.AllowAutoMerge = desired.AllowAutoMerge
		changed = true
	}
	if desired.DeleteBranchOnMerge != nil && *desired.DeleteBranchOnMerge != actual.DeleteBranchOnMerge {
		request.DeleteBranchOnMerge = desired.DeleteBranchOnMerge
		changed = true
	}
	if desired.Description != actual.Description {
		description := desired.Description
		request.Description = &description
		changed = true
	}
	if desired.DefaultBranch != "" && desired.DefaultBranch != actual.DefaultBranch {
		request.DefaultBranch = desired.DefaultBranch
		changed = true
	}
	if desired.Visibility != actual.Visibility && (desired.Visibility != "private" || !actual.Private) {
		if !desired.AllowVisibilityChange {
			return errors.New("repository visibility change requires allowVisibilityChange")
		}
		private := desired.Visibility == "private"
		request.Private = &private
		request.Visibility = desired.Visibility
		changed = true
	}
	if changed {
		if _, err := e.service.UpdateRepository(ctx, repository, request); err != nil {
			return fmt.Errorf("update repository %s: %w", repository, err)
		}
	}
	if desired.Topics != nil && !sameStrings(desired.Topics, actual.Topics) {
		if _, err := e.service.ReplaceTopics(ctx, repository, desired.Topics); err != nil {
			return fmt.Errorf("replace topics for %s: %w", repository, err)
		}
	}
	return nil
}

func (e *Engine) applySeed(ctx context.Context, repository githubapi.Repository, info githubapi.RepositoryInfo, cfg *config.Config, options Options, existing bool) error {
	if cfg.Seed == nil && len(cfg.Commands) == 0 {
		return nil
	}
	content, ok := e.service.(ContentService)
	if !ok {
		return errors.New("seed content operations are unavailable from the configured GitHub service")
	}
	workspace, err := seed.NewWorkspace()
	if err != nil {
		return err
	}
	defer func() { _ = workspace.Close() }()

	var sourceDir string
	if cfg.Seed != nil {
		var source seed.Source
		var materializer seed.TemplateMaterializer
		switch {
		case cfg.Seed.Directory != nil:
			source = seed.Source{Kind: seed.SourceDirectory, Path: cfg.Seed.Directory.Path}
		case cfg.Seed.ZIP != nil:
			source = seed.Source{Kind: seed.SourceZIP, Path: cfg.Seed.ZIP.Path}
		case cfg.Seed.Template != nil:
			templateRepo, parseErr := parseRepository(cfg.Seed.Template.Repository)
			if parseErr != nil {
				return parseErr
			}
			templateInfo, infoErr := e.service.GetRepository(ctx, templateRepo)
			if infoErr != nil {
				return fmt.Errorf("get template repository %s: %w", templateRepo, infoErr)
			}
			source = seed.Source{Kind: seed.SourceTemplate, Template: cfg.Seed.Template.Repository}
			ref := templateInfo.DefaultBranch
			if ref == "" {
				ref = "main"
			}
			materializer = templateMaterializer{service: content, repository: templateRepo, ref: ref}
		default:
			return errors.New("seed source is required")
		}
		sourceDir, err = seed.StageSource(ctx, workspace, source, materializer, seed.StageOptions{})
		if err != nil {
			return err
		}
	} else {
		sourceDir = workspace.SeedDir()
		if err := os.MkdirAll(sourceDir, 0o700); err != nil {
			return err
		}
	}

	commandWorkspace := &seed.Workspace{Root: sourceDir}
	for _, command := range cfg.Commands {
		timeout, timeoutErr := command.TimeoutDuration()
		if timeoutErr != nil {
			return fmt.Errorf("command %q timeout: %w", command.ID, timeoutErr)
		}
		if _, runErr := seed.RunApprovedCommand(ctx, commandWorkspace, seed.Command{
			ID: command.ID, Program: command.Program, Args: command.Args,
			CWD: command.CWD, Timeout: timeout, Environment: command.Environment,
		}, seed.CommandOptions{ApprovedCommandIDs: options.ApprovedCommandIDs, OutputLimit: 64 << 10}); runErr != nil {
			return runErr
		}
	}

	files, err := stagedFiles(sourceDir)
	if err != nil {
		return err
	}
	branch := info.DefaultBranch
	if branch == "" {
		branch = cfg.Repository.DefaultBranch
	}
	if branch == "" {
		branch = "main"
	}
	if existing {
		files, err = filterAndValidateExistingFiles(ctx, e.service, repository, branch, files, overwriteSet(cfg.Seed))
		if err != nil {
			return err
		}
	}
	if len(files) == 0 {
		return nil
	}
	if _, err := content.CommitFiles(ctx, repository, branch, "chore: bootstrap repository", files); err != nil {
		return fmt.Errorf("commit bootstrap files: %w", err)
	}
	return nil
}

type templateMaterializer struct {
	service    ContentService
	repository githubapi.Repository
	ref        string
}

func (m templateMaterializer) MaterializeTemplate(ctx context.Context, _ string, destination string) error {
	return m.service.MaterializeTemplateArchive(ctx, m.repository, m.ref, destination)
}

func parseRepository(value string) (githubapi.Repository, error) {
	parts := strings.Split(value, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return githubapi.Repository{}, fmt.Errorf("repository reference must be owner/name, got %q", value)
	}
	return githubapi.Repository{Owner: parts[0], Name: parts[1]}, nil
}

func overwriteSet(seedConfig *config.SeedConfig) map[string]bool {
	result := make(map[string]bool)
	if seedConfig == nil {
		return result
	}
	for _, path := range seedConfig.Overwrite {
		result[path] = true
	}
	return result
}

func stagedFiles(root string) ([]githubapi.File, error) {
	files := make([]githubapi.File, 0)
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("staged path is not a regular file: %s", path)
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		content, readErr := os.ReadFile(path) //nolint:gosec // path comes from the confined temporary staging workspace
		if readErr != nil {
			return readErr
		}
		// entry.Type() carries only file-type bits, never permission bits; the
		// executable bit requires the full FileInfo.
		info, infoErr := entry.Info()
		if infoErr != nil {
			return infoErr
		}
		mode := "100644"
		if info.Mode().Perm()&0o111 != 0 {
			mode = "100755"
		}
		files = append(files, githubapi.File{Path: filepath.ToSlash(rel), Content: content, Mode: mode})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("collect staged files: %w", err)
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return files, nil
}

func filterAndValidateExistingFiles(ctx context.Context, service Service, repository githubapi.Repository, branch string, files []githubapi.File, overwrite map[string]bool) ([]githubapi.File, error) {
	trees, ok := service.(treeService)
	if !ok {
		return nil, errors.New("remote tree inspection is unavailable; refusing to upload seed files")
	}
	head, err := trees.GetBranchHead(ctx, repository, branch)
	if err != nil {
		return nil, fmt.Errorf("get branch head for seed conflict check: %w", err)
	}
	commit, err := trees.GetCommit(ctx, repository, head.Object.SHA)
	if err != nil {
		return nil, fmt.Errorf("get branch commit for seed conflict check: %w", err)
	}
	tree, err := trees.GetTree(ctx, repository, commit.Tree.SHA, true)
	if err != nil {
		return nil, fmt.Errorf("get repository tree for seed conflict check: %w", err)
	}
	if tree.Truncated {
		return nil, errors.New("repository tree is truncated; refusing to apply seed")
	}
	existing := make(map[string]githubapi.GitTreeEntry, len(tree.Tree))
	for _, entry := range tree.Tree {
		if entry.Type == "blob" {
			existing[entry.Path] = entry
		}
	}
	filtered := make([]githubapi.File, 0, len(files))
	for _, file := range files {
		entry, exists := existing[file.Path]
		if !exists {
			filtered = append(filtered, file)
			continue
		}
		blob, blobErr := trees.GetBlob(ctx, repository, entry.SHA)
		if blobErr != nil {
			return nil, fmt.Errorf("get existing blob %q: %w", file.Path, blobErr)
		}
		encoded := strings.Join(strings.Fields(blob.Content), "")
		current, decodeErr := base64.StdEncoding.DecodeString(encoded)
		if decodeErr != nil {
			return nil, fmt.Errorf("decode existing blob %q: %w", file.Path, decodeErr)
		}
		if bytes.Equal(current, file.Content) {
			continue
		}
		if !overwrite[file.Path] {
			return nil, fmt.Errorf("seed conflicts with existing file %q; add it to seed.overwrite to replace it", file.Path)
		}
		filtered = append(filtered, file)
	}
	return filtered, nil
}

func (e *Engine) applyActions(ctx context.Context, repository githubapi.Repository, desired *config.ActionsConfig) error {
	if desired == nil {
		return nil
	}
	actual, err := e.service.GetActionsPermissions(ctx, repository)
	if err != nil {
		return fmt.Errorf("get Actions permissions for %s: %w", repository, err)
	}
	permissions := actual
	changed := false
	if desired.Enabled != nil && *desired.Enabled != permissions.Enabled {
		permissions.Enabled = *desired.Enabled
		changed = true
	}
	if desired.AllowedActions != "" && desired.AllowedActions != permissions.AllowedActions {
		permissions.AllowedActions = desired.AllowedActions
		changed = true
	}
	if desired.RequireSHAForGitHubActions != nil && *desired.RequireSHAForGitHubActions != permissions.SHAPinningRequired {
		permissions.SHAPinningRequired = *desired.RequireSHAForGitHubActions
		changed = true
	}
	if changed {
		if err := e.service.UpdateActionsPermissions(ctx, repository, permissions); err != nil {
			return fmt.Errorf("update Actions permissions for %s: %w", repository, err)
		}
	}
	if desired.AllowedActions == "selected" && desired.SelectedActions != nil {
		request := githubapi.SelectedActionsRequest{
			GitHubOwnedAllowed: desired.SelectedActions.GitHubOwnedAllowed,
			VerifiedAllowed:    desired.SelectedActions.VerifiedAllowed,
			PatternsAllowed:    desired.SelectedActions.PatternsAllowed,
		}
		if err := e.service.UpdateSelectedActions(ctx, repository, request); err != nil {
			return fmt.Errorf("update selected Actions and reusable workflows for %s: %w", repository, err)
		}
	}
	if desired.DefaultWorkflowPermissions != "" || desired.CanApprovePullRequestWorkflows != nil {
		workflow := githubapi.DefaultWorkflowPermissions{DefaultWorkflowPermissions: desired.DefaultWorkflowPermissions}
		if desired.CanApprovePullRequestWorkflows != nil {
			workflow.CanApprovePullRequest = *desired.CanApprovePullRequestWorkflows
		}
		if err := e.service.UpdateDefaultWorkflowPermissions(ctx, repository, workflow); err != nil {
			return fmt.Errorf("update default workflow permissions for %s: %w", repository, err)
		}
	}
	if err := e.applyActionVariables(ctx, repository, desired.Variables); err != nil {
		return err
	}
	for _, environment := range desired.Environments {
		if _, err := e.service.CreateOrUpdateEnvironment(ctx, repository, environment.Name, githubapi.EnvironmentRequest{}); err != nil {
			return fmt.Errorf("ensure Actions environment %q: %w", environment.Name, err)
		}
	}
	if len(desired.Variables) > 0 {
		current, err := e.service.ListActionVariables(ctx, repository)
		if err != nil {
			return fmt.Errorf("list Actions variables for %s: %w", repository, err)
		}
		existing := make(map[string]string, len(current.Variables))
		for _, variable := range current.Variables {
			existing[variable.Name] = variable.Value
		}
		for _, variable := range desired.Variables {
			if value, ok := existing[variable.Name]; ok && value == variable.Value {
				continue
			}
			request := githubapi.ActionVariable{Name: variable.Name, Value: variable.Value}
			if _, ok := existing[variable.Name]; ok {
				if err := e.service.UpdateActionVariable(ctx, repository, request); err != nil {
					return fmt.Errorf("update Actions variable %q: %w", variable.Name, err)
				}
			} else if err := e.service.CreateActionVariable(ctx, repository, request); err != nil {
				return fmt.Errorf("create Actions variable %q: %w", variable.Name, err)
			}
		}
	}
	for _, environment := range desired.Environments {
		if _, err := e.service.CreateOrUpdateEnvironment(ctx, repository, environment.Name, githubapi.EnvironmentRequest{}); err != nil {
			return fmt.Errorf("ensure Actions environment %q: %w", environment.Name, err)
		}
	}
	if len(desired.Secrets) > 0 {
		if err := e.applySecrets(ctx, repository, desired.Secrets, false); err != nil {
			return err
		}
	}
	return nil
}

func (e *Engine) applyActionVariables(ctx context.Context, repository githubapi.Repository, desired []config.VariableRef) error {
	if len(desired) == 0 {
		return nil
	}
	current, err := e.service.ListActionVariables(ctx, repository)
	if err != nil {
		return fmt.Errorf("list Actions variables for %s: %w", repository, err)
	}
	existing := make(map[string]string, len(current.Variables))
	for _, variable := range current.Variables {
		existing[variable.Name] = variable.Value
	}
	for _, variable := range desired {
		if actual, ok := existing[variable.Name]; ok {
			if actual == variable.Value {
				continue
			}
			if err := e.service.UpdateActionVariable(ctx, repository, githubapi.ActionVariable{Name: variable.Name, Value: variable.Value}); err != nil {
				return fmt.Errorf("update Actions variable %q: %w", variable.Name, err)
			}
			continue
		}
		if err := e.service.CreateActionVariable(ctx, repository, githubapi.ActionVariable{Name: variable.Name, Value: variable.Value}); err != nil {
			return fmt.Errorf("create Actions variable %q: %w", variable.Name, err)
		}
	}
	return nil
}

// applyPreContentSecurity configures secret scanning and push protection before any
// seed content is committed, so the initial commit is covered by push protection.
func (e *Engine) applyPreContentSecurity(ctx context.Context, repository githubapi.Repository, desired *config.SecurityConfig) error {
	if desired == nil {
		return nil
	}
	request := githubapi.SecurityAndAnalysisRequest{
		SecretScanning:            securityFeature(desired.SecretScanning),
		SecretScanningPushProtect: securityFeature(desired.PushProtection),
	}
	if request.SecretScanning != nil || request.SecretScanningPushProtect != nil {
		if _, err := e.service.UpdateSecurityAndAnalysis(ctx, repository, request); err != nil {
			if (desired.SecretScanning == nil || !desired.SecretScanning.Optional) && (desired.PushProtection == nil || !desired.PushProtection.Optional) {
				return fmt.Errorf("update pre-content security settings for %s: %w", repository, err)
			}
		}
	}
	return nil
}

// applyRemainingSecurity configures Dependabot, CodeQL default setup, and private
// vulnerability reporting after repository content, settings, and automation are in
// place, since these features act on the repository's actual content and workflows.
func (e *Engine) applyRemainingSecurity(ctx context.Context, repository githubapi.Repository, desired *config.SecurityConfig) error {
	if desired == nil {
		return nil
	}
	if dependabot, ok := e.service.(dependabotService); ok {
		if desired.DependabotAlerts != nil {
			if err := dependabot.SetVulnerabilityAlerts(ctx, repository, desired.DependabotAlerts.Enabled); err != nil {
				if !desired.DependabotAlerts.Optional {
					return fmt.Errorf("set Dependabot alerts for %s: %w", repository, err)
				}
			}
		}
		if desired.DependabotSecurityUpdates != nil {
			if err := dependabot.SetAutomatedSecurityFixes(ctx, repository, desired.DependabotSecurityUpdates.Enabled); err != nil {
				if !desired.DependabotSecurityUpdates.Optional {
					return fmt.Errorf("set Dependabot security updates for %s: %w", repository, err)
				}
			}
		}
	} else if (desired.DependabotAlerts != nil && !desired.DependabotAlerts.Optional) || (desired.DependabotSecurityUpdates != nil && !desired.DependabotSecurityUpdates.Optional) {
		return errors.New("dependabot operations are unavailable from the configured GitHub service")
	}
	if desired.CodeQLDefaultSetup != nil {
		state := "not-configured"
		if desired.CodeQLDefaultSetup.Enabled {
			state = "configured"
		}
		if err := e.service.ConfigureCodeQLDefaultSetup(ctx, repository, githubapi.CodeQLDefaultSetupRequest{State: state}); err != nil {
			if !desired.CodeQLDefaultSetup.Optional {
				return fmt.Errorf("configure CodeQL default setup for %s: %w", repository, err)
			}
		}
	}
	if desired.PrivateVulnerabilityReporting != nil {
		if err := e.service.SetPrivateVulnerabilityReporting(ctx, repository, desired.PrivateVulnerabilityReporting.Enabled); err != nil {
			if !desired.PrivateVulnerabilityReporting.Optional {
				return fmt.Errorf("set private vulnerability reporting for %s: %w", repository, err)
			}
		}
	}
	return nil
}

func securityFeature(feature *config.FeatureSetting) *githubapi.SecurityFeature {
	if feature == nil {
		return nil
	}
	status := "disabled"
	if feature.Enabled {
		status = "enabled"
	}
	return &githubapi.SecurityFeature{Status: status}
}

func (e *Engine) applyAgentVariables(ctx context.Context, repository githubapi.Repository, desired *config.AgentsConfig) error {
	if desired == nil || len(desired.Variables) == 0 {
		return nil
	}
	current, err := e.service.ListAgentVariables(ctx, repository)
	if err != nil {
		return fmt.Errorf("list agent variables for %s: %w", repository, err)
	}
	existing := make(map[string]string, len(current.Variables))
	for _, variable := range current.Variables {
		existing[variable.Name] = variable.Value
	}
	for _, variable := range desired.Variables {
		if actual, ok := existing[variable.Name]; ok {
			if actual == variable.Value {
				continue
			}
			if err := e.service.UpdateAgentVariable(ctx, repository, githubapi.AgentVariable{Name: variable.Name, Value: variable.Value}); err != nil {
				return fmt.Errorf("update agent variable %q: %w", variable.Name, err)
			}
			continue
		}
		if err := e.service.CreateAgentVariable(ctx, repository, githubapi.AgentVariable{Name: variable.Name, Value: variable.Value}); err != nil {
			return fmt.Errorf("create agent variable %q: %w", variable.Name, err)
		}
	}
	if len(desired.Secrets) > 0 {
		if err := e.applySecrets(ctx, repository, desired.Secrets, true); err != nil {
			return err
		}
	}
	return nil
}

func (e *Engine) applySecrets(ctx context.Context, repository githubapi.Repository, desired []config.SecretRef, agents bool) error {
	secrets, ok := e.service.(secretService)
	if !ok {
		return errors.New("secret operations are unavailable from the configured GitHub service")
	}
	var current githubapi.SecretList
	var err error
	if agents {
		current, err = secrets.ListAgentSecrets(ctx, repository)
	} else {
		current, err = secrets.ListActionsSecrets(ctx, repository)
	}
	if err != nil {
		return fmt.Errorf("list secrets for %s: %w", repository, err)
	}
	existing := make(map[string]bool, len(current.Secrets))
	for _, secret := range current.Secrets {
		existing[secret.Name] = true
	}
	var publicKey githubapi.PublicKey
	for _, secret := range desired {
		if existing[secret.Name] && defaultSecretPolicy(secret.UpdatePolicy) == "createOnly" {
			continue
		}
		value, exists := os.LookupEnv(secret.SourceEnv)
		if !exists {
			return fmt.Errorf("secret source environment variable is not set: %s", secret.SourceEnv)
		}
		if publicKey.KeyID == "" {
			if agents {
				publicKey, err = secrets.GetAgentSecretPublicKey(ctx, repository)
			} else {
				publicKey, err = secrets.GetActionsSecretPublicKey(ctx, repository)
			}
			if err != nil {
				return fmt.Errorf("get secret public key: %w", err)
			}
		}
		encrypted, err := githubapi.EncryptSecret(publicKey, []byte(value))
		if err != nil {
			return err
		}
		if agents {
			err = secrets.PutAgentSecret(ctx, repository, secret.Name, encrypted)
		} else {
			err = secrets.PutActionsSecret(ctx, repository, secret.Name, encrypted)
		}
		if err != nil {
			return fmt.Errorf("update secret %q: %w", secret.Name, err)
		}
	}
	return nil
}

func defaultSecretPolicy(policy string) string {
	if policy == "" {
		return "createOnly"
	}
	return policy
}

func (e *Engine) applyRulesets(ctx context.Context, repository githubapi.Repository, desired []config.RulesetConfig) error {
	if len(desired) == 0 {
		return nil
	}
	current, err := e.service.ListRulesets(ctx, repository)
	if err != nil {
		return fmt.Errorf("list rulesets for %s: %w", repository, err)
	}
	existing := make(map[string]githubapi.Ruleset, len(current))
	for _, ruleset := range current {
		existing[ruleset.Name] = ruleset
	}
	for _, ruleset := range desired {
		request := toRuleset(ruleset)
		if actual, ok := existing[ruleset.Name]; ok {
			if _, err := e.service.UpdateRuleset(ctx, repository, actual.ID, request); err != nil {
				return fmt.Errorf("update ruleset %q: %w", ruleset.Name, err)
			}
			continue
		}
		if _, err := e.service.CreateRuleset(ctx, repository, request); err != nil {
			return fmt.Errorf("create ruleset %q: %w", ruleset.Name, err)
		}
	}
	return nil
}

func toRuleset(ruleset config.RulesetConfig) githubapi.Ruleset {
	rules := make([]map[string]any, 0, len(ruleset.Rules))
	for _, rule := range ruleset.Rules {
		entry := map[string]any{"type": rule.Type}
		if len(rule.Parameters) > 0 {
			entry["parameters"] = rule.Parameters
		}
		rules = append(rules, entry)
	}
	include := slices.Clone(ruleset.Include)
	exclude := slices.Clone(ruleset.Exclude)
	return githubapi.Ruleset{
		Name:        ruleset.Name,
		Target:      ruleset.Target,
		Enforcement: ruleset.Enforcement,
		Conditions: map[string]any{
			"ref_name": map[string]any{"include": include, "exclude": exclude},
		},
		Rules: rules,
	}
}

func finalize(plan Plan) Plan {
	sort.SliceStable(plan.Changes, func(i, j int) bool {
		return stageRank(plan.Changes[i].Stage) < stageRank(plan.Changes[j].Stage)
	})
	return plan
}

func stageRank(stage Stage) int {
	switch stage {
	case StageRepository:
		return 0
	case StageSecurityPre:
		return 1
	case StageFiles:
		return 2
	case StageSettings:
		return 3
	case StageAutomation:
		return 4
	case StageSecurity:
		return 5
	case StageRulesets:
		return 6
	default:
		return 7
	}
}

func resolveRepository(repository config.RepositoryConfig, override string) (githubapi.Repository, error) {
	if override == "" {
		return githubapi.Repository{Owner: repository.Owner, Name: repository.Name}, nil
	}
	parts := strings.Split(override, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" || strings.Contains(parts[0], " ") || strings.Contains(parts[1], " ") {
		return githubapi.Repository{}, fmt.Errorf("repository override must be owner/name, got %q", override)
	}
	return githubapi.Repository{Owner: parts[0], Name: parts[1]}, nil
}

func appendRepositoryChanges(plan *Plan, desired config.RepositoryConfig, actual githubapi.RepositoryInfo) {
	if desired.Visibility != actual.Visibility && (desired.Visibility != "private" || !actual.Private) {
		if desired.AllowVisibilityChange {
			plan.Changes = append(plan.Changes, Change{Stage: StageRepository, Resource: "repository.visibility", State: ChangeUpdate, Summary: desired.Visibility})
		} else {
			plan.Changes = append(plan.Changes, Change{Stage: StageRepository, Resource: "repository.visibility", State: ChangeBlocked, Summary: "allowVisibilityChange is required"})
		}
	}
	if desired.Description != actual.Description {
		plan.Changes = append(plan.Changes, Change{Stage: StageSettings, Resource: "repository.description", State: ChangeUpdate, Summary: "update description"})
	}
	if desired.DefaultBranch != "" && desired.DefaultBranch != actual.DefaultBranch {
		plan.Changes = append(plan.Changes, Change{Stage: StageSettings, Resource: "repository.defaultBranch", State: ChangeUpdate, Summary: desired.DefaultBranch})
	}
	appendBoolChange(plan, "repository.allowMergeCommit", desired.AllowMergeCommit, actual.AllowMergeCommit)
	appendBoolChange(plan, "repository.allowSquashMerge", desired.AllowSquashMerge, actual.AllowSquashMerge)
	appendBoolChange(plan, "repository.allowRebaseMerge", desired.AllowRebaseMerge, actual.AllowRebaseMerge)
	appendBoolChange(plan, "repository.allowAutoMerge", desired.AllowAutoMerge, actual.AllowAutoMerge)
	appendBoolChange(plan, "repository.deleteBranchOnMerge", desired.DeleteBranchOnMerge, actual.DeleteBranchOnMerge)
	if desired.Topics != nil && !sameStrings(desired.Topics, actual.Topics) {
		plan.Changes = append(plan.Changes, Change{Stage: StageSettings, Resource: "repository.topics", State: ChangeUpdate, Summary: "replace declared topics"})
	}
}

func appendBoolChange(plan *Plan, resource string, desired *bool, actual bool) {
	if desired != nil && *desired != actual {
		plan.Changes = append(plan.Changes, Change{Stage: StageSettings, Resource: resource, State: ChangeUpdate, Summary: fmt.Sprintf("set to %t", *desired)})
	}
}

func appendActionsChanges(ctx context.Context, service Service, plan *Plan, desired *config.ActionsConfig, repository githubapi.Repository) {
	if desired == nil {
		return
	}
	actual, err := service.GetActionsPermissions(ctx, repository)
	if err != nil {
		plan.Changes = append(plan.Changes, Change{Stage: StageSettings, Resource: "actions", State: ChangeBlocked, Summary: capabilityError(err)})
		return
	}
	if desired.Enabled != nil && *desired.Enabled != actual.Enabled || desired.AllowedActions != "" && desired.AllowedActions != actual.AllowedActions || desired.RequireSHAForGitHubActions != nil && *desired.RequireSHAForGitHubActions != actual.SHAPinningRequired {
		plan.Changes = append(plan.Changes, Change{Stage: StageSettings, Resource: "actions.permissions", State: ChangeUpdate, Summary: "update Actions permissions"})
	}
	if desired.DefaultWorkflowPermissions != "" || desired.CanApprovePullRequestWorkflows != nil {
		plan.Changes = append(plan.Changes, Change{Stage: StageSettings, Resource: "actions.workflowPermissions", State: ChangeUpdate, Summary: "update GITHUB_TOKEN permissions"})
	}
	if desired.AllowedActions == "selected" && desired.SelectedActions != nil {
		plan.Changes = append(plan.Changes, Change{Stage: StageSettings, Resource: "actions.selectedActions", State: ChangeUpdate, Summary: "configure allowed Actions and reusable workflows"})
	}
	variables, err := service.ListActionVariables(ctx, repository)
	if err != nil {
		if len(desired.Variables) > 0 {
			plan.Changes = append(plan.Changes, Change{Stage: StageAutomation, Resource: "actions.variables", State: ChangeBlocked, Summary: capabilityError(err)})
		}
	} else {
		byName := make(map[string]string, len(variables.Variables))
		for _, variable := range variables.Variables {
			byName[variable.Name] = variable.Value
		}
		for _, variable := range desired.Variables {
			state := ChangeCreate
			if current, ok := byName[variable.Name]; ok {
				if current == variable.Value {
					state = ChangeNoop
				} else {
					state = ChangeUpdate
				}
			}
			plan.Changes = append(plan.Changes, Change{Stage: StageAutomation, Resource: "actions.variable." + variable.Name, State: state, Summary: "converge Actions variable"})
		}
	}
	for _, secret := range desired.Secrets {
		appendSecretChange(plan, "actions.secret."+secret.Name, secret)
	}
	for _, environment := range desired.Environments {
		plan.Changes = append(plan.Changes, Change{Stage: StageAutomation, Resource: "actions.environment." + environment.Name, State: ChangeUpdate, Summary: "ensure Actions environment"})
	}
}

func appendSecurityChanges(plan *Plan, desired *config.SecurityConfig) {
	if desired == nil {
		return
	}
	appendFeatureChange(plan, StageSecurityPre, "security.secretScanning", desired.SecretScanning, false)
	appendFeatureChange(plan, StageSecurityPre, "security.pushProtection", desired.PushProtection, false)
	// GitHub enables the dependency graph as part of enabling Dependabot alerts.
	dependencyGraphUnavailable := desired.DependabotAlerts == nil || !desired.DependabotAlerts.Enabled
	appendFeatureChange(plan, StageSecurity, "security.dependencyGraph", desired.DependencyGraph, dependencyGraphUnavailable)
	appendFeatureChange(plan, StageSecurity, "security.dependabotAlerts", desired.DependabotAlerts, false)
	appendFeatureChange(plan, StageSecurity, "security.dependabotSecurityUpdates", desired.DependabotSecurityUpdates, false)
	appendFeatureChange(plan, StageSecurity, "security.codeqlDefaultSetup", desired.CodeQLDefaultSetup, false)
	appendFeatureChange(plan, StageSecurity, "security.privateVulnerabilityReporting", desired.PrivateVulnerabilityReporting, false)
}

func appendFeatureChange(plan *Plan, stage Stage, resource string, feature *config.FeatureSetting, unavailable bool) {
	if feature == nil {
		return
	}
	if unavailable {
		state := ChangeBlocked
		if feature.Optional {
			state = ChangeManual
		}
		plan.Changes = append(plan.Changes, Change{Stage: stage, Resource: resource, State: state, Summary: "repository API support is unavailable"})
		return
	}
	plan.Changes = append(plan.Changes, Change{Stage: stage, Resource: resource, State: ChangeUpdate, Summary: fmt.Sprintf("set to %t", feature.Enabled)})
}

func appendRulesetChanges(ctx context.Context, service Service, plan *Plan, desired []config.RulesetConfig, repository githubapi.Repository) {
	if len(desired) == 0 {
		return
	}
	actual, err := service.ListRulesets(ctx, repository)
	if err != nil {
		plan.Changes = append(plan.Changes, Change{Stage: StageRulesets, Resource: "rulesets", State: ChangeBlocked, Summary: capabilityError(err)})
		return
	}
	byName := make(map[string]githubapi.Ruleset, len(actual))
	for _, ruleset := range actual {
		byName[ruleset.Name] = ruleset
	}
	for _, ruleset := range desired {
		state := ChangeCreate
		if _, ok := byName[ruleset.Name]; ok {
			state = ChangeUpdate
		}
		plan.Changes = append(plan.Changes, Change{Stage: StageRulesets, Resource: "ruleset." + ruleset.Name, State: state, Summary: "converge ruleset"})
	}
}

func appendAgentChanges(ctx context.Context, service Service, plan *Plan, desired *config.AgentsConfig, repository githubapi.Repository) {
	if desired == nil {
		return
	}
	actual, err := service.ListAgentVariables(ctx, repository)
	if err != nil {
		if len(desired.Variables) > 0 {
			plan.Changes = append(plan.Changes, Change{Stage: StageAutomation, Resource: "agents.variables", State: ChangeBlocked, Summary: capabilityError(err)})
		}
	} else {
		byName := make(map[string]string, len(actual.Variables))
		for _, variable := range actual.Variables {
			byName[variable.Name] = variable.Value
		}
		for _, variable := range desired.Variables {
			state := ChangeCreate
			if value, ok := byName[variable.Name]; ok {
				if value == variable.Value {
					state = ChangeNoop
				} else {
					state = ChangeUpdate
				}
			}
			plan.Changes = append(plan.Changes, Change{Stage: StageAutomation, Resource: "agents.variable." + variable.Name, State: state, Summary: "converge agent variable"})
		}
	}
	for _, secret := range desired.Secrets {
		appendSecretChange(plan, "agents.secret."+secret.Name, secret)
	}
	for _, path := range []string{desired.MCPConfigPath, desired.InstructionsPath, desired.CustomAgentsPath, desired.SkillsPath} {
		if path != "" {
			plan.Changes = append(plan.Changes, Change{Stage: StageFiles, Resource: "agents.file." + path, State: ChangeManual, Summary: "agent files are seeded repository content"})
		}
	}
}

func appendSecretChange(plan *Plan, resource string, secret config.SecretRef) {
	if _, ok := os.LookupEnv(secret.SourceEnv); !ok {
		plan.Changes = append(plan.Changes, Change{Stage: StageAutomation, Resource: resource, State: ChangeBlocked, Summary: "source environment variable is not set: " + secret.SourceEnv})
		return
	}
	plan.Changes = append(plan.Changes, Change{Stage: StageAutomation, Resource: resource, State: ChangeUpdate, Summary: "converge encrypted secret"})
}

func appendSeedChanges(plan *Plan, cfg *config.Config) {
	if cfg.Seed != nil {
		plan.Changes = append(plan.Changes, Change{Stage: StageFiles, Resource: "seed", State: ChangeManual, Summary: "seed content will be staged before upload"})
	}
	for _, command := range cfg.Commands {
		plan.Changes = append(plan.Changes, Change{Stage: StageFiles, Resource: "command." + command.ID, State: ChangeManual, Summary: "requires --approve-command " + command.ID})
	}
}

func appendDeferredChanges(plan *Plan, cfg *config.Config) {
	if cfg.Seed != nil || len(cfg.Commands) > 0 {
		appendSeedChanges(plan, cfg)
	}
	if cfg.Actions != nil {
		plan.Changes = append(plan.Changes, Change{Stage: StageSettings, Resource: "actions", State: ChangeUpdate, Summary: "configure after repository creation"})
	}
	appendSecurityChanges(plan, cfg.Security)
	for _, ruleset := range cfg.Rulesets {
		plan.Changes = append(plan.Changes, Change{Stage: StageRulesets, Resource: "ruleset." + ruleset.Name, State: ChangeCreate, Summary: "create after repository initialization"})
	}
	if cfg.Agents != nil {
		for _, variable := range cfg.Agents.Variables {
			plan.Changes = append(plan.Changes, Change{Stage: StageAutomation, Resource: "agents.variable." + variable.Name, State: ChangeCreate, Summary: "create after repository creation"})
		}
		for _, secret := range cfg.Agents.Secrets {
			appendSecretChange(plan, "agents.secret."+secret.Name, secret)
		}
	}
}

func capabilityError(err error) string {
	var apiError *githubapi.APIError
	if errors.As(err, &apiError) {
		return fmt.Sprintf("GitHub capability unavailable (HTTP %d)", apiError.StatusCode)
	}
	return "GitHub capability check failed"
}

func sameStrings(left, right []string) bool {
	leftCopy := slices.Clone(left)
	rightCopy := slices.Clone(right)
	sort.Strings(leftCopy)
	sort.Strings(rightCopy)
	return slices.Equal(leftCopy, rightCopy)
}
