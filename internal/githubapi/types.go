// Package githubapi contains the GitHub REST operations used by the planner and applier.
package githubapi

import (
	"errors"
	"time"
)

// APIVersion is sent with every request so GitHub REST behavior is stable.
const APIVersion = "2026-03-10"

// Repository identifies a GitHub repository.
type Repository struct {
	Owner string
	Name  string
}

// String returns the canonical owner/name form.
func (r Repository) String() string { return r.Owner + "/" + r.Name }

// RepositoryInfo is the subset of repository state needed by the planner
// and the importer.
type RepositoryInfo struct {
	ID                       int64    `json:"id"`
	Owner                    Owner    `json:"owner"`
	Name                     string   `json:"name"`
	FullName                 string   `json:"full_name"`
	Description              string   `json:"description"`
	Homepage                 string   `json:"homepage"`
	Private                  bool     `json:"private"`
	Visibility               string   `json:"visibility"`
	DefaultBranch            string   `json:"default_branch"`
	Archived                 bool     `json:"archived"`
	Topics                   []string `json:"topics"`
	HasIssues                bool     `json:"has_issues"`
	HasProjects              bool     `json:"has_projects"`
	HasWiki                  bool     `json:"has_wiki"`
	HasDiscussions           bool     `json:"has_discussions"`
	AllowSquashMerge         bool     `json:"allow_squash_merge"`
	AllowMergeCommit         bool     `json:"allow_merge_commit"`
	AllowRebaseMerge         bool     `json:"allow_rebase_merge"`
	AllowAutoMerge           bool     `json:"allow_auto_merge"`
	DeleteBranchOnMerge      bool     `json:"delete_branch_on_merge"`
	MergeCommitTitle         string   `json:"merge_commit_title"`
	MergeCommitMessage       string   `json:"merge_commit_message"`
	SquashMergeCommitTitle   string   `json:"squash_merge_commit_title"`
	SquashMergeCommitMessage string   `json:"squash_merge_commit_message"`
	// SecurityAndAnalysis is nil when GitHub omits the block entirely (a
	// repository type that does not support it). Import reports secretScanning
	// and pushProtection as unavailable rather than false in that case.
	SecurityAndAnalysis *SecurityAndAnalysisStatus `json:"security_and_analysis,omitempty"`
}

// SecurityAndAnalysisStatus is the security_and_analysis block returned by
// Get a repository. Its shape mirrors SecurityAndAnalysisRequest, which is
// used only for the write.
type SecurityAndAnalysisStatus struct {
	AdvancedSecurity          *SecurityFeature `json:"advanced_security,omitempty"`
	SecretScanning            *SecurityFeature `json:"secret_scanning,omitempty"`
	SecretScanningPushProtect *SecurityFeature `json:"secret_scanning_push_protection,omitempty"`
}

// Owner identifies the account that owns a repository.
type Owner struct {
	Login string `json:"login"`
	Type  string `json:"type"`
}

// CreateRepositoryRequest is used for either a user or organization repository.
// Owner is empty when creating a repository for the authenticated user.
type CreateRepositoryRequest struct {
	Owner               string `json:"-"`
	Name                string `json:"name"`
	Description         string `json:"description,omitempty"`
	Private             *bool  `json:"private,omitempty"`
	Visibility          string `json:"visibility,omitempty"`
	AutoInit            bool   `json:"auto_init"`
	AllowSquashMerge    *bool  `json:"allow_squash_merge,omitempty"`
	AllowMergeCommit    *bool  `json:"allow_merge_commit,omitempty"`
	AllowRebaseMerge    *bool  `json:"allow_rebase_merge,omitempty"`
	AllowAutoMerge      *bool  `json:"allow_auto_merge,omitempty"`
	DeleteBranchOnMerge *bool  `json:"delete_branch_on_merge,omitempty"`
}

// UpdateRepositoryRequest contains only fields that GitHub's update repository API supports.
// Pointer and pointer-like fields are sent only when set; false and empty-string
// values on those fields are meaningful (they clear or disable) rather than absent.
type UpdateRepositoryRequest struct {
	Description              *string `json:"description,omitempty"`
	Homepage                 *string `json:"homepage,omitempty"`
	Private                  *bool   `json:"private,omitempty"`
	Visibility               string  `json:"visibility,omitempty"`
	DefaultBranch            string  `json:"default_branch,omitempty"`
	Archived                 *bool   `json:"archived,omitempty"`
	HasIssues                *bool   `json:"has_issues,omitempty"`
	HasProjects              *bool   `json:"has_projects,omitempty"`
	HasWiki                  *bool   `json:"has_wiki,omitempty"`
	HasDiscussions           *bool   `json:"has_discussions,omitempty"`
	AllowSquashMerge         *bool   `json:"allow_squash_merge,omitempty"`
	AllowMergeCommit         *bool   `json:"allow_merge_commit,omitempty"`
	AllowRebaseMerge         *bool   `json:"allow_rebase_merge,omitempty"`
	AllowAutoMerge           *bool   `json:"allow_auto_merge,omitempty"`
	DeleteBranchOnMerge      *bool   `json:"delete_branch_on_merge,omitempty"`
	MergeCommitTitle         string  `json:"merge_commit_title,omitempty"`
	MergeCommitMessage       string  `json:"merge_commit_message,omitempty"`
	SquashMergeCommitTitle   string  `json:"squash_merge_commit_title,omitempty"`
	SquashMergeCommitMessage string  `json:"squash_merge_commit_message,omitempty"`
}

// ActionsPermissions is the repository Actions policy.
type ActionsPermissions struct {
	Enabled            bool   `json:"enabled"`
	AllowedActions     string `json:"allowed_actions,omitempty"`
	SHAPinningRequired bool   `json:"sha_pinning_required,omitempty"`
}

// DefaultWorkflowPermissions controls GITHUB_TOKEN defaults.
type DefaultWorkflowPermissions struct {
	DefaultWorkflowPermissions string `json:"default_workflow_permissions"`
	CanApprovePullRequest      bool   `json:"can_approve_pull_request_reviews"`
}

// SelectedActionsRequest configures the Actions and reusable workflows allowed to run
// when ActionsPermissions.AllowedActions is "selected". GitHub blocks every Action
// until this allowlist is set explicitly.
type SelectedActionsRequest struct {
	GitHubOwnedAllowed bool     `json:"github_owned_allowed"`
	VerifiedAllowed    bool     `json:"verified_allowed"`
	PatternsAllowed    []string `json:"patterns_allowed,omitempty"`
}

// SelectedActions is GitHub's currently configured Actions and reusable
// workflow allowlist, read back from the selected-actions endpoint. Its
// shape mirrors SelectedActionsRequest, which is used only for the write.
type SelectedActions struct {
	GitHubOwnedAllowed bool     `json:"github_owned_allowed"`
	VerifiedAllowed    bool     `json:"verified_allowed"`
	PatternsAllowed    []string `json:"patterns_allowed,omitempty"`
}

// ForkPullRequestApproval is the fork PR contributor approval policy. GitHub
// rejects this endpoint for private repositories, so ApprovalPolicy has no
// omitempty: a false-shaped zero value must never be silently dropped from
// the PUT body the way ActionsPermissions.SHAPinningRequired can be.
type ForkPullRequestApproval struct {
	ApprovalPolicy string `json:"approval_policy"`
}

// Team is the subset of an organization team needed to resolve a ruleset
// bypass actor slug to the numeric ID the rulesets API requires.
type Team struct {
	ID   int64  `json:"id"`
	Slug string `json:"slug"`
}

// App is the subset of a GitHub App needed to resolve a ruleset bypass actor
// slug to the numeric ID the rulesets API requires.
type App struct {
	ID   int64  `json:"id"`
	Slug string `json:"slug"`
}

// CustomRepositoryRole is one organization custom repository role, used to
// resolve a ruleset bypass actor customRole name to the numeric ID the
// rulesets API requires.
type CustomRepositoryRole struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// customRepositoryRoleList is GitHub's wrapped response for listing custom
// repository roles; unlike labels and milestones it is not a bare array, so
// listAll cannot decode it directly.
type customRepositoryRoleList struct {
	TotalCount  int                    `json:"total_count"`
	CustomRoles []CustomRepositoryRole `json:"custom_roles"`
}

func (l *customRepositoryRoleList) pageItems() []CustomRepositoryRole { return l.CustomRoles } //nolint:unused // called only through the wrappedList[T] generic constraint in listAllWrapped, which some analyzers cannot trace back to this concrete type

// Ruleset is an intentionally flexible representation of a repository ruleset.
// Rules and conditions retain GitHub's API shape without losing new rule types.
type Ruleset struct {
	ID          int64            `json:"id,omitempty"`
	Name        string           `json:"name"`
	Target      string           `json:"target,omitempty"`
	Enforcement string           `json:"enforcement"`
	Conditions  map[string]any   `json:"conditions,omitempty"`
	Rules       []map[string]any `json:"rules,omitempty"`
	// BypassActors deliberately has no omitempty: like Rules and Conditions, a
	// ruleset write replaces the whole declared set, so an empty or absent list
	// in configuration must clear bypass actors set through the GitHub UI
	// rather than silently leaving them in place.
	BypassActors []BypassActor `json:"bypass_actors"`
	// SourceType is read-only: "Repository", "Organization", or "Enterprise".
	// Import uses it to recognize an inherited ruleset that the repository
	// does not own and therefore cannot represent as a declared ruleset.
	SourceType string `json:"source_type,omitempty"`
}

// BypassActor is one rulesets API bypass_actors entry. Unlike rules and
// conditions its shape is fixed by the API, so it is typed rather than a raw
// map. ActorID has no omitempty because GitHub still requires the field for
// OrganizationAdmin even though it ignores the value there.
type BypassActor struct {
	ActorID    int64  `json:"actor_id"`
	ActorType  string `json:"actor_type"`
	BypassMode string `json:"bypass_mode"`
}

// SecurityAndAnalysisRequest is the security_and_analysis block of Update a repository.
type SecurityAndAnalysisRequest struct {
	AdvancedSecurity          *SecurityFeature `json:"advanced_security,omitempty"`
	SecretScanning            *SecurityFeature `json:"secret_scanning,omitempty"`
	SecretScanningPushProtect *SecurityFeature `json:"secret_scanning_push_protection,omitempty"`
}

// SecurityFeature is a GitHub repository security feature switch.
type SecurityFeature struct {
	Status string `json:"status"`
}

// CodeQLDefaultSetupRequest configures CodeQL default setup.
type CodeQLDefaultSetupRequest struct {
	State      string   `json:"state"`
	Languages  []string `json:"languages,omitempty"`
	QuerySuite string   `json:"query_suite,omitempty"`
}

// CodeQLDefaultSetupStatus is the current CodeQL default setup configuration.
// State is "configured" or "not-configured"; other fields the read endpoint
// returns (runner_type, schedule, updated_at, and so on) are not needed here
// and are silently ignored by the JSON decoder.
type CodeQLDefaultSetupStatus struct {
	State      string   `json:"state"`
	Languages  []string `json:"languages,omitempty"`
	QuerySuite string   `json:"query_suite,omitempty"`
}

// automatedSecurityFixesStatus is the response body GitHub returns when
// Dependabot security updates are enabled; a 404 means disabled and carries
// no body, matching GetVulnerabilityAlertsEnabled's pattern.
type automatedSecurityFixesStatus struct {
	Enabled bool `json:"enabled"`
}

// privateVulnerabilityReportingStatus is the response body for the private
// vulnerability reporting status endpoint.
type privateVulnerabilityReportingStatus struct {
	Enabled bool `json:"enabled"`
}

// PublicKey is used to encrypt a secret before it is sent to GitHub.
type PublicKey struct {
	KeyID string `json:"key_id"`
	Key   string `json:"key"`
}

// EncryptedSecret does not contain plaintext. Callers must encrypt locally with PublicKey.
type EncryptedSecret struct {
	EncryptedValue string `json:"encrypted_value"`
	KeyID          string `json:"key_id"`
}

// SecretMetadata is the non-sensitive metadata returned when listing secrets.
type SecretMetadata struct {
	Name string `json:"name"`
}

type SecretList struct {
	TotalCount int              `json:"total_count"`
	Secrets    []SecretMetadata `json:"secrets"`
}

func (l *SecretList) pageItems() []SecretMetadata { return l.Secrets } //nolint:unused // see customRepositoryRoleList.pageItems

// ActionVariable represents a repository Actions variable.
type ActionVariable struct {
	Name      string    `json:"name"`
	Value     string    `json:"value"`
	CreatedAt time.Time `json:"created_at,omitempty"`
	UpdatedAt time.Time `json:"updated_at,omitempty"`
}

// ActionVariableList is GitHub's paginated repository Actions variable response.
type ActionVariableList struct {
	TotalCount int              `json:"total_count"`
	Variables  []ActionVariable `json:"variables"`
}

func (l *ActionVariableList) pageItems() []ActionVariable { return l.Variables } //nolint:unused // see customRepositoryRoleList.pageItems

// EnvironmentRequest contains the repository environment properties supported by
// GitHub's create-or-update environment endpoint. Nil fields are left unchanged
// by callers that only need to ensure that an environment exists.
type EnvironmentRequest struct {
	WaitTimer              *int                    `json:"wait_timer,omitempty"`
	PreventSelfReview      *bool                   `json:"prevent_self_review,omitempty"`
	Reviewers              []EnvironmentReviewer   `json:"reviewers,omitempty"`
	DeploymentBranchPolicy *DeploymentBranchPolicy `json:"deployment_branch_policy,omitempty"`
}

// EnvironmentReviewer identifies a user or team required to review a deployment.
type EnvironmentReviewer struct {
	Type     string `json:"type"`
	Reviewer int64  `json:"reviewer"`
}

// DeploymentBranchPolicy controls which branches can deploy to an environment.
type DeploymentBranchPolicy struct {
	ProtectedBranches    bool `json:"protected_branches"`
	CustomBranchPolicies bool `json:"custom_branch_policies"`
}

// Environment is the subset of a GitHub repository environment returned after an update.
type Environment struct {
	ID             int64  `json:"id"`
	Name           string `json:"name"`
	URL            string `json:"url"`
	HTMLURL        string `json:"html_url"`
	CreatedAt      string `json:"created_at"`
	UpdatedAt      string `json:"updated_at"`
	ProtectionRule []any  `json:"protection_rules,omitempty"`
}

// environmentList is GitHub's wrapped response for listing repository
// environments; like custom repository roles it is not a bare array.
type environmentList struct {
	TotalCount   int           `json:"total_count"`
	Environments []Environment `json:"environments"`
}

func (l *environmentList) pageItems() []Environment { return l.Environments } //nolint:unused // see customRepositoryRoleList.pageItems

// GitReference identifies a branch ref and its current object SHA.
type GitReference struct {
	Ref    string    `json:"ref"`
	NodeID string    `json:"node_id,omitempty"`
	Object GitObject `json:"object"`
}

// GitObject is the SHA-bearing object in a Git API response.
type GitObject struct {
	Type string `json:"type,omitempty"`
	SHA  string `json:"sha"`
	URL  string `json:"url,omitempty"`
}

// GitCommit is the subset of a Git commit needed to construct a descendant commit.
type GitCommit struct {
	SHA     string      `json:"sha"`
	Tree    GitObject   `json:"tree"`
	Parents []GitObject `json:"parents,omitempty"`
}

// GitTree is a Git tree and its entries.
type GitTree struct {
	SHA       string         `json:"sha"`
	Truncated bool           `json:"truncated"`
	Tree      []GitTreeEntry `json:"tree"`
}

// GitTreeEntry describes one path in a Git tree.
type GitTreeEntry struct {
	Path string `json:"path"`
	Mode string `json:"mode"`
	Type string `json:"type"`
	SHA  string `json:"sha"`
	Size int64  `json:"size,omitempty"`
}

// GitBlob contains the base64-encoded content returned by the Git blobs endpoint.
type GitBlob struct {
	SHA      string `json:"sha"`
	Size     int64  `json:"size"`
	Encoding string `json:"encoding"`
	Content  string `json:"content"`
}

// File is a repository-relative file to place in a single Git commit.
// Mode may be 100644 (the default) or 100755.
type File struct {
	Path    string
	Content []byte
	Mode    string
}

// CommitResult identifies the commit and ref created by CommitFiles.
type CommitResult struct {
	CommitSHA   string
	TreeSHA     string
	PreviousSHA string
}

// Label is a repository issue label. Color is six hexadecimal digits without
// a leading '#'.
type Label struct {
	Name        string `json:"name"`
	Color       string `json:"color"`
	Description string `json:"description,omitempty"`
}

// Milestone is a repository milestone. Number is server-assigned; this
// extension matches milestones by Title, which GitHub does not deduplicate
// on its own but this contract requires to be unique.
type Milestone struct {
	Number      int    `json:"number,omitempty"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	State       string `json:"state,omitempty"`
	// DueOn is an RFC 3339 timestamp on the wire even though GitHub stores and
	// displays only the date part.
	DueOn string `json:"due_on,omitempty"`
}

// ImmutableReleases is the repository immutable release setting.
type ImmutableReleases struct {
	Enabled         bool `json:"enabled"`
	EnforcedByOwner bool `json:"enforced_by_owner"`
}

// AgentVariable represents a repository Copilot coding agent variable.
type AgentVariable struct {
	Name      string    `json:"name"`
	Value     string    `json:"value"`
	CreatedAt time.Time `json:"created_at,omitempty"`
	UpdatedAt time.Time `json:"updated_at,omitempty"`
}

// AgentVariableList is GitHub's paginated variable response.
type AgentVariableList struct {
	TotalCount int             `json:"total_count"`
	Variables  []AgentVariable `json:"variables"`
}

func (l *AgentVariableList) pageItems() []AgentVariable { return l.Variables } //nolint:unused // see customRepositoryRoleList.pageItems

// APIError is a typed, token-safe error response from GitHub.
type APIError struct {
	StatusCode int
	Method     string
	Path       string
	Message    string
	RequestID  string
	RetryAfter time.Duration
}

func (e *APIError) Error() string {
	if e.Message == "" {
		return e.Method + " " + e.Path + " returned HTTP " + itoa(e.StatusCode)
	}
	return e.Method + " " + e.Path + " returned HTTP " + itoa(e.StatusCode) + ": " + e.Message
}

// IsNotFound reports whether err is a GitHub 404 response. errors.As is used
// instead of a direct type assertion so a wrapped APIError (fmt.Errorf with
// %w) is still recognised.
func IsNotFound(err error) bool {
	var apiError *APIError
	return errors.As(err, &apiError) && apiError.StatusCode == 404
}
