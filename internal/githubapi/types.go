// Package githubapi contains the GitHub REST operations used by the planner and applier.
package githubapi

import "time"

// APIVersion is sent with every request so GitHub REST behavior is stable.
const APIVersion = "2026-03-10"

// Repository identifies a GitHub repository.
type Repository struct {
	Owner string
	Name  string
}

// String returns the canonical owner/name form.
func (r Repository) String() string { return r.Owner + "/" + r.Name }

// RepositoryInfo is the subset of repository state needed by the planner.
type RepositoryInfo struct {
	ID                  int64    `json:"id"`
	Owner               Owner    `json:"owner"`
	Name                string   `json:"name"`
	FullName            string   `json:"full_name"`
	Description         string   `json:"description"`
	Private             bool     `json:"private"`
	Visibility          string   `json:"visibility"`
	DefaultBranch       string   `json:"default_branch"`
	Topics              []string `json:"topics"`
	AllowSquashMerge    bool     `json:"allow_squash_merge"`
	AllowMergeCommit    bool     `json:"allow_merge_commit"`
	AllowRebaseMerge    bool     `json:"allow_rebase_merge"`
	AllowAutoMerge      bool     `json:"allow_auto_merge"`
	DeleteBranchOnMerge bool     `json:"delete_branch_on_merge"`
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
type UpdateRepositoryRequest struct {
	Description         *string `json:"description,omitempty"`
	Private             *bool   `json:"private,omitempty"`
	Visibility          string  `json:"visibility,omitempty"`
	DefaultBranch       string  `json:"default_branch,omitempty"`
	AllowSquashMerge    *bool   `json:"allow_squash_merge,omitempty"`
	AllowMergeCommit    *bool   `json:"allow_merge_commit,omitempty"`
	AllowRebaseMerge    *bool   `json:"allow_rebase_merge,omitempty"`
	AllowAutoMerge      *bool   `json:"allow_auto_merge,omitempty"`
	DeleteBranchOnMerge *bool   `json:"delete_branch_on_merge,omitempty"`
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

// Ruleset is an intentionally flexible representation of a repository ruleset.
// Rules and conditions retain GitHub's API shape without losing new rule types.
type Ruleset struct {
	ID           int64            `json:"id,omitempty"`
	Name         string           `json:"name"`
	Target       string           `json:"target,omitempty"`
	Enforcement  string           `json:"enforcement"`
	Conditions   map[string]any   `json:"conditions,omitempty"`
	Rules        []map[string]any `json:"rules,omitempty"`
	BypassActors []map[string]any `json:"bypass_actors,omitempty"`
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

// IsNotFound reports whether err is a GitHub 404 response.
func IsNotFound(err error) bool {
	e, ok := err.(*APIError)
	return ok && e.StatusCode == 404
}
