package githubapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

// PersonalIssueType is an issue type owned by the authenticated user.
type PersonalIssueType struct {
	ID          int64   `json:"id"`
	NodeID      string  `json:"node_id"`
	Name        string  `json:"name"`
	Description *string `json:"description"`
	Color       *string `json:"color"`
	IsEnabled   *bool   `json:"is_enabled"`
}

// PersonalIssueField is an issue field owned by the authenticated user.
type PersonalIssueField struct {
	ID          int64                      `json:"id"`
	NodeID      string                     `json:"node_id"`
	Name        string                     `json:"name"`
	Description *string                    `json:"description"`
	DataType    string                     `json:"data_type"`
	Visibility  *string                    `json:"visibility"`
	Options     []PersonalIssueFieldOption `json:"options"`
}

// PersonalIssueFieldOption is one selectable value for a personal issue field.
type PersonalIssueFieldOption struct {
	ID          int64   `json:"id"`
	Name        string  `json:"name"`
	Description *string `json:"description"`
	Color       string  `json:"color"`
	Priority    *int    `json:"priority"`
}

type graphQLIssueTypes struct {
	Data struct {
		Viewer struct {
			IssueTypes struct {
				Nodes []struct {
					ID          string  `json:"id"`
					Name        string  `json:"name"`
					Description *string `json:"description"`
					Color       *string `json:"color"`
					IsEnabled   *bool   `json:"isEnabled"`
				} `json:"nodes"`
			} `json:"issueTypes"`
		} `json:"viewer"`
	} `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

type graphQLIssueFields struct {
	Data struct {
		Viewer struct {
			IssueFields struct {
				Nodes []struct {
					ID          string  `json:"id"`
					Name        string  `json:"name"`
					Description *string `json:"description"`
					DataType    string  `json:"dataType"`
					Visibility  *string `json:"visibility"`
					Options     []struct {
						ID          string  `json:"id"`
						DatabaseID  *int64  `json:"databaseId"`
						Name        string  `json:"name"`
						Description *string `json:"description"`
						Color       string  `json:"color"`
						Priority    *int    `json:"priority"`
					} `json:"options"`
				} `json:"nodes"`
			} `json:"issueFields"`
		} `json:"viewer"`
	} `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

const personalIssueStateQuery = `query PersonalIssueTypes {
  viewer {
    issueTypes(first: 100) {
      nodes { id name description color isEnabled }
    }
  }
}`

const personalIssueFieldsQuery = `query PersonalIssueFields {
  viewer {
    issueFields(first: 100) {
      nodes {
        ... on Node { id }
        ... on IssueFieldCommon { name description dataType visibility }
        ... on IssueFieldSingleSelect { options { id databaseId name description color priority } }
        ... on IssueFieldMultiSelect { options { id databaseId name description color priority } }
      }
    }
  }
}`

// ListPersonalIssueTypes reads the REST identities and the GraphQL state that
// the REST list response omits.
func (c *Client) ListPersonalIssueTypes(ctx context.Context) ([]PersonalIssueType, error) {
	var types []PersonalIssueType
	if err := c.do(ctx, http.MethodGet, "/user/issue-types", nil, &types); err != nil {
		return nil, err
	}
	var state graphQLIssueTypes
	if err := c.do(ctx, http.MethodPost, "/graphql", map[string]string{"query": personalIssueStateQuery}, &state); err != nil {
		return nil, fmt.Errorf("read personal issue type state: %w", err)
	}
	if len(state.Errors) > 0 {
		return nil, fmt.Errorf("read personal issue type state: %s", state.Errors[0].Message)
	}
	byNodeID := make(map[string]struct {
		color       *string
		enabled     *bool
		description *string
	}, len(state.Data.Viewer.IssueTypes.Nodes))
	for _, node := range state.Data.Viewer.IssueTypes.Nodes {
		byNodeID[node.ID] = struct {
			color       *string
			enabled     *bool
			description *string
		}{color: lowerString(node.Color), enabled: node.IsEnabled, description: node.Description}
	}
	for i := range types {
		live, ok := byNodeID[types[i].NodeID]
		if !ok {
			return nil, fmt.Errorf("personal issue type %q has no GraphQL state", types[i].Name)
		}
		types[i].Color = live.color
		if live.enabled == nil {
			return nil, fmt.Errorf("personal issue type %q has no enabled state", types[i].Name)
		}
		types[i].IsEnabled = live.enabled
		if types[i].Description == nil {
			types[i].Description = live.description
		}
	}
	return types, nil
}

// ListPersonalIssueFields reads the REST identities and GraphQL field
// visibility, supplementing fields because the REST response omits visibility.
func (c *Client) ListPersonalIssueFields(ctx context.Context) ([]PersonalIssueField, error) {
	var fields []PersonalIssueField
	if err := c.do(ctx, http.MethodGet, "/user/issue-fields", nil, &fields); err != nil {
		return nil, err
	}
	var state graphQLIssueFields
	if err := c.do(ctx, http.MethodPost, "/graphql", map[string]string{"query": personalIssueFieldsQuery}, &state); err != nil {
		return nil, fmt.Errorf("read personal issue field state: %w", err)
	}
	if len(state.Errors) > 0 {
		return nil, fmt.Errorf("read personal issue field state: %s", state.Errors[0].Message)
	}
	byNodeID := make(map[string]struct {
		visibility *string
		options    []PersonalIssueFieldOption
	}, len(state.Data.Viewer.IssueFields.Nodes))
	for _, node := range state.Data.Viewer.IssueFields.Nodes {
		options := make([]PersonalIssueFieldOption, 0, len(node.Options))
		for _, option := range node.Options {
			id := int64(0)
			if option.DatabaseID != nil {
				id = int64(*option.DatabaseID)
			}
			options = append(options, PersonalIssueFieldOption{ID: id, Name: option.Name, Description: option.Description, Color: strings.ToLower(option.Color), Priority: option.Priority})
		}
		visibility := lowerString(node.Visibility)
		if visibility != nil && *visibility == "org_only" {
			restricted := "restricted"
			visibility = &restricted
		}
		byNodeID[node.ID] = struct {
			visibility *string
			options    []PersonalIssueFieldOption
		}{visibility: visibility, options: options}
	}
	for i := range fields {
		live, ok := byNodeID[fields[i].NodeID]
		if !ok {
			return nil, fmt.Errorf("personal issue field %q has no GraphQL state", fields[i].Name)
		}
		if live.visibility == nil || *live.visibility == "" {
			return nil, fmt.Errorf("personal issue field %q has no visibility state", fields[i].Name)
		}
		fields[i].Visibility = live.visibility
		if fields[i].DataType == "single_select" || fields[i].DataType == "multi_select" {
			if fields[i].Options != nil {
				for _, option := range fields[i].Options {
					if option.ID == 0 || option.Priority == nil || option.Name == "" || option.Color == "" {
						return nil, fmt.Errorf("personal issue field %q has incomplete REST option state", fields[i].Name)
					}
				}
			}
			if fields[i].Options != nil {
				continue
			}
			for _, option := range live.options {
				if option.ID == 0 || option.Priority == nil || option.Name == "" || option.Color == "" {
					return nil, fmt.Errorf("personal issue field %q has incomplete option state", fields[i].Name)
				}
			}
			fields[i].Options = live.options
		}
	}
	return fields, nil
}

// CreatePersonalIssueType creates an issue type for the authenticated user.
func (c *Client) CreatePersonalIssueType(ctx context.Context, issueType PersonalIssueType) error {
	if issueType.IsEnabled == nil {
		return errors.New("personal issue type isEnabled is required")
	}
	request := struct {
		Name        string  `json:"name"`
		IsEnabled   bool    `json:"is_enabled"`
		Description *string `json:"description"`
		Color       *string `json:"color"`
	}{issueType.Name, *issueType.IsEnabled, issueType.Description, issueType.Color}
	return c.do(ctx, http.MethodPost, "/user/issue-types", request, nil)
}

// UpdatePersonalIssueType replaces the editable issue type settings.
func (c *Client) UpdatePersonalIssueType(ctx context.Context, id int64, issueType PersonalIssueType) error {
	if issueType.IsEnabled == nil {
		return errors.New("personal issue type isEnabled is required")
	}
	request := struct {
		Name        string  `json:"name"`
		IsEnabled   bool    `json:"is_enabled"`
		Description *string `json:"description"`
		Color       *string `json:"color"`
	}{issueType.Name, *issueType.IsEnabled, issueType.Description, issueType.Color}
	return c.do(ctx, http.MethodPut, fmt.Sprintf("/user/issue-types/%d", id), request, nil)
}

// CreatePersonalIssueField creates a field for the authenticated user.
func (c *Client) CreatePersonalIssueField(ctx context.Context, field PersonalIssueField) error {
	request := personalIssueFieldRequest{
		Name: field.Name, Description: field.Description, DataType: field.DataType,
		Visibility: field.Visibility, Options: personalIssueFieldOptions(field.Options),
	}
	return c.do(ctx, http.MethodPost, "/user/issue-fields", request, nil)
}

// UpdatePersonalIssueField patches only the fields supplied by the caller.
func (c *Client) UpdatePersonalIssueField(ctx context.Context, id int64, field PersonalIssueField) error {
	request := personalIssueFieldRequest{
		Name: field.Name, Description: field.Description,
		Visibility: field.Visibility, Options: personalIssueFieldOptions(field.Options),
	}
	return c.doWithoutRetry(ctx, http.MethodPatch, fmt.Sprintf("/user/issue-fields/%d", id), request, nil)
}

func lowerString(value *string) *string {
	if value == nil {
		return nil
	}
	lowered := strings.ToLower(*value)
	return &lowered
}

type personalIssueFieldRequest struct {
	Name        string                          `json:"name,omitempty"`
	Description *string                         `json:"description,omitempty"`
	DataType    string                          `json:"data_type,omitempty"`
	Visibility  *string                         `json:"visibility,omitempty"`
	Options     []personalIssueFieldOptionInput `json:"options,omitempty"`
}

type personalIssueFieldOptionInput struct {
	ID          int64   `json:"id,omitempty"`
	Name        string  `json:"name"`
	Description *string `json:"description,omitempty"`
	Color       string  `json:"color"`
	Priority    *int    `json:"priority"`
}

func personalIssueFieldOptions(options []PersonalIssueFieldOption) []personalIssueFieldOptionInput {
	if options == nil {
		return nil
	}
	result := make([]personalIssueFieldOptionInput, 0, len(options))
	for _, option := range options {
		result = append(result, personalIssueFieldOptionInput{
			ID: option.ID, Name: option.Name, Description: option.Description,
			Color: option.Color, Priority: option.Priority,
		})
	}
	return result
}
