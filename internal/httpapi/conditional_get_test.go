package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/human-agent65535/modemdeck/internal/auth"
	"github.com/human-agent65535/modemdeck/internal/runtimeevents"
	"github.com/human-agent65535/modemdeck/internal/store"
)

func TestConditionalCollectionReadSkipsRepositoryWhenDurableWatermarkMatches(t *testing.T) {
	repository := &fakeRepository{contacts: []store.Contact{{ID: "contact-one"}}}
	hub := runtimeevents.NewHub()
	api, err := New(repository, Options{
		RuntimeEvents:         hub,
		disableAuthentication: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	first := httptest.NewRecorder()
	api.ServeHTTP(first, httptest.NewRequest(http.MethodGet, "/api/v1/contacts", nil))
	if first.Code != http.StatusOK {
		t.Fatalf("first status = %d, want 200", first.Code)
	}
	etag := first.Header().Get("ETag")
	if etag == "" {
		t.Fatal("first response is missing ETag")
	}
	if cache := first.Header().Get("Cache-Control"); cache != "no-store" {
		t.Fatalf("first Cache-Control = %q, want no-store", cache)
	}

	repository.contactLimit = 0
	request := httptest.NewRequest(http.MethodGet, "/api/v1/contacts", nil)
	request.Header.Set("If-None-Match", etag)
	notModified := httptest.NewRecorder()
	api.ServeHTTP(notModified, request)
	if notModified.Code != http.StatusNotModified {
		t.Fatalf("conditional status = %d, want 304", notModified.Code)
	}
	if repository.contactLimit != 0 {
		t.Fatalf("repository was queried with limit %d", repository.contactLimit)
	}
	if notModified.Body.Len() != 0 {
		t.Fatalf("304 body = %q, want empty", notModified.Body.String())
	}

	hub.Publish(runtimeevents.Change{Durable: true})
	request = httptest.NewRequest(http.MethodGet, "/api/v1/contacts", nil)
	request.Header.Set("If-None-Match", etag)
	changed := httptest.NewRecorder()
	api.ServeHTTP(changed, request)
	if changed.Code != http.StatusOK {
		t.Fatalf("changed status = %d, want 200", changed.Code)
	}
	if repository.contactLimit != store.DefaultQueryLimit {
		t.Fatalf("changed repository limit = %d", repository.contactLimit)
	}
	if changed.Header().Get("ETag") == etag {
		t.Fatal("durable change did not advance ETag")
	}
}

func TestConditionalCollectionETagSeparatesQueryAndPrincipal(t *testing.T) {
	hub := runtimeevents.NewHub()
	api, err := New(&fakeRepository{}, Options{
		RuntimeEvents:         hub,
		disableAuthentication: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	etag := func(path, userID string) string {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, path, nil)
		if userID != "" {
			request = request.WithContext(auth.ContextWithPrincipal(
				context.Background(),
				auth.Principal{UserID: userID},
			))
		}
		value, notModified := api.prepareConditionalCollectionRead(recorder, request)
		if notModified || value == "" {
			t.Fatalf("etag(%q, %q) = %q, notModified=%t", path, userID, value, notModified)
		}
		return value
	}

	base := etag("/api/v1/contacts", "user-one")
	if base == etag("/api/v1/contacts?q=alice", "user-one") {
		t.Fatal("query-specific representations share an ETag")
	}
	if base == etag("/api/v1/contacts", "user-two") {
		t.Fatal("principal-specific representations share an ETag")
	}
}
