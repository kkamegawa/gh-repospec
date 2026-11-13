package config

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// PersonalConfig declares account-wide issue definitions for a GitHub user.
type PersonalConfig struct {
	SchemaVersion int                `json:"schemaVersion" yaml:"schemaVersion"`
	Scope         string             `json:"scope" yaml:"scope"`
	Account       string             `json:"account" yaml:"account"`
	IssueTypes    []IssueTypeConfig  `json:"issueTypes,omitempty" yaml:"issueTypes,omitempty"`
	IssueFields   []IssueFieldConfig `json:"issueFields,omitempty" yaml:"issueFields,omitempty"`
}

type IssueTypeConfig struct {
	Name        string  `json:"name" yaml:"name"`
	Description *string `json:"description,omitempty" yaml:"description,omitempty"`
	Color       *string `json:"color,omitempty" yaml:"color,omitempty"`
	IsEnabled   *bool   `json:"isEnabled" yaml:"isEnabled"`
}

type IssueFieldConfig struct {
	Name        string                   `json:"name" yaml:"name"`
	Description *string                  `json:"description,omitempty" yaml:"description,omitempty"`
	DataType    string                   `json:"dataType" yaml:"dataType"`
	Visibility  *string                  `json:"visibility,omitempty" yaml:"visibility,omitempty"`
	Options     []IssueFieldOptionConfig `json:"options,omitempty" yaml:"options,omitempty"`
}

type IssueFieldOptionConfig struct {
	Name        string  `json:"name" yaml:"name"`
	Description *string `json:"description,omitempty" yaml:"description,omitempty"`
	Color       string  `json:"color" yaml:"color"`
	Priority    *int    `json:"priority" yaml:"priority"`
}

// ValidateFile selects the typed contract for a repository or personal document.
func ValidateFile(path string) error {
	data, err := os.ReadFile(path) //nolint:gosec // user-supplied path
	if err != nil {
		return fmt.Errorf("read configuration %q: %w", path, err)
	}
	var header struct {
		Scope string `yaml:"scope"`
	}
	if err := decodeDocument(data, &header, false); err != nil {
		return fmt.Errorf("decode configuration %q: %w", path, err)
	}
	if header.Scope == ScopePersonal {
		if _, err := DecodePersonal(data); err != nil {
			return fmt.Errorf("validate configuration %q: %w", path, err)
		}
		return nil
	}
	_, err = loadRepository(path, data, Validate)
	return err
}

func LoadPersonal(path string) (*PersonalConfig, error) {
	data, err := os.ReadFile(path) //nolint:gosec // user-supplied path
	if err != nil {
		return nil, fmt.Errorf("read configuration %q: %w", path, err)
	}
	cfg, err := DecodePersonal(data)
	if err != nil {
		return nil, fmt.Errorf("validate configuration %q: %w", path, err)
	}
	return cfg, nil
}

func DecodePersonal(data []byte) (*PersonalConfig, error) {
	var cfg PersonalConfig
	if err := decodeDocument(data, &cfg, true); err != nil {
		return nil, err
	}
	var raw yaml.Node
	if err := decodeDocument(data, &raw, false); err != nil {
		return nil, err
	}
	if err := validatePersonalNodes(&raw); err != nil {
		return nil, err
	}
	if err := ValidatePersonal(&cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// Check scalar and sequence types that yaml.v3 otherwise coerces or treats as omitted.
func validatePersonalNodes(node *yaml.Node) error {
	for i := 0; i < len(node.Content); i++ {
		child := node.Content[i]
		if node.Kind == yaml.MappingNode {
			i++
			tag := "!!str"
			switch child.Value {
			case "schemaVersion", "priority":
				tag = "!!int"
			case "isEnabled":
				tag = "!!bool"
			case "issueTypes", "issueFields", "options":
				tag = "!!seq"
			}
			child = node.Content[i]
			if child.ShortTag() != tag {
				return fmt.Errorf("%s must have YAML type %s", node.Content[i-1].Value, tag)
			}
		}
		if err := validatePersonalNodes(child); err != nil {
			return err
		}
	}
	return nil
}

// PersonalNameKey returns the case-insensitive key used for issue definitions.
func PersonalNameKey(name string) string {
	return strings.ToLower(name)
}

func ValidatePersonal(cfg *PersonalConfig) error {
	if cfg == nil || cfg.SchemaVersion != SchemaVersion2 || cfg.Scope != ScopePersonal {
		return errors.New("personal configuration requires schemaVersion: 2 and scope: personal")
	}
	if cfg.Account == "" || strings.ContainsAny(cfg.Account, " /\t\r\n") {
		return errors.New("account must be a GitHub login")
	}
	if len(cfg.IssueTypes) > 25 || len(cfg.IssueFields) > 25 {
		return errors.New("personal configuration supports at most 25 issue types and 25 issue fields")
	}
	typeNames := make(map[string]bool)
	for _, item := range cfg.IssueTypes {
		if err := validatePersonalName("issueTypes", item.Name, typeNames); err != nil {
			return err
		}
		if item.IsEnabled == nil {
			return fmt.Errorf("issueTypes[%s].isEnabled is required", item.Name)
		}
		if item.Color != nil && !validIssueColor(*item.Color) {
			return fmt.Errorf("issueTypes[%s].color is invalid", item.Name)
		}
	}
	fieldNames := make(map[string]bool)
	for _, field := range cfg.IssueFields {
		if err := validatePersonalName("issueFields", field.Name, fieldNames); err != nil {
			return err
		}
		if err := validateIssueField(field); err != nil {
			return err
		}
	}
	return nil
}

func validateIssueField(field IssueFieldConfig) error {
	path := fmt.Sprintf("issueFields[%s]", field.Name)
	switch field.DataType {
	case "single_select", "multi_select":
	case "text", "number", "date":
		if field.Options != nil {
			return fmt.Errorf("%s.options is only allowed on select fields", path)
		}
	default:
		return fmt.Errorf("%s.dataType is invalid", path)
	}
	if field.Visibility != nil && !oneOf(*field.Visibility, "restricted", "all") {
		return fmt.Errorf("%s.visibility must be restricted or all", path)
	}
	optionNames := make(map[string]bool)
	for _, option := range field.Options {
		if err := validatePersonalName(path+".options", option.Name, optionNames); err != nil {
			return err
		}
		if !validIssueColor(option.Color) || option.Priority == nil {
			return fmt.Errorf("%s.options[%s] requires a valid color and integer priority", path, option.Name)
		}
	}
	return nil
}

func validatePersonalName(path, name string, names map[string]bool) error {
	if name == "" || strings.TrimSpace(name) != name {
		return fmt.Errorf("%s requires a nonempty name without surrounding whitespace", path)
	}
	key := PersonalNameKey(name)
	if names[key] {
		return fmt.Errorf("%s contains duplicate name %q", path, name)
	}
	names[key] = true
	return nil
}

func validIssueColor(color string) bool {
	return oneOf(color, "gray", "blue", "green", "yellow", "orange", "red", "pink", "purple")
}
