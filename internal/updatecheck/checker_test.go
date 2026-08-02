package updatecheck

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync/atomic"
	"testing"
	"time"
)

func TestCheckerReportsAvailableStableRelease(t *testing.T) {
	t.Parallel()
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		requests.Add(1)
		if request.Header.Get("Accept") != "application/vnd.github+json" {
			t.Errorf("Accept = %q", request.Header.Get("Accept"))
		}
		if request.Header.Get("X-GitHub-Api-Version") != "2026-03-10" {
			t.Errorf("X-GitHub-Api-Version = %q", request.Header.Get("X-GitHub-Api-Version"))
		}
		response.Header().Set("Content-Type", "application/json")
		response.Header().Set("ETag", `"release-1"`)
		_, _ = response.Write([]byte(`{
			"tag_name":"v1.2.0",
			"name":"ModemDeck 1.2",
			"body":"## Highlights\n\n- One-click container updates\n\n## Fixed\n\n- Faster recovery",
			"published_at":"2026-07-27T10:00:00Z"
		}`))
	}))
	defer server.Close()

	now := time.Date(2026, 7, 28, 8, 0, 0, 0, time.UTC)
	checker := New(Options{
		CurrentVersion:   "v1.0.0",
		LatestReleaseURL: server.URL,
		ReleasePageURL:   "https://example.invalid/releases/tag/",
		Now:              func() time.Time { return now },
	})

	result := checker.Check(context.Background())
	if result.Status != StatusUpdateAvailable {
		t.Fatalf("status = %q; want %q", result.Status, StatusUpdateAvailable)
	}
	if result.CurrentVersion != "v1.0.0" || result.LatestVersion != "v1.2.0" {
		t.Fatalf("versions = %q -> %q", result.CurrentVersion, result.LatestVersion)
	}
	if result.ReleaseURL != "https://example.invalid/releases/tag/v1.2.0" {
		t.Fatalf("release URL = %q", result.ReleaseURL)
	}
	if result.ReleaseNotes != "## Highlights\n\n- One-click container updates\n\n## Fixed\n\n- Faster recovery" {
		t.Fatalf("release notes = %q", result.ReleaseNotes)
	}

	cached := checker.Check(context.Background())
	if !reflect.DeepEqual(cached, result) || requests.Load() != 1 {
		t.Fatalf("cached result = %+v; requests = %d", cached, requests.Load())
	}
}

func TestCheckerReportsUpToDate(t *testing.T) {
	t.Parallel()
	server := releaseServer(t, http.StatusOK, `{"tag_name":"V1.0.0"}`)
	defer server.Close()

	result := New(Options{
		CurrentVersion:   "v1.0.0",
		LatestReleaseURL: server.URL,
	}).Check(context.Background())

	if result.Status != StatusUpToDate || result.LatestVersion != "v1.0.0" {
		t.Fatalf("result = %+v", result)
	}
}

func TestCheckerCachesForOneDayAndManualRefreshBypassesCache(t *testing.T) {
	t.Parallel()
	var requests atomic.Int32
	now := time.Date(2026, 8, 2, 0, 0, 0, 0, time.UTC)
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		request := requests.Add(1)
		_, _ = response.Write([]byte(`{"tag_name":"v1.0.` + string(rune('0'+request)) + `"}`))
	}))
	defer server.Close()

	checker := New(Options{
		CurrentVersion:   "v1.0.0",
		LatestReleaseURL: server.URL,
		Now:              func() time.Time { return now },
	})
	first := checker.Check(context.Background())
	now = now.Add(23 * time.Hour)
	cached := checker.Check(context.Background())
	if first.LatestVersion != "v1.0.1" || cached.LatestVersion != first.LatestVersion || requests.Load() != 1 {
		t.Fatalf("23-hour cache = %+v then %+v; requests = %d", first, cached, requests.Load())
	}
	refreshed := checker.Check(WithRefresh(context.Background()))
	if refreshed.LatestVersion != "v1.0.2" || requests.Load() != 2 {
		t.Fatalf("manual refresh = %+v; requests = %d", refreshed, requests.Load())
	}
	now = now.Add(23 * time.Hour)
	afterRefresh := checker.Check(context.Background())
	if afterRefresh.LatestVersion != refreshed.LatestVersion || requests.Load() != 2 {
		t.Fatalf("refreshed 23-hour cache = %+v; requests = %d", afterRefresh, requests.Load())
	}
	now = now.Add(time.Hour)
	expired := checker.Check(context.Background())
	if expired.LatestVersion != "v1.0.3" || requests.Load() != 3 {
		t.Fatalf("expired manual refresh cache = %+v; requests = %d", expired, requests.Load())
	}
}

func TestCheckerRetriesUnavailableResultAfterShortCache(t *testing.T) {
	t.Parallel()
	var requests atomic.Int32
	now := time.Date(2026, 8, 2, 0, 0, 0, 0, time.UTC)
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		if requests.Add(1) == 1 {
			response.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = response.Write([]byte(`{"tag_name":"v1.0.1"}`))
	}))
	defer server.Close()

	checker := New(Options{
		CurrentVersion:   "v1.0.0",
		LatestReleaseURL: server.URL,
		Now:              func() time.Time { return now },
	})
	first := checker.Check(context.Background())
	now = now.Add(defaultUnavailableTTL - time.Second)
	cached := checker.Check(context.Background())
	if first.Status != StatusUnavailable || !reflect.DeepEqual(cached, first) || requests.Load() != 1 {
		t.Fatalf("unavailable cache = %+v then %+v; requests = %d", first, cached, requests.Load())
	}
	now = now.Add(time.Second)
	retried := checker.Check(context.Background())
	if retried.Status != StatusUpdateAvailable || retried.LatestVersion != "v1.0.1" || requests.Load() != 2 {
		t.Fatalf("retried result = %+v; requests = %d", retried, requests.Load())
	}
}

func TestCheckerReportsMissingReleaseAsUnavailable(t *testing.T) {
	t.Parallel()
	server := releaseServer(t, http.StatusNotFound, `{}`)
	defer server.Close()

	result := New(Options{
		CurrentVersion:   "v1.0.0",
		LatestReleaseURL: server.URL,
	}).Check(context.Background())

	if result.Status != StatusUnavailable || result.ErrorCode != "github_no_release" {
		t.Fatalf("result = %+v", result)
	}
}

func TestCheckerTreatsNonStableCurrentVersionAsDevelopment(t *testing.T) {
	t.Parallel()
	server := releaseServer(t, http.StatusOK, `{"tag_name":"v1.0.0"}`)
	defer server.Close()

	result := New(Options{
		CurrentVersion:   "dev",
		LatestReleaseURL: server.URL,
	}).Check(context.Background())

	if result.Status != StatusDevelopment || result.CurrentVersion != "dev" {
		t.Fatalf("result = %+v", result)
	}
}

func TestCheckerRejectsNonStableReleaseTag(t *testing.T) {
	t.Parallel()
	server := releaseServer(t, http.StatusOK, `{"tag_name":"v1.1.0-rc.1"}`)
	defer server.Close()

	result := New(Options{
		CurrentVersion:   "v1.0.0",
		LatestReleaseURL: server.URL,
	}).Check(context.Background())

	if result.Status != StatusUnavailable || result.ErrorCode != "invalid_release_tag" {
		t.Fatalf("result = %+v", result)
	}
}

func releaseServer(t *testing.T, status int, body string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		response.WriteHeader(status)
		_, _ = response.Write([]byte(body))
	}))
}
