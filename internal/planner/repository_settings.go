package planner

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/kkamegawa/gh-repospec/internal/config"
	"github.com/kkamegawa/gh-repospec/internal/githubapi"
)

type repositoryInteractionService interface {
	GetRepositoryInteractionLimit(context.Context, githubapi.Repository) (githubapi.RepositoryInteractionLimit, error)
	SetRepositoryInteractionLimit(context.Context, githubapi.Repository, githubapi.RepositoryInteractionLimitRequest) (githubapi.RepositoryInteractionLimit, error)
	DeleteRepositoryInteractionLimit(context.Context, githubapi.Repository) error
	GetPullRequestCreationCap(context.Context, githubapi.Repository) (githubapi.PullRequestCreationCap, error)
	UpdatePullRequestCreationCap(context.Context, githubapi.Repository, githubapi.PullRequestCreationCapRequest) (githubapi.PullRequestCreationCap, error)
	ListPullRequestCreationCapBypassUsers(context.Context, githubapi.Repository) ([]githubapi.InteractionBypassUser, error)
	AddPullRequestCreationCapBypassUsers(context.Context, githubapi.Repository, []string) error
	RemovePullRequestCreationCapBypassUsers(context.Context, githubapi.Repository, []string) error
}

type repositorySettingsService interface {
	GetPages(context.Context, githubapi.Repository) (githubapi.PagesSite, error)
	CreatePages(context.Context, githubapi.Repository, githubapi.PagesRequest) (githubapi.PagesSite, error)
	UpdatePages(context.Context, githubapi.Repository, githubapi.PagesRequest) (githubapi.PagesSite, error)
	DeletePages(context.Context, githubapi.Repository) error
	ListWebhooks(context.Context, githubapi.Repository) ([]githubapi.RepositoryWebhook, error)
	CreateWebhook(context.Context, githubapi.Repository, githubapi.RepositoryWebhook) (githubapi.RepositoryWebhook, error)
	UpdateWebhook(context.Context, githubapi.Repository, int64, githubapi.RepositoryWebhook) (githubapi.RepositoryWebhook, error)
	ListAutolinks(context.Context, githubapi.Repository) ([]githubapi.Autolink, error)
	CreateAutolink(context.Context, githubapi.Repository, githubapi.Autolink) (githubapi.Autolink, error)
	DeleteAutolink(context.Context, githubapi.Repository, int64) error
	ListDeployKeys(context.Context, githubapi.Repository) ([]githubapi.DeployKey, error)
	CreateDeployKey(context.Context, githubapi.Repository, githubapi.DeployKey) (githubapi.DeployKey, error)
	DeleteDeployKey(context.Context, githubapi.Repository, int64) error
	ListCollaborators(context.Context, githubapi.Repository) ([]githubapi.Collaborator, error)
	SetCollaboratorPermission(context.Context, githubapi.Repository, string, string) error
	ListRepositoryTeams(context.Context, githubapi.Repository) ([]githubapi.RepositoryTeam, error)
	SetTeamRepositoryPermission(context.Context, githubapi.Repository, string, string) error
	GetCustomPropertyValues(context.Context, githubapi.Repository) ([]githubapi.CustomPropertyValue, error)
	SetCustomPropertyValues(context.Context, githubapi.Repository, []githubapi.CustomPropertyValue) error
	ListProtectedBranches(context.Context, githubapi.Repository) ([]githubapi.Branch, error)
	GetBranchProtection(context.Context, githubapi.Repository, string) (map[string]any, error)
	SetBranchProtection(context.Context, githubapi.Repository, string, map[string]any) error
	GetRequiredSignatures(context.Context, githubapi.Repository, string) (bool, error)
	SetRequiredSignatures(context.Context, githubapi.Repository, string, bool) error
}

func appendRepositorySettingsChanges(ctx context.Context, service Service, plan *Plan, desired *config.RepositorySettings, repository githubapi.Repository, exists, empty bool) {
	if desired == nil {
		return
	}
	api, ok := service.(repositorySettingsService)
	append := func(resource string, state ChangeState, summary string) {
		plan.Changes = append(plan.Changes, Change{Stage: StageSettings, Resource: "settings." + resource, State: state, Summary: summary})
	}
	if desired.InteractionLimit != nil {
		state, summary := ChangeBlocked, "repository interaction settings API is unavailable"
		if !exists {
			state, summary = ChangeCreate, "set repository interaction limit after repository creation"
		} else if interactionAPI, available := service.(repositoryInteractionService); available {
			actual, err := interactionAPI.GetRepositoryInteractionLimit(ctx, repository)
			switch {
			case err != nil:
				summary = capabilityError(err)
			case desired.InteractionLimit.Enabled:
				switch {
				case actual.Limit != "" && actual.Origin != "repository":
					summary = "interaction limit is inherited and cannot be changed at repository scope"
				case actual.Limit == desired.InteractionLimit.Limit && interactionExpiryMatches(desired.InteractionLimit.Expiry, actual.ExpiresAt, time.Now().UTC()):
					state, summary = ChangeNoop, "repository interaction limit already converged"
				default:
					state, summary = ChangeUpdate, "set repository interaction limit"
				}
			case actual.Limit == "":
				state, summary = ChangeNoop, "repository interaction limit already disabled"
			case actual.Origin == "repository":
				state, summary = ChangeUpdate, "remove repository interaction limit"
			default:
				summary = "interaction limit is inherited and cannot be changed at repository scope"
			}
		}
		append("interactionLimit", state, summary)
	}
	if desired.PullRequestCreationCap != nil {
		state, summary := ChangeBlocked, "pull request creation cap API is unavailable"
		if !exists {
			state, summary = ChangeCreate, "set pull request creation cap after repository creation"
		} else if interactionAPI, available := service.(repositoryInteractionService); available {
			actual, err := interactionAPI.GetPullRequestCreationCap(ctx, repository)
			switch {
			case err != nil:
				summary = capabilityError(err)
			case pullRequestCreationCapMatches(*desired.PullRequestCreationCap, actual):
				state, summary = ChangeNoop, "pull request creation cap already converged"
			default:
				state, summary = ChangeUpdate, "converge pull request creation cap"
			}
		}
		append("pullRequestCreationCap", state, summary)
		if desired.PullRequestCreationCap.Enabled && desired.PullRequestCreationCap.BypassUsers != nil {
			state, summary = ChangeBlocked, "pull request creation cap bypass API is unavailable"
			if !exists {
				state, summary = ChangeCreate, "set pull request creation cap bypass users after repository creation"
			} else if interactionAPI, available := service.(repositoryInteractionService); available {
				actual, err := interactionAPI.ListPullRequestCreationCapBypassUsers(ctx, repository)
				switch {
				case err != nil:
					summary = capabilityError(err)
				case sameStrings(*desired.PullRequestCreationCap.BypassUsers, interactionBypassLogins(actual)):
					state, summary = ChangeNoop, "pull request creation cap bypass users already converged"
				default:
					state, summary = ChangeUpdate, "converge pull request creation cap bypass users"
				}
			}
			append("pullRequestCreationCap.bypassUsers", state, summary)
		}
	}
	if desired.Pages != nil {
		switch {
		case !ok:
			append("pages", ChangeBlocked, "repository settings API is unavailable")
		case !exists:
			append("pages", ChangeCreate, "configure GitHub Pages after repository creation")
		default:
			actual, err := api.GetPages(ctx, repository)
			switch {
			case githubapi.IsNotFound(err):
				if desired.Pages.Enabled {
					append("pages", ChangeCreate, "enable GitHub Pages")
				} else {
					append("pages", ChangeNoop, "GitHub Pages already disabled")
				}
			case err != nil:
				append("pages", ChangeBlocked, capabilityError(err))
			case pagesMatch(*desired.Pages, actual):
				append("pages", ChangeNoop, "GitHub Pages settings already converged")
			default:
				append("pages", ChangeUpdate, "converge GitHub Pages settings")
			}
		}
	}
	if !ok {
		for _, item := range desired.Webhooks {
			append("webhook."+item.URL, ChangeBlocked, "repository settings API is unavailable")
		}
		for _, item := range desired.Autolinks {
			append("autolink."+item.KeyPrefix, ChangeBlocked, "repository settings API is unavailable")
		}
		for _, item := range desired.DeployKeys {
			append("deployKey."+item.Title, ChangeBlocked, "repository settings API is unavailable")
		}
		for _, item := range desired.Collaborators {
			append("collaborator."+item.Login, ChangeBlocked, "repository settings API is unavailable")
		}
		for _, item := range desired.Teams {
			append("team."+item.Slug, ChangeBlocked, "repository settings API is unavailable")
		}
		for _, item := range desired.CustomProperties {
			append("customProperty."+item.Name, ChangeBlocked, "repository settings API is unavailable")
		}
		for _, item := range desired.BranchProtections {
			append("branchProtection."+item.Branch, ChangeBlocked, "repository settings API is unavailable")
		}
		return
	}

	if exists {
		webhooks, err := api.ListWebhooks(ctx, repository)
		if err != nil {
			for _, item := range desired.Webhooks {
				append("webhook."+item.URL, ChangeBlocked, capabilityError(err))
			}
		} else {
			for _, item := range desired.Webhooks {
				state := ChangeCreate
				actualURLCount := 0
				desiredURLCount := 0
				for _, actual := range webhooks {
					if actual.Config.URL == item.URL {
						actualURLCount++
						if webhookMatches(item, actual) {
							state = ChangeNoop
						}
					}
				}
				for _, other := range desired.Webhooks {
					if other.URL == item.URL {
						desiredURLCount++
					}
				}
				if state != ChangeNoop && actualURLCount == 1 && desiredURLCount == 1 {
					state = ChangeUpdate
				}
				append("webhook."+item.URL, state, "converge webhook configuration")
			}
		}
		autolinks, err := api.ListAutolinks(ctx, repository)
		if err != nil {
			for _, item := range desired.Autolinks {
				append("autolink."+item.KeyPrefix, ChangeBlocked, capabilityError(err))
			}
		} else {
			for _, item := range desired.Autolinks {
				state := ChangeCreate
				for _, actual := range autolinks {
					if actual.KeyPrefix == item.KeyPrefix {
						state = ChangeUpdate
						if autolinkMatches(item, actual) {
							state = ChangeNoop
						}
						break
					}
				}
				append("autolink."+item.KeyPrefix, state, "converge autolink configuration")
			}
		}
		keys, err := api.ListDeployKeys(ctx, repository)
		if err != nil {
			for _, item := range desired.DeployKeys {
				append("deployKey."+item.Title, ChangeBlocked, capabilityError(err))
			}
		} else {
			for _, item := range desired.DeployKeys {
				state := ChangeCreate
				for _, actual := range keys {
					if actual.Title == item.Title || actual.Key == item.Key {
						state = ChangeUpdate
						if deployKeyMatches(item, actual) {
							state = ChangeNoop
						}
						break
					}
				}
				append("deployKey."+item.Title, state, "converge deploy key")
			}
		}
		collaborators, err := api.ListCollaborators(ctx, repository)
		if err != nil {
			for _, item := range desired.Collaborators {
				append("collaborator."+item.Login, ChangeBlocked, capabilityError(err))
			}
		} else {
			for _, item := range desired.Collaborators {
				state := ChangeCreate
				for _, actual := range collaborators {
					if actual.Login == item.Login {
						state = ChangeUpdate
						if actual.EffectivePermission() == item.Permission {
							state = ChangeNoop
						}
						break
					}
				}
				append("collaborator."+item.Login, state, "converge collaborator access")
			}
		}
		teams, err := api.ListRepositoryTeams(ctx, repository)
		if err != nil {
			for _, item := range desired.Teams {
				append("team."+item.Slug, ChangeBlocked, capabilityError(err))
			}
		} else {
			for _, item := range desired.Teams {
				state := ChangeCreate
				for _, actual := range teams {
					if actual.Slug == item.Slug {
						state = ChangeUpdate
						if actual.Permission == item.Permission {
							state = ChangeNoop
						}
						break
					}
				}
				append("team."+item.Slug, state, "converge team access")
			}
		}
		properties, err := api.GetCustomPropertyValues(ctx, repository)
		if err != nil {
			for _, item := range desired.CustomProperties {
				append("customProperty."+item.Name, ChangeBlocked, capabilityError(err))
			}
		} else {
			actualProperties := make(map[string]any, len(properties))
			for _, item := range properties {
				actualProperties[item.PropertyName] = item.Value
			}
			for _, item := range desired.CustomProperties {
				state := ChangeCreate
				if value, found := actualProperties[item.Name]; found {
					state = ChangeUpdate
					if jsonEqual(item.Value, value) {
						state = ChangeNoop
					}
				}
				append("customProperty."+item.Name, state, "converge custom property value")
			}
		}
		protected, err := api.ListProtectedBranches(ctx, repository)
		if err != nil {
			for _, item := range desired.BranchProtections {
				append("branchProtection."+item.Branch, ChangeBlocked, capabilityError(err))
			}
		} else {
			actualBranches := make(map[string]bool, len(protected))
			for _, item := range protected {
				actualBranches[item.Name] = true
			}
			for _, item := range desired.BranchProtections {
				if !actualBranches[item.Branch] {
					state := ChangeBlocked
					if empty {
						state = ChangeCreate
					} else if branches, canCheckBranch := service.(treeService); canCheckBranch {
						if _, branchErr := branches.GetBranchHead(ctx, repository, item.Branch); branchErr == nil {
							state = ChangeCreate
						} else if !githubapi.IsNotFound(branchErr) {
							append("branchProtection."+item.Branch, ChangeBlocked, capabilityError(branchErr))
							continue
						}
					}
					summary := "configure protection after target branch exists"
					if state == ChangeBlocked {
						summary = "target branch does not exist or its status could not be read"
					}
					append("branchProtection."+item.Branch, state, summary)
					continue
				}
				actual, getErr := api.GetBranchProtection(ctx, repository, item.Branch)
				switch {
				case getErr != nil:
					append("branchProtection."+item.Branch, ChangeBlocked, capabilityError(getErr))
				case jsonEqual(item.Settings, githubapi.NormalizeBranchProtection(actual)):
					append("branchProtection."+item.Branch, ChangeNoop, "branch protection already converged")
				default:
					append("branchProtection."+item.Branch, ChangeUpdate, "converge classic branch protection")
				}
				if item.RequireSignedCommits != nil {
					signed, signatureErr := api.GetRequiredSignatures(ctx, repository, item.Branch)
					if githubapi.IsNotFound(signatureErr) {
						signed, signatureErr = false, nil
					}
					switch {
					case signatureErr != nil:
						append("branchProtection."+item.Branch+".requireSignedCommits", ChangeBlocked, capabilityError(signatureErr))
					case signed == *item.RequireSignedCommits:
						append("branchProtection."+item.Branch+".requireSignedCommits", ChangeNoop, "signed commit requirement already converged")
					default:
						append("branchProtection."+item.Branch+".requireSignedCommits", ChangeUpdate, "converge signed commit requirement")
					}
				}
			}
		}
	} else {
		for _, item := range desired.Webhooks {
			append("webhook."+item.URL, ChangeCreate, "create after repository creation")
		}
		for _, item := range desired.Autolinks {
			append("autolink."+item.KeyPrefix, ChangeCreate, "create after repository creation")
		}
		for _, item := range desired.DeployKeys {
			append("deployKey."+item.Title, ChangeCreate, "create after repository creation")
		}
		for _, item := range desired.Collaborators {
			append("collaborator."+item.Login, ChangeCreate, "create after repository creation")
		}
		for _, item := range desired.Teams {
			append("team."+item.Slug, ChangeCreate, "create after repository creation")
		}
		for _, item := range desired.CustomProperties {
			append("customProperty."+item.Name, ChangeCreate, "set after repository creation")
		}
		for _, item := range desired.BranchProtections {
			append("branchProtection."+item.Branch, ChangeCreate, "configure after target branch exists")
		}
	}
}

func (e *Engine) applyRepositorySettings(ctx context.Context, repository githubapi.Repository, desired *config.RepositorySettings) error {
	if desired == nil {
		return nil
	}
	if err := applyRepositoryInteractionSettings(ctx, e.service, repository, desired); err != nil {
		return err
	}
	api, ok := e.service.(repositorySettingsService)
	if !ok {
		return fmt.Errorf("repository settings API is unavailable")
	}
	if desired.Pages != nil {
		actual, err := api.GetPages(ctx, repository)
		if !desired.Pages.Enabled {
			if err == nil {
				if err := api.DeletePages(ctx, repository); err != nil {
					return fmt.Errorf("disable Pages for %s: %w", repository, err)
				}
			} else if !githubapi.IsNotFound(err) {
				return fmt.Errorf("get Pages for %s: %w", repository, err)
			}
		} else {
			request := pagesRequest(*desired.Pages)
			switch {
			case githubapi.IsNotFound(err):
				createRequest := githubapi.PagesRequest{BuildType: request.BuildType, Source: request.Source}
				if _, err = api.CreatePages(ctx, repository, createRequest); err != nil {
					return fmt.Errorf("create Pages for %s: %w", repository, err)
				}
				if request.CNAME != nil || request.HTTPSEnforced != nil {
					if _, err = api.UpdatePages(ctx, repository, request); err != nil {
						return fmt.Errorf("set Pages domain and HTTPS for %s: %w", repository, err)
					}
				}
			case err != nil:
				return fmt.Errorf("get Pages for %s: %w", repository, err)
			case !pagesMatch(*desired.Pages, actual):
				if _, err = api.UpdatePages(ctx, repository, request); err != nil {
					return fmt.Errorf("update Pages for %s: %w", repository, err)
				}
			}
		}
	}
	var webhooks []githubapi.RepositoryWebhook
	var err error
	if len(desired.Webhooks) > 0 {
		webhooks, err = api.ListWebhooks(ctx, repository)
	}
	if err != nil {
		return fmt.Errorf("list webhooks for %s: %w", repository, err)
	}
	for _, item := range desired.Webhooks {
		request := webhookRequest(item)
		var exact, sameURL *githubapi.RepositoryWebhook
		actualURLCount, desiredURLCount := 0, 0
		for _, actual := range webhooks {
			if actual.Config.URL != item.URL {
				continue
			}
			actualURLCount++
			if sameURL == nil {
				copy := actual
				sameURL = &copy
			}
			if webhookMatches(item, actual) {
				copy := actual
				exact = &copy
				break
			}
		}
		for _, other := range desired.Webhooks {
			if other.URL == item.URL {
				desiredURLCount++
			}
		}
		if exact != nil {
			continue
		}
		if actualURLCount == 1 && desiredURLCount == 1 && sameURL != nil {
			if _, err := api.UpdateWebhook(ctx, repository, sameURL.ID, request); err != nil {
				return fmt.Errorf("update webhook %q: %w", item.URL, err)
			}
		} else {
			// GitHub requires name "web" to create a repository webhook. It
			// identifies the hook kind rather than describing configuration,
			// so it is sent only on create and never compared.
			request.Name = "web"
			if _, err := api.CreateWebhook(ctx, repository, request); err != nil {
				return fmt.Errorf("create webhook %q: %w", item.URL, err)
			}
		}
	}
	var autolinks []githubapi.Autolink
	if len(desired.Autolinks) > 0 {
		autolinks, err = api.ListAutolinks(ctx, repository)
	}
	if err != nil {
		return fmt.Errorf("list autolinks for %s: %w", repository, err)
	}
	for _, item := range desired.Autolinks {
		request := githubapi.Autolink{KeyPrefix: item.KeyPrefix, URLTemplate: item.URLTemplate, IsAlphanumeric: item.IsAlphanumeric}
		found := false
		for _, actual := range autolinks {
			if actual.KeyPrefix != item.KeyPrefix {
				continue
			}
			found = true
			if !autolinkMatches(item, actual) {
				if err := api.DeleteAutolink(ctx, repository, actual.ID); err != nil {
					return fmt.Errorf("replace autolink %q: %w", item.KeyPrefix, err)
				}
				if _, err := api.CreateAutolink(ctx, repository, request); err != nil {
					return fmt.Errorf("recreate autolink %q: %w", item.KeyPrefix, err)
				}
			}
			break
		}
		if !found {
			if _, err := api.CreateAutolink(ctx, repository, request); err != nil {
				return fmt.Errorf("create autolink %q: %w", item.KeyPrefix, err)
			}
		}
	}
	var keys []githubapi.DeployKey
	if len(desired.DeployKeys) > 0 {
		keys, err = api.ListDeployKeys(ctx, repository)
	}
	if err != nil {
		return fmt.Errorf("list deploy keys for %s: %w", repository, err)
	}
	for _, item := range desired.DeployKeys {
		request := githubapi.DeployKey{Title: item.Title, Key: item.Key, ReadOnly: item.ReadOnly}
		found := false
		for _, actual := range keys {
			if actual.Title != item.Title && actual.Key != item.Key {
				continue
			}
			found = true
			if !deployKeyMatches(item, actual) {
				if err := api.DeleteDeployKey(ctx, repository, actual.ID); err != nil {
					return fmt.Errorf("replace deploy key %q: %w", item.Title, err)
				}
				if _, err := api.CreateDeployKey(ctx, repository, request); err != nil {
					return fmt.Errorf("recreate deploy key %q: %w", item.Title, err)
				}
			}
			break
		}
		if !found {
			if _, err := api.CreateDeployKey(ctx, repository, request); err != nil {
				return fmt.Errorf("create deploy key %q: %w", item.Title, err)
			}
		}
	}
	var collaborators []githubapi.Collaborator
	if len(desired.Collaborators) > 0 {
		collaborators, err = api.ListCollaborators(ctx, repository)
	}
	if err != nil {
		return fmt.Errorf("list collaborators for %s: %w", repository, err)
	}
	for _, item := range desired.Collaborators {
		found, permission := false, ""
		for _, actual := range collaborators {
			if actual.Login == item.Login {
				found, permission = true, actual.EffectivePermission()
				break
			}
		}
		if !found || permission != item.Permission {
			if err := api.SetCollaboratorPermission(ctx, repository, item.Login, item.Permission); err != nil {
				return fmt.Errorf("set collaborator %q permission: %w", item.Login, err)
			}
		}
	}
	var teams []githubapi.RepositoryTeam
	if len(desired.Teams) > 0 {
		teams, err = api.ListRepositoryTeams(ctx, repository)
	}
	if err != nil {
		return fmt.Errorf("list repository teams for %s: %w", repository, err)
	}
	for _, item := range desired.Teams {
		found, permission := false, ""
		for _, actual := range teams {
			if actual.Slug == item.Slug {
				found, permission = true, actual.Permission
				break
			}
		}
		if !found || permission != item.Permission {
			if err := api.SetTeamRepositoryPermission(ctx, repository, item.Slug, item.Permission); err != nil {
				return fmt.Errorf("set team %q permission: %w", item.Slug, err)
			}
		}
	}
	var properties []githubapi.CustomPropertyValue
	if len(desired.CustomProperties) > 0 {
		properties, err = api.GetCustomPropertyValues(ctx, repository)
	}
	if err != nil && !githubapi.IsNotFound(err) {
		return fmt.Errorf("get custom properties for %s: %w", repository, err)
	}
	actualProperties := make(map[string]any, len(properties))
	for _, item := range properties {
		actualProperties[item.PropertyName] = item.Value
	}
	propertyRequests := make([]githubapi.CustomPropertyValue, 0, len(desired.CustomProperties))
	propertiesChanged := false
	for _, item := range desired.CustomProperties {
		propertyRequests = append(propertyRequests, githubapi.CustomPropertyValue{PropertyName: item.Name, Value: item.Value})
		if !jsonEqual(item.Value, actualProperties[item.Name]) {
			propertiesChanged = true
		}
	}
	if propertiesChanged {
		if err := api.SetCustomPropertyValues(ctx, repository, propertyRequests); err != nil {
			return fmt.Errorf("set custom property values for %s: %w", repository, err)
		}
	}
	if len(desired.BranchProtections) > 0 {
		for _, item := range desired.BranchProtections {
			actual, err := api.GetBranchProtection(ctx, repository, item.Branch)
			switch {
			case githubapi.IsNotFound(err):
				if err := api.SetBranchProtection(ctx, repository, item.Branch, item.Settings); err != nil {
					return fmt.Errorf("set branch protection %q: %w", item.Branch, err)
				}
			case err != nil:
				return fmt.Errorf("get branch protection %q: %w", item.Branch, err)
			case !jsonEqual(item.Settings, githubapi.NormalizeBranchProtection(actual)):
				if err := api.SetBranchProtection(ctx, repository, item.Branch, item.Settings); err != nil {
					return fmt.Errorf("set branch protection %q: %w", item.Branch, err)
				}
			}
			if item.RequireSignedCommits != nil {
				signed, err := api.GetRequiredSignatures(ctx, repository, item.Branch)
				if githubapi.IsNotFound(err) {
					signed, err = false, nil
				}
				if err != nil {
					return fmt.Errorf("get required-signature protection for branch %q: %w", item.Branch, err)
				}
				if signed != *item.RequireSignedCommits {
					if err := api.SetRequiredSignatures(ctx, repository, item.Branch, *item.RequireSignedCommits); err != nil {
						return fmt.Errorf("set required-signature protection for branch %q: %w", item.Branch, err)
					}
				}
			}
		}
	}
	return nil
}

func applyRepositoryInteractionSettings(ctx context.Context, service Service, repository githubapi.Repository, desired *config.RepositorySettings) error {
	if desired.InteractionLimit == nil && desired.PullRequestCreationCap == nil {
		return nil
	}
	api, ok := service.(repositoryInteractionService)
	if !ok {
		return fmt.Errorf("repository interaction settings API is unavailable")
	}
	if desired.InteractionLimit != nil {
		actual, err := api.GetRepositoryInteractionLimit(ctx, repository)
		if err != nil {
			return fmt.Errorf("get repository interaction limit for %s: %w", repository, err)
		}
		if !desired.InteractionLimit.Enabled {
			if actual.Limit != "" && actual.Origin != "repository" {
				return fmt.Errorf("cannot remove inherited repository interaction limit for %s", repository)
			}
			if actual.Limit != "" {
				if err := api.DeleteRepositoryInteractionLimit(ctx, repository); err != nil {
					return fmt.Errorf("remove repository interaction limit for %s: %w", repository, err)
				}
			}
		} else {
			if actual.Limit != "" && actual.Origin != "repository" {
				return fmt.Errorf("cannot replace inherited repository interaction limit for %s", repository)
			}
			if actual.Limit != desired.InteractionLimit.Limit || !interactionExpiryMatches(desired.InteractionLimit.Expiry, actual.ExpiresAt, time.Now().UTC()) {
				request := githubapi.RepositoryInteractionLimitRequest{Limit: desired.InteractionLimit.Limit, Expiry: desired.InteractionLimit.Expiry}
				if _, err := api.SetRepositoryInteractionLimit(ctx, repository, request); err != nil {
					return fmt.Errorf("set repository interaction limit for %s: %w", repository, err)
				}
			}
		}
	}
	if desired.PullRequestCreationCap == nil {
		return nil
	}
	actualCap, err := api.GetPullRequestCreationCap(ctx, repository)
	if err != nil {
		return fmt.Errorf("get pull request creation cap for %s: %w", repository, err)
	}
	if !pullRequestCreationCapMatches(*desired.PullRequestCreationCap, actualCap) {
		request := githubapi.PullRequestCreationCapRequest{Enabled: desired.PullRequestCreationCap.Enabled}
		if desired.PullRequestCreationCap.Enabled {
			maximum := desired.PullRequestCreationCap.MaxOpenPullRequests
			includeDrafts := desired.PullRequestCreationCap.IncludeDrafts
			request.MaxOpenPullRequests = &maximum
			request.IncludeDrafts = &includeDrafts
		}
		if _, err := api.UpdatePullRequestCreationCap(ctx, repository, request); err != nil {
			return fmt.Errorf("update pull request creation cap for %s: %w", repository, err)
		}
	}
	if !desired.PullRequestCreationCap.Enabled || desired.PullRequestCreationCap.BypassUsers == nil {
		return nil
	}
	actualUsers, err := api.ListPullRequestCreationCapBypassUsers(ctx, repository)
	if err != nil {
		return fmt.Errorf("list pull request creation cap bypass users for %s: %w", repository, err)
	}
	wanted := make(map[string]bool, len(*desired.PullRequestCreationCap.BypassUsers))
	for _, login := range *desired.PullRequestCreationCap.BypassUsers {
		wanted[strings.ToLower(login)] = true
	}
	toRemove, toAdd := make([]string, 0), make([]string, 0)
	existing := make(map[string]bool, len(actualUsers))
	for _, user := range actualUsers {
		key := strings.ToLower(user.Login)
		existing[key] = true
		if !wanted[key] {
			toRemove = append(toRemove, user.Login)
		}
	}
	for _, login := range *desired.PullRequestCreationCap.BypassUsers {
		if !existing[strings.ToLower(login)] {
			toAdd = append(toAdd, login)
		}
	}
	sort.Strings(toRemove)
	sort.Strings(toAdd)
	for _, batch := range stringBatches(toRemove, 100) {
		if err := api.RemovePullRequestCreationCapBypassUsers(ctx, repository, batch); err != nil {
			return fmt.Errorf("remove pull request creation cap bypass users for %s: %w", repository, err)
		}
	}
	for _, batch := range stringBatches(toAdd, 100) {
		if err := api.AddPullRequestCreationCapBypassUsers(ctx, repository, batch); err != nil {
			return fmt.Errorf("add pull request creation cap bypass users for %s: %w", repository, err)
		}
	}
	return nil
}

func pullRequestCreationCapMatches(desired config.PullRequestCreationCapConfig, actual githubapi.PullRequestCreationCap) bool {
	if desired.Enabled != actual.Enabled {
		return false
	}
	return !desired.Enabled || desired.MaxOpenPullRequests == actual.MaxOpenPullRequests && desired.IncludeDrafts == actual.IncludeDrafts
}

func interactionBypassLogins(users []githubapi.InteractionBypassUser) []string {
	logins := make([]string, 0, len(users))
	for _, user := range users {
		logins = append(logins, user.Login)
	}
	return logins
}

func interactionExpiryMatches(desired, expiresAt string, now time.Time) bool {
	expires, err := time.Parse(time.RFC3339, expiresAt)
	if err != nil || expires.Sub(now) <= 0 {
		return false
	}
	remaining := expires.Sub(now)
	switch {
	case remaining <= 24*time.Hour:
		return desired == "one_day"
	case remaining <= 3*24*time.Hour:
		return desired == "three_days"
	case remaining <= 7*24*time.Hour:
		return desired == "one_week"
	case remaining <= 32*24*time.Hour:
		return desired == "one_month"
	default:
		return desired == "six_months"
	}
}

func stringBatches(values []string, size int) [][]string {
	var batches [][]string
	for len(values) > 0 {
		count := min(size, len(values))
		batches = append(batches, values[:count])
		values = values[count:]
	}
	return batches
}

func pagesRequest(value config.PagesConfig) githubapi.PagesRequest {
	request := githubapi.PagesRequest{BuildType: value.BuildType, CNAME: value.CNAME, HTTPSEnforced: value.HTTPSRequired}
	if value.Source != nil {
		request.Source = &githubapi.PagesSource{Branch: value.Source.Branch, Path: value.Source.Path}
	}
	return request
}

func pagesMatch(desired config.PagesConfig, actual githubapi.PagesSite) bool {
	if !desired.Enabled {
		return false
	}
	if desired.BuildType != "" && desired.BuildType != actual.BuildType || desired.CNAME != nil && *desired.CNAME != actual.CNAME {
		return false
	}
	if desired.HTTPSRequired != nil && *desired.HTTPSRequired != actual.HTTPSEnforced {
		return false
	}
	if desired.Source == nil {
		return actual.Source == nil
	}
	return actual.Source != nil && desired.Source.Branch == actual.Source.Branch && desired.Source.Path == actual.Source.Path
}

func webhookRequest(item config.WebhookConfig) githubapi.RepositoryWebhook {
	events := append([]string(nil), item.Events...)
	sort.Strings(events)
	return githubapi.RepositoryWebhook{Active: item.Active, Events: events, Config: githubapi.WebhookConfig{URL: item.URL, ContentType: item.ContentType, InsecureSSL: item.InsecureSSL}}
}

func webhookMatches(desired config.WebhookConfig, actual githubapi.RepositoryWebhook) bool {
	events := append([]string(nil), desired.Events...)
	sort.Strings(events)
	actualEvents := append([]string(nil), actual.Events...)
	sort.Strings(actualEvents)
	return desired.URL == actual.Config.URL && desired.ContentType == actual.Config.ContentType && desired.InsecureSSL == actual.Config.InsecureSSL && desired.Active == actual.Active && reflect.DeepEqual(events, actualEvents)
}

func autolinkMatches(desired config.AutolinkConfig, actual githubapi.Autolink) bool {
	return desired.KeyPrefix == actual.KeyPrefix && desired.URLTemplate == actual.URLTemplate && desired.IsAlphanumeric == actual.IsAlphanumeric
}

func deployKeyMatches(desired config.DeployKeyConfig, actual githubapi.DeployKey) bool {
	return desired.Title == actual.Title && desired.Key == actual.Key && desired.ReadOnly == actual.ReadOnly
}

func jsonEqual(left, right any) bool {
	a, errA := json.Marshal(left)
	b, errB := json.Marshal(right)
	return errA == nil && errB == nil && string(a) == string(b)
}
