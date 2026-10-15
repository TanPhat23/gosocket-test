package auth

import "context"

type teamScopeKey struct{}

// WithTeamScope restricts team-scoped authorization performed with the returned context to teamID.
// The API sets it for requests authenticated by a Personal Access Token, which is bound to one team.
func WithTeamScope(ctx context.Context, teamID string) context.Context {
	return context.WithValue(ctx, teamScopeKey{}, teamID)
}

// TeamAllowed reports whether ctx's team scope permits access to teamID. A context without a scope
// permits every team (membership checks still apply); a scoped context permits only its own team,
// and an empty scope permits none (fail closed).
func TeamAllowed(ctx context.Context, teamID string) bool {
	scope, scoped := ctx.Value(teamScopeKey{}).(string)
	return !scoped || (scope != "" && scope == teamID)
}
