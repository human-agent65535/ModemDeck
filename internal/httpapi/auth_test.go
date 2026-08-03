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
	"github.com/human-agent65535/modemdeck/internal/mobilepairing"
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
	webSessions       []auth.WebSession
	webSessionsError  error
	revokedSessionID  string
	revokedDigest     auth.SessionTokenDigest
	revokeSessionErr  error
	revokedOthers     []auth.SessionTokenDigest
	revokeOthersErr   error
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

func (authenticator *apiTestAuthenticator) WebSessions(
	context.Context,
	auth.SessionToken,
) ([]auth.WebSession, error) {
	return append([]auth.WebSession(nil), authenticator.webSessions...), authenticator.webSessionsError
}

func (authenticator *apiTestAuthenticator) RevokeWebSession(
	_ context.Context,
	_ auth.SessionToken,
	sessionID string,
) (auth.SessionTokenDigest, error) {
	authenticator.revokedSessionID = sessionID
	return authenticator.revokedDigest, authenticator.revokeSessionErr
}

func (authenticator *apiTestAuthenticator) RevokeOtherWebSessions(
	context.Context,
	auth.SessionToken,
) ([]auth.SessionTokenDigest, error) {
	return append([]auth.SessionTokenDigest(nil), authenticator.revokedOthers...), authenticator.revokeOthersErr
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

func TestAccountSessionsListAndRevocation(t *testing.T) {
	t.Parallel()

	authenticator, sessionToken, csrfToken := newAPIUserAuthenticator(t)
	now := time.Date(2026, 8, 2, 3, 0, 0, 0, time.UTC)
	authenticator.webSessions = []auth.WebSession{
		{
			ID:         "current-session",
			CreatedAt:  now,
			LastSeenAt: now.Add(30 * time.Minute),
			UserAgent:  "Current Browser",
			AccessIP:   "192.0.2.10",
			AccessHost: "192.168.50.111:7577",
			Current:    true,
		},
		{
			ID:         "other-session",
			CreatedAt:  now.Add(-time.Hour),
			LastSeenAt: now.Add(-10 * time.Minute),
			UserAgent:  "Other Browser",
			AccessIP:   "198.51.100.22",
			AccessHost: "call.example.test",
		},
	}
	authenticator.revokedDigest = auth.SessionTokenDigest{9}
	authenticator.revokedOthers = []auth.SessionTokenDigest{{8}}
	repository := &fakeRepository{
		iosPairingStatus: store.IOSPairingStatus{
			Allowed:             true,
			HasCredential:       true,
			CredentialCreatedAt: now.Add(-24 * time.Hour).Format(time.RFC3339),
			Paired:              true,
			PairedAt:            now.Add(-23 * time.Hour).Format(time.RFC3339),
			Device: mobilepairing.DeviceInfo{
				Name:            "Test iPhone",
				Model:           "iPhone",
				ModelIdentifier: "iPhone18,2",
				OSName:          "iOS",
				OSVersion:       "26.0",
				AppVersion:      "0.1.0",
				AppBuild:        "1",
			},
			LastSeenAt: now.Add(-5 * time.Minute).Format(time.RFC3339),
		},
		iosRevokedDigest: mobilepairing.TokenDigest{7},
	}
	api, err := New(repository, Options{Authenticator: authenticator})
	if err != nil {
		t.Fatal(err)
	}

	list := httptest.NewRecorder()
	api.ServeHTTP(
		list,
		authorizedAPIRequest(
			http.MethodGet,
			"/api/v1/account/sessions",
			nil,
			sessionToken,
			csrfToken,
		),
	)
	if list.Code != http.StatusOK {
		t.Fatalf("list status = %d, body = %s", list.Code, list.Body.String())
	}
	var listed accountSessionsResponse
	if err := json.Unmarshal(list.Body.Bytes(), &listed); err != nil {
		t.Fatal(err)
	}
	if len(listed.Sessions) != 3 ||
		!listed.Sessions[0].Current ||
		listed.Sessions[0].LastSeenAt != now.Add(30*time.Minute).Format(time.RFC3339) ||
		listed.Sessions[0].AccessIP != "192.0.2.10" ||
		listed.Sessions[1].ID != "other-session" ||
		listed.Sessions[1].LastSeenAt != now.Add(-10*time.Minute).Format(time.RFC3339) ||
		listed.Sessions[1].AccessIP != "198.51.100.22" ||
		listed.Sessions[2].ID != iosPairingDeviceID ||
		!listed.Sessions[2].Paired ||
		listed.Sessions[2].CreatedAt != now.Add(-24*time.Hour).Format(time.RFC3339) ||
		listed.Sessions[2].PairedAt != now.Add(-23*time.Hour).Format(time.RFC3339) ||
		listed.Sessions[2].LastSeenAt != now.Add(-5*time.Minute).Format(time.RFC3339) ||
		listed.Sessions[2].Device == nil ||
		listed.Sessions[2].Device.Name != "Test iPhone" {
		t.Fatalf("listed sessions = %+v", listed.Sessions)
	}

	revoke := httptest.NewRecorder()
	api.ServeHTTP(
		revoke,
		authorizedAPIRequest(
			http.MethodDelete,
			"/api/v1/account/sessions/other-session",
			nil,
			sessionToken,
			csrfToken,
		),
	)
	if revoke.Code != http.StatusNoContent ||
		authenticator.revokedSessionID != "other-session" {
		t.Fatalf(
			"revoke = status %d, session %q, body %s",
			revoke.Code,
			authenticator.revokedSessionID,
			revoke.Body.String(),
		)
	}

	revokeIOS := httptest.NewRecorder()
	api.ServeHTTP(
		revokeIOS,
		authorizedAPIRequest(
			http.MethodDelete,
			"/api/v1/account/sessions/ios-pairing",
			nil,
			sessionToken,
			csrfToken,
		),
	)
	if revokeIOS.Code != http.StatusNoContent ||
		repository.iosRevokedUserID != "user-1" {
		t.Fatalf(
			"revoke iOS = status %d, user %q, body %s",
			revokeIOS.Code,
			repository.iosRevokedUserID,
			revokeIOS.Body.String(),
		)
	}

	repository.iosPairingStatus.HasCredential = true
	revokeOthers := httptest.NewRecorder()
	api.ServeHTTP(
		revokeOthers,
		authorizedAPIRequest(
			http.MethodDelete,
			"/api/v1/account/sessions/others",
			nil,
			sessionToken,
			csrfToken,
		),
	)
	if revokeOthers.Code != http.StatusNoContent {
		t.Fatalf(
			"revoke others status = %d, body = %s",
			revokeOthers.Code,
			revokeOthers.Body.String(),
		)
	}
}

func TestMobileAccountSessionsListsCurrentIPhoneWithoutWebCookie(t *testing.T) {
	t.Parallel()

	token, digest, err := mobilepairing.NewToken()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 3, 6, 0, 0, 0, time.UTC)
	repository := &fakeRepository{
		mobileFound: true,
		mobilePrincipal: auth.Principal{
			UserID:            "user-1",
			Role:              auth.RoleAdmin,
			IOSPairingEnabled: true,
		},
		iosPairingStatus: store.IOSPairingStatus{
			Allowed:             true,
			HasCredential:       true,
			CredentialCreatedAt: now.Add(-time.Hour).Format(time.RFC3339),
			Paired:              true,
			PairedAt:            now.Add(-59 * time.Minute).Format(time.RFC3339),
			Device: mobilepairing.DeviceInfo{
				Name: "Test iPhone",
			},
			LastSeenAt: now.Format(time.RFC3339),
		},
	}
	api, err := New(repository, Options{disableAuthentication: true})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/account/sessions", nil)
	request.Header.Set("Authorization", "Bearer "+string(token))
	response := httptest.NewRecorder()
	api.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d; body = %s", response.Code, response.Body.String())
	}
	var listed accountSessionsResponse
	if err := json.Unmarshal(response.Body.Bytes(), &listed); err != nil {
		t.Fatal(err)
	}
	if len(listed.Sessions) != 1 ||
		listed.Sessions[0].ID != iosPairingDeviceID ||
		!listed.Sessions[0].Current ||
		listed.Sessions[0].Device == nil ||
		listed.Sessions[0].Device.Name != "Test iPhone" ||
		repository.mobileConfirmedDigest != digest {
		t.Fatalf("mobile sessions = %+v", listed.Sessions)
	}
}

func TestRequestSessionClientUsesNormalizedClientIP(t *testing.T) {
	t.Parallel()

	request := httptest.NewRequest(http.MethodGet, "https://call.example.test/api/v1/session", nil)
	request.Host = "call.example.test"
	request.RemoteAddr = "192.0.2.7:443"
	request.Header.Set("User-Agent", "Example Browser")
	request.Header.Set("CF-Connecting-IP", "203.0.113.8")
	request.Header.Set("X-ModemDeck-Client-IP", "2001:db8::8")
	client := requestSessionClient(request)
	if client.UserAgent != "Example Browser" ||
		client.AccessIP != "2001:db8::8" ||
		client.AccessHost != "call.example.test" {
		t.Fatalf("session client = %+v", client)
	}

	request.Header.Del("X-ModemDeck-Client-IP")
	request.Header.Set("X-Real-IP", "198.51.100.9")
	client = requestSessionClient(request)
	if client.AccessIP != "198.51.100.9" {
		t.Fatalf("X-Real-IP fallback = %q", client.AccessIP)
	}

	request.Header.Set("X-Real-IP", "not-an-ip")
	client = requestSessionClient(request)
	if client.AccessIP != "192.0.2.7" {
		t.Fatalf("remote address fallback = %q", client.AccessIP)
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
	leases := &fakeCallLeases{revokedCallIDs: []string{"call-logout"}}
	communications := &fakeCommunications{}
	media := &fakeCallMedia{}
	api, err := New(repository, Options{
		Authenticator:  authenticator,
		SecureCookies:  true,
		CallLeases:     leases,
		Communications: communications,
		CallMedia:      media,
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
	if leases.revokedHolderID != callLeaseHolderForSessionToken(
		auth.SessionToken(sessionToken),
	) {
		t.Fatalf("revoked holder = %q", leases.revokedHolderID)
	}
	if media.closed != "call-logout" {
		t.Fatalf("closed media call = %q, want call-logout", media.closed)
	}
	if communications.endCallID != "call-logout" {
		t.Fatalf("ended call = %q, want call-logout", communications.endCallID)
	}
}

func TestLoginFailureRateLimitReturnsRetryAfterAndClearsOnSuccess(t *testing.T) {
	authenticator, _, _ := newAPIAuthenticator(t)
	authenticator.loginError = auth.ErrInvalidCredentials
	authenticator.loginResult = auth.LoginResult{
		SessionToken: auth.SessionToken(opaqueTestToken(21)),
		CSRFToken:    auth.CSRFToken(opaqueTestToken(22)),
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
	events := runtimeevents.NewHub()
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
	signal, _, cancel := events.Subscribe()
	cancel()
	if signal.Revision != 1 || signal.DataRevision != 1 {
		t.Fatalf("password-change runtime signal = %+v", signal)
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
		},
	}
	service, err := auth.NewService(repository)
	if err != nil {
		t.Fatalf("auth.NewService() error = %v", err)
	}
	return &apiTestAuthenticator{service: service}, sessionToken, csrfToken
}

func newAPIUserAuthenticator(t *testing.T) (*apiTestAuthenticator, string, string) {
	t.Helper()

	sessionToken := opaqueTestToken(11)
	csrfToken := opaqueTestToken(12)
	sessionHash := sha256.Sum256([]byte(sessionToken))
	csrfHash := sha256.Sum256([]byte(csrfToken))
	repository := &streamAuthRepository{
		apiAuthRepository: &apiAuthRepository{
			configured: true,
			credentials: auth.AdminCredentials{
				Username:     "admin",
				PasswordHash: "test-password-hash",
			},
			found: true,
			session: auth.SessionRecord{
				SessionTokenDigest: auth.SessionTokenDigest(sessionHash),
				CSRFTokenDigest:    auth.CSRFTokenDigest(csrfHash),
				CreatedAt:          time.Now().UTC().Add(-time.Minute),
			},
		},
		principal: auth.Principal{
			UserID:            "user-1",
			Username:          "member",
			Role:              auth.RoleMember,
			IOSPairingEnabled: true,
		},
		found: true,
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
