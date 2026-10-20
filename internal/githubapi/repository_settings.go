package githubapi

import (
	"context"
	"net/http"
	"strconv"
)

func (c *Client) GetRepositoryInteractionLimit(ctx context.Context, repo Repository) (RepositoryInteractionLimit, error) {
	var result RepositoryInteractionLimit
	err := c.do(ctx, http.MethodGet, repoPath(repo)+"/interaction-limits", nil, &result)
	return result, err
}

func (c *Client) SetRepositoryInteractionLimit(ctx context.Context, repo Repository, request RepositoryInteractionLimitRequest) (RepositoryInteractionLimit, error) {
	var result RepositoryInteractionLimit
	err := c.do(ctx, http.MethodPut, repoPath(repo)+"/interaction-limits", request, &result)
	return result, err
}

func (c *Client) DeleteRepositoryInteractionLimit(ctx context.Context, repo Repository) error {
	return c.do(ctx, http.MethodDelete, repoPath(repo)+"/interaction-limits", nil, nil)
}

func (c *Client) ListPullRequestCreationCapBypassUsers(ctx context.Context, repo Repository) ([]InteractionBypassUser, error) {
	var result []InteractionBypassUser
	err := c.do(ctx, http.MethodGet, repoPath(repo)+"/interaction-limits/pulls/bypass-list", nil, &result)
	return result, err
}

func (c *Client) AddPullRequestCreationCapBypassUsers(ctx context.Context, repo Repository, users []string) error {
	return c.do(ctx, http.MethodPut, repoPath(repo)+"/interaction-limits/pulls/bypass-list", struct {
		Users []string `json:"users"`
	}{Users: users}, nil)
}

func (c *Client) RemovePullRequestCreationCapBypassUsers(ctx context.Context, repo Repository, users []string) error {
	return c.do(ctx, http.MethodDelete, repoPath(repo)+"/interaction-limits/pulls/bypass-list", struct {
		Users []string `json:"users"`
	}{Users: users}, nil)
}

func (c *Client) GetPullRequestCreationCap(ctx context.Context, repo Repository) (PullRequestCreationCap, error) {
	var result PullRequestCreationCap
	err := c.do(ctx, http.MethodGet, repoPath(repo)+"/interaction-limits/pulls/creation-cap", nil, &result)
	return result, err
}

func (c *Client) UpdatePullRequestCreationCap(ctx context.Context, repo Repository, request PullRequestCreationCapRequest) (PullRequestCreationCap, error) {
	var result PullRequestCreationCap
	err := c.do(ctx, http.MethodPatch, repoPath(repo)+"/interaction-limits/pulls/creation-cap", request, &result)
	return result, err
}

type PagesSite struct {
	Status        string       `json:"status,omitempty"`
	BuildType     string       `json:"build_type,omitempty"`
	Source        *PagesSource `json:"source,omitempty"`
	CNAME         string       `json:"cname,omitempty"`
	HTTPSEnforced bool         `json:"https_enforced,omitempty"`
}

type PagesSource struct {
	Branch string `json:"branch"`
	Path   string `json:"path"`
}

type PagesRequest struct {
	BuildType     string       `json:"build_type,omitempty"`
	Source        *PagesSource `json:"source,omitempty"`
	CNAME         *string      `json:"cname,omitempty"`
	HTTPSEnforced *bool        `json:"https_enforced,omitempty"`
}

type RepositoryWebhook struct {
	ID     int64         `json:"id,omitempty"`
	Name   string        `json:"name,omitempty"`
	Active bool          `json:"active"`
	Events []string      `json:"events"`
	Config WebhookConfig `json:"config"`
}

type WebhookConfig struct {
	URL         string `json:"url"`
	ContentType string `json:"content_type,omitempty"`
	InsecureSSL string `json:"insecure_ssl,omitempty"`
}

type Autolink struct {
	ID             int64  `json:"id,omitempty"`
	KeyPrefix      string `json:"key_prefix"`
	URLTemplate    string `json:"url_template"`
	IsAlphanumeric bool   `json:"is_alphanumeric"`
}

type DeployKey struct {
	ID       int64  `json:"id,omitempty"`
	Title    string `json:"title"`
	Key      string `json:"key"`
	ReadOnly bool   `json:"read_only"`
}

type Collaborator struct {
	Login       string `json:"login"`
	Permission  string `json:"permission"`
	RoleName    string `json:"role_name"`
	Permissions struct {
		Pull     bool `json:"pull"`
		Triage   bool `json:"triage"`
		Push     bool `json:"push"`
		Maintain bool `json:"maintain"`
		Admin    bool `json:"admin"`
	} `json:"permissions"`
}

// EffectivePermission returns the most specific role GitHub reports for a
// collaborator. GitHub's list response combines direct, team, organization,
// and enterprise grants, so this value is not evidence of a direct grant.
func (c Collaborator) EffectivePermission() string {
	permission := c.RoleName
	if permission == "" {
		permission = c.Permission
	}
	if permission == "" {
		switch {
		case c.Permissions.Admin:
			permission = "admin"
		case c.Permissions.Maintain:
			permission = "maintain"
		case c.Permissions.Push:
			permission = "push"
		case c.Permissions.Triage:
			permission = "triage"
		case c.Permissions.Pull:
			permission = "pull"
		}
	}
	switch permission {
	case "read":
		return "pull"
	case "write":
		return "push"
	default:
		return permission
	}
}

type RepositoryTeam struct {
	Slug       string `json:"slug"`
	Permission string `json:"permission"`
}

type CustomPropertyValue struct {
	PropertyName string `json:"property_name"`
	Value        any    `json:"value"`
}

type Branch struct {
	Name      string `json:"name"`
	Protected bool   `json:"protected"`
}

func (c *Client) GetPages(ctx context.Context, repo Repository) (PagesSite, error) {
	var result PagesSite
	err := c.do(ctx, http.MethodGet, repoPath(repo)+"/pages", nil, &result)
	return result, err
}

func (c *Client) CreatePages(ctx context.Context, repo Repository, request PagesRequest) (PagesSite, error) {
	var result PagesSite
	err := c.do(ctx, http.MethodPost, repoPath(repo)+"/pages", request, &result)
	return result, err
}

func (c *Client) UpdatePages(ctx context.Context, repo Repository, request PagesRequest) (PagesSite, error) {
	var result PagesSite
	err := c.do(ctx, http.MethodPut, repoPath(repo)+"/pages", request, &result)
	return result, err
}

func (c *Client) DeletePages(ctx context.Context, repo Repository) error {
	return c.do(ctx, http.MethodDelete, repoPath(repo)+"/pages", nil, nil)
}

func (c *Client) ListWebhooks(ctx context.Context, repo Repository) ([]RepositoryWebhook, error) {
	return listAll[RepositoryWebhook](ctx, c, repoPath(repo)+"/hooks")
}

func (c *Client) CreateWebhook(ctx context.Context, repo Repository, hook RepositoryWebhook) (RepositoryWebhook, error) {
	var result RepositoryWebhook
	err := c.do(ctx, http.MethodPost, repoPath(repo)+"/hooks", hook, &result)
	return result, err
}

func (c *Client) UpdateWebhook(ctx context.Context, repo Repository, id int64, hook RepositoryWebhook) (RepositoryWebhook, error) {
	var result RepositoryWebhook
	err := c.do(ctx, http.MethodPatch, repoPath(repo)+"/hooks/"+strconv.FormatInt(id, 10), hook, &result)
	return result, err
}

func (c *Client) DeleteWebhook(ctx context.Context, repo Repository, id int64) error {
	return c.do(ctx, http.MethodDelete, repoPath(repo)+"/hooks/"+strconv.FormatInt(id, 10), nil, nil)
}

func (c *Client) ListAutolinks(ctx context.Context, repo Repository) ([]Autolink, error) {
	return listAll[Autolink](ctx, c, repoPath(repo)+"/autolinks")
}

func (c *Client) CreateAutolink(ctx context.Context, repo Repository, autolink Autolink) (Autolink, error) {
	var result Autolink
	err := c.do(ctx, http.MethodPost, repoPath(repo)+"/autolinks", autolink, &result)
	return result, err
}

func (c *Client) DeleteAutolink(ctx context.Context, repo Repository, id int64) error {
	return c.do(ctx, http.MethodDelete, repoPath(repo)+"/autolinks/"+strconv.FormatInt(id, 10), nil, nil)
}

func (c *Client) ListDeployKeys(ctx context.Context, repo Repository) ([]DeployKey, error) {
	return listAll[DeployKey](ctx, c, repoPath(repo)+"/keys")
}

func (c *Client) CreateDeployKey(ctx context.Context, repo Repository, key DeployKey) (DeployKey, error) {
	var result DeployKey
	err := c.do(ctx, http.MethodPost, repoPath(repo)+"/keys", key, &result)
	return result, err
}

func (c *Client) DeleteDeployKey(ctx context.Context, repo Repository, id int64) error {
	return c.do(ctx, http.MethodDelete, repoPath(repo)+"/keys/"+strconv.FormatInt(id, 10), nil, nil)
}

func (c *Client) ListCollaborators(ctx context.Context, repo Repository) ([]Collaborator, error) {
	return listAll[Collaborator](ctx, c, repoPath(repo)+"/collaborators?affiliation=direct")
}

func (c *Client) SetCollaboratorPermission(ctx context.Context, repo Repository, login, permission string) error {
	return c.do(ctx, http.MethodPut, repoPath(repo)+"/collaborators/"+escape(login), struct {
		Permission string `json:"permission"`
	}{Permission: permission}, nil)
}

func (c *Client) ListRepositoryTeams(ctx context.Context, repo Repository) ([]RepositoryTeam, error) {
	return listAll[RepositoryTeam](ctx, c, repoPath(repo)+"/teams")
}

func (c *Client) SetTeamRepositoryPermission(ctx context.Context, repo Repository, slug, permission string) error {
	apiPath := "/orgs/" + escape(repo.Owner) + "/teams/" + escape(slug) + "/repos/" + escape(repo.Owner) + "/" + escape(repo.Name)
	return c.do(ctx, http.MethodPut, apiPath, struct {
		Permission string `json:"permission"`
	}{Permission: permission}, nil)
}

func (c *Client) GetCustomPropertyValues(ctx context.Context, repo Repository) ([]CustomPropertyValue, error) {
	var result []CustomPropertyValue
	err := c.do(ctx, http.MethodGet, repoPath(repo)+"/properties/values", nil, &result)
	return result, err
}

func (c *Client) SetCustomPropertyValues(ctx context.Context, repo Repository, values []CustomPropertyValue) error {
	return c.do(ctx, http.MethodPatch, repoPath(repo)+"/properties/values", struct {
		Properties []CustomPropertyValue `json:"properties"`
	}{Properties: values}, nil)
}

func (c *Client) ListProtectedBranches(ctx context.Context, repo Repository) ([]Branch, error) {
	return listAll[Branch](ctx, c, repoPath(repo)+"/branches?protected=true")
}

func (c *Client) GetBranchProtection(ctx context.Context, repo Repository, branch string) (map[string]any, error) {
	var result map[string]any
	err := c.do(ctx, http.MethodGet, repoPath(repo)+"/branches/"+escape(branch)+"/protection", nil, &result)
	return result, err
}

func (c *Client) SetBranchProtection(ctx context.Context, repo Repository, branch string, settings map[string]any) error {
	return c.do(ctx, http.MethodPut, repoPath(repo)+"/branches/"+escape(branch)+"/protection", settings, nil)
}

func (c *Client) GetRequiredSignatures(ctx context.Context, repo Repository, branch string) (bool, error) {
	var result struct {
		Enabled bool `json:"enabled"`
	}
	err := c.do(ctx, http.MethodGet, repoPath(repo)+"/branches/"+escape(branch)+"/protection/required_signatures", nil, &result)
	return result.Enabled, err
}

func (c *Client) SetRequiredSignatures(ctx context.Context, repo Repository, branch string, enabled bool) error {
	method := http.MethodDelete
	if enabled {
		method = http.MethodPost
	}
	return c.do(ctx, method, repoPath(repo)+"/branches/"+escape(branch)+"/protection/required_signatures", nil, nil)
}

// NormalizeBranchProtection removes response-only links and converts GitHub's
// expanded actor objects back to the string arrays accepted by the update API.
func NormalizeBranchProtection(protection map[string]any) map[string]any {
	clean := cloneJSONMap(protection)
	for _, key := range []string{"required_status_checks", "required_pull_request_reviews", "restrictions"} {
		if nested, ok := clean[key].(map[string]any); ok {
			for _, metadata := range []string{"url", "contexts_url", "users_url", "teams_url", "apps_url", "enforcement_level"} {
				delete(nested, metadata)
			}
			if key == "required_pull_request_reviews" {
				if dismissal, ok := nested["dismissal_restrictions"].(map[string]any); ok {
					stripBranchProtectionLinks(dismissal)
					normalizeActorLists(dismissal)
				}
				if bypass, ok := nested["bypass_pull_request_allowances"].(map[string]any); ok {
					stripBranchProtectionLinks(bypass)
					normalizeActorLists(bypass)
				}
			}
			if key == "restrictions" {
				normalizeActorLists(nested)
			}
		}
	}
	if value, ok := clean["enforce_admins"].(map[string]any); ok {
		clean["enforce_admins"] = value["enabled"]
	}
	for _, key := range []string{"required_linear_history", "allow_force_pushes", "allow_deletions", "block_creations", "required_conversation_resolution", "lock_branch", "allow_fork_syncing"} {
		if value, ok := clean[key].(map[string]any); ok {
			clean[key] = value["enabled"]
		}
	}
	delete(clean, "url")
	delete(clean, "required_signatures")
	return clean
}

func stripBranchProtectionLinks(values map[string]any) {
	for _, metadata := range []string{"url", "users_url", "teams_url", "apps_url"} {
		delete(values, metadata)
	}
}

func normalizeActorLists(values map[string]any) {
	for field, identityKey := range map[string]string{"users": "login", "teams": "slug", "apps": "slug"} {
		actors, ok := values[field].([]any)
		if !ok {
			continue
		}
		names := make([]any, 0, len(actors))
		for _, actor := range actors {
			if actorMap, ok := actor.(map[string]any); ok {
				if name, ok := actorMap[identityKey].(string); ok {
					names = append(names, name)
				}
			} else if name, ok := actor.(string); ok {
				names = append(names, name)
			}
		}
		values[field] = names
	}
}

func cloneJSONMap(value map[string]any) map[string]any {
	result := make(map[string]any, len(value))
	for key, item := range value {
		switch item := item.(type) {
		case map[string]any:
			result[key] = cloneJSONMap(item)
		case []any:
			items := make([]any, len(item))
			for index, nested := range item {
				if nestedMap, ok := nested.(map[string]any); ok {
					items[index] = cloneJSONMap(nestedMap)
				} else {
					items[index] = nested
				}
			}
			result[key] = items
		default:
			result[key] = item
		}
	}
	return result
}
