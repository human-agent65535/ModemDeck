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
	started, err := service.StartBrowser("web-session", UserKey("user"), "zh")
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
	if _, err := service.StartBrowser("web-session", "user"); !errors.Is(err, ErrBusy) {
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
	if _, err := service.StartBrowser("new-session", "user"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("start after close = %v", err)
	}
}

func TestBrowserCallTestUsesOwnedProjectionAndCleansUpOnStop(t *testing.T) {
	service := New(nil, nil)
	defer service.Close()
	started, err := service.StartBrowser("browser", "user")
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

func TestBrowserCallTestRestartsImmediatelyAfterEnd(t *testing.T) {
	service := New(nil, nil)
	defer service.Close()
	first, err := service.StartBrowser("browser", "user")
	if err != nil {
		t.Fatal(err)
	}
	if err := service.End(first.ID, "browser"); err != nil {
		t.Fatal(err)
	}
	second, err := service.StartBrowser("browser", "user")
	if err != nil || second.ID == first.ID {
		t.Fatalf("restart after end = %+v %v", second, err)
	}
	if status, err := service.Status(first.ID, "browser"); err != nil || status.Phase != "ended" {
		t.Fatalf("ended test should stay readable: %+v %v", status, err)
	}
}

func TestPushCallTestKeepsDeviceCooldown(t *testing.T) {
	push := make(localPush, 1)
	service := New(push, nil)
	defer service.Close()
	started, err := service.Start("user", "phone")
	if err != nil {
		t.Fatal(err)
	}
	if err := service.End(started.ID, "user\x00phone"); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Start("user", "phone"); !errors.Is(err, ErrBusy) {
		t.Fatalf("push restart inside cooldown = %v", err)
	}
}

func TestCallTestLimitsCountOnlyLiveSessionsPerUser(t *testing.T) {
	service := New(nil, nil)
	defer service.Close()
	for _, owner := range []string{"one", "two"} {
		if _, err := service.StartBrowser(owner, "member"); err != nil {
			t.Fatalf("start %s: %v", owner, err)
		}
	}
	if _, err := service.StartBrowser("three", "member"); !errors.Is(err, ErrBusy) {
		t.Fatalf("third live test for one user = %v", err)
	}
	if _, err := service.StartBrowser("other", "someone-else"); err != nil {
		t.Fatalf("another user's test = %v", err)
	}
}

func TestRestartingBrowserTestsCannotFillTheSharedLimit(t *testing.T) {
	service := New(nil, nil)
	defer service.Close()
	for attempt := range maxRetainedPerUser {
		started, err := service.StartBrowser("churn", "member")
		if err != nil {
			t.Fatalf("start %d: %v", attempt, err)
		}
		if err := service.End(started.ID, "churn"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := service.StartBrowser("churn", "member"); !errors.Is(err, ErrBusy) {
		t.Fatalf("restart beyond the per-user history = %v", err)
	}
	if _, err := service.StartBrowser("other", "someone-else"); err != nil {
		t.Fatalf("another user's test = %v", err)
	}
}

func TestIOSAndBrowserTestsShareTheAccountLimit(t *testing.T) {
	service := New(make(localPush, 1), nil)
	defer service.Close()
	if _, err := service.Start("user", "phone"); err != nil {
		t.Fatal(err)
	}
	if _, err := service.StartBrowser("tab-1", UserKey("user")); err != nil {
		t.Fatal(err)
	}
	if _, err := service.StartBrowser("tab-2", UserKey("user")); !errors.Is(err, ErrBusy) {
		t.Fatalf("third live test across iOS and browser = %v", err)
	}
	if _, err := service.StartBrowser("legacy", LegacyAdminUser); err != nil {
		t.Fatalf("legacy administrator test = %v", err)
	}
}

func TestCloseWaitsForAStartInFlight(t *testing.T) {
	service := New(nil, nil)
	reserved, release := make(chan struct{}), make(chan struct{})
	service.beforeBuild = func() {
		close(reserved)
		<-release
	}
	started := make(chan error, 1)
	go func() {
		_, err := service.StartBrowser("browser", "member")
		started <- err
	}()
	<-reserved
	closed := make(chan struct{})
	go func() {
		service.Close()
		close(closed)
	}()
	select {
	case <-closed:
		t.Fatal("Close returned while a start was still building its runtime")
	case <-time.After(100 * time.Millisecond):
	}
	close(release)
	select {
	case <-closed:
	case <-time.After(5 * time.Second):
		t.Fatal("Close did not return after the start finished")
	}
	if err := <-started; !errors.Is(err, ErrUnavailable) {
		t.Fatalf("start during Close = %v", err)
	}
	service.mu.Lock()
	defer service.mu.Unlock()
	if len(service.sessions) != 0 || len(service.starting) != 0 {
		t.Fatalf("Close left sessions=%d starting=%d", len(service.sessions), len(service.starting))
	}
}
