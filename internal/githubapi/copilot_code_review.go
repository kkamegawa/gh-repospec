package githubapi

import (
	"context"
	"net/http"
)

const copilotCodeReviewSettingsPath = "/copilot/code-review/settings"

type CopilotReviewEffort struct {
	Configured *string `json:"configured"`
	Effective  string  `json:"effective"`
}

type CopilotCodeReviewSettings struct {
	ReviewEffort              CopilotReviewEffort `json:"review_effort"`
	AvailableReviewEfforts    []string            `json:"available_review_efforts"`
	CustomInstructionsEnabled *bool               `json:"custom_instructions_enabled"`
	MCPEnabled                *bool               `json:"mcp_enabled"`
	SeverityFilter            *string             `json:"severity_filter"`
}

type CopilotCodeReviewSettingsRequest struct {
	ReviewEffort              *string `json:"review_effort,omitempty"`
	CustomInstructionsEnabled *bool   `json:"custom_instructions_enabled,omitempty"`
	MCPEnabled                *bool   `json:"mcp_enabled,omitempty"`
	SeverityFilter            *string `json:"severity_filter,omitempty"`
}

func (c *Client) GetCopilotCodeReviewSettings(ctx context.Context, repo Repository) (CopilotCodeReviewSettings, error) {
	var result CopilotCodeReviewSettings
	err := c.do(ctx, http.MethodGet, repoPath(repo)+copilotCodeReviewSettingsPath, nil, &result)
	return result, err
}

func (c *Client) UpdateCopilotCodeReviewSettings(ctx context.Context, repo Repository, request CopilotCodeReviewSettingsRequest) error {
	return c.do(ctx, http.MethodPatch, repoPath(repo)+copilotCodeReviewSettingsPath, request, nil)
}
