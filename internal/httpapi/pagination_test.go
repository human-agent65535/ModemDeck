package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/human-agent65535/modemdeck/internal/store"
)

func TestContactsCursorRoundTripsAndIsBoundToSearch(t *testing.T) {
	t.Parallel()

	repository := &fakeRepository{
		contacts: []store.Contact{
			{ID: "contact-a", DisplayName: "Alice"},
			{ID: "contact-b", DisplayName: "Bob"},
			{ID: "contact-c", DisplayName: "Casey"},
		},
	}
	api, err := New(repository, Options{disableAuthentication: true})
	if err != nil {
		t.Fatal(err)
	}

	first := httptest.NewRecorder()
	api.ServeHTTP(
		first,
		httptest.NewRequest(http.MethodGet, "/api/v1/contacts?limit=2&q=a", nil),
	)
	if first.Code != http.StatusOK {
		t.Fatalf("first page status = %d; body = %s", first.Code, first.Body)
	}
	var body contactsResponse
	if err := json.Unmarshal(first.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Contacts) != 2 ||
		!body.Meta.HasMore ||
		body.Meta.NextCursor == "" ||
		!repository.contactQuery.Lookahead {
		t.Fatalf(
			"first page = %d contacts, meta %+v, query %+v",
			len(body.Contacts),
			body.Meta,
			repository.contactQuery,
		)
	}

	second := httptest.NewRecorder()
	api.ServeHTTP(
		second,
		httptest.NewRequest(
			http.MethodGet,
			"/api/v1/contacts?limit=2&q=a&cursor="+body.Meta.NextCursor,
			nil,
		),
	)
	if second.Code != http.StatusOK {
		t.Fatalf("second page status = %d; body = %s", second.Code, second.Body)
	}
	if repository.contactQuery.After == nil ||
		repository.contactQuery.After.DisplayName != "Bob" ||
		repository.contactQuery.After.ID != "contact-b" {
		t.Fatalf("decoded contact cursor = %+v", repository.contactQuery.After)
	}

	mismatched := httptest.NewRecorder()
	api.ServeHTTP(
		mismatched,
		httptest.NewRequest(
			http.MethodGet,
			"/api/v1/contacts?limit=2&q=other&cursor="+body.Meta.NextCursor,
			nil,
		),
	)
	assertAPIError(
		t,
		mismatched,
		http.StatusBadRequest,
		"invalid_argument",
	)

	replacement := "A"
	if body.Meta.NextCursor[len(body.Meta.NextCursor)-1:] == replacement {
		replacement = "B"
	}
	tamperedCursor := body.Meta.NextCursor[:len(body.Meta.NextCursor)-1] +
		replacement
	tampered := httptest.NewRecorder()
	api.ServeHTTP(
		tampered,
		httptest.NewRequest(
			http.MethodGet,
			"/api/v1/contacts?limit=2&q=a&cursor="+tamperedCursor,
			nil,
		),
	)
	assertAPIError(
		t,
		tampered,
		http.StatusBadRequest,
		"invalid_argument",
	)
}

func TestPageItemsUsesOneLookaheadRow(t *testing.T) {
	t.Parallel()

	items, hasMore := pageItems([]int{1, 2, 3}, 2)
	if !hasMore || len(items) != 2 || items[0] != 1 || items[1] != 2 {
		t.Fatalf("page = %v, has more = %t", items, hasMore)
	}
	items, hasMore = pageItems([]int{1, 2}, 2)
	if hasMore || len(items) != 2 {
		t.Fatalf("terminal page = %v, has more = %t", items, hasMore)
	}
}

func TestMessagePageDropsTheOldestLookaheadRow(t *testing.T) {
	t.Parallel()

	repository := &fakeRepository{
		messages: []store.Message{
			{
				ID:            1,
				LineID:        "line-main",
				Peer:          "+810000000001",
				Type:          1,
				Timestamp:     "2026-07-30T01:00:00Z",
				SortTimestamp: "2026-07-30 01:00:00",
			},
			{
				ID:            2,
				LineID:        "line-main",
				Peer:          "+810000000001",
				Type:          1,
				Timestamp:     "2026-07-30T02:00:00Z",
				SortTimestamp: "2026-07-30 02:00:00",
			},
			{
				ID:            3,
				LineID:        "line-main",
				Peer:          "+810000000001",
				Type:          1,
				Timestamp:     "2026-07-30T03:00:00Z",
				SortTimestamp: "2026-07-30 03:00:00",
			},
		},
	}
	api, err := New(repository, Options{disableAuthentication: true})
	if err != nil {
		t.Fatal(err)
	}
	first := httptest.NewRecorder()
	api.ServeHTTP(
		first,
		httptest.NewRequest(
			http.MethodGet,
			"/api/v1/messages?line_id=line-main&peer=%2B810000000001&limit=2",
			nil,
		),
	)
	if first.Code != http.StatusOK {
		t.Fatalf("first page status = %d; body = %s", first.Code, first.Body)
	}
	var body messagesResponse
	if err := json.Unmarshal(first.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Messages) != 2 ||
		body.Messages[0].ID != 2 ||
		body.Messages[1].ID != 3 ||
		!body.Meta.HasMore ||
		body.Meta.NextCursor == "" {
		t.Fatalf("message page = %+v, meta = %+v", body.Messages, body.Meta)
	}

	second := httptest.NewRecorder()
	api.ServeHTTP(
		second,
		httptest.NewRequest(
			http.MethodGet,
			"/api/v1/messages?line_id=line-main&peer=%2B810000000001&limit=2&cursor="+
				body.Meta.NextCursor,
			nil,
		),
	)
	if second.Code != http.StatusOK {
		t.Fatalf("second page status = %d; body = %s", second.Code, second.Body)
	}
	if repository.messageQuery.After == nil ||
		repository.messageQuery.After.ID != 2 ||
		repository.messageQuery.After.Timestamp != "2026-07-30 02:00:00" {
		t.Fatalf("decoded message cursor = %+v", repository.messageQuery.After)
	}
}
