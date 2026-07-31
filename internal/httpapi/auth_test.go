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
	"github.com/human-agent65535/modemdeck/internal/runtimeevents"
	"github.com/human-agent65535/modemdeck/internal/store"
)

type apiAuthRepository struct {
	credentials auth.AdminCredentials
	configured  bool
	session     auth.SessionRecord
	found       bool
	err         error
}

func (repository *apiAuthRepository) AdminCredentials(context.Context) (auth.AdminCredentials, bool, error) {
	if repository.err != nil {
		return auth.AdminCredentials{}, false, repository.err
	}
	return repository.credentials, repository.configured, nil
}

func (repository *apiAuthRepository) CreateAdminIfAbsent(
	_ context.Context,
	credentials auth.AdminCredentials,
) (bool, error) {
	if repository.err != nil {
		return false, repository.err
	}
	if repository.configured {
		return false, nil
	}
	repository.credentials = credentials
	repository.configured = true
	return true, nil
}

func (*apiAuthRepository) ReplaceAdminPasswordHashIfCurrentAndRevokeSessions(
	context.Context,
	string,
	string,
) (bool, error) {
	return false, nil
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
	service           *auth.Service
	loginResult       auth.LoginResult
	loginError        error
	loginCalls        int
	loginUsername     string
	loginPassword     string
	logoutError       error
	setupError        error
	setupCalls        int
	changeError       error
	changeCalls       int
	currentPassword   string
	newPassword       string
	authenticateError error
}

func (authenticator *apiTestAuthenticator) Status(ctx context.Context) (auth.AdminStatus, error) {
	return authenticator.service.Status(ctx)
}

func (authenticator *apiTestAuthenticator) Setup(
	ctx context.Context,
	username, password string,
) error {
	authenticator.setupCalls++
	if authenticator.setupError != nil {
		return authenticator.setupError
	}
	return authenticator.service.Setup(ctx, username, password)
}

func (authenticator *apiTestAuthenticator) Login(
	_ context.Context,
	username, password string,
) (auth.LoginResult, error) {
	authenticator.loginCalls++
	authenticator.loginUsername = username
	authenticator.loginPassword = password
	return authenticator.loginResult, authenticator.loginError
}

func (authenticator *apiTestAuthenticator) ChangePassword(
	_ context.Context,
	currentPassword, newPassword string,
) error {
	authenticator.changeCalls++
	authenticator.currentPassword = currentPassword
	authenticator.newPassword = newPassword
	return authenticator.changeError
}

func (authenticator *apiTestAuthenticator) Authenticate(ctx context.Context, token auth.SessionToken) (auth.Authentication, error) {
	if authenticator.authenticateError != nil {
		return auth.Authentication{}, authenticator.authenticateError
	}
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
	api, err := New(repository, Options{Authenticator: authenticator})
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
	if err := json.Unmarshal(anonymousSession.Body.Bytes(), &anonymous); err != nil ||
		anonymous.Authenticated ||
		anonymous.SetupRequired {
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

func TestCanceledAuthenticationRequestEndsSilently(t *testing.T) {
	t.Parallel()

	authenticator, sessionToken, csrfToken := newAPIAuthenticator(t)
	authenticator.authenticateError = &auth.Error{
		Code: auth.CodeRepository,
		Op:   "authenticate",
		Err:  context.Canceled,
	}
	api, err := New(&fakeRepository{}, Options{Authenticator: authenticator})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	request := authorizedAPIRequest(
		http.MethodGet,
		"/api/v1/bootstrap",
		nil,
		sessionToken,
		csrfToken,
	)
	response := httptest.NewRecorder()

	_, _, authenticated, handled := api.requestAuthentication(response, request, true)
	if authenticated || !handled {
		t.Fatalf("authentication result = authenticated %t, handled %t", authenticated, handled)
	}
	if response.Body.Len() != 0 {
		t.Fatalf("canceled authentication wrote a response: %s", response.Body.String())
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
		Principal: &auth.Principal{
			UserID:         "user_member",
			Username:       "owner",
			Role:           auth.RoleMember,
			AllowedLineIDs: []string{"line_alpha"},
		},
	}
	repository := &fakeRepository{
		principalLanguage: store.SystemLanguageJaJP,
	}
	api, err := New(repository, Options{
		Authenticator: authenticator,
		SecureCookies: true,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	authenticator.loginError = auth.ErrInvalidCredentials
	wrongUsername := httptest.NewRequest(http.MethodPost, "/api/v1/session", bytes.NewBufferString(`{"username":"other","password":"secret"}`))
	wrongUsername.Header.Set("Content-Type", "application/json")
	wrongResponse := httptest.NewRecorder()
	api.ServeHTTP(wrongResponse, wrongUsername)
	assertAPIError(t, wrongResponse, http.StatusUnauthorized, "invalid_credentials")
	if authenticator.loginCalls != 1 ||
		authenticator.loginUsername != "other" ||
		authenticator.loginPassword != "secret" {
		t.Fatalf(
			"wrong login call = %d, %q, %q",
			authenticator.loginCalls,
			authenticator.loginUsername,
			authenticator.loginPassword,
		)
	}

	authenticator.loginError = nil
	login := httptest.NewRequest(http.MethodPost, "/api/v1/session", bytes.NewBufferString(`{"username":"owner","password":"secret"}`))
	login.Header.Set("Content-Type", "application/json")
	loginResponse := httptest.NewRecorder()
	api.ServeHTTP(loginResponse, login)
	if loginResponse.Code != http.StatusOK {
		t.Fatalf("login status = %d; body = %s", loginResponse.Code, loginResponse.Body.String())
	}
	if bytes.Contains(loginResponse.Body.Bytes(), []byte("must_change_password")) {
		t.Fatalf("login retained deprecated password-change state: %s", loginResponse.Body.String())
	}
	var loginSession sessionResponse
	if err := json.Unmarshal(loginResponse.Body.Bytes(), &loginSession); err != nil {
		t.Fatalf("decode login session: %v", err)
	}
	if loginSession.UserID != "user_member" ||
		loginSession.Role != string(auth.RoleMember) ||
		loginSession.Language != string(store.SystemLanguageJaJP) {
		t.Fatalf("login session = %+v", loginSession)
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

func TestLoginFailureRateLimitReturnsRetryAfterAndClearsOnSuccess(t *testing.T) {
	authenticator, _, _ := newAPIAuthenticator(t)
	authenticator.loginError = auth.ErrInvalidCredentials
	authenticator.loginResult = auth.LoginResult{
		SessionToken: auth.SessionToken(opaqueTestToken(21)),
		CSRFToken:    auth.CSRFToken(opaqueTestToken(22)),
		ExpiresAt:    time.Now().UTC().Add(auth.SessionLifetime),
	}
	api, err := New(&fakeRepository{}, Options{Authenticator: authenticator})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	api.loginFailures = newLoginFailureLimiter(loginFailurePolicy{
		window:           time.Minute,
		accountThreshold: 2,
		globalThreshold:  100,
		initialBackoff:   2 * time.Second,
		maximumBackoff:   8 * time.Second,
		maximumAccounts:  8,
	})
	api.loginFailures.now = func() time.Time { return now }

	attempt := func(username string) *httptest.ResponseRecorder {
		t.Helper()
		request := httptest.NewRequest(
			http.MethodPost,
			"/api/v1/session",
			bytes.NewBufferString(`{"username":"`+username+`","password":"secret"}`),
		)
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		api.ServeHTTP(response, request)
		return response
	}

	for range 2 {
		assertAPIError(t, attempt("Owner"), http.StatusUnauthorized, "invalid_credentials")
	}
	blocked := attempt("owner")
	assertAPIError(t, blocked, http.StatusTooManyRequests, "login_rate_limited")
	if retryAfter := blocked.Header().Get("Retry-After"); retryAfter != "2" {
		t.Fatalf("Retry-After = %q, want 2", retryAfter)
	}
	if authenticator.loginCalls != 2 {
		t.Fatalf("login calls while blocked = %d, want 2", authenticator.loginCalls)
	}

	now = now.Add(2 * time.Second)
	authenticator.loginError = nil
	successful := attempt("OWNER")
	if successful.Code != http.StatusOK {
		t.Fatalf("successful login status = %d; body = %s", successful.Code, successful.Body.String())
	}

	authenticator.loginError = auth.ErrInvalidCredentials
	for range 2 {
		assertAPIError(t, attempt("owner"), http.StatusUnauthorized, "invalid_credentials")
	}
	assertAPIError(t, attempt("owner"), http.StatusTooManyRequests, "login_rate_limited")
}

func TestQuickStartCreatesAdministratorAndSession(t *testing.T) {
	t.Parallel()

	authRepository := &apiAuthRepository{}
	service, err := auth.NewService(authRepository)
	if err != nil {
		t.Fatalf("auth.NewService() error = %v", err)
	}
	authenticator := &apiTestAuthenticator{
		service: service,
		loginResult: auth.LoginResult{
			SessionToken: auth.SessionToken(opaqueTestToken(11)),
			CSRFToken:    auth.CSRFToken(opaqueTestToken(12)),
			ExpiresAt:    time.Now().UTC().Add(auth.SessionLifetime),
		},
	}
	api, err := New(&fakeRepository{}, Options{Authenticator: authenticator})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	sessionRecorder := httptest.NewRecorder()
	api.ServeHTTP(
		sessionRecorder,
		httptest.NewRequest(http.MethodGet, "/api/v1/session", nil),
	)
	var before sessionResponse
	if err := json.Unmarshal(sessionRecorder.Body.Bytes(), &before); err != nil {
		t.Fatalf("decode setup status: %v", err)
	}
	if before.Authenticated || !before.SetupRequired {
		t.Fatalf("session before setup = %+v", before)
	}

	setupRequest := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/setup",
		bytes.NewBufferString(`{"username":"owner","password":"a secure initial password"}`),
	)
	setupRequest.Header.Set("Content-Type", "application/json")
	setupRecorder := httptest.NewRecorder()
	api.ServeHTTP(setupRecorder, setupRequest)
	if setupRecorder.Code != http.StatusCreated {
		t.Fatalf("setup status = %d; body = %s", setupRecorder.Code, setupRecorder.Body.String())
	}
	var after sessionResponse
	if err := json.Unmarshal(setupRecorder.Body.Bytes(), &after); err != nil {
		t.Fatalf("decode setup response: %v", err)
	}
	if !after.Authenticated || after.SetupRequired || after.Username != "owner" || after.CSRFToken == "" {
		t.Fatalf("setup response = %+v", after)
	}
	if authenticator.setupCalls != 1 ||
		authenticator.loginCalls != 1 ||
		authenticator.loginUsername != "owner" {
		t.Fatalf(
			"setup/login calls = %d/%d, login username %q",
			authenticator.setupCalls,
			authenticator.loginCalls,
			authenticator.loginUsername,
		)
	}
	matches, err := auth.VerifyPassword(
		"a secure initial password",
		authRepository.credentials.PasswordHash,
	)
	if err != nil || !matches || authRepository.credentials.Username != "owner" {
		t.Fatalf("stored credentials = %+v, matches = %v, error = %v", authRepository.credentials, matches, err)
	}

	second := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/setup",
		bytes.NewBufferString(`{"username":"other","password":"another secure password"}`),
	)
	second.Header.Set("Content-Type", "application/json")
	secondRecorder := httptest.NewRecorder()
	api.ServeHTTP(secondRecorder, second)
	assertAPIError(t, secondRecorder, http.StatusConflict, "setup_complete")
}

func TestChangePasswordRequiresCSRFAndClearsSession(t *testing.T) {
	t.Parallel()

	authenticator, sessionToken, csrfToken := newAPIAuthenticator(t)
	events := runtimeevents.NewBuffer(8)
	api, err := New(&fakeRepository{}, Options{
		Authenticator: authenticator,
		RuntimeEvents: events,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	body := []byte(`{"current_password":"current password","new_password":"new secure password"}`)

	withoutCSRF := authorizedAPIRequest(
		http.MethodPut,
		"/api/v1/account/password",
		bytes.NewReader(body),
		sessionToken,
		"",
	)
	withoutCSRF.Header.Set("Content-Type", "application/json")
	denied := httptest.NewRecorder()
	api.ServeHTTP(denied, withoutCSRF)
	assertAPIError(t, denied, http.StatusForbidden, "csrf_failed")
	if authenticator.changeCalls != 0 {
		t.Fatalf("change calls without CSRF = %d, want 0", authenticator.changeCalls)
	}

	change := authorizedAPIRequest(
		http.MethodPut,
		"/api/v1/account/password",
		bytes.NewReader(body),
		sessionToken,
		csrfToken,
	)
	change.Header.Set("Content-Type", "application/json")
	changed := httptest.NewRecorder()
	api.ServeHTTP(changed, change)
	if changed.Code != http.StatusNoContent {
		t.Fatalf("change status = %d; body = %s", changed.Code, changed.Body.String())
	}
	if authenticator.changeCalls != 1 ||
		authenticator.currentPassword != "current password" ||
		authenticator.newPassword != "new secure password" {
		t.Fatalf(
			"change call = %d, %q, %q",
			authenticator.changeCalls,
			authenticator.currentPassword,
			authenticator.newPassword,
		)
	}
	cookies := changed.Result().Cookies()
	if len(cookies) != 2 {
		t.Fatalf("cleared cookies = %d, want 2", len(cookies))
	}
	for _, cookie := range cookies {
		if cookie.MaxAge >= 0 {
			t.Fatalf("cleared cookie %s MaxAge = %d", cookie.Name, cookie.MaxAge)
		}
	}
	window, _, cancel := events.Subscribe(0)
	cancel()
	if len(window.Events) != 1 ||
		len(window.Events[0].Resources) != 1 ||
		window.Events[0].Resources[0] != runtimeevents.ResourceSession {
		t.Fatalf("password-change runtime events = %+v", window.Events)
	}
}

func TestChangePasswordMapsValidationErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		failure    error
		wantStatus int
		wantCode   string
	}{
		{
			name:       "current password",
			failure:    auth.ErrInvalidCredentials,
			wantStatus: http.StatusBadRequest,
			wantCode:   "invalid_current_password",
		},
		{
			name:       "short password",
			failure:    auth.ErrPasswordTooShort,
			wantStatus: http.StatusUnprocessableEntity,
			wantCode:   "password_too_short",
		},
		{
			name:       "unchanged password",
			failure:    auth.ErrPasswordUnchanged,
			wantStatus: http.StatusUnprocessableEntity,
			wantCode:   "password_unchanged",
		},
		{
			name:       "repository",
			failure:    auth.ErrRepository,
			wantStatus: http.StatusServiceUnavailable,
			wantCode:   "authentication_unavailable",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			authenticator, sessionToken, csrfToken := newAPIAuthenticator(t)
			authenticator.changeError = test.failure
			api, err := New(&fakeRepository{}, Options{Authenticator: authenticator})
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}
			request := authorizedAPIRequest(
				http.MethodPut,
				"/api/v1/account/password",
				bytes.NewReader([]byte(`{"current_password":"current password","new_password":"new secure password"}`)),
				sessionToken,
				csrfToken,
			)
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			api.ServeHTTP(response, request)
			assertAPIError(t, response, test.wantStatus, test.wantCode)
			if cookies := response.Result().Cookies(); len(cookies) != 0 {
				t.Fatalf("failed password change cleared %d cookies", len(cookies))
			}
		})
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
		configured: true,
		credentials: auth.AdminCredentials{
			Username:     "admin",
			PasswordHash: "test-password-hash",
		},
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
