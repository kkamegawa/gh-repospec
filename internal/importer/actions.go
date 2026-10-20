package importer

import (
	"context"
	"fmt"
	"sort"

	"github.com/kkamegawa/gh-repospec/internal/config"
	"github.com/kkamegawa/gh-repospec/internal/githubapi"
)

// importActions fetches Actions permissions, the selected-actions allowlist
// (only meaningful when allowedActions is "selected"), default workflow
// permissions, the fork pull request approval policy, variables, secret
// names, and environment names. GitHub rejects the fork approval endpoint
// outright for a private repository, so it is skipped rather than attempted
// and reported as a diagnostic instead of a failed request.
func importActions(ctx context.Context, reader Reader, repository githubapi.Repository, info githubapi.RepositoryInfo) (*config.ActionsConfig, []Diagnostic, error) {
	permissions, err := reader.GetActionsPermissions(ctx, repository)
	if err != nil {
		return nil, nil, fmt.Errorf("get Actions permissions for %s: %w", repository, err)
	}
	cfg := &config.ActionsConfig{
		Enabled:        boolPtr(permissions.Enabled),
		AllowedActions: permissions.AllowedActions,
	}

	if permissions.AllowedActions == "selected" {
		selected, err := reader.GetSelectedActions(ctx, repository)
		if err != nil {
			return nil, nil, fmt.Errorf("get selected Actions allowlist for %s: %w", repository, err)
		}
		cfg.SelectedActions = &config.SelectedActionsConfig{
			GitHubOwnedAllowed: selected.GitHubOwnedAllowed,
			VerifiedAllowed:    selected.VerifiedAllowed,
			PatternsAllowed:    append([]string(nil), selected.PatternsAllowed...),
		}
	}

	workflowPermissions, err := reader.GetDefaultWorkflowPermissions(ctx, repository)
	if err != nil {
		return nil, nil, fmt.Errorf("get default workflow permissions for %s: %w", repository, err)
	}
	cfg.DefaultWorkflowPermissions = workflowPermissions.DefaultWorkflowPermissions
	cfg.CanApprovePullRequestWorkflows = boolPtr(workflowPermissions.CanApprovePullRequest)
	workflowAccess, err := reader.GetWorkflowRepositoryAccess(ctx, repository)
	if err != nil {
		return nil, nil, fmt.Errorf("get workflow repository access for %s: %w", repository, err)
	}
	cfg.WorkflowAccessLevel = workflowAccess.AccessLevel
	oidc, err := reader.GetOIDCSubjectCustomization(ctx, repository)
	if err != nil {
		return nil, nil, fmt.Errorf("get Actions OIDC subject customization for %s: %w", repository, err)
	}
	cfg.OIDCSubjectCustomization = &config.OIDCSubjectCustomizationConfig{
		UseDefault: oidc.UseDefault, IncludeClaimKeys: append([]string(nil), oidc.IncludeClaimKeys...),
		UseImmutableSubject: oidc.UseImmutableSubject,
	}

	var diagnostics []Diagnostic
	if info.Private {
		diagnostics = append(diagnostics, Diagnostic{
			Resource: "actions.forkPullRequestApproval",
			Message:  "GitHub rejects this endpoint for a private repository; not imported",
		})
	} else {
		approval, err := reader.GetForkPullRequestApproval(ctx, repository)
		if err != nil {
			return nil, nil, fmt.Errorf("get fork pull request approval policy for %s: %w", repository, err)
		}
		cfg.ForkPullRequestApproval = approval.ApprovalPolicy
	}

	variables, err := reader.ListActionVariables(ctx, repository)
	if err != nil {
		return nil, nil, fmt.Errorf("list Actions variables for %s: %w", repository, err)
	}
	for _, variable := range variables.Variables {
		cfg.Variables = append(cfg.Variables, config.VariableRef{Name: variable.Name, Value: variable.Value})
	}
	sort.Slice(cfg.Variables, func(i, j int) bool { return cfg.Variables[i].Name < cfg.Variables[j].Name })

	secrets, err := reader.ListActionsSecrets(ctx, repository)
	if err != nil {
		return nil, nil, fmt.Errorf("list Actions secret names for %s: %w", repository, err)
	}
	for _, secret := range secrets.Secrets {
		diagnostics = append(diagnostics, secretDiagnostic("actions.secret."+secret.Name))
	}

	environments, err := reader.ListEnvironments(ctx, repository)
	if err != nil {
		return nil, nil, fmt.Errorf("list environments for %s: %w", repository, err)
	}
	for _, environment := range environments {
		mapped := mapEnvironment(environment)
		customRules, err := reader.ListDeploymentProtectionRules(ctx, repository, environment.Name)
		if err != nil {
			return nil, nil, fmt.Errorf("list custom deployment protection rules for environment %q: %w", environment.Name, err)
		}
		customRuleSlugs := make([]string, 0, len(customRules))
		for _, rule := range customRules {
			if rule.Enabled && rule.App.Slug != "" {
				customRuleSlugs = append(customRuleSlugs, rule.App.Slug)
			}
		}
		sort.Strings(customRuleSlugs)
		mapped.CustomProtectionRules = &customRuleSlugs
		if environment.DeploymentBranchPolicy != nil && environment.DeploymentBranchPolicy.CustomBranchPolicies {
			policies, err := reader.ListEnvironmentBranchPolicies(ctx, repository, environment.Name)
			if err != nil {
				return nil, nil, fmt.Errorf("list deployment branch policies for environment %q: %w", environment.Name, err)
			}
			branchPolicies := make([]config.EnvironmentBranchPolicy, 0, len(policies))
			for _, policy := range policies {
				// The REST response exposes the pattern but not the branch/tag
				// discriminator accepted by the write endpoint. Preserve the
				// supported branch default and report the unreadable distinction.
				branchPolicies = append(branchPolicies, config.EnvironmentBranchPolicy{Name: policy.Name, Type: "branch"})
			}
			mapped.BranchPolicies = &branchPolicies
			if len(policies) > 0 {
				diagnostics = append(diagnostics, Diagnostic{Resource: "actions.environment." + environment.Name + ".branchPolicyTypes", Message: "GitHub's list response omits whether each pattern targets a branch or tag; imported as branch for review"})
			}
		}
		secrets, err := reader.ListEnvironmentSecrets(ctx, repository, environment.Name)
		if err != nil {
			return nil, nil, fmt.Errorf("list secrets for environment %q: %w", environment.Name, err)
		}
		for _, secret := range secrets.Secrets {
			diagnostics = append(diagnostics, secretDiagnostic("actions.environment."+environment.Name+".secret."+secret.Name))
		}
		for _, rule := range environment.ProtectionRules {
			if rule.Type != "wait_timer" && rule.Type != "required_reviewers" && rule.Type != "branch_policy" {
				diagnostics = append(diagnostics, Diagnostic{Resource: "actions.environment." + environment.Name + ".protectionRule." + rule.Type, Message: "this deployment protection rule type is not represented in the configuration schema"})
			}
		}
		cfg.Environments = append(cfg.Environments, mapped)
	}
	sort.Slice(cfg.Environments, func(i, j int) bool { return cfg.Environments[i].Name < cfg.Environments[j].Name })

	return cfg, diagnostics, nil
}

func mapEnvironment(environment githubapi.Environment) config.Environment {
	waitTimer := 0
	preventSelfReview := false
	reviewers := make([]config.EnvironmentReviewer, 0)
	for _, rule := range environment.ProtectionRules {
		switch rule.Type {
		case "wait_timer":
			waitTimer = rule.WaitTimer
		case "required_reviewers":
			preventSelfReview = rule.PreventSelfReview
			for _, reviewer := range rule.Reviewers {
				reviewers = append(reviewers, config.EnvironmentReviewer{Type: reviewer.Type, ID: reviewer.Reviewer.ID})
			}
		}
	}
	var policy *config.DeploymentBranchPolicy
	if environment.DeploymentBranchPolicy == nil {
		policy = &config.DeploymentBranchPolicy{AllowAllBranches: true}
	} else {
		policy = &config.DeploymentBranchPolicy{
			ProtectedBranches:    environment.DeploymentBranchPolicy.ProtectedBranches,
			CustomBranchPolicies: environment.DeploymentBranchPolicy.CustomBranchPolicies,
		}
	}
	return config.Environment{
		Name:                   environment.Name,
		WaitTimer:              &waitTimer,
		PreventSelfReview:      &preventSelfReview,
		Reviewers:              &reviewers,
		DeploymentBranchPolicy: policy,
	}
}

// secretDiagnostic reports one secret name found on the repository. The
// value is never returned by the GitHub API, so the user supplies sourceEnv
// to bring the secret under management.
func secretDiagnostic(resource string) Diagnostic {
	return Diagnostic{Resource: resource, Message: "secret value is not returned by GitHub; declare sourceEnv to manage it"}
}
