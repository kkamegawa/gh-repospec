package githubapi

import (
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"sort"
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
	// GitHub mirrors every required check into the legacy contexts list, and
	// the update API rejects a body that carries both, so checks is kept as
	// the single representation.
	if checks, ok := clean["required_status_checks"].(map[string]any); ok {
		if _, hasChecks := checks["checks"]; hasChecks {
			delete(checks, "contexts")
		}
	}
	delete(clean, "url")
	delete(clean, "required_signatures")
	return clean
}

// BranchProtectionRequest converts declared branch protection settings into
// the body accepted by the update API. The API requires
// required_status_checks, enforce_admins, required_pull_request_reviews, and
// restrictions to be present, with null disabling a section, while the read
// API omits sections that are not in use.
func BranchProtectionRequest(settings map[string]any) map[string]any {
	request := cloneJSONMap(settings)
	for _, key := range []string{"required_status_checks", "enforce_admins", "required_pull_request_reviews", "restrictions"} {
		if _, ok := request[key]; !ok {
			request[key] = nil
		}
	}
	if checks, ok := request["required_status_checks"].(map[string]any); ok {
		// checks and contexts are alternative forms of the same list; sending
		// both matches more than one branch of the request schema.
		if list, hasChecks := checks["checks"].([]any); hasChecks {
			delete(checks, "contexts")
			for _, item := range list {
				// The read API reports a check that any app may provide as
				// app_id null, while the update API reads an omitted app_id as
				// "pin the app that last provided this check" and needs -1 for
				// any app.
				if check, ok := item.(map[string]any); ok {
					if appID, declared := check["app_id"]; declared && appID == nil {
						check["app_id"] = -1
					}
				}
			}
		}
	}
	return request
}

// BranchProtectionMatches reports whether declared settings describe the
// protection GitHub returned. Booleans that are false and sections that are
// null are the update API's defaults, so they compare equal to an absent key.
// A required check declared by context name only, either in contexts or as a
// check without app_id, matches the check GitHub resolved for that name,
// whichever app GitHub assigned. A check declared with app_id null or -1
// matches only a check that any app may provide.
func BranchProtectionMatches(desired, actual map[string]any) bool {
	want := comparableBranchProtection(desired)
	got := comparableBranchProtection(NormalizeBranchProtection(actual))
	wantChecks, _ := want["required_status_checks"].(map[string]any)
	gotChecks, _ := got["required_status_checks"].(map[string]any)
	if wantChecks != nil && gotChecks != nil {
		if wantList, declaresChecks := wantChecks["checks"].([]any); declaresChecks {
			unpinned := map[any]bool{}
			for _, item := range wantList {
				if check, ok := item.(map[string]any); ok {
					if _, declared := check["app_id"]; !declared {
						unpinned[check["context"]] = true
					}
				}
			}
			if gotList, ok := gotChecks["checks"].([]any); ok {
				for _, item := range gotList {
					if check, ok := item.(map[string]any); ok && unpinned[check["context"]] {
						delete(check, "app_id")
					}
				}
			}
		} else if _, declaresContexts := wantChecks["contexts"]; declaresContexts {
			contexts := make([]any, 0)
			if list, ok := gotChecks["checks"].([]any); ok {
				for _, item := range list {
					if check, ok := item.(map[string]any); ok {
						contexts = append(contexts, check["context"])
					}
				}
			}
			delete(gotChecks, "checks")
			gotChecks["contexts"] = contexts
		}
	}
	for _, values := range []map[string]any{wantChecks, gotChecks} {
		if values != nil {
			for _, key := range []string{"checks", "contexts"} {
				if list, ok := values[key].([]any); ok {
					sortJSONList(list)
				}
			}
		}
	}
	return reflect.DeepEqual(roundTripJSON(want), roundTripJSON(got))
}

// comparableBranchProtection drops default-valued keys and resolves the
// checks/contexts duplication so equivalent protections compare equal.
func comparableBranchProtection(settings map[string]any) map[string]any {
	clean := cloneJSONMap(settings)
	if checks, ok := clean["required_status_checks"].(map[string]any); ok {
		if list, hasChecks := checks["checks"].([]any); hasChecks {
			delete(checks, "contexts")
			for _, item := range list {
				// -1 on the update API and null on the read API both mean
				// any app; an omitted app_id stays omitted.
				if check, ok := item.(map[string]any); ok && isAnyAppID(check["app_id"]) {
					check["app_id"] = nil
				}
			}
		}
	}
	dropDefaultValues(clean)
	return clean
}

func isAnyAppID(value any) bool {
	switch value := value.(type) {
	case int:
		return value == -1
	case int64:
		return value == -1
	case float64:
		return value == -1
	}
	return false
}

// dropDefaultValues descends into nested objects only, never into arrays: an
// app_id null inside required_status_checks.checks means "any app", not a
// default, and must stay distinct from a check pinned to an app.
func dropDefaultValues(values map[string]any) {
	for key, value := range values {
		switch value := value.(type) {
		case nil:
			delete(values, key)
		case bool:
			if !value {
				delete(values, key)
			}
		case map[string]any:
			dropDefaultValues(value)
		}
	}
}

func sortJSONList(list []any) {
	sort.SliceStable(list, func(i, j int) bool {
		left, _ := json.Marshal(list[i])
		right, _ := json.Marshal(list[j])
		return string(left) < string(right)
	})
}

// roundTripJSON maps YAML- and JSON-decoded values onto the same Go types,
// for example int and float64 numbers, before they are compared.
func roundTripJSON(value any) any {
	encoded, err := json.Marshal(value)
	if err != nil {
		return value
	}
	var decoded any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		return value
	}
	return decoded
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
