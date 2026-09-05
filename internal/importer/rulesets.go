package importer

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/kkamegawa/gh-repospec/internal/config"
	"github.com/kkamegawa/gh-repospec/internal/githubapi"
)

// importRulesets fetches every ruleset and reverse-maps the ones this schema
// can represent. A ruleset that is inherited from the organization or
// enterprise, or that this schema cannot represent (no ref_name include
// condition, or a bypass actor of an unrecognized type), is reported as a
// diagnostic and omitted rather than silently reduced. Fetching a specific
// ruleset's detail is required because ListRulesets returns only identity,
// target, and enforcement: never rules, conditions, or bypass actors.
func importRulesets(ctx context.Context, reader Reader, repository githubapi.Repository) ([]config.RulesetConfig, []Diagnostic, error) {
	summaries, err := reader.ListRulesets(ctx, repository)
	if err != nil {
		return nil, nil, fmt.Errorf("list rulesets for %s: %w", repository, err)
	}

	var (
		mapped      []config.RulesetConfig
		diagnostics []Diagnostic
	)
	for _, summary := range summaries {
		if summary.SourceType != "" && !strings.EqualFold(summary.SourceType, "Repository") {
			diagnostics = append(diagnostics, Diagnostic{
				Resource: "ruleset." + summary.Name,
				Message:  fmt.Sprintf("inherited from the %s and cannot be represented as a declared ruleset", strings.ToLower(summary.SourceType)),
			})
			continue
		}
		detail, err := reader.GetRuleset(ctx, repository, summary.ID)
		if err != nil {
			return nil, nil, fmt.Errorf("get ruleset %q for %s: %w", summary.Name, repository, err)
		}
		ruleset, err := mapRuleset(detail)
		if err != nil {
			diagnostics = append(diagnostics, Diagnostic{Resource: "ruleset." + summary.Name, Message: err.Error()})
			continue
		}
		mapped = append(mapped, ruleset)
	}
	sort.Slice(mapped, func(i, j int) bool { return mapped[i].Name < mapped[j].Name })
	return mapped, diagnostics, nil
}

// mapRuleset reverse-maps one ruleset's full definition. Server-generated ID
// and timestamps are omitted; the ruleset name is the stable identity, as
// required for import output to be applied again without recreating rulesets.
func mapRuleset(detail githubapi.Ruleset) (config.RulesetConfig, error) {
	include, exclude := conditionRefs(detail.Conditions)
	if len(include) == 0 {
		return config.RulesetConfig{}, fmt.Errorf("has no ref_name include condition, which the schema requires")
	}

	actors := make([]config.BypassActorConfig, 0, len(detail.BypassActors))
	for _, actor := range detail.BypassActors {
		mappedActor, err := mapBypassActor(actor)
		if err != nil {
			return config.RulesetConfig{}, err
		}
		actors = append(actors, mappedActor)
	}

	rules := make([]config.RulesetRule, 0, len(detail.Rules))
	for _, rule := range detail.Rules {
		ruleType, _ := rule["type"].(string)
		if ruleType == "" {
			return config.RulesetConfig{}, fmt.Errorf("has a rule with no type")
		}
		rules = append(rules, config.RulesetRule{Type: ruleType, Parameters: asMap(rule["parameters"])})
	}

	return config.RulesetConfig{
		Name:         detail.Name,
		Enforcement:  detail.Enforcement,
		Target:       detail.Target,
		Include:      include,
		Exclude:      exclude,
		BypassActors: actors,
		Rules:        rules,
	}, nil
}

// conditionRefs extracts the ref_name include/exclude patterns from a
// ruleset's conditions. GitHub shapes this as
// {"ref_name":{"include":[...],"exclude":[...]}}; any other or absent shape
// (a push or repository target ruleset, which does not use ref_name
// conditions) yields no include patterns, which mapRuleset treats as
// unrepresentable because the schema requires a non-empty include list.
func conditionRefs(conditions map[string]any) (include, exclude []string) {
	refName := asMap(conditions["ref_name"])
	if refName == nil {
		return nil, nil
	}
	return stringSlice(refName["include"]), stringSlice(refName["exclude"])
}

// mapBypassActor reverses one rulesets API bypass_actors entry. RepositoryRole
// is shared by the fixed repository roles and by organization custom roles;
// an ID matching config.RepositoryRoleActorIDs is the symbolic role, and any
// other ID is a custom role addressed by config.ParseBypassActorID's
// "id:<number>" form, the same form Team and Integration always use because
// import cannot resolve a numeric ID back to a slug.
func mapBypassActor(actor githubapi.BypassActor) (config.BypassActorConfig, error) {
	switch actor.ActorType {
	case "OrganizationAdmin":
		return config.BypassActorConfig{OrgAdmin: true, BypassMode: actor.BypassMode}, nil
	case "Integration":
		return config.BypassActorConfig{App: resolvedActorID(actor.ActorID), BypassMode: actor.BypassMode}, nil
	case "Team":
		return config.BypassActorConfig{Team: resolvedActorID(actor.ActorID), BypassMode: actor.BypassMode}, nil
	case "RepositoryRole":
		if role, ok := roleNameForActorID(actor.ActorID); ok {
			return config.BypassActorConfig{Role: role, BypassMode: actor.BypassMode}, nil
		}
		return config.BypassActorConfig{CustomRole: resolvedActorID(actor.ActorID), BypassMode: actor.BypassMode}, nil
	default:
		return config.BypassActorConfig{}, fmt.Errorf("has a bypass actor of unrecognized type %q", actor.ActorType)
	}
}

func resolvedActorID(id int64) string { return "id:" + strconv.FormatInt(id, 10) }

func roleNameForActorID(id int64) (string, bool) {
	for name, roleID := range config.RepositoryRoleActorIDs {
		if roleID == id {
			return name, true
		}
	}
	return "", false
}
