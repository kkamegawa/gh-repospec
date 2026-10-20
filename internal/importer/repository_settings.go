package importer

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/kkamegawa/gh-repospec/internal/config"
	"github.com/kkamegawa/gh-repospec/internal/githubapi"
)

func importRepositorySettings(ctx context.Context, reader Reader, repository githubapi.Repository, ownerType string) (*config.RepositorySettings, []Diagnostic, error) {
	settings := &config.RepositorySettings{}
	var diagnostics []Diagnostic

	interactionLimit, err := reader.GetRepositoryInteractionLimit(ctx, repository)
	if err != nil {
		return nil, nil, fmt.Errorf("get repository interaction limit for %s: %w", repository, err)
	}
	if interactionLimit.Limit == "" {
		settings.InteractionLimit = &config.InteractionLimitConfig{Enabled: false}
	} else if interactionLimit.Origin != "repository" {
		diagnostics = append(diagnostics, Diagnostic{Resource: "settings.interactionLimit", Message: "interaction limit is inherited from the user or organization and cannot be imported as a repository setting"})
	} else {
		expiry, err := interactionExpiryPreset(interactionLimit.ExpiresAt, time.Now().UTC())
		if err != nil {
			return nil, nil, fmt.Errorf("map repository interaction limit expiry for %s: %w", repository, err)
		}
		settings.InteractionLimit = &config.InteractionLimitConfig{Enabled: true, Limit: interactionLimit.Limit, Expiry: expiry}
		diagnostics = append(diagnostics, Diagnostic{Resource: "settings.interactionLimit.expiresAt", Message: "GitHub exposes an absolute expiry timestamp but accepts only a relative preset; imported expiry may extend the source restriction"})
	}

	creationCap, err := reader.GetPullRequestCreationCap(ctx, repository)
	if err != nil {
		return nil, nil, fmt.Errorf("get pull request creation cap for %s: %w", repository, err)
	}
	settings.PullRequestCreationCap = &config.PullRequestCreationCapConfig{
		Enabled: creationCap.Enabled, MaxOpenPullRequests: creationCap.MaxOpenPullRequests,
		IncludeDrafts: creationCap.IncludeDrafts,
	}
	logins := make([]string, 0)
	if creationCap.Enabled {
		bypassUsers, err := reader.ListPullRequestCreationCapBypassUsers(ctx, repository)
		if err != nil {
			return nil, nil, fmt.Errorf("list pull request creation cap bypass users for %s: %w", repository, err)
		}
		logins = make([]string, 0, len(bypassUsers))
		for _, user := range bypassUsers {
			if user.Login == "" {
				return nil, nil, fmt.Errorf("pull request creation cap bypass list for %s returned a user without login", repository)
			}
			logins = append(logins, user.Login)
		}
	}
	sort.Strings(logins)
	settings.PullRequestCreationCap.BypassUsers = &logins

	pages, err := reader.GetPages(ctx, repository)
	switch {
	case err == nil:
		httpsRequired := pages.HTTPSEnforced
		cname := pages.CNAME
		settings.Pages = &config.PagesConfig{
			Enabled: true, BuildType: pages.BuildType,
			CNAME: &cname, HTTPSRequired: &httpsRequired,
		}
		if pages.Source != nil {
			settings.Pages.Source = &config.PagesSource{Branch: pages.Source.Branch, Path: pages.Source.Path}
		}
	case githubapi.IsNotFound(err):
		settings.Pages = &config.PagesConfig{Enabled: false}
	default:
		return nil, nil, fmt.Errorf("get GitHub Pages settings for %s: %w", repository, err)
	}

	webhooks, err := reader.ListWebhooks(ctx, repository)
	if err != nil {
		return nil, nil, fmt.Errorf("list webhooks for %s: %w", repository, err)
	}
	for _, hook := range webhooks {
		settings.Webhooks = append(settings.Webhooks, config.WebhookConfig{
			URL: hook.Config.URL, ContentType: hook.Config.ContentType,
			InsecureSSL: hook.Config.InsecureSSL, Events: append([]string(nil), hook.Events...), Active: hook.Active,
		})
		diagnostics = append(diagnostics, Diagnostic{Resource: "settings.webhooks." + hook.Config.URL + ".secret", Message: "GitHub does not return webhook secret values; webhook secrets are excluded by request"})
	}
	sort.Slice(settings.Webhooks, func(i, j int) bool { return settings.Webhooks[i].URL < settings.Webhooks[j].URL })

	autolinks, err := reader.ListAutolinks(ctx, repository)
	if err != nil {
		return nil, nil, fmt.Errorf("list autolinks for %s: %w", repository, err)
	}
	for _, link := range autolinks {
		settings.Autolinks = append(settings.Autolinks, config.AutolinkConfig{
			KeyPrefix: link.KeyPrefix, URLTemplate: link.URLTemplate, IsAlphanumeric: link.IsAlphanumeric,
		})
	}
	sort.Slice(settings.Autolinks, func(i, j int) bool { return settings.Autolinks[i].KeyPrefix < settings.Autolinks[j].KeyPrefix })

	keys, err := reader.ListDeployKeys(ctx, repository)
	if err != nil {
		return nil, nil, fmt.Errorf("list deploy keys for %s: %w", repository, err)
	}
	for _, key := range keys {
		settings.DeployKeys = append(settings.DeployKeys, config.DeployKeyConfig{Title: key.Title, Key: key.Key, ReadOnly: key.ReadOnly})
	}
	sort.Slice(settings.DeployKeys, func(i, j int) bool { return settings.DeployKeys[i].Title < settings.DeployKeys[j].Title })

	collaborators, err := reader.ListCollaborators(ctx, repository)
	if err != nil {
		return nil, nil, fmt.Errorf("list collaborators for %s: %w", repository, err)
	}
	for _, collaborator := range collaborators {
		permission := collaborator.EffectivePermission()
		if collaborator.Login == "" || permission == "" {
			return nil, nil, fmt.Errorf("list collaborators for %s returned an entry without login or permission", repository)
		}
		if strings.EqualFold(collaborator.Login, repository.Owner) {
			continue // The repository owner has implicit access and cannot be copied as a collaborator.
		}
		settings.Collaborators = append(settings.Collaborators, config.CollaboratorConfig{Login: collaborator.Login, Permission: permission})
		diagnostics = append(diagnostics, Diagnostic{Resource: "settings.collaborators." + collaborator.Login, Message: "GitHub returns effective access from repository, teams, organization, and enterprise together; direct grant source cannot be determined"})
	}
	sort.Slice(settings.Collaborators, func(i, j int) bool { return settings.Collaborators[i].Login < settings.Collaborators[j].Login })

	if ownerType == "Organization" {
		teams, err := reader.ListRepositoryTeams(ctx, repository)
		if err != nil {
			return nil, nil, fmt.Errorf("list team access for %s: %w", repository, err)
		}
		for _, team := range teams {
			if team.Slug == "" || team.Permission == "" {
				return nil, nil, fmt.Errorf("list team access for %s returned an entry without slug or permission", repository)
			}
			settings.Teams = append(settings.Teams, config.TeamAccessConfig{Slug: team.Slug, Permission: team.Permission})
		}
		sort.Slice(settings.Teams, func(i, j int) bool { return settings.Teams[i].Slug < settings.Teams[j].Slug })
	}

	properties, err := reader.GetCustomPropertyValues(ctx, repository)
	if err != nil {
		if githubapi.IsNotFound(err) {
			diagnostics = append(diagnostics, Diagnostic{Resource: "settings.customProperties", Message: "GitHub did not expose custom property values for this repository"})
		} else {
			return nil, nil, fmt.Errorf("get custom property values for %s: %w", repository, err)
		}
	} else {
		for _, property := range properties {
			settings.CustomProperties = append(settings.CustomProperties, config.CustomPropertyConfig{Name: property.PropertyName, Value: property.Value})
		}
		sort.Slice(settings.CustomProperties, func(i, j int) bool { return settings.CustomProperties[i].Name < settings.CustomProperties[j].Name })
	}

	branches, err := reader.ListProtectedBranches(ctx, repository)
	if err != nil {
		return nil, nil, fmt.Errorf("list protected branches for %s: %w", repository, err)
	}
	for _, branch := range branches {
		protection, err := reader.GetBranchProtection(ctx, repository, branch.Name)
		if githubapi.IsNotFound(err) {
			// GitHub marks branches protected by rulesets as protected in the
			// branches list too. Those have no classic branch-protection
			// resource and are already imported through the rulesets endpoint.
			continue
		}
		if err != nil {
			return nil, nil, fmt.Errorf("get protection for branch %q: %w", branch.Name, err)
		}
		signed, err := reader.GetRequiredSignatures(ctx, repository, branch.Name)
		if githubapi.IsNotFound(err) {
			// GitHub returns 404 when required-signature protection is not
			// enabled. The classic branch protection itself was read above.
			signed = false
		} else if err != nil {
			return nil, nil, fmt.Errorf("get required-signature protection for branch %q: %w", branch.Name, err)
		}
		settings.BranchProtections = append(settings.BranchProtections, config.BranchProtectionConfig{
			Branch: branch.Name, Settings: githubapi.NormalizeBranchProtection(protection), RequireSignedCommits: boolPtr(signed),
		})
	}
	sort.Slice(settings.BranchProtections, func(i, j int) bool {
		return settings.BranchProtections[i].Branch < settings.BranchProtections[j].Branch
	})

	return settings, diagnostics, nil
}

func interactionExpiryPreset(expiresAt string, now time.Time) (string, error) {
	expires, err := time.Parse(time.RFC3339, expiresAt)
	if err != nil {
		return "", fmt.Errorf("parse expires_at %q: %w", expiresAt, err)
	}
	remaining := expires.Sub(now)
	if remaining <= 0 {
		return "", fmt.Errorf("expires_at %q is not in the future", expiresAt)
	}
	switch {
	case remaining <= 24*time.Hour:
		return "one_day", nil
	case remaining <= 3*24*time.Hour:
		return "three_days", nil
	case remaining <= 7*24*time.Hour:
		return "one_week", nil
	case remaining <= 32*24*time.Hour:
		return "one_month", nil
	default:
		return "six_months", nil
	}
}
