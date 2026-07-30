package store

import (
	"context"
	"strings"

	"github.com/human-agent65535/modemdeck/internal/auth"
)

func principalLineScope(
	ctx context.Context,
	column string,
) (condition string, arguments []any, scoped bool) {
	principal, ok := auth.PrincipalFromContext(ctx)
	if !ok {
		return "", nil, false
	}
	lineIDs := uniqueNonEmptyStrings(principal.AllowedLineIDs)
	if len(lineIDs) == 0 {
		return "1 = 0", nil, true
	}
	condition = column + " IN (" + placeholders(len(lineIDs)) + ")"
	arguments = make([]any, 0, len(lineIDs))
	for _, lineID := range lineIDs {
		arguments = append(arguments, lineID)
	}
	return condition, arguments, true
}

func principalCanAccessLine(ctx context.Context, lineID string) bool {
	principal, ok := auth.PrincipalFromContext(ctx)
	return !ok || principal.CanAccessLine(strings.TrimSpace(lineID))
}

func contactOwnerSQL(ctx context.Context, alias string) string {
	principal, ok := auth.PrincipalFromContext(ctx)
	if !ok {
		return ""
	}
	userID := strings.ReplaceAll(principal.UserID, "'", "''")
	return " AND " + alias + ".owner_user_id = '" + userID + "'"
}
