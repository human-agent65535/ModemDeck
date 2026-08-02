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
		"modemdeck-build.json": {
			Data: []byte(`{"version":"v1.8.6"}`),
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
			name:         "entry document",
			target:       "/",
			wantStatus:   http.StatusOK,
			wantCache:    "no-cache",
			wantBodyText: "ModemDeck",
		},
		{
			name:         "entry query does not control caching",
			target:       "/?compose=1",
			wantStatus:   http.StatusOK,
			wantCache:    "no-cache",
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
			target:       "/settings/account",
			wantStatus:   http.StatusOK,
			wantCache:    "no-cache",
			wantBodyText: "ModemDeck",
		},
		{
			name:         "build identity",
			target:       "/modemdeck-build.json",
			wantStatus:   http.StatusOK,
			wantCache:    "no-cache",
			wantBodyText: "v1.8.6",
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

func TestHandlerKeepsApplicationVersionOutOfEntryURL(t *testing.T) {
	handler := Handler(testDistribution(), "v1.8.6")
	response := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodGet,
		"/?compose=1&source=call",
		nil,
	)

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	if location := response.Header().Get("Location"); location != "" {
		t.Fatalf("Location = %q, want empty", location)
	}
	if cache := response.Header().Get("Cloudflare-CDN-Cache-Control"); cache != "no-store" {
		t.Fatalf("Cloudflare-CDN-Cache-Control = %q, want no-store", cache)
	}
}

func TestHandlerPreventsEdgeCachingVersionSources(t *testing.T) {
	handler := Handler(testDistribution(), "v1.8.6")
	for _, target := range []string{"/", "/modemdeck-build.json"} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(
			response,
			httptest.NewRequest(http.MethodGet, target, nil),
		)
		if response.Code != http.StatusOK {
			t.Fatalf("%s status = %d, want %d", target, response.Code, http.StatusOK)
		}
		if cache := response.Header().Get("Cloudflare-CDN-Cache-Control"); cache != "no-store" {
			t.Fatalf("%s Cloudflare-CDN-Cache-Control = %q, want no-store", target, cache)
		}
	}
}

func TestHandlerAllowsMissingCSSPreloadToReachJavaScriptRecovery(t *testing.T) {
	handler := Handler(testDistribution(), "v1.8.6")

	response := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodGet,
		"/assets/CallsView-removed.css",
		nil,
	)
	request.Header.Set("Referer", "https://modemdeck.test/calls")
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	if contentType := response.Header().Get("Content-Type"); contentType != "text/css; charset=utf-8" {
		t.Fatalf("Content-Type = %q", contentType)
	}
	if response.Body.Len() != 0 {
		t.Fatalf("body length = %d, want 0", response.Body.Len())
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
		`globalThis.dispatchEvent`,
		`new CustomEvent("modemdeck:update-ready"`,
		`version:"v1.8.6"`,
		"export{}",
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("recovery module %q does not contain %q", body, expected)
		}
	}
	for _, forbidden := range []string{"searchParams", "location.reload", "location.replace"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("recovery module %q unexpectedly contains %q", body, forbidden)
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
