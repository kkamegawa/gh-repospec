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
		cfg.Environments = append(cfg.Environments, config.Environment{Name: environment.Name})
	}
	sort.Slice(cfg.Environments, func(i, j int) bool { return cfg.Environments[i].Name < cfg.Environments[j].Name })

	return cfg, diagnostics, nil
}

// secretDiagnostic reports one secret name found on the repository. The
// value is never returned by the GitHub API, so the user supplies sourceEnv
// to bring the secret under management.
func secretDiagnostic(resource string) Diagnostic {
	return Diagnostic{Resource: resource, Message: "secret value is not returned by GitHub; declare sourceEnv to manage it"}
}
