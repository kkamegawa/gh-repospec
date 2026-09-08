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

	"github.com/kkamegawa/gh-repospec/internal/config"
	"github.com/kkamegawa/gh-repospec/internal/githubapi"
	"github.com/kkamegawa/gh-repospec/internal/seed"
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

// Stage fixes the application order. Rulesets remain last among the mutable
// settings so they cannot prevent repository initialization. Security settings
// are split in two: StageSecurityPre (secret scanning, push protection) runs
// before files so the initial commit is protected; StageSecurity (dependency
// graph, Dependabot, CodeQL default setup, private vulnerability reporting)
// runs after settings and automation, once there is repository content for
// those features to act on. StageArchive is last of all: archiving a
// repository makes it read-only, so every other stage must already be applied.
// Unarchiving happens in StageRepository instead, before anything else, since
// GitHub rejects writes to an archived repository.
type Stage string

const (
	StageRepository  Stage = "repository"
	StageSecurityPre Stage = "security-pre"
	StageFiles       Stage = "files"
	StageSettings    Stage = "settings"
	StageAutomation  Stage = "automation"
	StageSecurity    Stage = "security"
	StageRulesets    Stage = "rulesets"
	StageArchive     Stage = "archive"
)

// Change is one visible capability or resource difference.
type Change struct {
	Stage    Stage       `json:"stage"`
	Resource string      `json:"resource"`
	State    ChangeState `json:"state"`
	Summary  string      `json:"summary"`
	// Opaque marks a resource whose live value GitHub does not return, so the
	// change is reported unconditionally and can never converge. Drift
	// detection ignores these. Secrets are the only such resource today.
	Opaque bool `json:"opaque,omitempty"`
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

// HasDrift reports whether the plan contains a difference that could
// converge. Secret updates are excluded: GitHub never returns a secret
// value, so a declared secret whose source environment variable is set is
// always reported as an update and can never converge. See docs/adr.md
// records 4 and 6.
func (p Plan) HasDrift() bool {
	return p.DriftCount() > 0
}

// DriftCount reports how many changes HasDrift considers. It backs plan
// --ci's drift notice, which reports how many differences were found.
func (p Plan) DriftCount() int {
	count := 0
	for _, change := range p.Changes {
		if !change.Opaque && (change.State == ChangeCreate || change.State == ChangeUpdate) {
			count++
		}
	}
	return count
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
	GetSelectedActions(context.Context, githubapi.Repository) (githubapi.SelectedActions, error)
	UpdateSelectedActions(context.Context, githubapi.Repository, githubapi.SelectedActionsRequest) error
	GetDefaultWorkflowPermissions(context.Context, githubapi.Repository) (githubapi.DefaultWorkflowPermissions, error)
	UpdateDefaultWorkflowPermissions(context.Context, githubapi.Repository, githubapi.DefaultWorkflowPermissions) error
	ListEnvironments(context.Context, githubapi.Repository) ([]githubapi.Environment, error)
	GetForkPullRequestApproval(context.Context, githubapi.Repository) (githubapi.ForkPullRequestApproval, error)
	SetForkPullRequestApproval(context.Context, githubapi.Repository, githubapi.ForkPullRequestApproval) error
	ListActionVariables(context.Context, githubapi.Repository) (githubapi.ActionVariableList, error)
	CreateActionVariable(context.Context, githubapi.Repository, githubapi.ActionVariable) error
	UpdateActionVariable(context.Context, githubapi.Repository, githubapi.ActionVariable) error
	CreateOrUpdateEnvironment(context.Context, githubapi.Repository, string, githubapi.EnvironmentRequest) (githubapi.Environment, error)
	ListRulesets(context.Context, githubapi.Repository) ([]githubapi.Ruleset, error)
	CreateRuleset(context.Context, githubapi.Repository, githubapi.Ruleset) (githubapi.Ruleset, error)
	UpdateRuleset(context.Context, githubapi.Repository, int64, githubapi.Ruleset) (githubapi.Ruleset, error)
	// GetTeam, GetApp, and ListCustomRepositoryRoles resolve ruleset bypass
	// actor selectors to the numeric IDs the rulesets API requires.
	GetTeam(context.Context, string, string) (githubapi.Team, error)
	GetApp(context.Context, string) (githubapi.App, error)
	ListCustomRepositoryRoles(context.Context, string) ([]githubapi.CustomRepositoryRole, error)
	UpdateSecurityAndAnalysis(context.Context, githubapi.Repository, githubapi.SecurityAndAnalysisRequest) (githubapi.RepositoryInfo, error)
	GetCodeQLDefaultSetup(context.Context, githubapi.Repository) (githubapi.CodeQLDefaultSetupStatus, error)
	ConfigureCodeQLDefaultSetup(context.Context, githubapi.Repository, githubapi.CodeQLDefaultSetupRequest) error
	GetPrivateVulnerabilityReportingEnabled(context.Context, githubapi.Repository) (bool, error)
	SetPrivateVulnerabilityReporting(context.Context, githubapi.Repository, bool) error
	GetVulnerabilityAlertsEnabled(context.Context, githubapi.Repository) (bool, error)
	SetVulnerabilityAlerts(context.Context, githubapi.Repository, bool) error
	GetAutomatedSecurityFixesEnabled(context.Context, githubapi.Repository) (bool, error)
	SetAutomatedSecurityFixes(context.Context, githubapi.Repository, bool) error
	ListLabels(context.Context, githubapi.Repository) ([]githubapi.Label, error)
	CreateLabel(context.Context, githubapi.Repository, githubapi.Label) (githubapi.Label, error)
	UpdateLabel(context.Context, githubapi.Repository, string, githubapi.Label) (githubapi.Label, error)
	ListMilestones(context.Context, githubapi.Repository) ([]githubapi.Milestone, error)
	CreateMilestone(context.Context, githubapi.Repository, githubapi.Milestone) (githubapi.Milestone, error)
	UpdateMilestone(context.Context, githubapi.Repository, int, githubapi.Milestone) (githubapi.Milestone, error)
	GetImmutableReleases(context.Context, githubapi.Repository) (githubapi.ImmutableReleases, error)
	SetImmutableReleases(context.Context, githubapi.Repository, bool) error
	ListAgentVariables(context.Context, githubapi.Repository) (githubapi.AgentVariableList, error)
	CreateAgentVariable(context.Context, githubapi.Repository, githubapi.AgentVariable) error
	UpdateAgentVariable(context.Context, githubapi.Repository, githubapi.AgentVariable) error
}

// Options controls planning and application without embedding CLI concerns.
type Options struct {
	RepositoryOverride string
	ApprovedCommandIDs map[string]bool
	// ForceSecrets treats every configured secret as updatePolicy: always for
	// this run, so a credential rotation is applied without editing the
	// file. It does not relax the source environment variable requirement.
	ForceSecrets bool
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
	plan, _, err := e.build(ctx, cfg, options)
	return plan, err
}

// build is Build's implementation. It also returns the bypass actor resolver
// used while planning, so Apply can reuse the identities it already resolved
// instead of looking them up a second time.
func (e *Engine) build(ctx context.Context, cfg *config.Config, options Options) (Plan, *bypassResolver, error) {
	if e == nil || e.service == nil {
		return Plan{}, nil, errors.New("planner is not initialized")
	}
	if err := config.Validate(cfg); err != nil {
		return Plan{}, nil, fmt.Errorf("validate configuration: %w", err)
	}
	repository, err := resolveRepository(cfg.Repository, options.RepositoryOverride)
	if err != nil {
		return Plan{}, nil, err
	}
	plan := Plan{Repository: repository}
	info, err := e.service.GetRepository(ctx, repository)
	if err != nil {
		if !githubapi.IsNotFound(err) {
			return Plan{}, nil, fmt.Errorf("get repository %s: %w", repository, err)
		}
		if !cfg.Repository.Create {
			return Plan{}, nil, fmt.Errorf("repository %s does not exist and repository.create is false", repository)
		}
		resolver := newRulesetBypassResolver(ctx, e.service, repository, githubapi.RepositoryInfo{}, false, cfg.Rulesets)
		plan.Changes = append(plan.Changes, Change{Stage: StageRepository, Resource: "repository", State: ChangeCreate, Summary: "create repository"})
		appendDeferredChanges(ctx, e.service, &plan, cfg, repository, resolver)
		return finalize(plan), resolver, nil
	}
	plan.Exists = true
	resolver := newRulesetBypassResolver(ctx, e.service, repository, info, true, cfg.Rulesets)
	appendRepositoryChanges(&plan, cfg.Repository, info)
	appendReleaseImmutabilityChanges(ctx, e.service, &plan, cfg.Repository, repository)
	appendLabelChanges(ctx, e.service, &plan, cfg.Labels, repository)
	appendMilestoneChanges(ctx, e.service, &plan, cfg.Milestones, repository)
	appendActionsChanges(ctx, e.service, &plan, cfg.Actions, repository, cfg.Repository)
	appendRulesetChanges(ctx, e.service, &plan, cfg.Rulesets, repository, resolver)
	appendSecurityChanges(ctx, e.service, &plan, cfg.Security, repository, info, true)
	appendAgentChanges(ctx, e.service, &plan, cfg.Agents, repository)
	appendSeedChanges(&plan, cfg)
	return finalize(plan), resolver, nil
}

// Apply recalculates the plan before making changes, then performs supported
// changes in stage order. Unsupported manual work remains visible in the
// returned plan. A partial result can be safely reconciled by running apply
// again after resolving the reported error.
func (e *Engine) Apply(ctx context.Context, cfg *config.Config, options Options) (Plan, error) {
	plan, resolver, err := e.build(ctx, cfg, options)
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
	// Unarchiving runs first, before any other write, because GitHub rejects
	// writes to an archived repository.
	if !created && cfg.Repository.Archived != nil && !*cfg.Repository.Archived && info.Archived {
		unarchived := false
		if _, err := e.service.UpdateRepository(ctx, repository, githubapi.UpdateRepositoryRequest{Archived: &unarchived}); err != nil {
			return plan, fmt.Errorf("unarchive repository %s: %w", repository, err)
		}
		info.Archived = false
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
	if err := e.applyReleaseImmutability(ctx, repository, cfg.Repository); err != nil {
		return plan, err
	}
	if err := e.applyPreContentSecurity(ctx, repository, cfg.Security); err != nil {
		return plan, err
	}
	if err := e.applySeed(ctx, repository, info, cfg, options, !created); err != nil {
		return plan, err
	}
	if err := e.applyLabels(ctx, repository, cfg.Labels); err != nil {
		return plan, err
	}
	if err := e.applyMilestones(ctx, repository, cfg.Milestones); err != nil {
		return plan, err
	}
	if err := e.applyActions(ctx, repository, cfg.Actions, options); err != nil {
		return plan, err
	}
	if err := e.applyAgentVariables(ctx, repository, cfg.Agents, options); err != nil {
		return plan, err
	}
	if err := e.applyRemainingSecurity(ctx, repository, cfg.Security); err != nil {
		return plan, err
	}
	if err := e.applyRulesets(ctx, repository, cfg.Rulesets, resolver); err != nil {
		return plan, err
	}
	// Archiving runs last of all, after every other change, because it makes
	// the repository read-only.
	if cfg.Repository.Archived != nil && *cfg.Repository.Archived && !info.Archived {
		archived := true
		if _, err := e.service.UpdateRepository(ctx, repository, githubapi.UpdateRepositoryRequest{Archived: &archived}); err != nil {
			return plan, fmt.Errorf("archive repository %s: %w", repository, err)
		}
	}
	return plan, nil
}

func createRequest(desired config.RepositoryConfig, repository githubapi.Repository) githubapi.CreateRepositoryRequest {
	private := desired.Visibility == "private"
	var description string
	if desired.Description != nil {
		description = *desired.Description
	}
	return githubapi.CreateRepositoryRequest{
		Owner:               repository.Owner,
		Name:                repository.Name,
		Description:         description,
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
	if desired.Description != nil && *desired.Description != actual.Description {
		request.Description = desired.Description
		changed = true
	}
	if desired.Homepage != nil && *desired.Homepage != actual.Homepage {
		request.Homepage = desired.Homepage
		changed = true
	}
	if desired.DefaultBranch != "" && desired.DefaultBranch != actual.DefaultBranch {
		request.DefaultBranch = desired.DefaultBranch
		changed = true
	}
	if desired.Features != nil {
		if desired.Features.Issues != nil && *desired.Features.Issues != actual.HasIssues {
			request.HasIssues = desired.Features.Issues
			changed = true
		}
		if desired.Features.Projects != nil && *desired.Features.Projects != actual.HasProjects {
			request.HasProjects = desired.Features.Projects
			changed = true
		}
		if desired.Features.Wiki != nil && *desired.Features.Wiki != actual.HasWiki {
			request.HasWiki = desired.Features.Wiki
			changed = true
		}
		if desired.Features.Discussions != nil && *desired.Features.Discussions != actual.HasDiscussions {
			request.HasDiscussions = desired.Features.Discussions
			changed = true
		}
	}
	if desired.MergeCommitTitle != "" && desired.MergeCommitTitle != actual.MergeCommitTitle {
		request.MergeCommitTitle = desired.MergeCommitTitle
		changed = true
	}
	if desired.MergeCommitMessage != "" && desired.MergeCommitMessage != actual.MergeCommitMessage {
		request.MergeCommitMessage = desired.MergeCommitMessage
		changed = true
	}
	if desired.SquashMergeCommitTitle != "" && desired.SquashMergeCommitTitle != actual.SquashMergeCommitTitle {
		request.SquashMergeCommitTitle = desired.SquashMergeCommitTitle
		changed = true
	}
	if desired.SquashMergeCommitMessage != "" && desired.SquashMergeCommitMessage != actual.SquashMergeCommitMessage {
		request.SquashMergeCommitMessage = desired.SquashMergeCommitMessage
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

// applyReleaseImmutability converges the repository's immutable release
// setting through its dedicated endpoint; the repository update call does not
// accept it.
func (e *Engine) applyReleaseImmutability(ctx context.Context, repository githubapi.Repository, desired config.RepositoryConfig) error {
	if desired.ReleaseImmutability == nil {
		return nil
	}
	actual, err := e.service.GetImmutableReleases(ctx, repository)
	if err != nil {
		return fmt.Errorf("get immutable releases for %s: %w", repository, err)
	}
	if actual.Enabled == *desired.ReleaseImmutability {
		return nil
	}
	if err := e.service.SetImmutableReleases(ctx, repository, *desired.ReleaseImmutability); err != nil {
		return fmt.Errorf("set immutable releases for %s: %w", repository, err)
	}
	return nil
}

// applyLabels creates or updates every declared label. Labels present on the
// repository but absent from configuration are left untouched.
func (e *Engine) applyLabels(ctx context.Context, repository githubapi.Repository, desired []config.LabelConfig) error {
	if len(desired) == 0 {
		return nil
	}
	actual, err := e.service.ListLabels(ctx, repository)
	if err != nil {
		return fmt.Errorf("list labels for %s: %w", repository, err)
	}
	byName := make(map[string]githubapi.Label, len(actual))
	for _, label := range actual {
		byName[strings.ToLower(label.Name)] = label
	}
	for _, label := range desired {
		request := githubapi.Label{Name: label.Name, Color: strings.ToLower(label.Color), Description: label.Description}
		if current, ok := byName[strings.ToLower(label.Name)]; ok {
			if strings.EqualFold(current.Color, label.Color) && current.Description == label.Description {
				continue
			}
			if _, err := e.service.UpdateLabel(ctx, repository, current.Name, request); err != nil {
				return fmt.Errorf("update label %q for %s: %w", label.Name, repository, err)
			}
			continue
		}
		if _, err := e.service.CreateLabel(ctx, repository, request); err != nil {
			return fmt.Errorf("create label %q for %s: %w", label.Name, repository, err)
		}
	}
	return nil
}

// applyMilestones creates or updates every declared milestone, matched by
// title. Milestones present on the repository but absent from configuration
// are left untouched; a milestone is retired with state: closed.
func (e *Engine) applyMilestones(ctx context.Context, repository githubapi.Repository, desired []config.MilestoneConfig) error {
	if len(desired) == 0 {
		return nil
	}
	actual, err := e.service.ListMilestones(ctx, repository)
	if err != nil {
		return fmt.Errorf("list milestones for %s: %w", repository, err)
	}
	byTitle := make(map[string]githubapi.Milestone, len(actual))
	for _, milestone := range actual {
		byTitle[milestone.Title] = milestone
	}
	for _, milestone := range desired {
		state := milestone.State
		if state == "" {
			state = "open"
		}
		request := githubapi.Milestone{
			Title:       milestone.Title,
			Description: milestone.Description,
			State:       state,
			DueOn:       milestoneDueOnWire(milestone.DueOn),
		}
		if current, ok := byTitle[milestone.Title]; ok {
			if milestoneConverged(state, milestone, current) {
				continue
			}
			if _, err := e.service.UpdateMilestone(ctx, repository, current.Number, request); err != nil {
				return fmt.Errorf("update milestone %q for %s: %w", milestone.Title, repository, err)
			}
			continue
		}
		if _, err := e.service.CreateMilestone(ctx, repository, request); err != nil {
			return fmt.Errorf("create milestone %q for %s: %w", milestone.Title, repository, err)
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

func (e *Engine) applyActions(ctx context.Context, repository githubapi.Repository, desired *config.ActionsConfig, options Options) error {
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
	if desired.ForkPullRequestApproval != "" {
		// Build already blocks this field on a private repository, because
		// GitHub rejects the endpoint outright (HTTP 422) in that case.
		request := githubapi.ForkPullRequestApproval{ApprovalPolicy: desired.ForkPullRequestApproval}
		if err := e.service.SetForkPullRequestApproval(ctx, repository, request); err != nil {
			return fmt.Errorf("set fork pull request approval for %s: %w", repository, err)
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
	if len(desired.Secrets) > 0 {
		if err := e.applySecrets(ctx, repository, desired.Secrets, secretTargetActions, options); err != nil {
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
	if desired.DependabotAlerts != nil {
		if err := e.service.SetVulnerabilityAlerts(ctx, repository, desired.DependabotAlerts.Enabled); err != nil {
			if !desired.DependabotAlerts.Optional {
				return fmt.Errorf("set Dependabot alerts for %s: %w", repository, err)
			}
		}
	}
	if desired.DependabotSecurityUpdates != nil {
		// Build already blocks this combination when the dependency is not
		// satisfied; re-checking here means a stale plan can never reach a
		// write GitHub is known to reject.
		alertsEnabled, alertsKnown := effectiveDependabotAlertsEnabled(ctx, e.service, repository, true, desired)
		if !alertsKnown || !alertsEnabled {
			if !desired.DependabotSecurityUpdates.Optional {
				return fmt.Errorf("set Dependabot security updates for %s: requires security.dependabotAlerts to be enabled", repository)
			}
		} else if err := e.service.SetAutomatedSecurityFixes(ctx, repository, desired.DependabotSecurityUpdates.Enabled); err != nil {
			if !desired.DependabotSecurityUpdates.Optional {
				return fmt.Errorf("set Dependabot security updates for %s: %w", repository, err)
			}
		}
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

func (e *Engine) applyAgentVariables(ctx context.Context, repository githubapi.Repository, desired *config.AgentsConfig, options Options) error {
	if desired == nil {
		return nil
	}
	if len(desired.Variables) > 0 {
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
	}
	if len(desired.Secrets) > 0 {
		if err := e.applySecrets(ctx, repository, desired.Secrets, secretTargetAgents, options); err != nil {
			return err
		}
	}
	return nil
}

// secretTarget selects which secret store applySecrets operates on. A named
// type reads at the call site (applySecrets(..., secretTargetAgents, ...))
// where a second bare bool would not.
type secretTarget string

const (
	secretTargetActions secretTarget = "actions"
	secretTargetAgents  secretTarget = "agents"
)

func (e *Engine) applySecrets(ctx context.Context, repository githubapi.Repository, desired []config.SecretRef, target secretTarget, options Options) error {
	secrets, ok := e.service.(secretService)
	if !ok {
		return errors.New("secret operations are unavailable from the configured GitHub service")
	}
	var current githubapi.SecretList
	var err error
	if target == secretTargetAgents {
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
		if !options.ForceSecrets && existing[secret.Name] && defaultSecretPolicy(secret.UpdatePolicy) == "createOnly" {
			continue
		}
		value, exists := os.LookupEnv(secret.SourceEnv)
		if !exists {
			return fmt.Errorf("secret source environment variable is not set: %s", secret.SourceEnv)
		}
		if publicKey.KeyID == "" {
			if target == secretTargetAgents {
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
		if target == secretTargetAgents {
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

func (e *Engine) applyRulesets(ctx context.Context, repository githubapi.Repository, desired []config.RulesetConfig, resolver *bypassResolver) error {
	if len(desired) == 0 {
		return nil
	}
	// build passes the resolver used during planning so identities resolved
	// there are not looked up a second time; a caller that exercises apply
	// directly (as some tests do) still gets correct, if unmemoized, behavior.
	if resolver == nil {
		resolver = newBypassResolver(e.service, repository.Owner, false)
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
		bypassActors, failures := resolver.resolveAll(ctx, ruleset.BypassActors)
		if len(failures) > 0 {
			// Build already blocks any unresolvable bypass actor; this is a
			// last-resort guard against writing a ruleset with an incomplete
			// bypass list if that invariant is somehow violated.
			return fmt.Errorf("resolve bypass actors for ruleset %q: %w", ruleset.Name, failures[0].Err)
		}
		request := toRuleset(ruleset, bypassActors)
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

func toRuleset(ruleset config.RulesetConfig, bypassActors []githubapi.BypassActor) githubapi.Ruleset {
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
		Rules:        rules,
		BypassActors: bypassActors,
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
	case StageArchive:
		return 7
	default:
		return 8
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
	if desired.Description != nil && *desired.Description != actual.Description {
		plan.Changes = append(plan.Changes, Change{Stage: StageSettings, Resource: "repository.description", State: ChangeUpdate, Summary: "update description"})
	}
	if desired.Homepage != nil && *desired.Homepage != actual.Homepage {
		plan.Changes = append(plan.Changes, Change{Stage: StageSettings, Resource: "repository.homepage", State: ChangeUpdate, Summary: "update homepage"})
	}
	if desired.DefaultBranch != "" && desired.DefaultBranch != actual.DefaultBranch {
		plan.Changes = append(plan.Changes, Change{Stage: StageSettings, Resource: "repository.defaultBranch", State: ChangeUpdate, Summary: desired.DefaultBranch})
	}
	if desired.Features != nil {
		appendBoolChange(plan, "repository.features.issues", desired.Features.Issues, actual.HasIssues)
		appendBoolChange(plan, "repository.features.projects", desired.Features.Projects, actual.HasProjects)
		appendBoolChange(plan, "repository.features.wiki", desired.Features.Wiki, actual.HasWiki)
		appendBoolChange(plan, "repository.features.discussions", desired.Features.Discussions, actual.HasDiscussions)
	}
	appendBoolChange(plan, "repository.allowMergeCommit", desired.AllowMergeCommit, actual.AllowMergeCommit)
	appendBoolChange(plan, "repository.allowSquashMerge", desired.AllowSquashMerge, actual.AllowSquashMerge)
	appendBoolChange(plan, "repository.allowRebaseMerge", desired.AllowRebaseMerge, actual.AllowRebaseMerge)
	appendBoolChange(plan, "repository.allowAutoMerge", desired.AllowAutoMerge, actual.AllowAutoMerge)
	appendBoolChange(plan, "repository.deleteBranchOnMerge", desired.DeleteBranchOnMerge, actual.DeleteBranchOnMerge)
	if desired.MergeCommitTitle != "" && desired.MergeCommitTitle != actual.MergeCommitTitle {
		plan.Changes = append(plan.Changes, Change{Stage: StageSettings, Resource: "repository.mergeCommitTitle", State: ChangeUpdate, Summary: desired.MergeCommitTitle})
	}
	if desired.MergeCommitMessage != "" && desired.MergeCommitMessage != actual.MergeCommitMessage {
		plan.Changes = append(plan.Changes, Change{Stage: StageSettings, Resource: "repository.mergeCommitMessage", State: ChangeUpdate, Summary: desired.MergeCommitMessage})
	}
	if desired.SquashMergeCommitTitle != "" && desired.SquashMergeCommitTitle != actual.SquashMergeCommitTitle {
		plan.Changes = append(plan.Changes, Change{Stage: StageSettings, Resource: "repository.squashMergeCommitTitle", State: ChangeUpdate, Summary: desired.SquashMergeCommitTitle})
	}
	if desired.SquashMergeCommitMessage != "" && desired.SquashMergeCommitMessage != actual.SquashMergeCommitMessage {
		plan.Changes = append(plan.Changes, Change{Stage: StageSettings, Resource: "repository.squashMergeCommitMessage", State: ChangeUpdate, Summary: desired.SquashMergeCommitMessage})
	}
	if desired.Topics != nil && !sameStrings(desired.Topics, actual.Topics) {
		plan.Changes = append(plan.Changes, Change{Stage: StageSettings, Resource: "repository.topics", State: ChangeUpdate, Summary: "replace declared topics"})
	}
	if desired.Archived != nil && *desired.Archived != actual.Archived {
		if *desired.Archived {
			plan.Changes = append(plan.Changes, Change{Stage: StageArchive, Resource: "repository.archived", State: ChangeUpdate, Summary: "archive repository after every other change"})
		} else {
			plan.Changes = append(plan.Changes, Change{Stage: StageRepository, Resource: "repository.archived", State: ChangeUpdate, Summary: "unarchive repository"})
		}
	}
}

func appendBoolChange(plan *Plan, resource string, desired *bool, actual bool) {
	if desired != nil && *desired != actual {
		plan.Changes = append(plan.Changes, Change{Stage: StageSettings, Resource: resource, State: ChangeUpdate, Summary: fmt.Sprintf("set to %t", *desired)})
	}
}

func appendReleaseImmutabilityChanges(ctx context.Context, service Service, plan *Plan, desired config.RepositoryConfig, repository githubapi.Repository) {
	if desired.ReleaseImmutability == nil {
		return
	}
	actual, err := service.GetImmutableReleases(ctx, repository)
	if err != nil {
		plan.Changes = append(plan.Changes, Change{Stage: StageSettings, Resource: "repository.releaseImmutability", State: ChangeBlocked, Summary: capabilityError(err)})
		return
	}
	if actual.Enabled == *desired.ReleaseImmutability {
		plan.Changes = append(plan.Changes, Change{Stage: StageSettings, Resource: "repository.releaseImmutability", State: ChangeNoop, Summary: fmt.Sprintf("already set to %t", *desired.ReleaseImmutability)})
		return
	}
	plan.Changes = append(plan.Changes, Change{Stage: StageSettings, Resource: "repository.releaseImmutability", State: ChangeUpdate, Summary: fmt.Sprintf("set to %t", *desired.ReleaseImmutability)})
}

// appendLabelChanges diffs declared labels against the repository. Labels are
// additive: a label present on GitHub but absent from configuration never
// produces a change.
func appendLabelChanges(ctx context.Context, service Service, plan *Plan, desired []config.LabelConfig, repository githubapi.Repository) {
	if len(desired) == 0 {
		return
	}
	actual, err := service.ListLabels(ctx, repository)
	if err != nil {
		plan.Changes = append(plan.Changes, Change{Stage: StageSettings, Resource: "labels", State: ChangeBlocked, Summary: capabilityError(err)})
		return
	}
	byName := make(map[string]githubapi.Label, len(actual))
	for _, label := range actual {
		byName[strings.ToLower(label.Name)] = label
	}
	for _, label := range desired {
		state := ChangeCreate
		if current, ok := byName[strings.ToLower(label.Name)]; ok {
			if strings.EqualFold(current.Color, label.Color) && current.Description == label.Description {
				state = ChangeNoop
			} else {
				state = ChangeUpdate
			}
		}
		plan.Changes = append(plan.Changes, Change{Stage: StageSettings, Resource: "label." + label.Name, State: state, Summary: "converge label"})
	}
}

// appendMilestoneChanges diffs declared milestones against the repository,
// matched by title. Milestones are additive; retire one with state: closed.
func appendMilestoneChanges(ctx context.Context, service Service, plan *Plan, desired []config.MilestoneConfig, repository githubapi.Repository) {
	if len(desired) == 0 {
		return
	}
	actual, err := service.ListMilestones(ctx, repository)
	if err != nil {
		plan.Changes = append(plan.Changes, Change{Stage: StageSettings, Resource: "milestones", State: ChangeBlocked, Summary: capabilityError(err)})
		return
	}
	byTitle := make(map[string]githubapi.Milestone, len(actual))
	for _, milestone := range actual {
		byTitle[milestone.Title] = milestone
	}
	for _, milestone := range desired {
		desiredState := milestone.State
		if desiredState == "" {
			desiredState = "open"
		}
		state := ChangeCreate
		if current, ok := byTitle[milestone.Title]; ok {
			if milestoneConverged(desiredState, milestone, current) {
				state = ChangeNoop
			} else {
				state = ChangeUpdate
			}
		}
		plan.Changes = append(plan.Changes, Change{Stage: StageSettings, Resource: "milestone." + milestone.Title, State: state, Summary: "converge milestone"})
	}
}

func appendActionsChanges(ctx context.Context, service Service, plan *Plan, desired *config.ActionsConfig, repository githubapi.Repository, desiredRepository config.RepositoryConfig) {
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
		state, summary := ChangeUpdate, "update GITHUB_TOKEN permissions"
		if current, err := service.GetDefaultWorkflowPermissions(ctx, repository); err == nil {
			permissionsMatch := desired.DefaultWorkflowPermissions == "" || desired.DefaultWorkflowPermissions == current.DefaultWorkflowPermissions
			approvalMatches := desired.CanApprovePullRequestWorkflows == nil || *desired.CanApprovePullRequestWorkflows == current.CanApprovePullRequest
			if permissionsMatch && approvalMatches {
				state, summary = ChangeNoop, "GITHUB_TOKEN permissions already converged"
			}
		}
		plan.Changes = append(plan.Changes, Change{Stage: StageSettings, Resource: "actions.workflowPermissions", State: state, Summary: summary})
	}
	if desired.AllowedActions == "selected" && desired.SelectedActions != nil {
		state, summary := ChangeUpdate, "configure allowed Actions and reusable workflows"
		if current, err := service.GetSelectedActions(ctx, repository); err == nil {
			if current.GitHubOwnedAllowed == desired.SelectedActions.GitHubOwnedAllowed &&
				current.VerifiedAllowed == desired.SelectedActions.VerifiedAllowed &&
				sameStrings(current.PatternsAllowed, desired.SelectedActions.PatternsAllowed) {
				state, summary = ChangeNoop, "allowlist already converged"
			}
		}
		plan.Changes = append(plan.Changes, Change{Stage: StageSettings, Resource: "actions.selectedActions", State: state, Summary: summary})
	}
	appendForkPullRequestApprovalChange(plan, desired, desiredRepository, true)
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
	if len(desired.Environments) > 0 {
		existing := map[string]bool{}
		environments, err := service.ListEnvironments(ctx, repository)
		known := err == nil
		if known {
			for _, environment := range environments {
				existing[environment.Name] = true
			}
		}
		for _, environment := range desired.Environments {
			state := ChangeUpdate
			if known {
				state = ChangeCreate
				if existing[environment.Name] {
					state = ChangeNoop
				}
			}
			plan.Changes = append(plan.Changes, Change{Stage: StageAutomation, Resource: "actions.environment." + environment.Name, State: state, Summary: "ensure Actions environment"})
		}
	}
}

func appendSecurityChanges(ctx context.Context, service Service, plan *Plan, desired *config.SecurityConfig, repository githubapi.Repository, info githubapi.RepositoryInfo, exists bool) {
	if desired == nil {
		return
	}
	secretScanningCurrent, secretScanningKnown := securityAndAnalysisFeatureEnabled(info, func(s *githubapi.SecurityAndAnalysisStatus) *githubapi.SecurityFeature { return s.SecretScanning })
	appendFeatureChange(plan, StageSecurityPre, "security.secretScanning", desired.SecretScanning, "", secretScanningCurrent, exists && secretScanningKnown)

	pushProtectionCurrent, pushProtectionKnown := securityAndAnalysisFeatureEnabled(info, func(s *githubapi.SecurityAndAnalysisStatus) *githubapi.SecurityFeature {
		return s.SecretScanningPushProtect
	})
	appendFeatureChange(plan, StageSecurityPre, "security.pushProtection", desired.PushProtection, "", pushProtectionCurrent, exists && pushProtectionKnown)

	// alertsEnabled/alertsKnown answers "will alerts effectively be on once
	// this run finishes" (declared value takes precedence, since this run may
	// be about to change it) and only gates dependencyGraph and
	// dependabotSecurityUpdates. security.dependabotAlerts itself must
	// converge against GitHub's actual current state instead, fetched here
	// as liveAlertsEnabled/liveAlertsKnown — using the effective value there
	// would make it noop merely because the declared value equals itself,
	// never actually checking GitHub.
	var alertsEnabled, alertsKnown bool
	if desired.DependencyGraph != nil || desired.DependabotAlerts != nil || desired.DependabotSecurityUpdates != nil {
		alertsEnabled, alertsKnown = effectiveDependabotAlertsEnabled(ctx, service, repository, exists, desired)
	}
	var liveAlertsEnabled bool
	liveAlertsKnown := false
	if exists {
		if current, err := service.GetVulnerabilityAlertsEnabled(ctx, repository); err == nil {
			liveAlertsEnabled, liveAlertsKnown = current, true
		}
	}

	// GitHub enables the dependency graph as part of enabling Dependabot alerts.
	// There is no read endpoint for dependency graph status at all, so this
	// stays an unconditional update whenever it is not blocked outright.
	dependencyGraphReason := ""
	if desired.DependencyGraph != nil && (!alertsKnown || !alertsEnabled) {
		dependencyGraphReason = "only settable by enabling security.dependabotAlerts"
	}
	appendFeatureChange(plan, StageSecurity, "security.dependencyGraph", desired.DependencyGraph, dependencyGraphReason, false, false)
	appendFeatureChange(plan, StageSecurity, "security.dependabotAlerts", desired.DependabotAlerts, "", liveAlertsEnabled, exists && liveAlertsKnown)

	securityUpdatesReason := ""
	if desired.DependabotSecurityUpdates != nil && (!alertsKnown || !alertsEnabled) {
		securityUpdatesReason = "requires security.dependabotAlerts to be enabled"
	}
	securityUpdatesCurrent, securityUpdatesKnown := false, false
	if securityUpdatesReason == "" && exists {
		if current, err := service.GetAutomatedSecurityFixesEnabled(ctx, repository); err == nil {
			securityUpdatesCurrent, securityUpdatesKnown = current, true
		}
	}
	appendFeatureChange(plan, StageSecurity, "security.dependabotSecurityUpdates", desired.DependabotSecurityUpdates, securityUpdatesReason, securityUpdatesCurrent, securityUpdatesKnown)

	codeQLCurrent, codeQLKnown := false, false
	if exists {
		if status, err := service.GetCodeQLDefaultSetup(ctx, repository); err == nil {
			codeQLCurrent, codeQLKnown = status.State == "configured", true
		}
	}
	appendFeatureChange(plan, StageSecurity, "security.codeqlDefaultSetup", desired.CodeQLDefaultSetup, "", codeQLCurrent, codeQLKnown)

	privateVRCurrent, privateVRKnown := false, false
	if exists {
		if current, err := service.GetPrivateVulnerabilityReportingEnabled(ctx, repository); err == nil {
			privateVRCurrent, privateVRKnown = current, true
		}
	}
	appendFeatureChange(plan, StageSecurity, "security.privateVulnerabilityReporting", desired.PrivateVulnerabilityReporting, "", privateVRCurrent, privateVRKnown)
}

// securityAndAnalysisFeatureEnabled reads one feature's status out of a
// repository's security_and_analysis block, if GitHub reported one at all.
func securityAndAnalysisFeatureEnabled(info githubapi.RepositoryInfo, pick func(*githubapi.SecurityAndAnalysisStatus) *githubapi.SecurityFeature) (enabled, known bool) {
	if info.SecurityAndAnalysis == nil {
		return false, false
	}
	feature := pick(info.SecurityAndAnalysis)
	if feature == nil {
		return false, false
	}
	return feature.Status == "enabled", true
}

// effectiveDependabotAlertsEnabled reports whether Dependabot alerts will be
// enabled once this run finishes: the declared value when configuration
// declares one, otherwise the repository's current state on GitHub, which is
// unknowable before the repository exists. known is false only when neither
// source could determine the answer; callers must then treat any dependency
// on alerts as unsatisfied rather than assume it is.
func effectiveDependabotAlertsEnabled(ctx context.Context, service Service, repository githubapi.Repository, exists bool, desired *config.SecurityConfig) (enabled, known bool) {
	if desired.DependabotAlerts != nil {
		return desired.DependabotAlerts.Enabled, true
	}
	if !exists {
		return false, false
	}
	current, err := service.GetVulnerabilityAlertsEnabled(ctx, repository)
	if err != nil {
		return false, false
	}
	return current, true
}

// appendFeatureChange records one security feature. An empty reason means the
// feature can be applied; a non-empty reason blocks it, or degrades to a
// manual change when the declaration is optional. current is the live value
// on GitHub and is only meaningful when known is true — a repository that
// does not exist yet, or a read that failed, reports an update rather than
// guessing at a noop that might not hold once the value is actually knowable.
func appendFeatureChange(plan *Plan, stage Stage, resource string, feature *config.FeatureSetting, reason string, current, known bool) {
	if feature == nil {
		return
	}
	if reason != "" {
		state := ChangeBlocked
		if feature.Optional {
			state = ChangeManual
		}
		plan.Changes = append(plan.Changes, Change{Stage: stage, Resource: resource, State: state, Summary: reason})
		return
	}
	if known && current == feature.Enabled {
		plan.Changes = append(plan.Changes, Change{Stage: stage, Resource: resource, State: ChangeNoop, Summary: fmt.Sprintf("already set to %t", feature.Enabled)})
		return
	}
	plan.Changes = append(plan.Changes, Change{Stage: stage, Resource: resource, State: ChangeUpdate, Summary: fmt.Sprintf("set to %t", feature.Enabled)})
}

// appendForkPullRequestApprovalChange evaluates actions.forkPullRequestApproval.
// GitHub rejects the fork PR contributor approval endpoint outright on a
// private repository (HTTP 422), so that combination is blocked before any
// network call. This is planned unconditionally, like the sibling
// actions.workflowPermissions above it, rather than diffed against a live
// GET: a repository that is currently private but whose visibility this same
// run is changing to public would otherwise 422 on the read at plan time,
// even though the write would succeed once applyRepository's visibility
// change runs ahead of it during apply. live distinguishes only the
// deferred (repository-not-yet-created) path's summary wording.
func appendForkPullRequestApprovalChange(plan *Plan, desired *config.ActionsConfig, desiredRepository config.RepositoryConfig, live bool) {
	if desired.ForkPullRequestApproval == "" {
		return
	}
	if desiredRepository.Visibility == "private" {
		plan.Changes = append(plan.Changes, Change{Stage: StageSettings, Resource: "actions.forkPullRequestApproval", State: ChangeBlocked, Summary: "GitHub rejects fork pull request approval on private repositories"})
		return
	}
	summary := "set to " + desired.ForkPullRequestApproval
	if !live {
		summary = "configure after repository creation"
	}
	plan.Changes = append(plan.Changes, Change{Stage: StageSettings, Resource: "actions.forkPullRequestApproval", State: ChangeUpdate, Summary: summary})
}

func appendRulesetChanges(ctx context.Context, service Service, plan *Plan, desired []config.RulesetConfig, repository githubapi.Repository, resolver *bypassResolver) {
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
		appendBypassActorChanges(ctx, plan, ruleset, resolver, state, "")
	}
}

// appendBypassActorChanges resolves a ruleset's declared bypass actors and
// records the result. suffix distinguishes the deferred, create-only path
// ("after repository creation") from the live path; the resource and state
// otherwise match exactly, so a resolution failure is reported identically
// whether or not the repository exists yet.
func appendBypassActorChanges(ctx context.Context, plan *Plan, ruleset config.RulesetConfig, resolver *bypassResolver, state ChangeState, suffix string) {
	if len(ruleset.BypassActors) == 0 {
		return
	}
	resolved, failures := resolver.resolveAll(ctx, ruleset.BypassActors)
	for _, failure := range failures {
		resource := fmt.Sprintf("ruleset.%s.bypassActors[%d]", ruleset.Name, failure.Index)
		plan.Changes = append(plan.Changes, Change{Stage: StageRulesets, Resource: resource, State: ChangeBlocked, Summary: failure.Err.Error()})
	}
	if len(failures) > 0 {
		return
	}
	summary := fmt.Sprintf("converge %d bypass actors", len(resolved))
	if suffix != "" {
		summary += " " + suffix
	}
	plan.Changes = append(plan.Changes, Change{Stage: StageRulesets, Resource: "ruleset." + ruleset.Name + ".bypassActors", State: state, Summary: summary})
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
		plan.Changes = append(plan.Changes, Change{Stage: StageAutomation, Resource: resource, State: ChangeBlocked, Summary: "source environment variable is not set: " + secret.SourceEnv, Opaque: true})
		return
	}
	plan.Changes = append(plan.Changes, Change{Stage: StageAutomation, Resource: resource, State: ChangeUpdate, Summary: "converge encrypted secret", Opaque: true})
}

func appendSeedChanges(plan *Plan, cfg *config.Config) {
	if cfg.Seed != nil {
		plan.Changes = append(plan.Changes, Change{Stage: StageFiles, Resource: "seed", State: ChangeManual, Summary: "seed content will be staged before upload"})
	}
	for _, command := range cfg.Commands {
		plan.Changes = append(plan.Changes, Change{Stage: StageFiles, Resource: "command." + command.ID, State: ChangeManual, Summary: "requires --approve-command " + command.ID})
	}
}

func appendDeferredChanges(ctx context.Context, service Service, plan *Plan, cfg *config.Config, repository githubapi.Repository, resolver *bypassResolver) {
	if cfg.Seed != nil || len(cfg.Commands) > 0 {
		appendSeedChanges(plan, cfg)
	}
	if cfg.Repository.ReleaseImmutability != nil {
		plan.Changes = append(plan.Changes, Change{Stage: StageSettings, Resource: "repository.releaseImmutability", State: ChangeUpdate, Summary: "configure after repository creation"})
	}
	if cfg.Repository.Archived != nil && *cfg.Repository.Archived {
		plan.Changes = append(plan.Changes, Change{Stage: StageArchive, Resource: "repository.archived", State: ChangeUpdate, Summary: "archive repository after every other change"})
	}
	for _, label := range cfg.Labels {
		plan.Changes = append(plan.Changes, Change{Stage: StageSettings, Resource: "label." + label.Name, State: ChangeCreate, Summary: "create after repository creation"})
	}
	for _, milestone := range cfg.Milestones {
		plan.Changes = append(plan.Changes, Change{Stage: StageSettings, Resource: "milestone." + milestone.Title, State: ChangeCreate, Summary: "create after repository creation"})
	}
	if cfg.Actions != nil {
		plan.Changes = append(plan.Changes, Change{Stage: StageSettings, Resource: "actions", State: ChangeUpdate, Summary: "configure after repository creation"})
		appendForkPullRequestApprovalChange(plan, cfg.Actions, cfg.Repository, false)
	}
	appendSecurityChanges(ctx, service, plan, cfg.Security, repository, githubapi.RepositoryInfo{}, false)
	for _, ruleset := range cfg.Rulesets {
		plan.Changes = append(plan.Changes, Change{Stage: StageRulesets, Resource: "ruleset." + ruleset.Name, State: ChangeCreate, Summary: "create after repository initialization"})
		appendBypassActorChanges(ctx, plan, ruleset, resolver, ChangeCreate, "after repository creation")
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

// milestoneDueOnWire converts a YYYY-MM-DD date into the RFC 3339 timestamp
// GitHub's milestone API expects. An empty input clears the due date.
func milestoneDueOnWire(dueOn string) string {
	if dueOn == "" {
		return ""
	}
	return dueOn + "T00:00:00Z"
}

// milestoneConverged reports whether a live milestone already matches the
// declared one. GitHub returns due_on as a full timestamp even though only
// its date part is meaningful, so dates are compared by their first 10
// characters instead of byte-for-byte.
func milestoneConverged(desiredState string, desired config.MilestoneConfig, actual githubapi.Milestone) bool {
	if desired.Description != actual.Description || desiredState != actual.State {
		return false
	}
	return milestoneDatePart(desired.DueOn) == milestoneDatePart(actual.DueOn)
}

func milestoneDatePart(value string) string {
	if len(value) < 10 {
		return value
	}
	return value[:10]
}

func sameStrings(left, right []string) bool {
	leftCopy := slices.Clone(left)
	rightCopy := slices.Clone(right)
	sort.Strings(leftCopy)
	sort.Strings(rightCopy)
	return slices.Equal(leftCopy, rightCopy)
}
