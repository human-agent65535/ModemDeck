package webapp

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func testDistribution() fs.FS {
	return fstest.MapFS{
		"index.html": {
			Data: []byte("<!doctype html><title>ModemDeck</title>"),
		},
		"assets/index-current.js": {
			Data: []byte("export const current = true"),
		},
		"favicon.svg": {
			Data: []byte("<svg></svg>"),
		},
	}
}

func TestHandlerSetsDeploymentSafeCachePolicies(t *testing.T) {
	handler := Handler(testDistribution(), "v1.8.6")
	tests := []struct {
		name         string
		target       string
		wantStatus   int
		wantCache    string
		wantBodyText string
	}{
		{
			name:         "unversioned entry redirects",
			target:       "/",
			wantStatus:   http.StatusTemporaryRedirect,
			wantCache:    "no-store",
			wantBodyText: "Temporary Redirect",
		},
		{
			name:         "versioned entry document",
			target:       "/?v=v1.8.6",
			wantStatus:   http.StatusOK,
			wantCache:    "public, max-age=31536000, immutable",
			wantBodyText: "ModemDeck",
		},
		{
			name:         "hashed asset",
			target:       "/assets/index-current.js",
			wantStatus:   http.StatusOK,
			wantCache:    "public, max-age=31536000, immutable",
			wantBodyText: "current",
		},
		{
			name:         "unhashed static file",
			target:       "/favicon.svg",
			wantStatus:   http.StatusOK,
			wantCache:    "no-cache",
			wantBodyText: "<svg>",
		},
		{
			name:         "history fallback",
			target:       "/settings/account?v=v1.8.6",
			wantStatus:   http.StatusOK,
			wantCache:    "public, max-age=31536000, immutable",
			wantBodyText: "ModemDeck",
		},
		{
			name:         "missing non-script asset",
			target:       "/assets/removed.png",
			wantStatus:   http.StatusNotFound,
			wantCache:    "no-store",
			wantBodyText: "404 page not found",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, test.target, nil)

			handler.ServeHTTP(response, request)

			if response.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d", response.Code, test.wantStatus)
			}
			if cache := response.Header().Get("Cache-Control"); cache != test.wantCache {
				t.Fatalf("Cache-Control = %q, want %q", cache, test.wantCache)
			}
			if body := response.Body.String(); !strings.Contains(body, test.wantBodyText) {
				t.Fatalf("body = %q, want text %q", body, test.wantBodyText)
			}
		})
	}
}

func TestHandlerRedirectsStaleEntryToCurrentVersion(t *testing.T) {
	handler := Handler(testDistribution(), "v1.8.6")
	response := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodGet,
		"/?compose=1&v=v1.8.5",
		nil,
	)

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusTemporaryRedirect {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusTemporaryRedirect)
	}
	if location := response.Header().Get("Location"); location != "/?compose=1&v=v1.8.6" {
		t.Fatalf("Location = %q", location)
	}
	if cache := response.Header().Get("Cloudflare-CDN-Cache-Control"); cache != "no-store" {
		t.Fatalf("Cloudflare-CDN-Cache-Control = %q, want no-store", cache)
	}
}

func TestHandlerAllowsLegacyCSSPreloadToReachJavaScriptRecovery(t *testing.T) {
	handler := Handler(testDistribution(), "v1.8.6")

	legacyResponse := httptest.NewRecorder()
	legacyRequest := httptest.NewRequest(
		http.MethodGet,
		"/assets/CallsView-removed.css",
		nil,
	)
	legacyRequest.Header.Set("Referer", "https://modemdeck.test/?v=v1.8.5#/calls")
	handler.ServeHTTP(legacyResponse, legacyRequest)

	if legacyResponse.Code != http.StatusOK {
		t.Fatalf("legacy status = %d, want %d", legacyResponse.Code, http.StatusOK)
	}
	if contentType := legacyResponse.Header().Get("Content-Type"); contentType != "text/css; charset=utf-8" {
		t.Fatalf("legacy Content-Type = %q", contentType)
	}
	if legacyResponse.Body.Len() != 0 {
		t.Fatalf("legacy body length = %d, want 0", legacyResponse.Body.Len())
	}

	currentResponse := httptest.NewRecorder()
	currentRequest := httptest.NewRequest(
		http.MethodGet,
		"/assets/CallsView-removed.css",
		nil,
	)
	currentRequest.Header.Set("Referer", "https://modemdeck.test/?v=v1.8.6#/calls")
	handler.ServeHTTP(currentResponse, currentRequest)
	if currentResponse.Code != http.StatusNotFound {
		t.Fatalf("current status = %d, want %d", currentResponse.Code, http.StatusNotFound)
	}
}

func TestHandlerRecoversRequestsForRemovedJavaScriptChunks(t *testing.T) {
	handler := Handler(testDistribution(), "v1.8.6")

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/assets/CallsView-removed.js", nil)
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	if cache := response.Header().Get("Cache-Control"); cache != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", cache)
	}
	if contentType := response.Header().Get("Content-Type"); contentType != "text/javascript; charset=utf-8" {
		t.Fatalf("Content-Type = %q, want JavaScript", contentType)
	}
	body := response.Body.String()
	for _, expected := range []string{
		`const v="v1.8.6"`,
		`searchParams.get("v")!==v`,
		`searchParams.set("v",v)`,
		"location.replace",
		"export{}",
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("recovery module %q does not contain %q", body, expected)
		}
	}

	headResponse := httptest.NewRecorder()
	headRequest := httptest.NewRequest(http.MethodHead, "/assets/CallsView-removed.js", nil)
	handler.ServeHTTP(headResponse, headRequest)
	if headResponse.Code != http.StatusOK {
		t.Fatalf("HEAD status = %d, want %d", headResponse.Code, http.StatusOK)
	}
	if headResponse.Body.Len() != 0 {
		t.Fatalf("HEAD body length = %d, want 0", headResponse.Body.Len())
	}
}
