package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestDeploymentReleaseCheckerReloadsInstalledVersion(t *testing.T) {
	t.Parallel()
	installedVersionPath := filepath.Join(t.TempDir(), "installed-version")
	if err := os.WriteFile(installedVersionPath, []byte("v1.9.5\n"), 0o600); err != nil {
		t.Fatalf("write installed version: %v", err)
	}
	checker := newDeploymentReleaseChecker(installedVersionPath, "v1.9.3")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	checker.Check(ctx)
	if checker.currentVersion != "v1.9.5" {
		t.Fatalf("current version = %q", checker.currentVersion)
	}
	if err := os.WriteFile(installedVersionPath, []byte("v1.9.6\n"), 0o600); err != nil {
		t.Fatalf("update installed version: %v", err)
	}
	checker.Check(ctx)
	if checker.currentVersion != "v1.9.6" {
		t.Fatalf("reloaded current version = %q", checker.currentVersion)
	}
}
