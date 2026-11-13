package refresh

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/kkamegawa/gh-repospec/internal/config"
	yamlv3 "gopkg.in/yaml.v3"
)

// RefreshPersonal updates only personal collections already declared by the
// destination document. Missing remote entries remain in the document, and
// remote entries absent from a declared collection are appended.
func RefreshPersonal(original []byte, format Format, current, desired *config.PersonalConfig) (Result, error) {
	if current == nil || desired == nil {
		return Result{}, fmt.Errorf("current and imported personal configurations are required")
	}
	if err := config.ValidatePersonal(current); err != nil {
		return Result{}, fmt.Errorf("validate destination personal configuration: %w", err)
	}
	if err := config.ValidatePersonal(desired); err != nil {
		return Result{}, fmt.Errorf("validate imported personal configuration: %w", err)
	}
	if current.Scope != desired.Scope || !strings.EqualFold(current.Account, desired.Account) {
		return Result{}, fmt.Errorf("personal refresh account and scope must match")
	}
	for _, declared := range current.IssueFields {
		for _, imported := range desired.IssueFields {
			if strings.EqualFold(declared.Name, imported.Name) && declared.DataType != imported.DataType {
				return Result{}, fmt.Errorf("issueFields[%s].dataType changed from %s to %s", declared.Name, declared.DataType, imported.DataType)
			}
		}
	}
	plan := personalPlan(current, desired)
	if !plan.Changed() {
		return Result{Updated: original, Plan: plan}, nil
	}

	var updated []byte
	var err error
	switch format {
	case FormatYAML:
		updated, err = refreshPersonalYAML(original, current, desired)
	case FormatJSON:
		updated, err = refreshPersonalJSON(current, desired)
	default:
		return Result{}, fmt.Errorf("unsupported format %v", format)
	}
	if err != nil {
		return Result{}, err
	}
	if _, err := config.DecodePersonal(updated); err != nil {
		return Result{}, fmt.Errorf("refreshed personal document failed validation: %w", err)
	}
	return Result{Updated: updated, Changed: true, Plan: plan}, nil
}

func personalPlan(current, desired *config.PersonalConfig) Plan {
	var plan Plan
	if current.IssueTypes != nil {
		change := personalTypeChange(current.IssueTypes, desired.IssueTypes)
		if change.Updated != nil || change.Added != nil || change.NotApplied != nil {
			plan.Collections = append(plan.Collections, change)
		}
	}
	if current.IssueFields != nil {
		change := personalFieldChange(current.IssueFields, desired.IssueFields)
		if change.Updated != nil || change.Added != nil || change.NotApplied != nil {
			plan.Collections = append(plan.Collections, change)
		}
	}
	return plan
}

func personalTypeChange(current, desired []config.IssueTypeConfig) CollectionChange {
	change := CollectionChange{Path: "issueTypes"}
	byName := make(map[string]config.IssueTypeConfig, len(desired))
	for _, item := range desired {
		byName[strings.ToLower(item.Name)] = item
	}
	for _, item := range current {
		remote, ok := byName[strings.ToLower(item.Name)]
		if !ok {
			change.NotApplied = append(change.NotApplied, item.Name)
			continue
		}
		if !samePersonalType(item, remote) {
			change.Updated = append(change.Updated, EntryUpdate{Key: item.Name})
		}
	}
	declared := make(map[string]bool, len(current))
	for _, item := range current {
		declared[strings.ToLower(item.Name)] = true
	}
	for _, item := range desired {
		if !declared[strings.ToLower(item.Name)] {
			change.Added = append(change.Added, EntryAddition{Key: item.Name})
		}
	}
	return change
}

func samePersonalType(current, desired config.IssueTypeConfig) bool {
	return current.Name == desired.Name &&
		(current.Description == nil || desired.Description == nil || *current.Description == *desired.Description) &&
		(current.Color == nil || desired.Color == nil || *current.Color == *desired.Color) &&
		(current.IsEnabled == nil || desired.IsEnabled == nil || *current.IsEnabled == *desired.IsEnabled)
}

func personalFieldChange(current, desired []config.IssueFieldConfig) CollectionChange {
	change := CollectionChange{Path: "issueFields"}
	byName := make(map[string]config.IssueFieldConfig, len(desired))
	for _, item := range desired {
		byName[strings.ToLower(item.Name)] = item
	}
	for _, item := range current {
		remote, ok := byName[strings.ToLower(item.Name)]
		if !ok {
			change.NotApplied = append(change.NotApplied, item.Name)
			continue
		}
		if item.Name != remote.Name || item.DataType != remote.DataType || !sameOptionalString(item.Description, remote.Description) ||
			!sameOptionalString(item.Visibility, remote.Visibility) || (item.Options != nil && !samePersonalOptions(item.Options, remote.Options)) {
			change.Updated = append(change.Updated, EntryUpdate{Key: item.Name})
		}
	}
	declared := make(map[string]bool, len(current))
	for _, item := range current {
		declared[strings.ToLower(item.Name)] = true
	}
	for _, item := range desired {
		if !declared[strings.ToLower(item.Name)] {
			change.Added = append(change.Added, EntryAddition{Key: item.Name})
		}
	}
	return change
}

func sameOptionalString(a, b *string) bool {
	return a == nil || b == nil || *a == *b
}

func samePersonalOptions(current, desired []config.IssueFieldOptionConfig) bool {
	byName := make(map[string]config.IssueFieldOptionConfig, len(desired))
	for _, option := range desired {
		byName[strings.ToLower(option.Name)] = option
	}
	declared := make(map[string]bool, len(current))
	for _, option := range current {
		key := strings.ToLower(option.Name)
		declared[key] = true
		remote, ok := byName[key]
		if !ok {
			continue
		}
		if option.Name != remote.Name || option.Color != remote.Color ||
			!sameOptionalString(option.Description, remote.Description) ||
			(option.Priority != nil && remote.Priority != nil && *option.Priority != *remote.Priority) {
			return false
		}
	}
	for _, option := range desired {
		if !declared[strings.ToLower(option.Name)] {
			return false
		}
	}
	return true
}

func refreshPersonalJSON(current, desired *config.PersonalConfig) ([]byte, error) {
	updated := mergePersonal(current, desired)
	// A declared empty collection still participates in subsequent refreshes.
	document := struct {
		*config.PersonalConfig
		IssueTypes  *[]config.IssueTypeConfig  `json:"issueTypes,omitempty"`
		IssueFields *[]config.IssueFieldConfig `json:"issueFields,omitempty"`
	}{PersonalConfig: updated}
	if current.IssueTypes != nil {
		document.IssueTypes = &updated.IssueTypes
	}
	if current.IssueFields != nil {
		document.IssueFields = &updated.IssueFields
	}
	data, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("render refreshed personal JSON: %w", err)
	}
	return append(data, '\n'), nil
}

func mergePersonal(current, desired *config.PersonalConfig) *config.PersonalConfig {
	updated := *current
	if current.IssueTypes != nil {
		updated.IssueTypes = mergePersonalTypes(current.IssueTypes, desired.IssueTypes)
	}
	if current.IssueFields != nil {
		updated.IssueFields = mergePersonalFields(current.IssueFields, desired.IssueFields)
	}
	return &updated
}

func mergePersonalTypes(current, desired []config.IssueTypeConfig) []config.IssueTypeConfig {
	byName := make(map[string]config.IssueTypeConfig, len(desired))
	for _, item := range desired {
		byName[strings.ToLower(item.Name)] = item
	}
	updated := append([]config.IssueTypeConfig{}, current...)
	seen := make(map[string]bool, len(current))
	for i, item := range current {
		key := strings.ToLower(item.Name)
		seen[key] = true
		remote, ok := byName[key]
		if !ok {
			continue
		}
		updated[i] = mergePersonalType(item, remote)
	}
	for _, item := range desired {
		if !seen[strings.ToLower(item.Name)] {
			updated = append(updated, item)
		}
	}
	return updated
}

func mergePersonalType(current, desired config.IssueTypeConfig) config.IssueTypeConfig {
	current.Name = desired.Name
	if current.Description != nil && desired.Description != nil {
		current.Description = cloneString(desired.Description)
	}
	if current.Color != nil && desired.Color != nil {
		current.Color = cloneString(desired.Color)
	}
	if desired.IsEnabled != nil {
		current.IsEnabled = cloneBool(desired.IsEnabled)
	}
	return current
}

func mergePersonalFields(current, desired []config.IssueFieldConfig) []config.IssueFieldConfig {
	byName := make(map[string]config.IssueFieldConfig, len(desired))
	for _, item := range desired {
		byName[strings.ToLower(item.Name)] = item
	}
	updated := append([]config.IssueFieldConfig{}, current...)
	seen := make(map[string]bool, len(current))
	for i, item := range current {
		key := strings.ToLower(item.Name)
		seen[key] = true
		remote, ok := byName[key]
		if !ok {
			continue
		}
		updated[i] = mergePersonalField(item, remote)
	}
	for _, item := range desired {
		if !seen[strings.ToLower(item.Name)] {
			updated = append(updated, item)
		}
	}
	return updated
}

func mergePersonalField(current, desired config.IssueFieldConfig) config.IssueFieldConfig {
	current.Name = desired.Name
	if current.Description != nil && desired.Description != nil {
		current.Description = cloneString(desired.Description)
	}
	if current.Visibility != nil && desired.Visibility != nil {
		current.Visibility = cloneString(desired.Visibility)
	}
	if desired.DataType != "" {
		current.DataType = desired.DataType
	}
	if current.Options != nil {
		current.Options = mergePersonalOptions(current.Options, desired.Options)
	}
	return current
}

func mergePersonalOptions(current, desired []config.IssueFieldOptionConfig) []config.IssueFieldOptionConfig {
	byName := make(map[string]config.IssueFieldOptionConfig, len(desired))
	for _, item := range desired {
		byName[strings.ToLower(item.Name)] = item
	}
	updated := append([]config.IssueFieldOptionConfig(nil), current...)
	seen := make(map[string]bool, len(current))
	for i, item := range current {
		key := strings.ToLower(item.Name)
		seen[key] = true
		remote, ok := byName[key]
		if !ok {
			continue
		}
		item.Name = remote.Name
		if item.Description != nil && remote.Description != nil {
			item.Description = cloneString(remote.Description)
		}
		if remote.Color != "" {
			item.Color = remote.Color
		}
		if remote.Priority != nil {
			item.Priority = cloneInt(remote.Priority)
		}
		updated[i] = item
	}
	for _, item := range desired {
		if !seen[strings.ToLower(item.Name)] {
			updated = append(updated, item)
		}
	}
	return updated
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

func refreshPersonalYAML(original []byte, current, desired *config.PersonalConfig) ([]byte, error) {
	var document yamlv3.Node
	decoder := yamlv3.NewDecoder(bytes.NewReader(original))
	if err := decoder.Decode(&document); err != nil {
		return nil, fmt.Errorf("decode personal YAML: %w", err)
	}
	if len(document.Content) != 1 || document.Content[0].Kind != yamlv3.MappingNode {
		return nil, fmt.Errorf("personal YAML must contain one mapping document")
	}
	root := document.Content[0]
	if current.IssueTypes != nil {
		if err := updatePersonalSequence(root, "issueTypes", current.IssueTypes, mergePersonalTypes(current.IssueTypes, desired.IssueTypes), "name"); err != nil {
			return nil, err
		}
	}
	if current.IssueFields != nil {
		if err := updatePersonalSequence(root, "issueFields", current.IssueFields, mergePersonalFields(current.IssueFields, desired.IssueFields), "name"); err != nil {
			return nil, err
		}
	}
	var out bytes.Buffer
	encoder := yamlv3.NewEncoder(&out)
	encoder.SetIndent(2)
	if err := encoder.Encode(&document); err != nil {
		return nil, fmt.Errorf("render refreshed personal YAML: %w", err)
	}
	if err := encoder.Close(); err != nil {
		return nil, fmt.Errorf("finish refreshed personal YAML: %w", err)
	}
	return out.Bytes(), nil
}

func updatePersonalSequence(root *yamlv3.Node, key string, current, updated any, nameKey string) error {
	sequence, err := personalSequence(root, key)
	if err != nil {
		return err
	}
	updates := updated
	switch list := updates.(type) {
	case []config.IssueTypeConfig:
		items, ok := current.([]config.IssueTypeConfig)
		if !ok {
			return fmt.Errorf("current %s has unexpected type %T", key, current)
		}
		return updatePersonalItems(sequence, key, items, list, nameKey)
	case []config.IssueFieldConfig:
		items, ok := current.([]config.IssueFieldConfig)
		if !ok {
			return fmt.Errorf("current %s has unexpected type %T", key, current)
		}
		return updatePersonalItems(sequence, key, items, list, nameKey)
	default:
		return fmt.Errorf("unsupported personal collection %T", updated)
	}
}

func updatePersonalItems[T any](sequence *yamlv3.Node, key string, current, updated []T, nameKey string) error {
	currentNames := personalNames(current)
	updatedNodes, err := personalNodes(updated)
	if err != nil {
		return err
	}
	for _, node := range updatedNodes {
		name, _ := mappingString(node, nameKey)
		if !currentNames[strings.ToLower(name)] {
			sequence.Content = append(sequence.Content, node)
		}
	}
	for _, existing := range sequence.Content {
		name, ok := mappingString(existing, nameKey)
		if !ok {
			continue
		}
		index := personalIndex(updated, name)
		if index < 0 {
			continue
		}
		if err := mergePersonalNode(existing, updatedNodes[index]); err != nil {
			return fmt.Errorf("refresh %s[%s]: %w", key, name, err)
		}
	}
	return nil
}

func personalNames[T any](items []T) map[string]bool {
	result := make(map[string]bool, len(items))
	for _, item := range items {
		result[strings.ToLower(personalItemName(item))] = true
	}
	return result
}

func personalIndex[T any](items []T, name string) int {
	for i, item := range items {
		if strings.EqualFold(personalItemName(item), name) {
			return i
		}
	}
	return -1
}

func personalItemName(item any) string {
	switch value := item.(type) {
	case config.IssueTypeConfig:
		return value.Name
	case config.IssueFieldConfig:
		return value.Name
	}
	return ""
}

func personalNodes(items any) ([]*yamlv3.Node, error) {
	data, err := yamlv3.Marshal(items)
	if err != nil {
		return nil, err
	}
	var sequence yamlv3.Node
	if err := yamlv3.Unmarshal(data, &sequence); err != nil {
		return nil, err
	}
	if len(sequence.Content) != 1 || sequence.Content[0].Kind != yamlv3.SequenceNode {
		return nil, fmt.Errorf("rendered collection is not a sequence")
	}
	return sequence.Content[0].Content, nil
}

func mappingString(mapping *yamlv3.Node, key string) (string, bool) {
	if mapping.Kind != yamlv3.MappingNode {
		return "", false
	}
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			return mapping.Content[i+1].Value, true
		}
	}
	return "", false
}

func personalSequence(root *yamlv3.Node, key string) (*yamlv3.Node, error) {
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value != key {
			continue
		}
		if root.Content[i+1].Kind != yamlv3.SequenceNode {
			return nil, fmt.Errorf("%s is not a sequence", key)
		}
		return root.Content[i+1], nil
	}
	return nil, fmt.Errorf("%s is not declared in the destination document", key)
}

func mergePersonalNode(existing, desired *yamlv3.Node) error {
	if existing.Kind != yamlv3.MappingNode || desired.Kind != yamlv3.MappingNode {
		return fmt.Errorf("entry must be a mapping")
	}
	for i := 0; i+1 < len(desired.Content); i += 2 {
		key, value := desired.Content[i], desired.Content[i+1]
		old, _ := nodeMappingValue(existing, key.Value)
		if key.Value == "options" && old != nil {
			if err := mergePersonalOptionsNode(old, value); err != nil {
				return err
			}
			continue
		}
		if old == nil {
			existing.Content = append(existing.Content, key, value)
			continue
		}
		if old.Kind == yamlv3.ScalarNode && value.Kind == yamlv3.ScalarNode {
			old.Value, old.Tag, old.Style = value.Value, value.Tag, value.Style
		}
	}
	return nil
}

func mergePersonalOptionsNode(existing, desired *yamlv3.Node) error {
	if existing.Kind != yamlv3.SequenceNode || desired.Kind != yamlv3.SequenceNode {
		return fmt.Errorf("options must be a sequence")
	}
	for _, option := range desired.Content {
		name, ok := mappingString(option, "name")
		if !ok {
			continue
		}
		found := false
		for _, old := range existing.Content {
			oldName, ok := mappingString(old, "name")
			if !ok || !strings.EqualFold(name, oldName) {
				continue
			}
			if err := mergePersonalNode(old, option); err != nil {
				return err
			}
			found = true
			break
		}
		if !found {
			existing.Content = append(existing.Content, option)
		}
	}
	return nil
}

func nodeMappingValue(mapping *yamlv3.Node, key string) (*yamlv3.Node, int) {
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			return mapping.Content[i+1], i + 1
		}
	}
	return nil, -1
}
