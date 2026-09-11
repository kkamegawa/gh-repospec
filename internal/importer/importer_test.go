package importer

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/kkamegawa/gh-repospec/internal/config"
	"github.com/kkamegawa/gh-repospec/internal/githubapi"
	"gopkg.in/yaml.v3"
)

// fakeReader is a small, fully scriptable Reader. Every field defaults to a
// zero value that succeeds with an empty result, so a test only sets what it
// exercises.
type fakeReader struct {
	repository         githubapi.RepositoryInfo
	getRepositoryErr   error
	immutableReleases  githubapi.ImmutableReleases
	labels             []githubapi.Label
	milestones         []githubapi.Milestone
	rulesets           []githubapi.Ruleset
	rulesetDetail      map[int64]githubapi.Ruleset
	getRulesetErr      error
	actionsPermissions githubapi.ActionsPermissions
	selectedActions    githubapi.SelectedActions
	workflowPerms      githubapi.DefaultWorkflowPermissions
	forkApproval       githubapi.ForkPullRequestApproval
	getForkApprovalErr error
	actionVariables    githubapi.ActionVariableList
	actionSecrets      githubapi.SecretList
	environments       []githubapi.Environment
	vulnAlertsEnabled  bool
	securityFixes      bool
	privateVR          bool
	codeQL             githubapi.CodeQLDefaultSetupStatus
	getCodeQLErr       error
	aiScan             githubapi.AIScanSetting
	getAIScanErr       error
	agentVariables     githubapi.AgentVariableList
	agentSecrets       githubapi.SecretList
}

func (f *fakeReader) GetRepository(context.Context, githubapi.Repository) (githubapi.RepositoryInfo, error) {
	return f.repository, f.getRepositoryErr
}
func (f *fakeReader) GetImmutableReleases(context.Context, githubapi.Repository) (githubapi.ImmutableReleases, error) {
	return f.immutableReleases, nil
}
func (f *fakeReader) ListLabels(context.Context, githubapi.Repository) ([]githubapi.Label, error) {
	return f.labels, nil
}
func (f *fakeReader) ListMilestones(context.Context, githubapi.Repository) ([]githubapi.Milestone, error) {
	return f.milestones, nil
}
func (f *fakeReader) ListRulesets(context.Context, githubapi.Repository) ([]githubapi.Ruleset, error) {
	return f.rulesets, nil
}
func (f *fakeReader) GetRuleset(_ context.Context, _ githubapi.Repository, id int64) (githubapi.Ruleset, error) {
	if f.getRulesetErr != nil {
		return githubapi.Ruleset{}, f.getRulesetErr
	}
	return f.rulesetDetail[id], nil
}
func (f *fakeReader) GetActionsPermissions(context.Context, githubapi.Repository) (githubapi.ActionsPermissions, error) {
	return f.actionsPermissions, nil
}
func (f *fakeReader) GetSelectedActions(context.Context, githubapi.Repository) (githubapi.SelectedActions, error) {
	return f.selectedActions, nil
}
func (f *fakeReader) GetDefaultWorkflowPermissions(context.Context, githubapi.Repository) (githubapi.DefaultWorkflowPermissions, error) {
	return f.workflowPerms, nil
}
func (f *fakeReader) GetForkPullRequestApproval(context.Context, githubapi.Repository) (githubapi.ForkPullRequestApproval, error) {
	return f.forkApproval, f.getForkApprovalErr
}
func (f *fakeReader) ListActionVariables(context.Context, githubapi.Repository) (githubapi.ActionVariableList, error) {
	return f.actionVariables, nil
}
func (f *fakeReader) ListActionsSecrets(context.Context, githubapi.Repository) (githubapi.SecretList, error) {
	return f.actionSecrets, nil
}
func (f *fakeReader) ListEnvironments(context.Context, githubapi.Repository) ([]githubapi.Environment, error) {
	return f.environments, nil
}
func (f *fakeReader) GetVulnerabilityAlertsEnabled(context.Context, githubapi.Repository) (bool, error) {
	return f.vulnAlertsEnabled, nil
}
func (f *fakeReader) GetAutomatedSecurityFixesEnabled(context.Context, githubapi.Repository) (bool, error) {
	return f.securityFixes, nil
}
func (f *fakeReader) GetPrivateVulnerabilityReportingEnabled(context.Context, githubapi.Repository) (bool, error) {
	return f.privateVR, nil
}
func (f *fakeReader) GetCodeQLDefaultSetup(context.Context, githubapi.Repository) (githubapi.CodeQLDefaultSetupStatus, error) {
	return f.codeQL, f.getCodeQLErr
}
func (f *fakeReader) GetAIScan(context.Context, githubapi.Repository) (githubapi.AIScanSetting, error) {
	return f.aiScan, f.getAIScanErr
}
func (f *fakeReader) ListAgentVariables(context.Context, githubapi.Repository) (githubapi.AgentVariableList, error) {
	return f.agentVariables, nil
}
func (f *fakeReader) ListAgentSecrets(context.Context, githubapi.Repository) (githubapi.SecretList, error) {
	return f.agentSecrets, nil
}

var _ Reader = (*fakeReader)(nil)

func newFakeReader() *fakeReader {
	return &fakeReader{
		repository:    githubapi.RepositoryInfo{Visibility: "public"},
		rulesetDetail: map[int64]githubapi.Ruleset{},
		codeQL:        githubapi.CodeQLDefaultSetupStatus{State: "not-configured"},
		aiScan:        githubapi.AIScanSetting{PRScan: "disabled"},
	}
}

func testRepo() githubapi.Repository { return githubapi.Repository{Owner: "octo", Name: "demo"} }

func TestImportMapsRepositorySettings(t *testing.T) {
	t.Parallel()
	reader := newFakeReader()
	reader.repository = githubapi.RepositoryInfo{
		Description: "existing description", Homepage: "https://example.com",
		Visibility: "public", DefaultBranch: "main", Archived: false,
		Topics: []string{"b", "a"}, HasIssues: true, HasWiki: false,
		AllowMergeCommit: true, MergeCommitTitle: "PR_TITLE",
	}
	reader.immutableReleases = githubapi.ImmutableReleases{Enabled: true}

	result, err := Import(context.Background(), reader, testRepo())
	if err != nil {
		t.Fatal(err)
	}
	repo := result.Config.Repository
	if repo.Description == nil || *repo.Description != "existing description" {
		t.Fatalf("Description = %v, want a pointer to %q", repo.Description, "existing description")
	}
	if repo.Homepage == nil || *repo.Homepage != "https://example.com" {
		t.Fatalf("Homepage = %v", repo.Homepage)
	}
	if repo.Archived == nil || *repo.Archived {
		t.Fatalf("Archived = %v, want a pointer to false", repo.Archived)
	}
	if len(repo.Topics) != 2 {
		t.Fatalf("Topics = %v, want the two declared topics preserved", repo.Topics)
	}
	if repo.Features == nil || repo.Features.Issues == nil || !*repo.Features.Issues {
		t.Fatalf("Features.Issues = %v, want true", repo.Features)
	}
	if repo.Features.Wiki == nil || *repo.Features.Wiki {
		t.Fatalf("Features.Wiki = %v, want false", repo.Features.Wiki)
	}
	if repo.ReleaseImmutability == nil || !*repo.ReleaseImmutability {
		t.Fatalf("ReleaseImmutability = %v, want true", repo.ReleaseImmutability)
	}
	if repo.MergeCommitTitle != "PR_TITLE" {
		t.Fatalf("MergeCommitTitle = %q", repo.MergeCommitTitle)
	}
	// Local intent must never be inferred from repository state.
	if repo.Create {
		t.Fatal("Create was set from repository state; it is local intent")
	}
	if repo.AllowVisibilityChange {
		t.Fatal("AllowVisibilityChange was set from repository state; it is local intent")
	}
}

func TestImportDeclaresEmptyTopicsExplicitly(t *testing.T) {
	t.Parallel()
	reader := newFakeReader()
	result, err := Import(context.Background(), reader, testRepo())
	if err != nil {
		t.Fatal(err)
	}
	if result.Config.Repository.Topics == nil {
		t.Fatal("Topics was nil, want a non-nil empty slice so an empty topic set is actively declared, not left unmanaged")
	}
	if len(result.Config.Repository.Topics) != 0 {
		t.Fatalf("Topics = %v, want empty", result.Config.Repository.Topics)
	}
}

func TestImportMapsLabelsAndMilestonesSortedWithDateOnlyDueOn(t *testing.T) {
	t.Parallel()
	reader := newFakeReader()
	reader.labels = []githubapi.Label{
		{Name: "zeta", Color: "ffffff"},
		{Name: "alpha", Color: "d73a4a", Description: "first"},
	}
	reader.milestones = []githubapi.Milestone{
		{Title: "v2", State: "open"},
		{Title: "v1", State: "closed", DueOn: "2026-12-01T00:00:00Z"},
	}

	result, err := Import(context.Background(), reader, testRepo())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Config.Labels) != 2 || result.Config.Labels[0].Name != "alpha" || result.Config.Labels[1].Name != "zeta" {
		t.Fatalf("Labels = %#v, want alpha then zeta", result.Config.Labels)
	}
	if len(result.Config.Milestones) != 2 || result.Config.Milestones[0].Title != "v1" {
		t.Fatalf("Milestones = %#v, want sorted by title", result.Config.Milestones)
	}
	if result.Config.Milestones[0].DueOn != "2026-12-01" {
		t.Fatalf("DueOn = %q, want the date part only", result.Config.Milestones[0].DueOn)
	}
}

func TestImportMapsRulesetDetailNotJustTheListSummary(t *testing.T) {
	t.Parallel()
	reader := newFakeReader()
	reader.rulesets = []githubapi.Ruleset{{ID: 7, Name: "protect-main", Target: "branch", Enforcement: "active"}}
	reader.rulesetDetail[7] = githubapi.Ruleset{
		ID: 7, Name: "protect-main", Target: "branch", Enforcement: "active",
		Conditions: map[string]any{"ref_name": map[string]any{"include": []any{"~DEFAULT_BRANCH"}, "exclude": []any{}}},
		Rules:      []map[string]any{{"type": "deletion"}},
		BypassActors: []githubapi.BypassActor{
			{ActorID: 5, ActorType: "RepositoryRole", BypassMode: "always"},
		},
	}

	result, err := Import(context.Background(), reader, testRepo())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Config.Rulesets) != 1 {
		t.Fatalf("Rulesets = %#v, want one ruleset", result.Config.Rulesets)
	}
	ruleset := result.Config.Rulesets[0]
	if ruleset.Name != "protect-main" || len(ruleset.Include) != 1 || ruleset.Include[0] != "~DEFAULT_BRANCH" {
		t.Fatalf("ruleset = %#v", ruleset)
	}
	if len(ruleset.Rules) != 1 || ruleset.Rules[0].Type != "deletion" {
		t.Fatalf("Rules = %#v, want the deletion rule the list summary omits", ruleset.Rules)
	}
	if len(ruleset.BypassActors) != 1 || ruleset.BypassActors[0].Role != "admin" {
		t.Fatalf("BypassActors = %#v, want role admin for actor ID 5", ruleset.BypassActors)
	}
}

// TestImportNormalizesRuleParametersForByteStableOutput guards against the
// two ways decoded JSON rule parameters could make import output vary
// between runs of the same live state: a whole number decoding as float64
// (which must read back as an integer, not 5 vs 5.0), and Go's randomized
// map iteration order (which encoding/json and yaml.v3 already resolve by
// always encoding a map's keys sorted — this test locks that combination in
// rather than trusting it silently).
func TestImportNormalizesRuleParametersForByteStableOutput(t *testing.T) {
	t.Parallel()
	reader := newFakeReader()
	reader.rulesets = []githubapi.Ruleset{{ID: 1, Name: "r", Target: "branch", Enforcement: "active"}}
	reader.rulesetDetail[1] = githubapi.Ruleset{
		Name: "r", Target: "branch", Enforcement: "active",
		Conditions: map[string]any{"ref_name": map[string]any{"include": []any{"~ALL"}}},
		Rules: []map[string]any{{
			"type": "required_status_checks",
			"parameters": map[string]any{
				"zebra_max_count": float64(5),   // whole number: must import as an int, not 5.0
				"apple_ratio":     float64(1.5), // genuine fraction: must be preserved exactly
				"mango_nested":    map[string]any{"count": float64(3)},
			},
		}},
	}

	result, err := Import(context.Background(), reader, testRepo())
	if err != nil {
		t.Fatal(err)
	}
	params := result.Config.Rulesets[0].Rules[0].Parameters
	if v, ok := params["zebra_max_count"].(int64); !ok || v != 5 {
		t.Fatalf("zebra_max_count = %#v, want int64(5)", params["zebra_max_count"])
	}
	if v, ok := params["apple_ratio"].(float64); !ok || v != 1.5 {
		t.Fatalf("apple_ratio = %#v, want float64(1.5) preserved", params["apple_ratio"])
	}
	nested, ok := params["mango_nested"].(map[string]any)
	if !ok {
		t.Fatalf("mango_nested = %#v, want a nested map", params["mango_nested"])
	}
	if v, ok := nested["count"].(int64); !ok || v != 3 {
		t.Fatalf("mango_nested.count = %#v, want int64(3), normalization must recurse", nested["count"])
	}

	// Marshal twice and confirm byte-identical output despite Go's
	// randomized map iteration order — this is the actual guarantee a
	// repeated import needs, not just the typed assertions above.
	first, err := yaml.Marshal(result.Config)
	if err != nil {
		t.Fatal(err)
	}
	second, err := yaml.Marshal(result.Config)
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Fatalf("two marshals of the same import result differ:\n--- first ---\n%s\n--- second ---\n%s", first, second)
	}
	if strings.Contains(string(first), "5.0") {
		t.Fatalf("output = %s, want the whole-number parameter rendered as 5, not 5.0", first)
	}
}

func TestImportReversesEveryBypassActorType(t *testing.T) {
	t.Parallel()
	reader := newFakeReader()
	include := map[string]any{"ref_name": map[string]any{"include": []any{"~ALL"}}}
	reader.rulesets = []githubapi.Ruleset{{ID: 1, Name: "r", Target: "branch", Enforcement: "active"}}
	reader.rulesetDetail[1] = githubapi.Ruleset{
		Name: "r", Target: "branch", Enforcement: "active", Conditions: include,
		BypassActors: []githubapi.BypassActor{
			{ActorID: 2, ActorType: "RepositoryRole", BypassMode: "always"},   // maintain
			{ActorID: 4, ActorType: "RepositoryRole", BypassMode: "always"},   // write
			{ActorID: 5, ActorType: "RepositoryRole", BypassMode: "always"},   // admin
			{ActorID: 900, ActorType: "RepositoryRole", BypassMode: "always"}, // custom role
			{ActorID: 1, ActorType: "OrganizationAdmin", BypassMode: "always"},
			{ActorID: 42, ActorType: "Team", BypassMode: "always"},
			{ActorID: 7, ActorType: "Integration", BypassMode: "pull_request"},
		},
	}

	result, err := Import(context.Background(), reader, testRepo())
	if err != nil {
		t.Fatal(err)
	}
	actors := result.Config.Rulesets[0].BypassActors
	if len(actors) != 7 {
		t.Fatalf("len(actors) = %d, want 7", len(actors))
	}
	want := []config.BypassActorConfig{
		{Role: "maintain", BypassMode: "always"},
		{Role: "write", BypassMode: "always"},
		{Role: "admin", BypassMode: "always"},
		{CustomRole: "id:900", BypassMode: "always"},
		{OrgAdmin: true, BypassMode: "always"},
		{Team: "id:42", BypassMode: "always"},
		{App: "id:7", BypassMode: "pull_request"},
	}
	for i, w := range want {
		if actors[i] != w {
			t.Fatalf("actors[%d] = %#v, want %#v", i, actors[i], w)
		}
	}
}

func TestImportReportsInheritedRulesetAsDiagnosticAndSkipsIt(t *testing.T) {
	t.Parallel()
	reader := newFakeReader()
	reader.rulesets = []githubapi.Ruleset{
		{ID: 1, Name: "org-wide", Target: "branch", Enforcement: "active", SourceType: "Organization"},
	}

	result, err := Import(context.Background(), reader, testRepo())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Config.Rulesets) != 0 {
		t.Fatalf("Rulesets = %#v, want the inherited ruleset omitted", result.Config.Rulesets)
	}
	if !hasDiagnostic(result.Diagnostics, "ruleset.org-wide") {
		t.Fatalf("Diagnostics = %#v, want one for ruleset.org-wide", result.Diagnostics)
	}
}

func TestImportReportsUnrepresentableRulesetWithoutRefNameInclude(t *testing.T) {
	t.Parallel()
	reader := newFakeReader()
	reader.rulesets = []githubapi.Ruleset{{ID: 1, Name: "push-rule", Target: "push", Enforcement: "active"}}
	reader.rulesetDetail[1] = githubapi.Ruleset{Name: "push-rule", Target: "push", Enforcement: "active"}

	result, err := Import(context.Background(), reader, testRepo())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Config.Rulesets) != 0 {
		t.Fatalf("Rulesets = %#v, want the unrepresentable ruleset omitted", result.Config.Rulesets)
	}
	if !hasDiagnostic(result.Diagnostics, "ruleset.push-rule") {
		t.Fatalf("Diagnostics = %#v, want one for ruleset.push-rule", result.Diagnostics)
	}
}

func TestImportActionsFetchesSelectedActionsOnlyWhenSelected(t *testing.T) {
	t.Parallel()
	reader := newFakeReader()
	reader.actionsPermissions = githubapi.ActionsPermissions{Enabled: true, AllowedActions: "all"}

	result, err := Import(context.Background(), reader, testRepo())
	if err != nil {
		t.Fatal(err)
	}
	if result.Config.Actions.SelectedActions != nil {
		t.Fatalf("SelectedActions = %#v, want nil when allowedActions is not \"selected\"", result.Config.Actions.SelectedActions)
	}

	reader.actionsPermissions = githubapi.ActionsPermissions{Enabled: true, AllowedActions: "selected"}
	reader.selectedActions = githubapi.SelectedActions{GitHubOwnedAllowed: true, PatternsAllowed: []string{"actions/checkout@*"}}
	result, err = Import(context.Background(), reader, testRepo())
	if err != nil {
		t.Fatal(err)
	}
	if result.Config.Actions.SelectedActions == nil || !result.Config.Actions.SelectedActions.GitHubOwnedAllowed {
		t.Fatalf("SelectedActions = %#v, want the fetched allowlist", result.Config.Actions.SelectedActions)
	}
}

func TestImportSkipsForkApprovalForPrivateRepositoryAndReportsWhy(t *testing.T) {
	t.Parallel()
	reader := newFakeReader()
	reader.repository = githubapi.RepositoryInfo{Visibility: "private", Private: true}
	reader.getForkApprovalErr = errors.New("GitHub would reject this on a private repository")

	result, err := Import(context.Background(), reader, testRepo())
	if err != nil {
		t.Fatal(err)
	}
	if result.Config.Actions.ForkPullRequestApproval != "" {
		t.Fatalf("ForkPullRequestApproval = %q, want empty for a private repository", result.Config.Actions.ForkPullRequestApproval)
	}
	if !hasDiagnostic(result.Diagnostics, "actions.forkPullRequestApproval") {
		t.Fatalf("Diagnostics = %#v, want one for actions.forkPullRequestApproval", result.Diagnostics)
	}
}

func TestImportReportsActionsAndAgentSecretNamesAsDiagnosticsWithoutValues(t *testing.T) {
	t.Parallel()
	reader := newFakeReader()
	reader.actionSecrets = githubapi.SecretList{Secrets: []githubapi.SecretMetadata{{Name: "RELEASE_TOKEN"}}}
	reader.agentSecrets = githubapi.SecretList{Secrets: []githubapi.SecretMetadata{{Name: "AGENT_TOKEN"}}}
	reader.agentVariables = githubapi.AgentVariableList{Variables: []githubapi.AgentVariable{{Name: "REGION", Value: "us-east-1"}}}

	result, err := Import(context.Background(), reader, testRepo())
	if err != nil {
		t.Fatal(err)
	}
	if !hasDiagnostic(result.Diagnostics, "actions.secret.RELEASE_TOKEN") {
		t.Fatalf("Diagnostics = %#v, want actions.secret.RELEASE_TOKEN", result.Diagnostics)
	}
	if !hasDiagnostic(result.Diagnostics, "agents.secret.AGENT_TOKEN") {
		t.Fatalf("Diagnostics = %#v, want agents.secret.AGENT_TOKEN", result.Diagnostics)
	}
	// Confirm no secret value ever reaches the configuration itself.
	marshaled, _ := yaml.Marshal(result.Config)
	if strings.Contains(string(marshaled), "RELEASE_TOKEN") || strings.Contains(string(marshaled), "AGENT_TOKEN") {
		t.Fatalf("secret names leaked into the marshaled configuration:\n%s", marshaled)
	}
}

func TestImportOmitsAgentsSectionWhenThereAreNoAgentVariables(t *testing.T) {
	t.Parallel()
	reader := newFakeReader()
	result, err := Import(context.Background(), reader, testRepo())
	if err != nil {
		t.Fatal(err)
	}
	if result.Config.Agents != nil {
		t.Fatalf("Agents = %#v, want nil when there are no agent variables", result.Config.Agents)
	}
}

func TestImportSecurityFeatures(t *testing.T) {
	t.Parallel()
	reader := newFakeReader()
	reader.vulnAlertsEnabled = true
	reader.securityFixes = false
	reader.privateVR = true
	reader.codeQL = githubapi.CodeQLDefaultSetupStatus{State: "configured"}
	reader.aiScan = githubapi.AIScanSetting{PRScan: "enabled"}
	reader.repository = githubapi.RepositoryInfo{
		Visibility: "public",
		SecurityAndAnalysis: &githubapi.SecurityAndAnalysisStatus{
			SecretScanning:            &githubapi.SecurityFeature{Status: "enabled"},
			SecretScanningPushProtect: &githubapi.SecurityFeature{Status: "disabled"},
		},
	}

	result, err := Import(context.Background(), reader, testRepo())
	if err != nil {
		t.Fatal(err)
	}
	security := result.Config.Security
	if security.DependabotAlerts == nil || !security.DependabotAlerts.Enabled {
		t.Fatalf("DependabotAlerts = %#v", security.DependabotAlerts)
	}
	if security.DependabotSecurityUpdates == nil || security.DependabotSecurityUpdates.Enabled {
		t.Fatalf("DependabotSecurityUpdates = %#v", security.DependabotSecurityUpdates)
	}
	if security.SecretScanning == nil || !security.SecretScanning.Enabled {
		t.Fatalf("SecretScanning = %#v", security.SecretScanning)
	}
	if security.PushProtection == nil || security.PushProtection.Enabled {
		t.Fatalf("PushProtection = %#v", security.PushProtection)
	}
	if security.CodeQLDefaultSetup == nil || !security.CodeQLDefaultSetup.Enabled {
		t.Fatalf("CodeQLDefaultSetup = %#v", security.CodeQLDefaultSetup)
	}
	if security.PrivateVulnerabilityReporting == nil || !security.PrivateVulnerabilityReporting.Enabled {
		t.Fatalf("PrivateVulnerabilityReporting = %#v", security.PrivateVulnerabilityReporting)
	}
	if security.AIPullRequestScan == nil || !security.AIPullRequestScan.Enabled {
		t.Fatalf("AIPullRequestScan = %#v", security.AIPullRequestScan)
	}
	if !hasDiagnostic(result.Diagnostics, "security.dependencyGraph") {
		t.Fatalf("Diagnostics = %#v, want security.dependencyGraph (no read endpoint exists)", result.Diagnostics)
	}
	if hasDiagnostic(result.Diagnostics, "security.aiPullRequestScan") {
		t.Fatalf("Diagnostics = %#v, want no security.aiPullRequestScan diagnostic when its state was read", result.Diagnostics)
	}
}

func TestImportMapsAIPullRequestScanStatus(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		prScan string
		want   bool
	}{
		{prScan: "enabled", want: true},
		{prScan: "disabled", want: false},
	} {
		t.Run(tc.prScan, func(t *testing.T) {
			t.Parallel()
			reader := newFakeReader()
			reader.aiScan = githubapi.AIScanSetting{PRScan: tc.prScan}

			result, err := Import(context.Background(), reader, testRepo())
			if err != nil {
				t.Fatal(err)
			}
			got := result.Config.Security.AIPullRequestScan
			if got == nil || got.Enabled != tc.want || got.Optional {
				t.Fatalf("AIPullRequestScan = %#v, want enabled=%t with optional left unset (local intent)", got, tc.want)
			}
			if hasDiagnostic(result.Diagnostics, "security.aiPullRequestScan") {
				t.Fatalf("Diagnostics = %#v, want none for security.aiPullRequestScan", result.Diagnostics)
			}
		})
	}
}

// TestImportReportsUnreadableAIPullRequestScanInsteadOfWritingFalse guards
// the design rule that a feature whose state cannot be read is reported,
// not written as disabled: GitHub answers 403 when Advanced Security is not
// enabled, and an imported enabled: false would never converge.
func TestImportReportsUnreadableAIPullRequestScanInsteadOfWritingFalse(t *testing.T) {
	t.Parallel()
	for _, status := range []int{403, 404} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			t.Parallel()
			reader := newFakeReader()
			reader.getAIScanErr = &githubapi.APIError{StatusCode: status}

			result, err := Import(context.Background(), reader, testRepo())
			if err != nil {
				t.Fatalf("Import() error = %v, want HTTP %d reported as a diagnostic", err, status)
			}
			if result.Config.Security.AIPullRequestScan != nil {
				t.Fatalf("AIPullRequestScan = %#v, want nil when GitHub answered HTTP %d", result.Config.Security.AIPullRequestScan, status)
			}
			if !hasDiagnostic(result.Diagnostics, "security.aiPullRequestScan") {
				t.Fatalf("Diagnostics = %#v, want one for security.aiPullRequestScan", result.Diagnostics)
			}
		})
	}
}

func TestImportAIPullRequestScanServerErrorIsAnError(t *testing.T) {
	t.Parallel()
	reader := newFakeReader()
	reader.getAIScanErr = &githubapi.APIError{StatusCode: 500}

	_, err := Import(context.Background(), reader, testRepo())
	if err == nil {
		t.Fatal("Import() unexpectedly succeeded despite an HTTP 500 from the AI Scan read")
	}
	if !strings.Contains(err.Error(), "AI Scan for pull requests") {
		t.Fatalf("Import() error = %v, want it to name the AI Scan read", err)
	}
}

func TestImportTreatsCodeQL404AsNotConfigured(t *testing.T) {
	t.Parallel()
	reader := newFakeReader()
	reader.getCodeQLErr = &githubapi.APIError{StatusCode: 404}

	result, err := Import(context.Background(), reader, testRepo())
	if err != nil {
		t.Fatal(err)
	}
	if result.Config.Security.CodeQLDefaultSetup == nil || result.Config.Security.CodeQLDefaultSetup.Enabled {
		t.Fatalf("CodeQLDefaultSetup = %#v, want disabled on a 404", result.Config.Security.CodeQLDefaultSetup)
	}
}

func TestImportReportsSecurityFieldsAbsentFromSecurityAndAnalysis(t *testing.T) {
	t.Parallel()
	reader := newFakeReader() // SecurityAndAnalysis left nil
	result, err := Import(context.Background(), reader, testRepo())
	if err != nil {
		t.Fatal(err)
	}
	if result.Config.Security.SecretScanning != nil {
		t.Fatalf("SecretScanning = %#v, want nil when GitHub reported no security_and_analysis block", result.Config.Security.SecretScanning)
	}
	if !hasDiagnostic(result.Diagnostics, "security.secretScanning") || !hasDiagnostic(result.Diagnostics, "security.pushProtection") {
		t.Fatalf("Diagnostics = %#v, want secretScanning and pushProtection reported", result.Diagnostics)
	}
}

// TestImportPropagatesAPIFailureAsError guards the distinction the design
// draws between structurally unavailable state (a diagnostic) and an actual
// authorization or communication failure (an error): the latter must never
// be silently downgraded into a diagnostic that lets Import return a
// misleadingly complete result.
func TestImportPropagatesAPIFailureAsError(t *testing.T) {
	t.Parallel()
	reader := newFakeReader()
	reader.getRepositoryErr = &githubapi.APIError{StatusCode: 403}

	_, err := Import(context.Background(), reader, testRepo())
	if err == nil {
		t.Fatal("Import() unexpectedly succeeded despite a GetRepository failure")
	}
}

func TestImportRulesetDetailFetchFailureIsAnError(t *testing.T) {
	t.Parallel()
	reader := newFakeReader()
	reader.rulesets = []githubapi.Ruleset{{ID: 1, Name: "r", Target: "branch", Enforcement: "active"}}
	reader.getRulesetErr = errors.New("network error")

	_, err := Import(context.Background(), reader, testRepo())
	if err == nil {
		t.Fatal("Import() unexpectedly succeeded despite a GetRuleset failure")
	}
}

// TestImportOutputReloadsThroughConfigLoad guards the contract that import
// output is a valid, strictly parseable schemaVersion 1 document: every
// field Import can set must exist in the schema and decode back unchanged.
func TestImportOutputReloadsThroughConfigLoad(t *testing.T) {
	t.Parallel()
	reader := newFakeReader()
	reader.repository = githubapi.RepositoryInfo{
		Description: "d", Homepage: "https://example.com", Visibility: "public",
		DefaultBranch: "main", Topics: []string{"go"},
		SecurityAndAnalysis: &githubapi.SecurityAndAnalysisStatus{
			SecretScanning:            &githubapi.SecurityFeature{Status: "enabled"},
			SecretScanningPushProtect: &githubapi.SecurityFeature{Status: "enabled"},
		},
	}
	reader.labels = []githubapi.Label{{Name: "bug", Color: "d73a4a"}}
	reader.milestones = []githubapi.Milestone{{Title: "v1", State: "open", DueOn: "2026-01-01T00:00:00Z"}}
	reader.rulesets = []githubapi.Ruleset{{ID: 3, Name: "protect", Target: "branch", Enforcement: "active"}}
	reader.rulesetDetail[3] = githubapi.Ruleset{
		Name: "protect", Target: "branch", Enforcement: "active",
		Conditions: map[string]any{"ref_name": map[string]any{"include": []any{"~DEFAULT_BRANCH"}}},
		Rules:      []map[string]any{{"type": "deletion"}},
		BypassActors: []githubapi.BypassActor{
			{ActorID: 42, ActorType: "Team", BypassMode: "always"},
		},
	}
	reader.actionVariables = githubapi.ActionVariableList{Variables: []githubapi.ActionVariable{{Name: "V", Value: "1"}}}
	reader.environments = []githubapi.Environment{{Name: "production"}}
	reader.agentVariables = githubapi.AgentVariableList{Variables: []githubapi.AgentVariable{{Name: "A", Value: "1"}}}
	reader.aiScan = githubapi.AIScanSetting{PRScan: "enabled"}

	result, err := Import(context.Background(), reader, testRepo())
	if err != nil {
		t.Fatal(err)
	}

	out, err := yaml.Marshal(result.Config)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "imported.yaml")
	if err := os.WriteFile(path, out, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := config.Load(path); err != nil {
		t.Fatalf("config.Load(import output) error = %v\n--- document ---\n%s", err, out)
	}
}

func hasDiagnostic(diagnostics []Diagnostic, resource string) bool {
	for _, d := range diagnostics {
		if d.Resource == resource {
			return true
		}
	}
	return false
}
