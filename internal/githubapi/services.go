package githubapi

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"sort"
	"strconv"
	"strings"
)

// API is the planner-facing contract. It also supports lightweight fakes in unit tests.
type API interface {
	GetRepository(context.Context, Repository) (RepositoryInfo, error)
	CreateRepository(context.Context, CreateRepositoryRequest) (RepositoryInfo, error)
	UpdateRepository(context.Context, Repository, UpdateRepositoryRequest) (RepositoryInfo, error)
	ReplaceTopics(context.Context, Repository, []string) ([]string, error)
	GetActionsPermissions(context.Context, Repository) (ActionsPermissions, error)
	UpdateActionsPermissions(context.Context, Repository, ActionsPermissions) error
	UpdateSelectedActions(context.Context, Repository, SelectedActionsRequest) error
	UpdateDefaultWorkflowPermissions(context.Context, Repository, DefaultWorkflowPermissions) error
	GetForkPullRequestApproval(context.Context, Repository) (ForkPullRequestApproval, error)
	SetForkPullRequestApproval(context.Context, Repository, ForkPullRequestApproval) error
	ListRulesets(context.Context, Repository) ([]Ruleset, error)
	CreateRuleset(context.Context, Repository, Ruleset) (Ruleset, error)
	UpdateRuleset(context.Context, Repository, int64, Ruleset) (Ruleset, error)
	// GetTeam, GetApp, and ListCustomRepositoryRoles are org-scoped, not
	// repository-scoped, because they resolve ruleset bypass actor selectors
	// that identify an organization team, GitHub App, or custom role rather
	// than anything tied to one repository.
	GetTeam(context.Context, string, string) (Team, error)
	GetApp(context.Context, string) (App, error)
	ListCustomRepositoryRoles(context.Context, string) ([]CustomRepositoryRole, error)
	UpdateSecurityAndAnalysis(context.Context, Repository, SecurityAndAnalysisRequest) (RepositoryInfo, error)
	ConfigureCodeQLDefaultSetup(context.Context, Repository, CodeQLDefaultSetupRequest) error
	SetPrivateVulnerabilityReporting(context.Context, Repository, bool) error
	GetVulnerabilityAlertsEnabled(context.Context, Repository) (bool, error)
	SetVulnerabilityAlerts(context.Context, Repository, bool) error
	SetAutomatedSecurityFixes(context.Context, Repository, bool) error
	ListLabels(context.Context, Repository) ([]Label, error)
	CreateLabel(context.Context, Repository, Label) (Label, error)
	UpdateLabel(context.Context, Repository, string, Label) (Label, error)
	ListMilestones(context.Context, Repository) ([]Milestone, error)
	CreateMilestone(context.Context, Repository, Milestone) (Milestone, error)
	UpdateMilestone(context.Context, Repository, int, Milestone) (Milestone, error)
	GetImmutableReleases(context.Context, Repository) (ImmutableReleases, error)
	SetImmutableReleases(context.Context, Repository, bool) error
	GetAgentSecretPublicKey(context.Context, Repository) (PublicKey, error)
	PutAgentSecret(context.Context, Repository, string, EncryptedSecret) error
	ListAgentVariables(context.Context, Repository) (AgentVariableList, error)
	CreateAgentVariable(context.Context, Repository, AgentVariable) error
	UpdateAgentVariable(context.Context, Repository, AgentVariable) error
	GetActionsSecretPublicKey(context.Context, Repository) (PublicKey, error)
	PutActionsSecret(context.Context, Repository, string, EncryptedSecret) error
	ListActionVariables(context.Context, Repository) (ActionVariableList, error)
	CreateActionVariable(context.Context, Repository, ActionVariable) error
	UpdateActionVariable(context.Context, Repository, ActionVariable) error
	CreateOrUpdateEnvironment(context.Context, Repository, string, EnvironmentRequest) (Environment, error)
	GetBranchHead(context.Context, Repository, string) (GitReference, error)
	GetCommit(context.Context, Repository, string) (GitCommit, error)
	GetTree(context.Context, Repository, string, bool) (GitTree, error)
	GetBlob(context.Context, Repository, string) (GitBlob, error)
	CreateBlob(context.Context, Repository, []byte) (GitBlob, error)
	CreateTree(context.Context, Repository, string, []GitTreeEntry) (GitTree, error)
	CreateCommit(context.Context, Repository, string, string, []string) (GitCommit, error)
	CreateBranch(context.Context, Repository, string, string) (GitReference, error)
	UpdateBranchHead(context.Context, Repository, string, string) (GitReference, error)
	CommitFiles(context.Context, Repository, string, string, []File) (CommitResult, error)
}

var _ API = (*Client)(nil)

// GetAuthenticatedUser returns the account represented by the current gh login.
func (c *Client) GetAuthenticatedUser(ctx context.Context) (Owner, error) {
	var result Owner
	err := c.do(ctx, http.MethodGet, "/user", nil, &result)
	return result, err
}

// GetRepository retrieves the current repository settings.
func (c *Client) GetRepository(ctx context.Context, repo Repository) (RepositoryInfo, error) {
	var result RepositoryInfo
	err := c.do(ctx, http.MethodGet, repoPath(repo), nil, &result)
	return result, err
}

// CreateRepository creates a repository for the authenticated user or request.Owner organization.
func (c *Client) CreateRepository(ctx context.Context, request CreateRepositoryRequest) (RepositoryInfo, error) {
	var result RepositoryInfo
	path := "/user/repos"
	if request.Owner != "" {
		path = "/orgs/" + escape(request.Owner) + "/repos"
	}
	err := c.do(ctx, http.MethodPost, path, request, &result)
	return result, err
}

// UpdateRepository updates only declared repository settings.
func (c *Client) UpdateRepository(ctx context.Context, repo Repository, request UpdateRepositoryRequest) (RepositoryInfo, error) {
	var result RepositoryInfo
	err := c.do(ctx, http.MethodPatch, repoPath(repo), request, &result)
	return result, err
}

// ReplaceTopics replaces the repository topics with the supplied declaration.
func (c *Client) ReplaceTopics(ctx context.Context, repo Repository, topics []string) ([]string, error) {
	var result struct {
		Names []string `json:"names"`
	}
	err := c.do(ctx, http.MethodPut, repoPath(repo)+"/topics", struct {
		Names []string `json:"names"`
	}{Names: topics}, &result)
	return result.Names, err
}

func (c *Client) GetActionsPermissions(ctx context.Context, repo Repository) (ActionsPermissions, error) {
	var result ActionsPermissions
	err := c.do(ctx, http.MethodGet, repoPath(repo)+"/actions/permissions", nil, &result)
	return result, err
}

func (c *Client) UpdateActionsPermissions(ctx context.Context, repo Repository, settings ActionsPermissions) error {
	return c.do(ctx, http.MethodPut, repoPath(repo)+"/actions/permissions", settings, nil)
}

func (c *Client) UpdateDefaultWorkflowPermissions(ctx context.Context, repo Repository, settings DefaultWorkflowPermissions) error {
	return c.do(ctx, http.MethodPut, repoPath(repo)+"/actions/permissions/workflow", settings, nil)
}

// UpdateSelectedActions sets the Actions and reusable workflows allowlist used when
// Actions permissions are configured as "selected".
func (c *Client) UpdateSelectedActions(ctx context.Context, repo Repository, request SelectedActionsRequest) error {
	return c.do(ctx, http.MethodPut, repoPath(repo)+"/actions/permissions/selected-actions", request, nil)
}

// GetForkPullRequestApproval retrieves the fork PR contributor approval policy.
// GitHub rejects this endpoint for private repositories with HTTP 422.
func (c *Client) GetForkPullRequestApproval(ctx context.Context, repo Repository) (ForkPullRequestApproval, error) {
	var result ForkPullRequestApproval
	err := c.do(ctx, http.MethodGet, repoPath(repo)+"/actions/permissions/fork-pr-contributor-approval", nil, &result)
	return result, err
}

// SetForkPullRequestApproval sets the fork PR contributor approval policy.
func (c *Client) SetForkPullRequestApproval(ctx context.Context, repo Repository, request ForkPullRequestApproval) error {
	return c.do(ctx, http.MethodPut, repoPath(repo)+"/actions/permissions/fork-pr-contributor-approval", request, nil)
}

// ListRulesets lists every ruleset declared directly on the repository. It
// paginates like ListLabels and ListMilestones: GitHub defaults to 30 rulesets
// per page, and an unpaginated read would misreport an existing ruleset past
// that page as absent, causing CreateRuleset to attempt a duplicate.
func (c *Client) ListRulesets(ctx context.Context, repo Repository) ([]Ruleset, error) {
	return listAll[Ruleset](ctx, c, repoPath(repo)+"/rulesets")
}

func (c *Client) CreateRuleset(ctx context.Context, repo Repository, ruleset Ruleset) (Ruleset, error) {
	var result Ruleset
	err := c.do(ctx, http.MethodPost, repoPath(repo)+"/rulesets", ruleset, &result)
	return result, err
}

func (c *Client) UpdateRuleset(ctx context.Context, repo Repository, id int64, ruleset Ruleset) (Ruleset, error) {
	if id <= 0 {
		return Ruleset{}, fmt.Errorf("ruleset ID must be positive")
	}
	var result Ruleset
	err := c.do(ctx, http.MethodPut, repoPath(repo)+"/rulesets/"+strconv.FormatInt(id, 10), ruleset, &result)
	return result, err
}

// GetTeam resolves an organization team slug to its numeric ID for a ruleset
// bypass actor. Org-scoped: it has no relation to any single repository.
func (c *Client) GetTeam(ctx context.Context, org, slug string) (Team, error) {
	var result Team
	err := c.do(ctx, http.MethodGet, "/orgs/"+escape(org)+"/teams/"+escape(slug), nil, &result)
	return result, err
}

// GetApp resolves a GitHub App slug to its numeric ID for a ruleset bypass
// actor. This is a public, unauthenticated-shape endpoint scoped to the App
// itself, not to any organization or repository.
func (c *Client) GetApp(ctx context.Context, slug string) (App, error) {
	var result App
	err := c.do(ctx, http.MethodGet, "/apps/"+escape(slug), nil, &result)
	return result, err
}

// ListCustomRepositoryRoles lists an organization's custom repository roles,
// used to resolve a ruleset bypass actor customRole name to its numeric ID.
// The response is a wrapped object, not a bare array, so listAll cannot
// decode it directly.
func (c *Client) ListCustomRepositoryRoles(ctx context.Context, org string) ([]CustomRepositoryRole, error) {
	var result customRepositoryRoleList
	err := c.do(ctx, http.MethodGet, "/orgs/"+escape(org)+"/custom-repository-roles", nil, &result)
	return result.CustomRoles, err
}

func (c *Client) UpdateSecurityAndAnalysis(ctx context.Context, repo Repository, settings SecurityAndAnalysisRequest) (RepositoryInfo, error) {
	var result RepositoryInfo
	request := struct {
		SecurityAndAnalysis SecurityAndAnalysisRequest `json:"security_and_analysis"`
	}{SecurityAndAnalysis: settings}
	err := c.do(ctx, http.MethodPatch, repoPath(repo), request, &result)
	return result, err
}

func (c *Client) ConfigureCodeQLDefaultSetup(ctx context.Context, repo Repository, settings CodeQLDefaultSetupRequest) error {
	return c.do(ctx, http.MethodPatch, repoPath(repo)+"/code-scanning/default-setup", settings, nil)
}

func (c *Client) SetPrivateVulnerabilityReporting(ctx context.Context, repo Repository, enabled bool) error {
	method := http.MethodDelete
	if enabled {
		method = http.MethodPut
	}
	return c.do(ctx, method, repoPath(repo)+"/private-vulnerability-reporting", nil, nil)
}

// maxListPages bounds listAll so a misbehaving or unexpectedly large endpoint
// fails loudly instead of looping indefinitely.
const maxListPages = 20

// listAll walks every page of a GitHub list endpoint at 100 items per page,
// stopping at the first short page. The client does not expose response
// headers, so this cannot follow the Link header; it relies on GitHub's
// page-number pagination instead.
func listAll[T any](ctx context.Context, c *Client, basePath string) ([]T, error) {
	separator := "?"
	if strings.Contains(basePath, "?") {
		separator = "&"
	}
	var all []T
	for page := 1; page <= maxListPages; page++ {
		var pageResult []T
		apiPath := fmt.Sprintf("%s%sper_page=100&page=%d", basePath, separator, page)
		if err := c.do(ctx, http.MethodGet, apiPath, nil, &pageResult); err != nil {
			return nil, err
		}
		all = append(all, pageResult...)
		if len(pageResult) < 100 {
			return all, nil
		}
	}
	return nil, fmt.Errorf("list %s exceeded %d pages", basePath, maxListPages)
}

// ListLabels lists every label declared on the repository.
func (c *Client) ListLabels(ctx context.Context, repo Repository) ([]Label, error) {
	return listAll[Label](ctx, c, repoPath(repo)+"/labels")
}

// CreateLabel creates a repository label.
func (c *Client) CreateLabel(ctx context.Context, repo Repository, label Label) (Label, error) {
	var result Label
	err := c.do(ctx, http.MethodPost, repoPath(repo)+"/labels", label, &result)
	return result, err
}

// UpdateLabel updates an existing repository label identified by its current name.
func (c *Client) UpdateLabel(ctx context.Context, repo Repository, name string, label Label) (Label, error) {
	if name == "" {
		return Label{}, errors.New("label name is required")
	}
	var result Label
	err := c.do(ctx, http.MethodPatch, repoPath(repo)+"/labels/"+escape(name), label, &result)
	return result, err
}

// ListMilestones lists every milestone on the repository regardless of state,
// so open and previously closed milestones can both be matched by title.
func (c *Client) ListMilestones(ctx context.Context, repo Repository) ([]Milestone, error) {
	return listAll[Milestone](ctx, c, repoPath(repo)+"/milestones?state=all")
}

// CreateMilestone creates a repository milestone.
func (c *Client) CreateMilestone(ctx context.Context, repo Repository, milestone Milestone) (Milestone, error) {
	var result Milestone
	err := c.do(ctx, http.MethodPost, repoPath(repo)+"/milestones", milestone, &result)
	return result, err
}

// UpdateMilestone updates an existing repository milestone identified by its number.
func (c *Client) UpdateMilestone(ctx context.Context, repo Repository, number int, milestone Milestone) (Milestone, error) {
	if number <= 0 {
		return Milestone{}, errors.New("milestone number must be positive")
	}
	var result Milestone
	err := c.do(ctx, http.MethodPatch, repoPath(repo)+"/milestones/"+strconv.Itoa(number), milestone, &result)
	return result, err
}

// GetImmutableReleases retrieves the repository's immutable release setting.
// This is a dedicated endpoint: the repository update call does not accept it.
func (c *Client) GetImmutableReleases(ctx context.Context, repo Repository) (ImmutableReleases, error) {
	var result ImmutableReleases
	err := c.do(ctx, http.MethodGet, repoPath(repo)+"/immutable-releases", nil, &result)
	return result, err
}

// SetImmutableReleases enables or disables immutable releases for the repository.
func (c *Client) SetImmutableReleases(ctx context.Context, repo Repository, enabled bool) error {
	method := http.MethodDelete
	if enabled {
		method = http.MethodPut
	}
	return c.do(ctx, method, repoPath(repo)+"/immutable-releases", nil, nil)
}

// SetVulnerabilityAlerts enables or disables Dependabot alerts. Enabling alerts
// also enables the repository dependency graph on GitHub.
func (c *Client) SetVulnerabilityAlerts(ctx context.Context, repo Repository, enabled bool) error {
	method := http.MethodDelete
	if enabled {
		method = http.MethodPut
	}
	return c.do(ctx, method, repoPath(repo)+"/vulnerability-alerts", nil, nil)
}

// GetVulnerabilityAlertsEnabled reports whether Dependabot alerts are
// currently enabled. GitHub represents this as a 204 (enabled) or 404
// (disabled) response with no body, rather than a JSON boolean.
func (c *Client) GetVulnerabilityAlertsEnabled(ctx context.Context, repo Repository) (bool, error) {
	err := c.do(ctx, http.MethodGet, repoPath(repo)+"/vulnerability-alerts", nil, nil)
	if err == nil {
		return true, nil
	}
	if IsNotFound(err) {
		return false, nil
	}
	return false, err
}

// SetAutomatedSecurityFixes enables or disables Dependabot security updates.
func (c *Client) SetAutomatedSecurityFixes(ctx context.Context, repo Repository, enabled bool) error {
	method := http.MethodDelete
	if enabled {
		method = http.MethodPut
	}
	return c.do(ctx, method, repoPath(repo)+"/automated-security-fixes", nil, nil)
}

func (c *Client) GetAgentSecretPublicKey(ctx context.Context, repo Repository) (PublicKey, error) {
	var result PublicKey
	err := c.do(ctx, http.MethodGet, repoPath(repo)+"/agents/secrets/public-key", nil, &result)
	return result, err
}

func (c *Client) PutAgentSecret(ctx context.Context, repo Repository, name string, secret EncryptedSecret) error {
	return c.do(ctx, http.MethodPut, repoPath(repo)+"/agents/secrets/"+escape(name), secret, nil)
}

// ListAgentSecrets lists repository agent secret names without values.
func (c *Client) ListAgentSecrets(ctx context.Context, repo Repository) (SecretList, error) {
	var result SecretList
	err := c.do(ctx, http.MethodGet, repoPath(repo)+"/agents/secrets", nil, &result)
	return result, err
}

func (c *Client) ListAgentVariables(ctx context.Context, repo Repository) (AgentVariableList, error) {
	var result AgentVariableList
	err := c.do(ctx, http.MethodGet, repoPath(repo)+"/agents/variables", nil, &result)
	return result, err
}

func (c *Client) CreateAgentVariable(ctx context.Context, repo Repository, variable AgentVariable) error {
	return c.do(ctx, http.MethodPost, repoPath(repo)+"/agents/variables", variable, nil)
}

func (c *Client) UpdateAgentVariable(ctx context.Context, repo Repository, variable AgentVariable) error {
	if variable.Name == "" {
		return fmt.Errorf("agent variable name is required")
	}
	return c.do(ctx, http.MethodPatch, repoPath(repo)+"/agents/variables/"+escape(variable.Name), variable, nil)
}

// GetActionsSecretPublicKey retrieves the public key used to encrypt a repository Actions secret.
func (c *Client) GetActionsSecretPublicKey(ctx context.Context, repo Repository) (PublicKey, error) {
	var result PublicKey
	err := c.do(ctx, http.MethodGet, repoPath(repo)+"/actions/secrets/public-key", nil, &result)
	return result, err
}

// PutActionsSecret creates or updates an encrypted repository Actions secret.
func (c *Client) PutActionsSecret(ctx context.Context, repo Repository, name string, secret EncryptedSecret) error {
	if name == "" {
		return errors.New("actions secret name is required")
	}
	return c.do(ctx, http.MethodPut, repoPath(repo)+"/actions/secrets/"+escape(name), secret, nil)
}

// ListActionsSecrets lists repository Actions secret names without values.
func (c *Client) ListActionsSecrets(ctx context.Context, repo Repository) (SecretList, error) {
	var result SecretList
	err := c.do(ctx, http.MethodGet, repoPath(repo)+"/actions/secrets", nil, &result)
	return result, err
}

// ListActionVariables lists repository Actions variables. Callers should paginate only
// after this minimal bootstrap operation needs more than GitHub's default page size.
func (c *Client) ListActionVariables(ctx context.Context, repo Repository) (ActionVariableList, error) {
	var result ActionVariableList
	err := c.do(ctx, http.MethodGet, repoPath(repo)+"/actions/variables", nil, &result)
	return result, err
}

// CreateActionVariable creates a repository Actions variable.
func (c *Client) CreateActionVariable(ctx context.Context, repo Repository, variable ActionVariable) error {
	if variable.Name == "" {
		return errors.New("actions variable name is required")
	}
	return c.do(ctx, http.MethodPost, repoPath(repo)+"/actions/variables", variable, nil)
}

// UpdateActionVariable updates a repository Actions variable by name.
func (c *Client) UpdateActionVariable(ctx context.Context, repo Repository, variable ActionVariable) error {
	if variable.Name == "" {
		return errors.New("actions variable name is required")
	}
	return c.do(ctx, http.MethodPatch, repoPath(repo)+"/actions/variables/"+escape(variable.Name), variable, nil)
}

// CreateOrUpdateEnvironment creates a repository environment or updates its declared settings.
func (c *Client) CreateOrUpdateEnvironment(ctx context.Context, repo Repository, name string, request EnvironmentRequest) (Environment, error) {
	if name == "" {
		return Environment{}, errors.New("environment name is required")
	}
	var result Environment
	err := c.do(ctx, http.MethodPut, repoPath(repo)+"/environments/"+escape(name), request, &result)
	return result, err
}

// GetBranchHead retrieves the current commit ref for a branch.
func (c *Client) GetBranchHead(ctx context.Context, repo Repository, branch string) (GitReference, error) {
	if branch == "" {
		return GitReference{}, errors.New("branch is required")
	}
	var result GitReference
	err := c.do(ctx, http.MethodGet, repoPath(repo)+"/git/ref/heads/"+escape(branch), nil, &result)
	return result, err
}

// GetCommit retrieves the Git commit used as the base for a new tree.
func (c *Client) GetCommit(ctx context.Context, repo Repository, sha string) (GitCommit, error) {
	if sha == "" {
		return GitCommit{}, errors.New("commit SHA is required")
	}
	var result GitCommit
	err := c.do(ctx, http.MethodGet, repoPath(repo)+"/git/commits/"+escape(sha), nil, &result)
	return result, err
}

// GetTree retrieves a Git tree. recursive requests are limited by GitHub and expose
// Truncated so callers can reject incomplete views.
func (c *Client) GetTree(ctx context.Context, repo Repository, sha string, recursive bool) (GitTree, error) {
	if sha == "" {
		return GitTree{}, errors.New("tree SHA is required")
	}
	apiPath := repoPath(repo) + "/git/trees/" + escape(sha)
	if recursive {
		apiPath += "?recursive=1"
	}
	var result GitTree
	err := c.do(ctx, http.MethodGet, apiPath, nil, &result)
	return result, err
}

// GetBlob retrieves a Git blob. GitHub returns its content encoded as base64.
func (c *Client) GetBlob(ctx context.Context, repo Repository, sha string) (GitBlob, error) {
	if sha == "" {
		return GitBlob{}, errors.New("blob SHA is required")
	}
	var result GitBlob
	err := c.do(ctx, http.MethodGet, repoPath(repo)+"/git/blobs/"+escape(sha), nil, &result)
	return result, err
}

// CreateBlob creates a base64 Git blob without placing it on a branch.
func (c *Client) CreateBlob(ctx context.Context, repo Repository, content []byte) (GitBlob, error) {
	var result GitBlob
	request := struct {
		Content  string `json:"content"`
		Encoding string `json:"encoding"`
	}{
		Content:  base64.StdEncoding.EncodeToString(content),
		Encoding: "base64",
	}
	err := c.do(ctx, http.MethodPost, repoPath(repo)+"/git/blobs", request, &result)
	return result, err
}

// CreateTree creates a tree based on baseTreeSHA with the supplied path entries.
func (c *Client) CreateTree(ctx context.Context, repo Repository, baseTreeSHA string, entries []GitTreeEntry) (GitTree, error) {
	if baseTreeSHA == "" {
		return GitTree{}, errors.New("base tree SHA is required")
	}
	if len(entries) == 0 {
		return GitTree{}, errors.New("at least one tree entry is required")
	}
	var result GitTree
	request := struct {
		BaseTree string         `json:"base_tree"`
		Tree     []GitTreeEntry `json:"tree"`
	}{BaseTree: baseTreeSHA, Tree: entries}
	err := c.do(ctx, http.MethodPost, repoPath(repo)+"/git/trees", request, &result)
	return result, err
}

// CreateCommit creates a commit with the supplied tree and parents.
func (c *Client) CreateCommit(ctx context.Context, repo Repository, message, treeSHA string, parents []string) (GitCommit, error) {
	if message == "" {
		return GitCommit{}, errors.New("commit message is required")
	}
	if treeSHA == "" {
		return GitCommit{}, errors.New("tree SHA is required")
	}
	if len(parents) == 0 {
		return GitCommit{}, errors.New("at least one parent commit SHA is required")
	}
	var result GitCommit
	request := struct {
		Message string   `json:"message"`
		Tree    string   `json:"tree"`
		Parents []string `json:"parents"`
	}{Message: message, Tree: treeSHA, Parents: parents}
	err := c.do(ctx, http.MethodPost, repoPath(repo)+"/git/commits", request, &result)
	return result, err
}

// CreateBranch creates a branch ref at an existing commit SHA.
func (c *Client) CreateBranch(ctx context.Context, repo Repository, branch, commitSHA string) (GitReference, error) {
	if branch == "" {
		return GitReference{}, errors.New("branch is required")
	}
	if commitSHA == "" {
		return GitReference{}, errors.New("commit SHA is required")
	}
	var result GitReference
	request := struct {
		Ref string `json:"ref"`
		SHA string `json:"sha"`
	}{Ref: "refs/heads/" + branch, SHA: commitSHA}
	err := c.do(ctx, http.MethodPost, repoPath(repo)+"/git/refs", request, &result)
	return result, err
}

// UpdateBranchHead fast-forwards a branch to commitSHA. It never force-updates a ref.
func (c *Client) UpdateBranchHead(ctx context.Context, repo Repository, branch, commitSHA string) (GitReference, error) {
	if branch == "" {
		return GitReference{}, errors.New("branch is required")
	}
	if commitSHA == "" {
		return GitReference{}, errors.New("commit SHA is required")
	}
	var result GitReference
	request := struct {
		SHA   string `json:"sha"`
		Force bool   `json:"force"`
	}{SHA: commitSHA, Force: false}
	err := c.do(ctx, http.MethodPatch, repoPath(repo)+"/git/refs/heads/"+escape(branch), request, &result)
	return result, err
}

// CommitFiles writes a set of files in exactly one commit on an existing branch.
// It obtains the current head first and uses a non-forced ref update, so concurrent
// changes are surfaced as a GitHub error instead of being overwritten.
func (c *Client) CommitFiles(ctx context.Context, repo Repository, branch, message string, files []File) (CommitResult, error) {
	if branch == "" {
		return CommitResult{}, errors.New("branch is required")
	}
	if message == "" {
		return CommitResult{}, errors.New("commit message is required")
	}
	if len(files) == 0 {
		return CommitResult{}, errors.New("at least one file is required")
	}

	files = append([]File(nil), files...)
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	entries := make([]GitTreeEntry, 0, len(files))
	seen := make(map[string]struct{}, len(files))
	for _, file := range files {
		if err := validateRepositoryPath(file.Path); err != nil {
			return CommitResult{}, err
		}
		if _, duplicate := seen[file.Path]; duplicate {
			return CommitResult{}, fmt.Errorf("duplicate file path %q", file.Path)
		}
		seen[file.Path] = struct{}{}
		mode := file.Mode
		if mode == "" {
			mode = "100644"
		}
		if mode != "100644" && mode != "100755" {
			return CommitResult{}, fmt.Errorf("unsupported file mode %q for %q", mode, file.Path)
		}
		blob, err := c.CreateBlob(ctx, repo, file.Content)
		if err != nil {
			return CommitResult{}, fmt.Errorf("create blob for %q: %w", file.Path, err)
		}
		entries = append(entries, GitTreeEntry{Path: file.Path, Mode: mode, Type: "blob", SHA: blob.SHA})
	}

	head, err := c.GetBranchHead(ctx, repo, branch)
	if err != nil {
		return CommitResult{}, fmt.Errorf("get branch head: %w", err)
	}
	commit, err := c.GetCommit(ctx, repo, head.Object.SHA)
	if err != nil {
		return CommitResult{}, fmt.Errorf("get branch commit: %w", err)
	}
	tree, err := c.CreateTree(ctx, repo, commit.Tree.SHA, entries)
	if err != nil {
		return CommitResult{}, fmt.Errorf("create file tree: %w", err)
	}
	created, err := c.CreateCommit(ctx, repo, message, tree.SHA, []string{head.Object.SHA})
	if err != nil {
		return CommitResult{}, fmt.Errorf("create file commit: %w", err)
	}
	if _, err := c.UpdateBranchHead(ctx, repo, branch, created.SHA); err != nil {
		return CommitResult{}, fmt.Errorf("update branch head: %w", err)
	}
	return CommitResult{CommitSHA: created.SHA, TreeSHA: tree.SHA, PreviousSHA: head.Object.SHA}, nil
}

func validateRepositoryPath(value string) error {
	if value == "" || strings.HasPrefix(value, "/") || strings.Contains(value, "\\") {
		return fmt.Errorf("file path must be a non-empty repository-relative slash path: %q", value)
	}
	clean := path.Clean(value)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") || clean != value {
		return fmt.Errorf("unsafe repository-relative file path %q", value)
	}
	for _, segment := range strings.Split(value, "/") {
		if segment == ".git" {
			return fmt.Errorf("file path %q must not contain .git", value)
		}
	}
	return nil
}

func repoPath(repo Repository) string {
	return "/repos/" + escape(repo.Owner) + "/" + escape(repo.Name)
}

func escape(segment string) string { return url.PathEscape(segment) }
