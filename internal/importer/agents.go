package importer

import (
	"context"
	"fmt"
	"sort"

	"github.com/kkamegawa/gh-repospec/internal/config"
	"github.com/kkamegawa/gh-repospec/internal/githubapi"
)

// importAgents fetches agent variables and secret names. Agent file paths
// (mcpConfigPath, instructionsPath, customAgentsPath, skillsPath) are local
// intent no repository state implies and are never inferred. The result is
// nil when there are no agent variables at all, so an import of a repository
// with no Copilot coding agent configuration does not declare an empty
// agents section.
func importAgents(ctx context.Context, reader Reader, repository githubapi.Repository) (*config.AgentsConfig, []Diagnostic, error) {
	variables, err := reader.ListAgentVariables(ctx, repository)
	if err != nil {
		return nil, nil, fmt.Errorf("list agent variables for %s: %w", repository, err)
	}
	cfg := &config.AgentsConfig{}
	for _, variable := range variables.Variables {
		cfg.Variables = append(cfg.Variables, config.VariableRef{Name: variable.Name, Value: variable.Value})
	}
	sort.Slice(cfg.Variables, func(i, j int) bool { return cfg.Variables[i].Name < cfg.Variables[j].Name })

	secrets, err := reader.ListAgentSecrets(ctx, repository)
	if err != nil {
		return nil, nil, fmt.Errorf("list agent secrets for %s: %w", repository, err)
	}
	var diagnostics []Diagnostic
	for _, secret := range secrets.Secrets {
		diagnostics = append(diagnostics, secretDiagnostic("agents.secret."+secret.Name))
	}

	if len(cfg.Variables) == 0 {
		return nil, diagnostics, nil
	}
	return cfg, diagnostics, nil
}
