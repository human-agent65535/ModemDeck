package updatecheck

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	DefaultLatestReleaseURL = "https://api.github.com/repos/human-agent65535/ModemDeck/releases/latest"
	DefaultReleasePageURL   = "https://github.com/human-agent65535/ModemDeck/releases/tag/"
	defaultCacheTTL         = 15 * time.Minute
	defaultRequestTimeout   = 5 * time.Second
	maximumResponseBytes    = 1 << 20
)

type Status string

const (
	StatusUpToDate        Status = "up_to_date"
	StatusUpdateAvailable Status = "update_available"
	StatusDevelopment     Status = "development"
	StatusUnavailable     Status = "unavailable"
)

type Result struct {
	Status         Status `json:"status"`
	CurrentVersion string `json:"current_version"`
	LatestVersion  string `json:"latest_version,omitempty"`
	ReleaseName    string `json:"release_name,omitempty"`
	ReleaseURL     string `json:"release_url,omitempty"`
	PublishedAt    string `json:"published_at,omitempty"`
	CheckedAt      string `json:"checked_at"`
	ErrorCode      string `json:"error_code,omitempty"`
}

type HTTPClient interface {
	Do(*http.Request) (*http.Response, error)
}

type Options struct {
	CurrentVersion   string
	LatestReleaseURL string
	ReleasePageURL   string
	Client           HTTPClient
	CacheTTL         time.Duration
	Now              func() time.Time
}

type Checker struct {
	currentVersion   string
	latestReleaseURL string
	releasePageURL   string
	client           HTTPClient
	cacheTTL         time.Duration
	now              func() time.Time

	mu              sync.Mutex
	cachedResult    Result
	cacheExpiresAt  time.Time
	etag            string
	cachedRelease   *githubRelease
	cachedNoRelease bool
}

type githubRelease struct {
	TagName     string `json:"tag_name"`
	Name        string `json:"name"`
	PublishedAt string `json:"published_at"`
}

var stableVersionPattern = regexp.MustCompile(
	`^[vV](0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`,
)

func New(options Options) *Checker {
	latestReleaseURL := strings.TrimSpace(options.LatestReleaseURL)
	if latestReleaseURL == "" {
		latestReleaseURL = DefaultLatestReleaseURL
	}
	releasePageURL := strings.TrimSpace(options.ReleasePageURL)
	if releasePageURL == "" {
		releasePageURL = DefaultReleasePageURL
	}
	client := options.Client
	if client == nil {
		client = &http.Client{Timeout: defaultRequestTimeout}
	}
	cacheTTL := options.CacheTTL
	if cacheTTL <= 0 {
		cacheTTL = defaultCacheTTL
	}
	now := options.Now
	if now == nil {
		now = time.Now
	}
	return &Checker{
		currentVersion:   displayVersion(options.CurrentVersion),
		latestReleaseURL: latestReleaseURL,
		releasePageURL:   releasePageURL,
		client:           client,
		cacheTTL:         cacheTTL,
		now:              now,
	}
}

func (checker *Checker) Check(ctx context.Context) Result {
	checker.mu.Lock()
	defer checker.mu.Unlock()

	now := checker.now().UTC()
	if !checker.cacheExpiresAt.IsZero() && now.Before(checker.cacheExpiresAt) {
		return checker.cachedResult
	}

	result := checker.fetch(ctx, now)
	checker.cachedResult = result
	checker.cacheExpiresAt = now.Add(checker.cacheTTL)
	return result
}

func (checker *Checker) fetch(ctx context.Context, now time.Time) Result {
	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		checker.latestReleaseURL,
		nil,
	)
	if err != nil {
		return checker.unavailable(now, "github_request_invalid")
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("X-GitHub-Api-Version", "2026-03-10")
	request.Header.Set("User-Agent", "ModemDeck/"+checker.currentVersion)
	if checker.etag != "" {
		request.Header.Set("If-None-Match", checker.etag)
	}

	response, err := checker.client.Do(request)
	if err != nil {
		return checker.unavailable(now, "github_unavailable")
	}
	defer response.Body.Close()

	switch response.StatusCode {
	case http.StatusNotModified:
		if checker.cachedNoRelease {
			return checker.unavailable(now, "github_no_release")
		}
		if checker.cachedRelease != nil {
			return checker.compare(*checker.cachedRelease, now)
		}
		return checker.unavailable(now, "github_invalid_response")
	case http.StatusNotFound:
		checker.etag = response.Header.Get("ETag")
		checker.cachedRelease = nil
		checker.cachedNoRelease = true
		return checker.unavailable(now, "github_no_release")
	case http.StatusOK:
	default:
		return checker.unavailable(now, "github_unavailable")
	}

	var release githubRelease
	decoder := json.NewDecoder(io.LimitReader(response.Body, maximumResponseBytes))
	if err := decoder.Decode(&release); err != nil {
		return checker.unavailable(now, "github_invalid_response")
	}
	if _, ok := parseStableVersion(release.TagName); !ok {
		return checker.unavailable(now, "invalid_release_tag")
	}

	checker.etag = response.Header.Get("ETag")
	checker.cachedRelease = &release
	checker.cachedNoRelease = false
	return checker.compare(release, now)
}

func (checker *Checker) compare(release githubRelease, now time.Time) Result {
	latestVersion := displayVersion(release.TagName)
	result := Result{
		Status:         StatusDevelopment,
		CurrentVersion: checker.currentVersion,
		LatestVersion:  latestVersion,
		ReleaseName:    strings.TrimSpace(release.Name),
		ReleaseURL:     checker.releasePageURL + url.PathEscape(latestVersion),
		PublishedAt:    strings.TrimSpace(release.PublishedAt),
		CheckedAt:      now.Format(time.RFC3339),
	}

	current, currentOK := parseStableVersion(checker.currentVersion)
	latest, latestOK := parseStableVersion(latestVersion)
	if !currentOK || !latestOK {
		return result
	}
	switch compareVersions(current, latest) {
	case -1:
		result.Status = StatusUpdateAvailable
	case 0:
		result.Status = StatusUpToDate
	default:
		result.Status = StatusDevelopment
	}
	return result
}

func (checker *Checker) unavailable(now time.Time, code string) Result {
	return Result{
		Status:         StatusUnavailable,
		CurrentVersion: checker.currentVersion,
		CheckedAt:      now.Format(time.RFC3339),
		ErrorCode:      code,
	}
}

func displayVersion(value string) string {
	value = strings.TrimSpace(value)
	if parsed, ok := parseStableVersion(value); ok {
		return "v" + strconv.FormatUint(parsed[0], 10) + "." +
			strconv.FormatUint(parsed[1], 10) + "." +
			strconv.FormatUint(parsed[2], 10)
	}
	if value == "" {
		return "dev"
	}
	return value
}

func parseStableVersion(value string) ([3]uint64, bool) {
	match := stableVersionPattern.FindStringSubmatch(strings.TrimSpace(value))
	if len(match) != 4 {
		return [3]uint64{}, false
	}
	var parsed [3]uint64
	for index := range parsed {
		part, err := strconv.ParseUint(match[index+1], 10, 64)
		if err != nil {
			return [3]uint64{}, false
		}
		parsed[index] = part
	}
	return parsed, true
}

func compareVersions(left, right [3]uint64) int {
	for index := range left {
		if left[index] < right[index] {
			return -1
		}
		if left[index] > right[index] {
			return 1
		}
	}
	return 0
}
