package httpapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/human-agent65535/modemdeck/internal/auth"
	"github.com/human-agent65535/modemdeck/internal/calllease"
	"github.com/human-agent65535/modemdeck/internal/store"
)

type fakeCallLeases struct {
	callID           string
	holderID         string
	reservationID    string
	lineID           string
	status           calllease.Status
	control          calllease.ControlState
	reservations     []calllease.OutgoingReservation
	projection       *calllease.ActiveProjection
	projected        []store.Call
	err              error
	reserveErr       error
	activateErr      error
	releaseErr       error
	reserveReplay    bool
	claims           int
	reserves         int
	activations      int
	reads            int
	projects         int
	reservationReads int
	requires         int
	releases         int
}

func (leases *fakeCallLeases) ReserveOutgoing(
	_ context.Context,
	reservationID string,
	lineID string,
	holderID string,
) (calllease.OutgoingReservation, error) {
	leases.reservationID = reservationID
	leases.lineID = lineID
	leases.holderID = holderID
	leases.reserves++
	return calllease.OutgoingReservation{
		ID:           reservationID,
		LineID:       lineID,
		HolderID:     holderID,
		Created:      !leases.reserveReplay,
		ControlState: calllease.ControlOwned,
	}, leases.reserveErr
}

func (leases *fakeCallLeases) ActivateOutgoing(
	_ context.Context,
	reservationID string,
	callID string,
	holderID string,
) (calllease.Status, error) {
	leases.reservationID = reservationID
	leases.callID = callID
	leases.holderID = holderID
	leases.activations++
	return leases.status, leases.activateErr
}

func (leases *fakeCallLeases) ReleaseOutgoing(
	reservationID string,
	holderID string,
) (bool, error) {
	leases.reservationID = reservationID
	leases.holderID = holderID
	leases.releases++
	return true, leases.releaseErr
}

func (leases *fakeCallLeases) OutgoingReservations(
	holderID string,
) ([]calllease.OutgoingReservation, error) {
	leases.holderID = holderID
	leases.reservationReads++
	return append([]calllease.OutgoingReservation(nil), leases.reservations...), leases.err
}

func (leases *fakeCallLeases) ProjectActive(
	calls []store.Call,
	holderID string,
) (calllease.ActiveProjection, error) {
	leases.holderID = holderID
	leases.projects++
	leases.projected = append([]store.Call(nil), calls...)
	if leases.err != nil {
		return calllease.ActiveProjection{}, leases.err
	}
	if leases.projection != nil {
		return *leases.projection, nil
	}
	projectedCalls := make([]calllease.ProjectedCall, 0, len(calls))
	for _, call := range calls {
		projectedCalls = append(projectedCalls, calllease.ProjectedCall{
			Call:         call,
			ControlState: leases.control,
		})
	}
	return calllease.ActiveProjection{
		Calls: projectedCalls,
		Reservations: append(
			[]calllease.OutgoingReservation(nil),
			leases.reservations...,
		),
	}, nil
}

func (leases *fakeCallLeases) Claim(
	_ context.Context,
	callID string,
	holderID string,
) (calllease.Status, error) {
	leases.callID = callID
	leases.holderID = holderID
	leases.claims++
	return leases.status, leases.err
}

func (leases *fakeCallLeases) Renew(
	_ context.Context,
	callID string,
	holderID string,
) (calllease.Status, error) {
	leases.callID = callID
	leases.holderID = holderID
	return leases.status, leases.err
}

func (leases *fakeCallLeases) ControlState(
	_ context.Context,
	callID string,
	holderID string,
) (calllease.ControlState, error) {
	leases.callID = callID
	leases.holderID = holderID
	leases.reads++
	return leases.control, leases.err
}

func (leases *fakeCallLeases) Require(
	_ context.Context,
	callID string,
	holderID string,
) error {
	leases.callID = callID
	leases.holderID = holderID
	leases.requires++
	return leases.err
}

func (leases *fakeCallLeases) Release(
	_ context.Context,
	callID string,
	holderID string,
) error {
	leases.callID = callID
	leases.holderID = holderID
	leases.releases++
	return leases.err
}

func TestRenewCallLease(t *testing.T) {
	t.Parallel()
	expiresAt := time.Date(2026, time.July, 28, 12, 0, 15, 0, time.UTC)
	leases := &fakeCallLeases{status: calllease.Status{
		CallID:    "call-1",
		HolderID:  "browser-1",
		ExpiresAt: expiresAt,
	}}
	api, err := New(&fakeRepository{}, Options{
		CallLeases:            leases,
		disableAuthentication: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(
		http.MethodPut,
		"/api/v1/calls/call-1/lease",
		bytes.NewBufferString(`{"holder_id":"browser-1"}`),
	)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	api.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if leases.callID != "call-1" || leases.holderID != "browser-1" {
		t.Fatalf("renewed lease = call %q holder %q", leases.callID, leases.holderID)
	}
}

func TestRenewCallLeaseRequiresConfiguredService(t *testing.T) {
	t.Parallel()
	api, err := New(
		&fakeRepository{},
		Options{disableAuthentication: true},
	)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(
		http.MethodPut,
		"/api/v1/calls/call-1/lease",
		bytes.NewBufferString(`{"holder_id":"browser-1"}`),
	)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	api.ServeHTTP(response, request)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
}

func TestCallLeaseHolderIsBoundToAuthenticatedSession(t *testing.T) {
	t.Parallel()

	sessionToken := opaqueTestToken(31)
	csrfToken := opaqueTestToken(32)
	sessionDigest := sha256.Sum256([]byte(sessionToken))
	csrfDigest := sha256.Sum256([]byte(csrfToken))
	now := time.Now().UTC()
	repository := &apiAuthRepository{
		configured: true,
		credentials: auth.AdminCredentials{
			Username:     "admin",
			PasswordHash: "test-password-hash",
		},
		found: true,
		session: auth.SessionRecord{
			SessionTokenDigest: auth.SessionTokenDigest(sessionDigest),
			CSRFTokenDigest:    auth.CSRFTokenDigest(csrfDigest),
			CreatedAt:          now.Add(-time.Minute),
			ExpiresAt:          now.Add(time.Hour),
		},
	}
	service, err := auth.NewService(repository)
	if err != nil {
		t.Fatalf("auth.NewService() error = %v", err)
	}
	leases := &fakeCallLeases{status: calllease.Status{
		CallID:    "call-1",
		ExpiresAt: now.Add(time.Minute),
	}}
	api, err := New(&fakeRepository{}, Options{
		Authenticator: &apiTestAuthenticator{service: service},
		CallLeases:    leases,
	})
	if err != nil {
		t.Fatal(err)
	}

	renew := func(sessionToken, csrfToken string) (string, calllease.Status) {
		request := authorizedAPIRequest(
			http.MethodPut,
			"/api/v1/calls/call-1/lease",
			bytes.NewReader([]byte(`{"holder_id":"browser-1"}`)),
			sessionToken,
			csrfToken,
		)
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		api.ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
		}
		var status calllease.Status
		if err := json.Unmarshal(response.Body.Bytes(), &status); err != nil {
			t.Fatalf("decode call lease status: %v", err)
		}
		return leases.holderID, status
	}

	firstHolder, firstStatus := renew(sessionToken, csrfToken)
	replayedHolder, _ := renew(sessionToken, csrfToken)
	if firstHolder != replayedHolder {
		t.Fatalf("same session holder changed from %q to %q", firstHolder, replayedHolder)
	}
	if firstHolder == "browser-1" ||
		!strings.HasPrefix(firstHolder, callLeaseHolderScopePrefix) {
		t.Fatalf("internal holder = %q", firstHolder)
	}
	if firstStatus.HolderID != "browser-1" {
		t.Fatalf("public holder = %q, want browser-1", firstStatus.HolderID)
	}

	secondSessionToken := opaqueTestToken(33)
	secondCSRFToken := opaqueTestToken(34)
	secondSessionDigest := sha256.Sum256([]byte(secondSessionToken))
	secondCSRFDigest := sha256.Sum256([]byte(secondCSRFToken))
	repository.session.SessionTokenDigest = auth.SessionTokenDigest(secondSessionDigest)
	repository.session.CSRFTokenDigest = auth.CSRFTokenDigest(secondCSRFDigest)

	secondHolder, secondStatus := renew(secondSessionToken, secondCSRFToken)
	if secondHolder == firstHolder {
		t.Fatalf("different sessions shared holder %q", secondHolder)
	}
	if secondStatus.HolderID != "browser-1" {
		t.Fatalf("public holder = %q, want browser-1", secondStatus.HolderID)
	}
}
