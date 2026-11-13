package personal

import (
	"context"
	"errors"
	"testing"

	"github.com/kkamegawa/gh-repospec/internal/config"
	"github.com/kkamegawa/gh-repospec/internal/githubapi"
	"github.com/kkamegawa/gh-repospec/internal/planner"
)

type fakeService struct {
	user          githubapi.Owner
	types         []githubapi.PersonalIssueType
	fields        []githubapi.PersonalIssueField
	listCalls     int
	writes        int
	lastField     githubapi.PersonalIssueField
	fieldUpdateID int64
	writeError    error
}

func (f *fakeService) GetAuthenticatedUser(context.Context) (githubapi.Owner, error) {
	return f.user, nil
}
func (f *fakeService) ListPersonalIssueTypes(context.Context) ([]githubapi.PersonalIssueType, error) {
	f.listCalls++
	return f.types, nil
}
func (f *fakeService) ListPersonalIssueFields(context.Context) ([]githubapi.PersonalIssueField, error) {
	f.listCalls++
	return f.fields, nil
}
func (f *fakeService) CreatePersonalIssueType(_ context.Context, value githubapi.PersonalIssueType) error {
	f.writes++
	if f.writeError != nil {
		return f.writeError
	}
	f.types = append(f.types, value)
	return nil
}
func (f *fakeService) UpdatePersonalIssueType(_ context.Context, id int64, value githubapi.PersonalIssueType) error {
	f.writes++
	if f.writeError != nil {
		return f.writeError
	}
	for i := range f.types {
		if f.types[i].ID == id {
			value.ID = id
			f.types[i] = value
		}
	}
	return nil
}
func (f *fakeService) CreatePersonalIssueField(_ context.Context, value githubapi.PersonalIssueField) error {
	f.writes++
	if f.writeError != nil {
		return f.writeError
	}
	f.fields = append(f.fields, value)
	return nil
}
func (f *fakeService) UpdatePersonalIssueField(_ context.Context, id int64, value githubapi.PersonalIssueField) error {
	f.writes++
	f.fieldUpdateID = id
	f.lastField = value
	if f.writeError != nil {
		return f.writeError
	}
	for i := range f.fields {
		if f.fields[i].ID != id {
			continue
		}
		f.fields[i].Name = value.Name
		if value.Description != nil {
			f.fields[i].Description = value.Description
		}
		if value.Visibility != nil {
			f.fields[i].Visibility = value.Visibility
		}
		if value.Options != nil {
			f.fields[i].Options = value.Options
		}
	}
	return nil
}

func testConfig() *config.PersonalConfig {
	enabled := true
	visibility := "restricted"
	priority := 1
	return &config.PersonalConfig{
		SchemaVersion: config.SchemaVersion2,
		Scope:         config.ScopePersonal,
		Account:       "octocat",
		IssueTypes: []config.IssueTypeConfig{{
			Name: "Bug", IsEnabled: &enabled,
		}},
		IssueFields: []config.IssueFieldConfig{{
			Name: "Priority", DataType: "single_select", Visibility: &visibility,
			Options: []config.IssueFieldOptionConfig{{Name: "High", Color: "red", Priority: &priority}},
		}},
	}
}

func TestBuildChecksAccountBeforeListingSettings(t *testing.T) {
	service := &fakeService{user: githubapi.Owner{Login: "somebody-else"}}
	_, err := New(service).Build(context.Background(), testConfig())
	if err == nil {
		t.Fatal("Build succeeded for a different authenticated account")
	}
	if service.listCalls != 0 || service.writes != 0 {
		t.Fatalf("settings were accessed after account mismatch: list=%d writes=%d", service.listCalls, service.writes)
	}
}

func TestApplyPreservesOmittedDefinitionsAndOptions(t *testing.T) {
	priority := 1
	otherPriority := 2
	enabled := true
	visibility := "restricted"
	service := &fakeService{
		user: githubapi.Owner{Login: "octocat"},
		types: []githubapi.PersonalIssueType{
			{ID: 17, Name: "Bug", IsEnabled: &enabled},
			{ID: 18, Name: "Feature", IsEnabled: &enabled},
		},
		fields: []githubapi.PersonalIssueField{{
			ID: 27, Name: "Priority", DataType: "single_select", Visibility: &visibility,
			Options: []githubapi.PersonalIssueFieldOption{
				{ID: 37, Name: "High", Color: "blue", Priority: &priority},
				{ID: 38, Name: "Low", Color: "gray", Priority: &otherPriority},
			},
		}},
	}
	cfg := testConfig()
	plan, err := New(service).Build(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	if plan.DriftCount() != 1 || plan.HasBlocked() {
		t.Fatalf("plan = %+v, want one update and no blocked changes", plan)
	}
	if err := New(service).Apply(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	if service.writes != 1 || service.fieldUpdateID != 27 {
		t.Fatalf("writes=%d field id=%d, want one field update for ID 27", service.writes, service.fieldUpdateID)
	}
	if len(service.lastField.Options) != 2 {
		t.Fatalf("update options = %+v, want configured and omitted options retained", service.lastField.Options)
	}
	if service.lastField.Options[0].ID != 37 || service.lastField.Options[1].ID != 38 {
		t.Fatalf("option IDs were not retained: %+v", service.lastField.Options)
	}
	if err := New(service).Apply(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	if service.writes != 1 {
		t.Fatalf("repeat apply made %d additional writes", service.writes-1)
	}
}

func TestBuildBlocksDataTypeChangesBeforeAnyWrite(t *testing.T) {
	enabled := true
	visibility := "restricted"
	service := &fakeService{
		user:   githubapi.Owner{Login: "octocat"},
		types:  []githubapi.PersonalIssueType{{ID: 1, Name: "Bug", IsEnabled: &enabled}},
		fields: []githubapi.PersonalIssueField{{ID: 2, Name: "Priority", DataType: "text", Visibility: &visibility}},
	}
	cfg := testConfig()
	plan, err := New(service).Build(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !plan.HasBlocked() {
		t.Fatalf("plan = %+v, want blocked field type change", plan)
	}
	if err := New(service).Apply(context.Background(), cfg); err == nil {
		t.Fatal("Apply succeeded with a blocked field type change")
	}
	if service.writes != 0 {
		t.Fatalf("Apply made %d writes despite a blocked change", service.writes)
	}
}

func TestImportReturnsAuthenticatedAccountAndAllDefinitions(t *testing.T) {
	enabled := true
	visibility := "restricted"
	priority := 1
	service := &fakeService{
		user:  githubapi.Owner{Login: "octocat"},
		types: []githubapi.PersonalIssueType{{Name: "Bug", Description: stringPointer("A bug"), IsEnabled: &enabled}},
		fields: []githubapi.PersonalIssueField{{
			Name: "Priority", DataType: "single_select", Visibility: &visibility,
			Options: []githubapi.PersonalIssueFieldOption{{Name: "High", Color: "red", Priority: &priority}},
		}},
	}
	cfg, err := New(service).Import(context.Background(), "OCTOCAT")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Account != "octocat" || cfg.SchemaVersion != config.SchemaVersion2 || cfg.Scope != config.ScopePersonal {
		t.Fatalf("import header = %+v", cfg)
	}
	if len(cfg.IssueTypes) != 1 || len(cfg.IssueFields) != 1 || len(cfg.IssueFields[0].Options) != 1 {
		t.Fatalf("imported definitions = %+v", cfg)
	}
}

func TestImportRejectsAccountMismatchBeforeListing(t *testing.T) {
	service := &fakeService{user: githubapi.Owner{Login: "octocat"}}
	_, err := New(service).Import(context.Background(), "different")
	if err == nil {
		t.Fatal("Import succeeded for a different account")
	}
	if service.listCalls != 0 {
		t.Fatalf("Import listed settings %d times after mismatch", service.listCalls)
	}
}

func TestImportAndPlanConvergeWithScalarAndSelectFields(t *testing.T) {
	enabled := true
	visibility := "restricted"
	priority := 1
	service := &fakeService{
		user:  githubapi.Owner{Login: "octocat"},
		types: []githubapi.PersonalIssueType{{ID: 1, Name: "Bug", IsEnabled: &enabled}},
		fields: []githubapi.PersonalIssueField{
			{ID: 2, Name: "Target date", DataType: "date", Visibility: &visibility},
			{ID: 3, Name: "Priority", DataType: "single_select", Visibility: &visibility, Options: []githubapi.PersonalIssueFieldOption{
				{ID: 4, Name: "High", Color: "red", Priority: &priority},
			}},
		},
	}
	ctx := context.Background()
	engine := New(service)
	cfg, err := engine.Import(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	plan, err := engine.Build(ctx, cfg)
	if err != nil || plan.HasBlocked() || plan.DriftCount() != 0 {
		t.Fatalf("plan = %+v, err = %v", plan, err)
	}
	if err := engine.Apply(ctx, cfg); err != nil {
		t.Fatal(err)
	}
	if service.writes != 0 {
		t.Fatalf("applying imported state made %d writes", service.writes)
	}
}

func TestApplyReturnsWriteError(t *testing.T) {
	service := &fakeService{user: githubapi.Owner{Login: "octocat"}, writeError: errors.New("write failed")}
	if err := New(service).Apply(context.Background(), testConfig()); err == nil {
		t.Fatal("Apply succeeded after the API rejected a write")
	}
	if service.writes != 1 {
		t.Fatalf("writes=%d, want the first failed write only", service.writes)
	}
}

func TestPlanDriftCountsOnlyWrites(t *testing.T) {
	plan := Plan{Changes: []planner.Change{
		{State: planner.ChangeCreate},
		{State: planner.ChangeUpdate},
		{State: planner.ChangeNoop},
		{State: planner.ChangeBlocked},
	}}
	if plan.DriftCount() != 2 || !plan.HasBlocked() {
		t.Fatalf("plan drift=%d blocked=%t", plan.DriftCount(), plan.HasBlocked())
	}
}

func stringPointer(value string) *string { return &value }
