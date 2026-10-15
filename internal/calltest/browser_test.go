package calltest

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestBrowserCallTestStartsImmediatelyAndExpiresWithoutPush(t *testing.T) {
	push := make(localPush, 1)
	service := New(push, nil)
	defer service.Close()
	started, err := service.StartBrowser("web-session", "zh")
	if err != nil || started.Phase != "active" || time.Until(started.ExpiresAt) > time.Minute || time.Until(started.ExpiresAt) < 59*time.Second {
		t.Fatalf("browser start = %+v %v", started, err)
	}
	entry, err := service.lookup(started.ID, "web-session")
	if err != nil {
		t.Fatal(err)
	}
	want, _ := prompts.ReadFile("prompts/zh-guide.pcm")
	if len(entry.audio.guide) != len(want) {
		t.Fatal("browser prompt language was ignored")
	}
	if _, err := service.StartBrowser("web-session"); !errors.Is(err, ErrBusy) {
		t.Fatalf("parallel start = %v", err)
	}
	if _, err := service.Status(started.ID, "other-session"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign status = %v", err)
	}
	entry.mu.Lock()
	entry.status.ExpiresAt = time.Now().Add(-time.Second)
	entry.mu.Unlock()
	select {
	case <-entry.ctx.Done():
	case <-time.After(3 * time.Second):
		t.Fatal("browser test did not expire")
	}
	service.Close()
	entry.audio.mu.Lock()
	if !entry.audio.closed || len(entry.audio.clip) != 0 {
		t.Error("test audio retained after expiry")
	}
	entry.audio.mu.Unlock()
	select {
	case <-push:
		t.Fatal("browser test sent a push")
	default:
	}
	if _, err := service.StartBrowser("new-session"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("start after close = %v", err)
	}
}

func TestBrowserCallTestUsesOwnedProjectionAndCleansUpOnStop(t *testing.T) {
	service := New(nil, nil)
	defer service.Close()
	started, err := service.StartBrowser("browser")
	if err != nil {
		t.Fatal(err)
	}
	projection, _, err := service.Active(context.Background(), started.ID, "browser")
	if err != nil || len(projection.Calls) != 1 || projection.Calls[0].ControlState != "owned" {
		t.Fatalf("projection = %+v %v", projection, err)
	}
	if err := service.End(started.ID, "other"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign end = %v", err)
	}
	if err := service.End(started.ID, "browser"); err != nil {
		t.Fatal(err)
	}
	projection, _, err = service.Active(context.Background(), started.ID, "browser")
	if err != nil || len(projection.Calls) != 0 {
		t.Fatalf("ended projection = %+v %v", projection, err)
	}
	if _, err := service.Start("user", "phone"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("iOS still requires push: %v", err)
	}
}
