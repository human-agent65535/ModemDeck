package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestOwnerStatusHealthRequiresReadyFreshState(t *testing.T) {
	now := time.Now().UTC()
	path := filepath.Join(t.TempDir(), "owner.json")
	if err := writeOwnerStatus(path, ownerStatus{
		Ready:     true,
		UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	if err := checkOwnerStatus(path, 5*time.Second, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := checkOwnerStatus(path, 5*time.Second, now.Add(10*time.Second)); err == nil {
		t.Fatal("stale owner status passed health")
	}

	if err := writeOwnerStatus(path, ownerStatus{
		Ready:     false,
		UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	if err := checkOwnerStatus(path, 5*time.Second, now); err == nil {
		t.Fatal("unready owner status passed health")
	}
}

func TestOwnerStatusIsPublishedAtomicallyWithPrivateMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "owner.json")
	if err := writeOwnerStatus(path, ownerStatus{
		Ready:     true,
		UpdatedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o640 {
		t.Fatalf("mode = %o", info.Mode().Perm())
	}
}
