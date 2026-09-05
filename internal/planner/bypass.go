package planner

import (
	"context"
	"fmt"
	"strings"

	"github.com/kkamegawa/gh-repospec/internal/config"
	"github.com/kkamegawa/gh-repospec/internal/githubapi"
)

// bypassFailure names the declaration index of one unresolvable bypass actor,
// so a blocked plan entry can point at the same document position
// config.Validate would (ruleset "<name>" bypassActors[<index>]).
type bypassFailure struct {
	Index int
	Err   error
}

// bypassResolver turns configured bypass actor selectors into the numeric
// actor IDs the rulesets API requires. Lookups are cached, including
// failures, so one plan or apply does not repeat the same organization or
// GitHub App request for every ruleset that names the same actor.
type bypassResolver struct {
	service  Service
	owner    string
	personal bool

	teams    map[string]int64
	apps     map[string]int64
	roles    map[string]int64
	rolesErr error
	rolesSet bool
	failures map[string]error
}

// newBypassResolver constructs a resolver for one plan or apply run. personal
// marks a User-owned repository, on which team, orgAdmin, and customRole
// bypass actors can never resolve: GitHub's organization endpoints return 404
// for a personal account rather than reporting the actor type as
// inapplicable, which would otherwise read as an ordinary lookup failure.
func newBypassResolver(service Service, owner string, personal bool) *bypassResolver {
	return &bypassResolver{
		service:  service,
		owner:    owner,
		personal: personal,
		teams:    make(map[string]int64),
		apps:     make(map[string]int64),
		failures: make(map[string]error),
	}
}

// newRulesetBypassResolver builds the resolver used for one Build or Apply
// run, or returns nil when no ruleset declares a bypass actor, so a
// configuration without rulesets never pays for the personal-repository
// check below. exists and info describe the target repository as already
// determined by the caller; when the repository does not exist yet, personal
// is inferred from the authenticated user instead of Owner.Type.
func newRulesetBypassResolver(ctx context.Context, service Service, repository githubapi.Repository, info githubapi.RepositoryInfo, exists bool, rulesets []config.RulesetConfig) *bypassResolver {
	if !anyBypassActors(rulesets) {
		return nil
	}
	return newBypassResolver(service, repository.Owner, ownerIsPersonal(ctx, service, repository, info, exists))
}

// anyBypassActors reports whether at least one ruleset declares a bypass
// actor, so newRulesetBypassResolver can skip the personal-repository check
// (and, on the create path, the GetAuthenticatedUser call it can trigger)
// for a configuration that declares rulesets but no bypass actors.
func anyBypassActors(rulesets []config.RulesetConfig) bool {
	for _, ruleset := range rulesets {
		if len(ruleset.BypassActors) > 0 {
			return true
		}
	}
	return false
}

// ownerIsPersonal reports whether the ruleset's repository is (or will be)
// owned by a User account rather than an organization. GitHub's organization
// endpoints 404 for a personal account, so this is checked up front instead
// of being inferred from a lookup failure that could otherwise mean anything.
func ownerIsPersonal(ctx context.Context, service Service, repository githubapi.Repository, info githubapi.RepositoryInfo, exists bool) bool {
	if exists {
		return strings.EqualFold(info.Owner.Type, "User")
	}
	if users, ok := service.(userService); ok {
		if user, err := users.GetAuthenticatedUser(ctx); err == nil {
			return strings.EqualFold(user.Login, repository.Owner)
		}
	}
	// Unknown defaults to organization: blocking every ruleset with a bypass
	// actor on an unrelated authentication failure would be worse than
	// letting GitHub's own 404 report the mistake if the guess is wrong.
	return false
}

// resolveAll resolves every declared bypass actor for one ruleset. Both the
// resolved entries and any failures are returned; a ruleset with any failure
// is never partially applied by the caller.
func (r *bypassResolver) resolveAll(ctx context.Context, actors []config.BypassActorConfig) ([]githubapi.BypassActor, []bypassFailure) {
	resolved := make([]githubapi.BypassActor, 0, len(actors))
	var failures []bypassFailure
	for index, actor := range actors {
		wire, err := r.resolve(ctx, actor)
		if err != nil {
			failures = append(failures, bypassFailure{Index: index, Err: err})
			continue
		}
		resolved = append(resolved, wire)
	}
	return resolved, failures
}

// resolve returns the wire form of one declared actor. Its error is a plan
// summary, not an operational fault: callers record it as a blocked change
// rather than failing the run.
func (r *bypassResolver) resolve(ctx context.Context, actor config.BypassActorConfig) (githubapi.BypassActor, error) {
	switch {
	case actor.Role != "":
		id, ok := config.RepositoryRoleActorIDs[actor.Role]
		if !ok {
			// Unreachable once config.Validate has run: role is restricted to
			// admin, write, or maintain.
			return githubapi.BypassActor{}, fmt.Errorf("bypass actor role %q is not a recognized repository role", actor.Role)
		}
		return githubapi.BypassActor{ActorID: id, ActorType: "RepositoryRole", BypassMode: actor.BypassMode}, nil
	case actor.OrgAdmin:
		if r.personal {
			return githubapi.BypassActor{}, fmt.Errorf("bypass actor orgAdmin requires an organization-owned repository")
		}
		return githubapi.BypassActor{ActorID: config.OrganizationAdminActorID, ActorType: "OrganizationAdmin", BypassMode: actor.BypassMode}, nil
	case actor.Team != "":
		if r.personal {
			return githubapi.BypassActor{}, fmt.Errorf("bypass actor team %q requires an organization-owned repository", actor.Team)
		}
		id, err := r.resolveTeam(ctx, actor.Team)
		if err != nil {
			return githubapi.BypassActor{}, err
		}
		return githubapi.BypassActor{ActorID: id, ActorType: "Team", BypassMode: actor.BypassMode}, nil
	case actor.App != "":
		id, err := r.resolveApp(ctx, actor.App)
		if err != nil {
			return githubapi.BypassActor{}, err
		}
		return githubapi.BypassActor{ActorID: id, ActorType: "Integration", BypassMode: actor.BypassMode}, nil
	case actor.CustomRole != "":
		if r.personal {
			return githubapi.BypassActor{}, fmt.Errorf("bypass actor customRole %q requires an organization-owned repository", actor.CustomRole)
		}
		id, err := r.resolveCustomRole(ctx, actor.CustomRole)
		if err != nil {
			return githubapi.BypassActor{}, err
		}
		return githubapi.BypassActor{ActorID: id, ActorType: "RepositoryRole", BypassMode: actor.BypassMode}, nil
	default:
		// Unreachable once config.Validate has run: exactly one selector is required.
		return githubapi.BypassActor{}, fmt.Errorf("bypass actor sets no selector")
	}
}

func (r *bypassResolver) resolveTeam(ctx context.Context, slug string) (int64, error) {
	if id, found, err := config.ParseBypassActorID(slug); found {
		return id, err
	}
	if id, ok := r.teams[slug]; ok {
		return id, nil
	}
	key := "team:" + slug
	if err, ok := r.failures[key]; ok {
		return 0, err
	}
	team, err := r.service.GetTeam(ctx, r.owner, slug)
	if err != nil {
		wrapped := fmt.Errorf("bypass actor team %q was not found in organization %s: %w", slug, r.owner, err)
		r.failures[key] = wrapped
		return 0, wrapped
	}
	r.teams[slug] = team.ID
	return team.ID, nil
}

func (r *bypassResolver) resolveApp(ctx context.Context, slug string) (int64, error) {
	if id, found, err := config.ParseBypassActorID(slug); found {
		return id, err
	}
	if id, ok := r.apps[slug]; ok {
		return id, nil
	}
	key := "app:" + slug
	if err, ok := r.failures[key]; ok {
		return 0, err
	}
	app, err := r.service.GetApp(ctx, slug)
	if err != nil {
		wrapped := fmt.Errorf("bypass actor app %q was not found: %w", slug, err)
		r.failures[key] = wrapped
		return 0, wrapped
	}
	r.apps[slug] = app.ID
	return app.ID, nil
}

func (r *bypassResolver) resolveCustomRole(ctx context.Context, name string) (int64, error) {
	if id, found, err := config.ParseBypassActorID(name); found {
		return id, err
	}
	if !r.rolesSet {
		roles, err := r.service.ListCustomRepositoryRoles(ctx, r.owner)
		r.rolesSet = true
		if err != nil {
			r.rolesErr = fmt.Errorf("list custom repository roles for organization %s: %w", r.owner, err)
		} else {
			r.roles = make(map[string]int64, len(roles))
			for _, role := range roles {
				r.roles[strings.ToLower(role.Name)] = role.ID
			}
		}
	}
	if r.rolesErr != nil {
		return 0, r.rolesErr
	}
	if id, ok := r.roles[strings.ToLower(name)]; ok {
		return id, nil
	}
	return 0, fmt.Errorf("bypass actor customRole %q was not found in organization %s", name, r.owner)
}
