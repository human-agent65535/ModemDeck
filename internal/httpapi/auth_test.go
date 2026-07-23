package httpapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/human-agent65535/modemdeck/internal/auth"
	"github.com/human-agent65535/modemdeck/internal/store"
)

type apiAuthRepository struct {
	session auth.SessionRecord
	found   bool
	err     error
}

func (*apiAuthRepository) AdminPasswordHash(context.Context) (string, bool, error) {
	return "", false, nil
}

func (*apiAuthRepository) ReplaceAdminPasswordHashAndRevokeSessions(context.Context, string) error {
	return nil
}

func (*apiAuthRepository) CreateSessionIfPasswordHash(context.Context, string, auth.SessionRecord) (bool, error) {
	return false, nil
}

func (repository *apiAuthRepository) SessionByTokenDigest(_ context.Context, digest auth.SessionTokenDigest) (auth.SessionRecord, bool, error) {
	if repository.err != nil {
		return auth.SessionRecord{}, false, repository.err
	}
	if !repository.found || digest != repository.session.SessionTokenDigest {
		return auth.SessionRecord{}, false, nil
	}
	return repository.session, true, nil
}

func (repository *apiAuthRepository) DeleteSessionByTokenDigest(_ context.Context, digest auth.SessionTokenDigest) error {
	if repository.err != nil {
		return repository.err
	}
	if digest == repository.session.SessionTokenDigest {
		repository.found = false
	}
	return nil
}

type apiTestAuthenticator struct {
	service     *auth.Service
	loginResult auth.LoginResult
	loginError  error
	loginCalls  int
	logoutError error
}

func (authenticator *apiTestAuthenticator) Login(context.Context, string) (auth.LoginResult, error) {
	authenticator.loginCalls++
	return authenticator.loginResult, authenticator.loginError
}

func (authenticator *apiTestAuthenticator) Authenticate(ctx context.Context, token auth.SessionToken) (auth.Authentication, error) {
	return authenticator.service.Authenticate(ctx, token)
}

func (authenticator *apiTestAuthenticator) Logout(ctx context.Context, token auth.SessionToken) error {
	if authenticator.logoutError != nil {
		return authenticator.logoutError
	}
	return authenticator.service.Logout(ctx, token)
}

func TestNewRequiresAuthenticator(t *testing.T) {
	t.Parallel()

	_, err := New(&fakeRepository{}, Options{})
	if !errors.Is(err, ErrAuthenticatorRequired) {
		t.Fatalf("New() error = %v, want ErrAuthenticatorRequired", err)
	}
}

func TestSessionAndProtectedAPI(t *testing.T) {
	t.Parallel()

	authenticator, sessionToken, csrfToken := newAPIAuthenticator(t)
	repository := &fakeRepository{lines: []store.LineSummary{}}
	api, err := New(repository, Options{Authenticator: authenticator, AdminUsername: "admin"})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	missing := httptest.NewRecorder()
	api.ServeHTTP(missing, httptest.NewRequest(http.MethodGet, "/api/v1/bootstrap", nil))
	assertAPIError(t, missing, http.StatusUnauthorized, "authentication_required")

	anonymousSession := httptest.NewRecorder()
	api.ServeHTTP(anonymousSession, httptest.NewRequest(http.MethodGet, "/api/v1/session", nil))
	if anonymousSession.Code != http.StatusOK {
		t.Fatalf("anonymous session status = %d", anonymousSession.Code)
	}
	var anonymous sessionResponse
	if err := json.Unmarshal(anonymousSession.Body.Bytes(), &anonymous); err != nil || anonymous.Authenticated {
		t.Fatalf("anonymous session = %+v, err = %v", anonymous, err)
	}

	sessionRequest := authorizedAPIRequest(http.MethodGet, "/api/v1/session", nil, sessionToken, csrfToken)
	sessionResponseRecorder := httptest.NewRecorder()
	api.ServeHTTP(sessionResponseRecorder, sessionRequest)
	if sessionResponseRecorder.Code != http.StatusOK {
		t.Fatalf("session status = %d; body = %s", sessionResponseRecorder.Code, sessionResponseRecorder.Body.String())
	}
	var session sessionResponse
	if err := json.Unmarshal(sessionResponseRecorder.Body.Bytes(), &session); err != nil {
		t.Fatalf("decode session: %v", err)
	}
	if !session.Authenticated || session.Username != "admin" || session.CSRFToken != csrfToken {
		t.Fatalf("session = %+v", session)
	}

	bootstrap := httptest.NewRecorder()
	api.ServeHTTP(bootstrap, authorizedAPIRequest(http.MethodGet, "/api/v1/bootstrap", nil, sessionToken, ""))
	if bootstrap.Code != http.StatusOK {
		t.Fatalf("bootstrap status = %d; body = %s", bootstrap.Code, bootstrap.Body.String())
	}
}

func TestProtectedWriteRequiresCSRF(t *testing.T) {
	t.Parallel()

	authenticator, sessionToken, csrfToken := newAPIAuthenticator(t)
	repository := &fakeRepository{contact: store.Contact{ID: "contact-1", DisplayName: "Aiko", Revision: 1}}
	api, err := New(repository, Options{Authenticator: authenticator})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	body := []byte(`{"display_name":"Aiko","phones":[{"label":"mobile","number":"+819012345678","primary":true}]}`)
	withoutCSRF := authorizedAPIRequest(http.MethodPost, "/api/v1/contacts", bytes.NewReader(body), sessionToken, "")
	withoutCSRF.Header.Set("Content-Type", "application/json")
	denied := httptest.NewRecorder()
	api.ServeHTTP(denied, withoutCSRF)
	assertAPIError(t, denied, http.StatusForbidden, "csrf_failed")

	withCSRF := authorizedAPIRequest(http.MethodPost, "/api/v1/contacts", bytes.NewReader(body), sessionToken, csrfToken)
	withCSRF.Header.Set("Content-Type", "application/json")
	created := httptest.NewRecorder()
	api.ServeHTTP(created, withCSRF)
	if created.Code != http.StatusCreated {
		t.Fatalf("create status = %d; body = %s", created.Code, created.Body.String())
	}
}

func TestLoginAndLogoutCookies(t *testing.T) {
	t.Parallel()

	authenticator, sessionToken, csrfToken := newAPIAuthenticator(t)
	authenticator.loginResult = auth.LoginResult{
		SessionToken: auth.SessionToken(sessionToken),
		CSRFToken:    auth.CSRFToken(csrfToken),
		ExpiresAt:    time.Now().UTC().Add(auth.SessionLifetime),
	}
	api, err := New(&fakeRepository{}, Options{
		Authenticator: authenticator,
		AdminUsername: "owner",
		SecureCookies: true,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	wrongUsername := httptest.NewRequest(http.MethodPost, "/api/v1/session", bytes.NewBufferString(`{"username":"other","password":"secret"}`))
	wrongUsername.Header.Set("Content-Type", "application/json")
	wrongResponse := httptest.NewRecorder()
	api.ServeHTTP(wrongResponse, wrongUsername)
	assertAPIError(t, wrongResponse, http.StatusUnauthorized, "invalid_credentials")
	if authenticator.loginCalls != 0 {
		t.Fatalf("login calls = %d, want 0 for wrong username", authenticator.loginCalls)
	}

	login := httptest.NewRequest(http.MethodPost, "/api/v1/session", bytes.NewBufferString(`{"username":"owner","password":"secret"}`))
	login.Header.Set("Content-Type", "application/json")
	loginResponse := httptest.NewRecorder()
	api.ServeHTTP(loginResponse, login)
	if loginResponse.Code != http.StatusOK {
		t.Fatalf("login status = %d; body = %s", loginResponse.Code, loginResponse.Body.String())
	}
	cookies := loginResponse.Result().Cookies()
	if len(cookies) != 2 {
		t.Fatalf("login cookies = %d, want 2", len(cookies))
	}
	for _, cookie := range cookies {
		if !cookie.Secure || cookie.SameSite != http.SameSiteStrictMode || cookie.Path != "/" {
			t.Fatalf("cookie %s attributes = %#v", cookie.Name, cookie)
		}
		if cookie.Name == sessionCookieName && !cookie.HttpOnly {
			t.Fatal("session cookie is not HttpOnly")
		}
	}

	logout := authorizedAPIRequest(http.MethodDelete, "/api/v1/session", nil, sessionToken, csrfToken)
	logoutResponse := httptest.NewRecorder()
	api.ServeHTTP(logoutResponse, logout)
	if logoutResponse.Code != http.StatusNoContent {
		t.Fatalf("logout status = %d; body = %s", logoutResponse.Code, logoutResponse.Body.String())
	}
	for _, cookie := range logoutResponse.Result().Cookies() {
		if cookie.MaxAge >= 0 {
			t.Fatalf("cleared cookie %s MaxAge = %d", cookie.Name, cookie.MaxAge)
		}
	}
}

func TestSessionRepositoryFailureIsUnavailable(t *testing.T) {
	t.Parallel()

	authenticator, sessionToken, csrfToken := newAPIAuthenticator(t)
	authenticator.service, _ = auth.NewService(&apiAuthRepository{err: errors.New("database down")})
	api, err := New(&fakeRepository{}, Options{Authenticator: authenticator})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	response := httptest.NewRecorder()
	api.ServeHTTP(response, authorizedAPIRequest(http.MethodGet, "/api/v1/session", nil, sessionToken, csrfToken))
	assertAPIError(t, response, http.StatusServiceUnavailable, "authentication_unavailable")
}

func TestLogoutFailureKeepsBrowserCookies(t *testing.T) {
	t.Parallel()

	authenticator, sessionToken, csrfToken := newAPIAuthenticator(t)
	authenticator.logoutError = errors.New("database down")
	api, err := New(&fakeRepository{}, Options{Authenticator: authenticator})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	response := httptest.NewRecorder()
	api.ServeHTTP(
		response,
		authorizedAPIRequest(http.MethodDelete, "/api/v1/session", nil, sessionToken, csrfToken),
	)
	assertAPIError(t, response, http.StatusServiceUnavailable, "authentication_unavailable")
	if cookies := response.Result().Cookies(); len(cookies) != 0 {
		t.Fatalf("logout failure cleared %d cookies", len(cookies))
	}
}

func newAPIAuthenticator(t *testing.T) (*apiTestAuthenticator, string, string) {
	t.Helper()

	sessionToken := opaqueTestToken(1)
	csrfToken := opaqueTestToken(2)
	sessionHash := sha256.Sum256([]byte(sessionToken))
	csrfHash := sha256.Sum256([]byte(csrfToken))
	now := time.Now().UTC()
	repository := &apiAuthRepository{
		found: true,
		session: auth.SessionRecord{
			SessionTokenDigest: auth.SessionTokenDigest(sessionHash),
			CSRFTokenDigest:    auth.CSRFTokenDigest(csrfHash),
			CreatedAt:          now.Add(-time.Minute),
			ExpiresAt:          now.Add(time.Hour),
		},
	}
	service, err := auth.NewService(repository)
	if err != nil {
		t.Fatalf("auth.NewService() error = %v", err)
	}
	return &apiTestAuthenticator{service: service}, sessionToken, csrfToken
}

func opaqueTestToken(value byte) string {
	return base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{value}, auth.TokenBytes))
}

func authorizedAPIRequest(method, path string, body *bytes.Reader, sessionToken, csrfToken string) *http.Request {
	var request *http.Request
	if body == nil {
		request = httptest.NewRequest(method, path, nil)
	} else {
		request = httptest.NewRequest(method, path, body)
	}
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: sessionToken})
	if csrfToken != "" {
		request.AddCookie(&http.Cookie{Name: csrfCookieName, Value: csrfToken})
		request.Header.Set(csrfHeaderName, csrfToken)
	}
	return request
}

func assertAPIError(t *testing.T, response *httptest.ResponseRecorder, status int, code string) {
	t.Helper()

	if response.Code != status {
		t.Fatalf("status = %d, want %d; body = %s", response.Code, status, response.Body.String())
	}
	var body errorResponse
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if body.Code != code {
		t.Fatalf("code = %q, want %q", body.Code, code)
	}
}
