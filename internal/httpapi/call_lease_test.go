package httpapi

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

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
