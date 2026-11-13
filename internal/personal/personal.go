// Package personal plans and applies personal issue type and field settings.
package personal

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/kkamegawa/gh-repospec/internal/config"
	"github.com/kkamegawa/gh-repospec/internal/githubapi"
	"github.com/kkamegawa/gh-repospec/internal/planner"
)

// Service is the GitHub account API used by the personal settings engine.
type Service interface {
	GetAuthenticatedUser(context.Context) (githubapi.Owner, error)
	ListPersonalIssueTypes(context.Context) ([]githubapi.PersonalIssueType, error)
	ListPersonalIssueFields(context.Context) ([]githubapi.PersonalIssueField, error)
	CreatePersonalIssueType(context.Context, githubapi.PersonalIssueType) error
	UpdatePersonalIssueType(context.Context, int64, githubapi.PersonalIssueType) error
	CreatePersonalIssueField(context.Context, githubapi.PersonalIssueField) error
	UpdatePersonalIssueField(context.Context, int64, githubapi.PersonalIssueField) error
}

// Engine builds and applies plans for the authenticated personal account.
type Engine struct{ service Service }

// New creates a personal settings engine.
func New(service Service) *Engine { return &Engine{service: service} }

// Plan describes changes to one personal account's issue definitions.
type Plan struct {
	Account string           `json:"account"`
	Changes []planner.Change `json:"changes"`
}

// HasBlocked reports whether missing state or an incompatible definition
// prevents safe application.
func (p Plan) HasBlocked() bool {
	for _, change := range p.Changes {
		if change.State == planner.ChangeBlocked {
			return true
		}
	}
	return false
}

// DriftCount returns the number of create or update operations in the plan.
func (p Plan) DriftCount() int {
	count := 0
	for _, change := range p.Changes {
		if change.State == planner.ChangeCreate || change.State == planner.ChangeUpdate {
			count++
		}
	}
	return count
}

// Build checks the account identity and compares configured definitions with
// the current personal settings. Definitions and options omitted from the
// document are not removed.
func (e *Engine) Build(ctx context.Context, cfg *config.PersonalConfig) (Plan, error) {
	if err := config.ValidatePersonal(cfg); err != nil {
		return Plan{}, err
	}
	user, err := e.service.GetAuthenticatedUser(ctx)
	if err != nil {
		return Plan{}, fmt.Errorf("get authenticated account: %w", err)
	}
	if !strings.EqualFold(user.Login, cfg.Account) {
		return Plan{}, fmt.Errorf("personal settings target account %q does not match authenticated account %q", cfg.Account, user.Login)
	}
	types, err := e.service.ListPersonalIssueTypes(ctx)
	if err != nil {
		return Plan{}, fmt.Errorf("list personal issue types: %w", err)
	}
	fields, err := e.service.ListPersonalIssueFields(ctx)
	if err != nil {
		return Plan{}, fmt.Errorf("list personal issue fields: %w", err)
	}
	return buildPlan(cfg, types, fields), nil
}

// Apply rebuilds the plan immediately before writing, rejects every blocked
// change before making a write request, and preserves undeclared options.
func (e *Engine) Apply(ctx context.Context, cfg *config.PersonalConfig) error {
	plan, err := e.Build(ctx, cfg)
	if err != nil {
		return err
	}
	if plan.HasBlocked() {
		return errors.New("personal issue settings plan is blocked; resolve blocked changes before retrying")
	}
	if plan.DriftCount() == 0 {
		return nil
	}
	types, err := e.service.ListPersonalIssueTypes(ctx)
	if err != nil {
		return fmt.Errorf("refresh personal issue types before apply: %w", err)
	}
	fields, err := e.service.ListPersonalIssueFields(ctx)
	if err != nil {
		return fmt.Errorf("refresh personal issue fields before apply: %w", err)
	}
	if buildPlan(cfg, types, fields).HasBlocked() {
		return errors.New("personal issue settings changed before apply; resolve blocked changes before retrying")
	}
	for _, desired := range cfg.IssueTypes {
		live, found := findType(types, desired.Name)
		if !found {
			value, err := apiType(desired)
			if err != nil {
				return err
			}
			if err := e.service.CreatePersonalIssueType(ctx, value); err != nil {
				return fmt.Errorf("create personal issue type %q: %w", desired.Name, err)
			}
			continue
		}
		if !typeNeedsUpdate(desired, live) {
			continue
		}
		value, err := apiType(desired)
		if err != nil {
			return err
		}
		if desired.Description == nil {
			value.Description = live.Description
		}
		if desired.Color == nil {
			value.Color = live.Color
		}
		if err := e.service.UpdatePersonalIssueType(ctx, live.ID, value); err != nil {
			return fmt.Errorf("update personal issue type %q: %w", desired.Name, err)
		}
	}
	for _, desired := range cfg.IssueFields {
		live, found := findField(fields, desired.Name)
		if !found {
			value, err := apiField(desired)
			if err != nil {
				return err
			}
			if err := e.service.CreatePersonalIssueField(ctx, value); err != nil {
				return fmt.Errorf("create personal issue field %q: %w", desired.Name, err)
			}
			continue
		}
		if !fieldNeedsUpdate(desired, live) {
			continue
		}
		// Options replace the complete list in GitHub's field API. Read again
		// immediately before an update so IDs and values added since the plan
		// was built are retained.
		if len(desired.Options) > 0 {
			latest, err := e.service.ListPersonalIssueFields(ctx)
			if err != nil {
				return fmt.Errorf("refresh options for personal issue field %q: %w", desired.Name, err)
			}
			var foundLatest bool
			live, foundLatest = findField(latest, desired.Name)
			if !foundLatest {
				return fmt.Errorf("personal issue field %q disappeared before update", desired.Name)
			}
			if problem := fieldBlocked(desired, live); problem != "" {
				return errors.New(problem)
			}
			if !fieldNeedsUpdate(desired, live) {
				continue
			}
		}
		value, err := updateField(desired, live)
		if err != nil {
			return err
		}
		if err := e.service.UpdatePersonalIssueField(ctx, live.ID, value); err != nil {
			return fmt.Errorf("update personal issue field %q: %w", desired.Name, err)
		}
	}
	return nil
}

// Import reads the personal settings for the authenticated account. If
// expectedAccount is non-empty, it must match before either list is read.
func (e *Engine) Import(ctx context.Context, expectedAccount string) (*config.PersonalConfig, error) {
	user, err := e.service.GetAuthenticatedUser(ctx)
	if err != nil {
		return nil, fmt.Errorf("get authenticated account: %w", err)
	}
	if user.Login == "" {
		return nil, errors.New("authenticated account has no login")
	}
	if expectedAccount != "" && !strings.EqualFold(user.Login, expectedAccount) {
		return nil, fmt.Errorf("personal settings target account %q does not match authenticated account %q", expectedAccount, user.Login)
	}
	types, err := e.service.ListPersonalIssueTypes(ctx)
	if err != nil {
		return nil, fmt.Errorf("list personal issue types: %w", err)
	}
	fields, err := e.service.ListPersonalIssueFields(ctx)
	if err != nil {
		return nil, fmt.Errorf("list personal issue fields: %w", err)
	}
	result := &config.PersonalConfig{
		SchemaVersion: config.SchemaVersion2,
		Scope:         config.ScopePersonal,
		Account:       user.Login,
		IssueTypes:    make([]config.IssueTypeConfig, 0, len(types)),
		IssueFields:   make([]config.IssueFieldConfig, 0, len(fields)),
	}
	for _, value := range types {
		if value.IsEnabled == nil {
			return nil, fmt.Errorf("import issue type %q: enabled state is unavailable", value.Name)
		}
		result.IssueTypes = append(result.IssueTypes, config.IssueTypeConfig{
			Name: value.Name, Description: cloneString(value.Description), Color: cloneString(value.Color), IsEnabled: cloneBool(value.IsEnabled),
		})
	}
	for _, value := range fields {
		if value.Visibility == nil {
			return nil, fmt.Errorf("import issue field %q: visibility is unavailable", value.Name)
		}
		field := config.IssueFieldConfig{
			Name: value.Name, Description: cloneString(value.Description), DataType: value.DataType, Visibility: cloneString(value.Visibility),
		}
		for _, option := range value.Options {
			field.Options = append(field.Options, config.IssueFieldOptionConfig{
				Name: option.Name, Description: cloneString(option.Description), Color: option.Color, Priority: cloneInt(option.Priority),
			})
		}
		result.IssueFields = append(result.IssueFields, field)
	}
	if err := config.ValidatePersonal(result); err != nil {
		return nil, fmt.Errorf("validate imported personal settings: %w", err)
	}
	return result, nil
}

func buildPlan(cfg *config.PersonalConfig, types []githubapi.PersonalIssueType, fields []githubapi.PersonalIssueField) Plan {
	plan := Plan{Account: cfg.Account, Changes: make([]planner.Change, 0, len(cfg.IssueTypes)+len(cfg.IssueFields))}
	for _, desired := range cfg.IssueTypes {
		live, found := findType(types, desired.Name)
		resource := "issueType." + desired.Name
		if !found {
			plan.Changes = append(plan.Changes, planner.Change{Stage: "personal", Resource: resource, State: planner.ChangeCreate, Summary: fmt.Sprintf("create issue type %q", desired.Name)})
			continue
		}
		if problem := typeBlocked(desired, live); problem != "" {
			plan.Changes = append(plan.Changes, planner.Change{Stage: "personal", Resource: resource, State: planner.ChangeBlocked, Summary: problem})
			continue
		}
		state := planner.ChangeNoop
		summary := fmt.Sprintf("issue type %q matches declared settings", desired.Name)
		if typeNeedsUpdate(desired, live) {
			state = planner.ChangeUpdate
			summary = typeUpdateSummary(desired, live)
		}
		plan.Changes = append(plan.Changes, planner.Change{Stage: "personal", Resource: resource, State: state, Summary: summary})
	}
	for _, desired := range cfg.IssueFields {
		live, found := findField(fields, desired.Name)
		resource := "issueField." + desired.Name
		if !found {
			plan.Changes = append(plan.Changes, planner.Change{Stage: "personal", Resource: resource, State: planner.ChangeCreate, Summary: fmt.Sprintf("create issue field %q", desired.Name)})
			continue
		}
		if problem := fieldBlocked(desired, live); problem != "" {
			plan.Changes = append(plan.Changes, planner.Change{Stage: "personal", Resource: resource, State: planner.ChangeBlocked, Summary: problem})
			continue
		}
		state := planner.ChangeNoop
		summary := fmt.Sprintf("issue field %q matches declared settings", desired.Name)
		if fieldNeedsUpdate(desired, live) {
			state = planner.ChangeUpdate
			summary = fieldUpdateSummary(desired, live)
		}
		plan.Changes = append(plan.Changes, planner.Change{Stage: "personal", Resource: resource, State: state, Summary: summary})
	}
	return plan
}

func findType(values []githubapi.PersonalIssueType, name string) (githubapi.PersonalIssueType, bool) {
	for _, value := range values {
		if config.PersonalNameKey(value.Name) == config.PersonalNameKey(name) {
			return value, true
		}
	}
	return githubapi.PersonalIssueType{}, false
}

func findField(values []githubapi.PersonalIssueField, name string) (githubapi.PersonalIssueField, bool) {
	for _, value := range values {
		if config.PersonalNameKey(value.Name) == config.PersonalNameKey(name) {
			return value, true
		}
	}
	return githubapi.PersonalIssueField{}, false
}

func typeBlocked(desired config.IssueTypeConfig, live githubapi.PersonalIssueType) string {
	if desired.IsEnabled != nil && live.IsEnabled == nil {
		return fmt.Sprintf("issue type %q cannot be compared because its enabled state is unavailable", desired.Name)
	}
	return ""
}

func fieldBlocked(desired config.IssueFieldConfig, live githubapi.PersonalIssueField) string {
	if desired.DataType != live.DataType {
		return fmt.Sprintf("issue field %q has data type %q, but GitHub does not support changing it to %q", desired.Name, live.DataType, desired.DataType)
	}
	if desired.Visibility != nil && live.Visibility == nil {
		return fmt.Sprintf("issue field %q cannot be compared because its visibility is unavailable", desired.Name)
	}
	if len(desired.Options) > 0 && live.Options == nil {
		return fmt.Sprintf("issue field %q cannot safely update options because the current options are unavailable", desired.Name)
	}
	if len(desired.Options) > 0 && slices.ContainsFunc(live.Options, func(option githubapi.PersonalIssueFieldOption) bool {
		return option.ID <= 0 || option.Priority == nil || option.Name == "" || option.Color == ""
	}) {
		return fmt.Sprintf("issue field %q cannot safely update options because a current option is incomplete", desired.Name)
	}
	return ""
}

func typeNeedsUpdate(desired config.IssueTypeConfig, live githubapi.PersonalIssueType) bool {
	return (desired.Name != live.Name) ||
		(desired.Description != nil && !sameString(desired.Description, live.Description)) ||
		(desired.Color != nil && !sameString(desired.Color, live.Color)) ||
		(desired.IsEnabled != nil && !sameBool(desired.IsEnabled, live.IsEnabled))
}

func fieldNeedsUpdate(desired config.IssueFieldConfig, live githubapi.PersonalIssueField) bool {
	return desired.Name != live.Name ||
		(desired.Description != nil && !sameString(desired.Description, live.Description)) ||
		(desired.Visibility != nil && !sameString(desired.Visibility, live.Visibility)) ||
		!optionsMatch(desired.Options, live.Options)
}

func optionsMatch(desired []config.IssueFieldOptionConfig, live []githubapi.PersonalIssueFieldOption) bool {
	for _, option := range desired {
		var current *githubapi.PersonalIssueFieldOption
		for i := range live {
			if config.PersonalNameKey(option.Name) == config.PersonalNameKey(live[i].Name) {
				current = &live[i]
				break
			}
		}
		if current == nil || current.Name != option.Name || current.Color != option.Color ||
			(option.Description != nil && !sameString(option.Description, current.Description)) ||
			(option.Priority != nil && !sameInt(option.Priority, current.Priority)) {
			return false
		}
	}
	return true
}

func apiType(value config.IssueTypeConfig) (githubapi.PersonalIssueType, error) {
	if value.IsEnabled == nil {
		return githubapi.PersonalIssueType{}, fmt.Errorf("new issue type %q requires isEnabled", value.Name)
	}
	return githubapi.PersonalIssueType{
		Name: value.Name, Description: cloneString(value.Description), Color: cloneString(value.Color), IsEnabled: cloneBool(value.IsEnabled),
	}, nil
}

func apiField(value config.IssueFieldConfig) (githubapi.PersonalIssueField, error) {
	if (value.DataType == "single_select" || value.DataType == "multi_select") && len(value.Options) == 0 {
		return githubapi.PersonalIssueField{}, fmt.Errorf("new select issue field %q requires options", value.Name)
	}
	field := githubapi.PersonalIssueField{
		Name: value.Name, Description: cloneString(value.Description), DataType: value.DataType,
		Visibility: cloneString(value.Visibility), Options: make([]githubapi.PersonalIssueFieldOption, 0, len(value.Options)),
	}
	for _, option := range value.Options {
		field.Options = append(field.Options, apiOption(option, 0))
	}
	return field, nil
}

func updateField(desired config.IssueFieldConfig, live githubapi.PersonalIssueField) (githubapi.PersonalIssueField, error) {
	field := githubapi.PersonalIssueField{
		Name:        desired.Name,
		Description: cloneString(desired.Description), Visibility: cloneString(desired.Visibility),
	}
	if optionsMatch(desired.Options, live.Options) {
		return field, nil
	}
	field.Options = make([]githubapi.PersonalIssueFieldOption, 0, len(desired.Options)+len(live.Options))
	for _, current := range live.Options {
		configured := false
		for _, option := range desired.Options {
			if config.PersonalNameKey(current.Name) == config.PersonalNameKey(option.Name) {
				updated := apiOption(option, current.ID)
				if option.Description == nil {
					updated.Description = cloneString(current.Description)
				}
				if option.Priority == nil {
					updated.Priority = cloneInt(current.Priority)
				}
				field.Options = append(field.Options, updated)
				configured = true
				break
			}
		}
		if !configured {
			field.Options = append(field.Options, current)
		}
	}
	for _, option := range desired.Options {
		if !slices.ContainsFunc(live.Options, func(current githubapi.PersonalIssueFieldOption) bool {
			return config.PersonalNameKey(current.Name) == config.PersonalNameKey(option.Name)
		}) {
			field.Options = append(field.Options, apiOption(option, 0))
		}
	}
	for _, option := range field.Options {
		if option.Priority == nil {
			return githubapi.PersonalIssueField{}, fmt.Errorf("issue field %q cannot update options because priority is unavailable for %q", desired.Name, option.Name)
		}
	}
	return field, nil
}

func apiOption(value config.IssueFieldOptionConfig, id int64) githubapi.PersonalIssueFieldOption {
	return githubapi.PersonalIssueFieldOption{ID: id, Name: value.Name, Description: cloneString(value.Description), Color: value.Color, Priority: cloneInt(value.Priority)}
}

func typeUpdateSummary(desired config.IssueTypeConfig, live githubapi.PersonalIssueType) string {
	var changes []string
	if desired.Name != live.Name {
		changes = append(changes, fmt.Sprintf("name %q → %q", live.Name, desired.Name))
	}
	if desired.Description != nil && !sameString(desired.Description, live.Description) {
		changes = append(changes, "description")
	}
	if desired.Color != nil && !sameString(desired.Color, live.Color) {
		changes = append(changes, fmt.Sprintf("color %q → %q", stringValue(live.Color), *desired.Color))
	}
	if desired.IsEnabled != nil && !sameBool(desired.IsEnabled, live.IsEnabled) {
		changes = append(changes, fmt.Sprintf("isEnabled %t → %t", *live.IsEnabled, *desired.IsEnabled))
	}
	return fmt.Sprintf("update issue type %q (%s)", desired.Name, strings.Join(changes, ", "))
}

func fieldUpdateSummary(desired config.IssueFieldConfig, live githubapi.PersonalIssueField) string {
	var changes []string
	if desired.Name != live.Name {
		changes = append(changes, fmt.Sprintf("name %q → %q", live.Name, desired.Name))
	}
	if desired.Description != nil && !sameString(desired.Description, live.Description) {
		changes = append(changes, "description")
	}
	if desired.Visibility != nil && !sameString(desired.Visibility, live.Visibility) {
		changes = append(changes, fmt.Sprintf("visibility %q → %q", stringValue(live.Visibility), *desired.Visibility))
	}
	for _, option := range desired.Options {
		var current *githubapi.PersonalIssueFieldOption
		for i := range live.Options {
			if config.PersonalNameKey(option.Name) == config.PersonalNameKey(live.Options[i].Name) {
				current = &live.Options[i]
				break
			}
		}
		if current == nil {
			changes = append(changes, fmt.Sprintf("add option %q", option.Name))
		} else if current.Name != option.Name || current.Color != option.Color ||
			(option.Description != nil && !sameString(option.Description, current.Description)) ||
			(option.Priority != nil && !sameInt(option.Priority, current.Priority)) {
			changes = append(changes, fmt.Sprintf("update option %q", option.Name))
		}
	}
	return fmt.Sprintf("update issue field %q (%s)", desired.Name, strings.Join(changes, ", "))
}

func sameString(left, right *string) bool {
	if left == nil || right == nil {
		return (left == nil || *left == "") && (right == nil || *right == "")
	}
	return *left == *right
}

func sameBool(left, right *bool) bool { return left != nil && right != nil && *left == *right }
func sameInt(left, right *int) bool   { return left != nil && right != nil && *left == *right }
func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func cloneString(value *string) *string {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}
func cloneBool(value *bool) *bool {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}
func cloneInt(value *int) *int {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}
