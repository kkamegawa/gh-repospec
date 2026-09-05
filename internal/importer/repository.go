package importer

import (
	"sort"

	"github.com/kkamegawa/gh-repospec/internal/config"
	"github.com/kkamegawa/gh-repospec/internal/githubapi"
)

// mapRepository reverse-maps the repository, features, merge settings,
// archived state, and release immutability into config.RepositoryConfig.
// repository.create and repository.allowVisibilityChange are local intent
// no repository state implies, and are left at their zero value.
func mapRepository(repository githubapi.Repository, info githubapi.RepositoryInfo, immutableReleases githubapi.ImmutableReleases) config.RepositoryConfig {
	description := info.Description
	homepage := info.Homepage
	archived := info.Archived
	releaseImmutability := immutableReleases.Enabled

	return config.RepositoryConfig{
		Owner:         repository.Owner,
		Name:          repository.Name,
		Description:   &description,
		Homepage:      &homepage,
		Visibility:    repositoryVisibility(info),
		DefaultBranch: info.DefaultBranch,
		Archived:      &archived,
		// Topics is declared explicitly, even when empty: import establishes
		// active management of the field, unlike the nil ("unmanaged") case a
		// hand-authored file can still choose.
		Topics: append([]string{}, info.Topics...),
		Features: &config.RepositoryFeatures{
			Issues:      boolPtr(info.HasIssues),
			Projects:    boolPtr(info.HasProjects),
			Wiki:        boolPtr(info.HasWiki),
			Discussions: boolPtr(info.HasDiscussions),
		},
		AllowMergeCommit:         boolPtr(info.AllowMergeCommit),
		AllowSquashMerge:         boolPtr(info.AllowSquashMerge),
		AllowRebaseMerge:         boolPtr(info.AllowRebaseMerge),
		AllowAutoMerge:           boolPtr(info.AllowAutoMerge),
		DeleteBranchOnMerge:      boolPtr(info.DeleteBranchOnMerge),
		MergeCommitTitle:         info.MergeCommitTitle,
		MergeCommitMessage:       info.MergeCommitMessage,
		SquashMergeCommitTitle:   info.SquashMergeCommitTitle,
		SquashMergeCommitMessage: info.SquashMergeCommitMessage,
		ReleaseImmutability:      &releaseImmutability,
	}
}

// repositoryVisibility prefers GitHub's own visibility enum, falling back to
// the private flag for an older API response shape that omits it.
func repositoryVisibility(info githubapi.RepositoryInfo) string {
	if info.Visibility != "" {
		return info.Visibility
	}
	if info.Private {
		return "private"
	}
	return "public"
}

// mapLabels reverse-maps every repository label. The result is nil, not an
// empty declared list, when the repository has no labels: unlike topics,
// labels are already an additive collection with no "unmanaged" distinction
// to preserve, so there is nothing import needs to declare.
func mapLabels(labels []githubapi.Label) []config.LabelConfig {
	var mapped []config.LabelConfig
	for _, label := range labels {
		mapped = append(mapped, config.LabelConfig{
			Name:        label.Name,
			Color:       label.Color,
			Description: label.Description,
		})
	}
	sort.Slice(mapped, func(i, j int) bool { return mapped[i].Name < mapped[j].Name })
	return mapped
}

// mapMilestones reverse-maps every milestone regardless of state, converting
// GitHub's RFC 3339 due_on timestamp to the YYYY-MM-DD form the schema
// requires; only the date part is meaningful to either side.
func mapMilestones(milestones []githubapi.Milestone) []config.MilestoneConfig {
	var mapped []config.MilestoneConfig
	for _, milestone := range milestones {
		mapped = append(mapped, config.MilestoneConfig{
			Title:       milestone.Title,
			Description: milestone.Description,
			State:       milestone.State,
			DueOn:       milestoneDueOnDate(milestone.DueOn),
		})
	}
	sort.Slice(mapped, func(i, j int) bool { return mapped[i].Title < mapped[j].Title })
	return mapped
}

func milestoneDueOnDate(dueOn string) string {
	if len(dueOn) < len("2006-01-02") {
		return ""
	}
	return dueOn[:len("2006-01-02")]
}
