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

// contactOwnerSQL returns a fragment that is spliced into larger statements
// whose arguments are positional, so the user ID is inlined as a literal. Only
// identifiers made of ASCII letters, digits, '_' and '-' (as every generated
// user ID is) are accepted; any other value matches no contacts instead of
// reaching the SQL text.
func contactOwnerSQL(ctx context.Context, alias string) string {
	principal, ok := auth.PrincipalFromContext(ctx)
	if !ok {
		return ""
	}
	if !safeUserIdentifier(principal.UserID) {
		return " AND 1 = 0"
	}
	return " AND " + alias + ".owner_user_id = '" + principal.UserID + "'"
}

func safeUserIdentifier(userID string) bool {
	if userID == "" || len(userID) > 128 {
		return false
	}
	for _, character := range userID {
		switch {
		case character >= 'a' && character <= 'z',
			character >= 'A' && character <= 'Z',
			character >= '0' && character <= '9',
			character == '_', character == '-':
		default:
			return false
		}
	}
	return true
}
