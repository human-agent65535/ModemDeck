package store

import "testing"

func TestMessageThreadKeyUsesStableLineIdentity(t *testing.T) {
	t.Parallel()

	got := MessageThreadKey(" line_stable ", " +818012345678 ")
	if got != "line_stable|+818012345678" {
		t.Fatalf("MessageThreadKey() = %q, want stable line key", got)
	}
}
