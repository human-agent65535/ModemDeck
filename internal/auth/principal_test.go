package auth

import "testing"

func TestPrincipalLineAccessAlwaysUsesExplicitAssignments(t *testing.T) {
	t.Parallel()

	for _, role := range []Role{RoleAdmin, RoleMember} {
		principal := Principal{
			Role:           role,
			AllowedLineIDs: []string{"line-assigned"},
		}
		if !principal.CanAccessLine("line-assigned") {
			t.Fatalf("%s cannot access its assigned line", role)
		}
		if principal.CanAccessLine("line-unassigned") {
			t.Fatalf("%s can access an unassigned line", role)
		}
	}
}
