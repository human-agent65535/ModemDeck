package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/human-agent65535/modemdeck/internal/calllease"
	"github.com/human-agent65535/modemdeck/internal/callmedia"
	"github.com/human-agent65535/modemdeck/internal/communication"
	"github.com/human-agent65535/modemdeck/internal/mediaapp"
	"github.com/human-agent65535/modemdeck/internal/store"
)

const outgoingPrepareDuration = 30 * time.Second

type preparationState uint8

const (
	preparationPrepared preparationState = iota
	preparationCommitted
	preparationFailed
	preparationCancelled
)

type outgoingPreparation struct {
	mu              sync.Mutex
	dispatchMu      sync.Mutex
	dispatchCancel  context.CancelFunc
	cancelRequested atomic.Bool
	id              string
	owner           calllease.Owner
	input           startCallRequest
	recordingChoice *bool
	created         time.Time
	expires         time.Time
	session         *callmedia.Session
	transport       *mediaSocketTransport
	call            store.Call
	state           preparationState
}
type outgoingLeaseService interface {
	RequireOutgoing(string, string) error
	BeginOutgoing(string, string) error
	CancelOutgoing(string, string) (string, error)
	RenewOutgoing(string, string, time.Duration) (time.Time, error)
	PrepareOutgoingFor(context.Context, string, string, calllease.Owner) (calllease.OutgoingReservation, error)
}
type preparedSocketService interface {
	PrepareSocket(context.Context, string, string, callmedia.SocketTransport) (*callmedia.Session, error)
	BindPreparedSocket(context.Context, string, string, *callmedia.Session) error
	ActivatePreparedSocket(context.Context, string, string, *callmedia.Session) error
}

func (api *API) preparationLease() (outgoingLeaseService, bool) {
	s, ok := api.callLeases.(outgoingLeaseService)
	return s, ok
}

func (api *API) prepareOutgoingCall(w http.ResponseWriter, r *http.Request) {
	leases, ok := api.preparationLease()
	_, mediaOK := api.callMedia.(preparedSocketService)
	if !ok || !mediaOK || api.communications == nil {
		api.writeCallMediaError(w, r, mediaapp.ErrUnavailable)
		return
	}
	var input startCallRequest
	if !decodeJSONBody(w, r, &input) || !api.requireLineAccess(w, r, input.LineID) {
		return
	}
	if strings.TrimSpace(input.Number) == "" || strings.TrimSpace(input.LineID) == "" {
		writeError(w, 422, "invalid_argument", "line_id and number are required", "")
		return
	}
	owner, err := api.callLeaseOwner(r.Context())
	if err != nil {
		api.writeCallLeaseError(w, r, "prepare outgoing owner", err)
		return
	}
	requestID, ok := commandRequestID(w, r, input.RequestID)
	if !ok {
		return
	}
	api.preparationsMu.Lock()
	defer api.preparationsMu.Unlock()
	if api.preparations == nil {
		api.preparations = make(map[string]*outgoingPreparation)
	}
	if requestID != "" {
		for _, p := range api.preparations {
			if p.owner != owner || p.input.RequestID != requestID {
				continue
			}
			p.mu.Lock()
			defer p.mu.Unlock()
			if p.input.LineID != input.LineID || p.input.Number != input.Number || !sameRecordingChoice(p.recordingChoice, input.RecordingEnabled) {
				writeError(w, 409, "call_request_conflict", "Call request parameters changed", "")
				return
			}
			if p.state == preparationCancelled || p.state == preparationFailed || !time.Now().Before(p.expires) {
				writeError(w, 409, "outgoing_preparation_expired", "Prepare a new call request", "")
				return
			}
			writeJSON(w, 201, preparationResponse(p))
			return
		}
	}
	recordingChoice := input.RecordingEnabled
	recordingEnabled := false
	if recordingChoice != nil {
		if api.recordings == nil {
			writeError(w, http.StatusServiceUnavailable, "recording_unavailable", "Call recording is unavailable", "")
			return
		}
		recordingEnabled = *recordingChoice
	} else if api.recordings != nil {
		settings, err := api.recordings.Settings(r.Context())
		if err != nil {
			api.writeRecordingError(w, r, "load outgoing call recording default", err, nil)
			return
		}
		recordingEnabled = settings.DefaultEnabled
	}
	input.RecordingEnabled = &recordingEnabled
	if len(api.preparations) >= 1024 {
		writeError(w, 503, "media_unavailable", "Too many call preparations", "")
		return
	}
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		writeError(w, 503, "media_unavailable", "Call preparation is unavailable", "")
		return
	}
	id := "prepare_" + hex.EncodeToString(random[:])
	if requestID == "" {
		requestID = id
	}
	input.RequestID = requestID
	_, err = leases.PrepareOutgoingFor(r.Context(), id, input.LineID, owner)
	if err != nil {
		api.writeCallLeaseError(w, r, "reserve outgoing media", err)
		return
	}
	expires, err := leases.RenewOutgoing(id, owner.HolderID, outgoingPrepareDuration)
	if err != nil {
		api.callLeases.ReleaseOutgoing(id, owner.HolderID)
		api.writeCallLeaseError(w, r, "renew outgoing preparation", err)
		return
	}
	p := &outgoingPreparation{id: id, owner: owner, input: input, recordingChoice: recordingChoice, created: time.Now(), expires: expires}
	api.preparations[id] = p
	time.AfterFunc(10*time.Minute, func() { api.preparationsMu.Lock(); delete(api.preparations, id); api.preparationsMu.Unlock() })
	time.AfterFunc(outgoingPrepareDuration, func() { api.expirePreparation(p) })
	api.logger.Info("outgoing media prepared", "component", "calls", "preparation_id", id, "request_id", requestID, "line_id", input.LineID)
	api.publishLiveState()
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 201, preparationResponse(p))
}

func preparationResponse(p *outgoingPreparation) any {
	return map[string]any{"preparation_id": p.id, "media_socket_path": "/api/v1/calls/preparations/" + p.id + "/media/ws", "expires_at": p.expires, "recording_enabled": *p.input.RecordingEnabled}
}

func (api *API) expirePreparation(p *outgoingPreparation) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.state != preparationPrepared {
		return
	}
	if delay := time.Until(p.expires); delay > 0 {
		time.AfterFunc(delay, func() { api.expirePreparation(p) })
		return
	}
	p.state = preparationCancelled
	api.callLeases.ReleaseOutgoing(p.id, p.owner.HolderID)
	if p.session != nil {
		_ = p.session.Close(context.Background())
	}
	api.logger.Info("outgoing media preparation expired", "component", "calls", "preparation_id", p.id, "elapsed_ms", time.Since(p.created).Milliseconds())
	api.publishLiveState()
}

func (api *API) outgoingPreparationResource(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/v1/calls/preparations/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		writeError(w, 404, "not_found", "Preparation not found", "")
		return
	}
	owner, err := api.callLeaseOwner(r.Context())
	if err != nil {
		api.writeCallLeaseError(w, r, "validate preparation owner", err)
		return
	}
	api.preparationsMu.Lock()
	p := api.preparations[parts[0]]
	api.preparationsMu.Unlock()
	if p == nil || p.owner != owner {
		writeError(w, 404, "not_found", "Preparation not found", "")
		return
	}
	if !api.requireLineAccess(w, r, p.input.LineID) {
		return
	}
	action := strings.Join(parts[1:], "/")
	if action == "media/ws" {
		api.preparedMediaSocket(w, r, p)
		return
	}
	if action == "" && r.Method == http.MethodDelete {
		p.cancelRequested.Store(true)
		p.dispatchMu.Lock()
		if p.dispatchCancel != nil {
			p.dispatchCancel()
		}
		p.dispatchMu.Unlock()
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if action == "" && r.Method == http.MethodDelete {
		p.state = preparationCancelled
		if p.session != nil {
			_ = p.session.Close(context.Background())
		}
		callID := p.call.ID
		if leases, ok := api.preparationLease(); ok {
			if known, err := leases.CancelOutgoing(p.id, p.owner.HolderID); err == nil && known != "" {
				callID = known
			}
		}
		if callID != "" {
			if err := api.communications.EndCall(r.Context(), callID); err != nil {
				api.writeCommunicationError(w, r, "cancel committed preparation", err)
				return
			}
		}
		api.logger.Info("outgoing media preparation cancelled", "component", "calls", "preparation_id", p.id, "call_id", p.call.ID, "elapsed_ms", time.Since(p.created).Milliseconds())
		api.publishLiveState()
		w.WriteHeader(204)
		return
	}
	leases, ok := api.preparationLease()
	if !ok {
		api.writeCallMediaError(w, r, mediaapp.ErrUnavailable)
		return
	}
	if p.cancelRequested.Load() || p.state == preparationCancelled || p.state == preparationFailed || (p.state != preparationCommitted && !time.Now().Before(p.expires)) {
		api.logPreparationRejection(p, "preparation_expired")
		writeError(w, 409, "outgoing_preparation_expired", "Outgoing preparation is no longer usable", "")
		return
	}
	if action == "lease" && r.Method == http.MethodPut {
		expires, err := leases.RenewOutgoing(p.id, p.owner.HolderID, outgoingPrepareDuration)
		if err != nil {
			api.writeCallLeaseError(w, r, "renew preparation lease", err)
			return
		}
		p.expires = expires
		writeJSON(w, 200, map[string]any{"preparation_id": p.id, "expires_at": expires})
		return
	}
	if action != "commit" || r.Method != http.MethodPost {
		writeError(w, 405, "method_not_allowed", "Unsupported preparation operation", "")
		return
	}
	var input struct {
		AudioRunning bool `json:"audio_running"`
	}
	if !decodeJSONBody(w, r, &input) {
		return
	}
	if p.state == preparationCommitted {
		call := p.call
		if current, replayed, err := api.communications.ReplayStartCall(r.Context(), communication.StartCallInput{RequestID: p.input.RequestID, RequestScope: p.owner.HolderID, LineID: p.input.LineID, Number: p.input.Number}); err == nil && replayed && current.ID == call.ID {
			call = current
		}
		state := calllease.ControlOccupied
		if current, err := api.callLeases.ControlState(r.Context(), call.ID, p.owner.HolderID); err == nil {
			state = current
		}
		writeJSON(w, 201, callSessionEnvelope{Call: callSession(call, state)})
		return
	}
	if !input.AudioRunning || p.session == nil || !p.session.TransportReady() {
		api.logPreparationRejection(p, "media_not_ready")
		writeError(w, 409, "outgoing_media_not_ready", "Running local audio and valid socket audio are required", "")
		return
	}
	if err := api.revalidateSocketAuthentication(r); err != nil {
		api.writeCallLeaseError(w, r, "validate commit authentication", err)
		return
	}
	if err := leases.BeginOutgoing(p.id, p.owner.HolderID); err != nil {
		api.logPreparationRejection(p, "lease_unavailable")
		api.writeCallLeaseError(w, r, "consume prepared call lease", err)
		return
	}
	dispatchCtx, cancelDispatch := context.WithCancel(r.Context())
	p.dispatchMu.Lock()
	p.dispatchCancel = cancelDispatch
	if p.cancelRequested.Load() {
		cancelDispatch()
	}
	p.dispatchMu.Unlock()
	stopMediaCancellation := context.AfterFunc(p.session.Context(), cancelDispatch)
	defer func() { stopMediaCancellation(); cancelDispatch() }()
	if dispatchCtx.Err() != nil || !p.session.TransportReady() {
		api.callLeases.ReleaseOutgoing(p.id, p.owner.HolderID)
		p.state = preparationFailed
		api.logPreparationRejection(p, "cancelled_before_dispatch")
		writeError(w, 409, "outgoing_media_not_ready", "Outgoing media was cancelled before dial", "")
		return
	}
	api.logger.Info("outgoing media commit dispatch", "component", "calls", "preparation_id", p.id, "request_id", p.input.RequestID, "elapsed_ms", time.Since(p.created).Milliseconds())
	request := r.WithContext(dispatchCtx)
	api.commitPreparedCall(w, request, p)
	if p.state != preparationCommitted {
		p.state = preparationFailed
		if p.session != nil {
			_ = p.session.Close(context.Background())
		}
	}
}

func (api *API) bindOutgoingMedia(ctx context.Context, p *outgoingPreparation) error {
	service, ok := api.callMedia.(preparedSocketService)
	if !ok {
		return mediaapp.ErrUnavailable
	}
	if p.session == nil || !p.session.TransportReady() {
		return mediaapp.ErrUnavailable
	}
	if err := service.ActivatePreparedSocket(ctx, p.id, p.call.ID, p.session); err != nil {
		return err
	}
	go api.attachPreparedHardware(p, service)
	return nil
}

func sameRecordingChoice(a, b *bool) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

func (api *API) attachPreparedHardware(p *outgoingPreparation, service preparedSocketService) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	stop := context.AfterFunc(p.session.Context(), cancel)
	defer stop()
	api.logger.Info("outgoing media hardware await", "component", "calls", "preparation_id", p.id, "call_id", p.call.ID, "elapsed_ms", time.Since(p.created).Milliseconds())
	err := service.BindPreparedSocket(ctx, p.id, p.call.ID, p.session)
	if err == nil {
		p.mu.Lock()
		if p.state == preparationCommitted && !p.cancelRequested.Load() && p.transport != nil {
			err = p.transport.json(map[string]any{"type": "bound", "call_id": p.call.ID, "hardware_media": true})
		}
		p.mu.Unlock()
		if err == nil {
			api.logger.Info("outgoing media hardware bound", "component", "calls", "preparation_id", p.id, "call_id", p.call.ID, "elapsed_ms", time.Since(p.created).Milliseconds())
			return
		}
	}
	api.logger.Warn("outgoing media hardware binding failed", "component", "calls", "preparation_id", p.id, "call_id", p.call.ID, "elapsed_ms", time.Since(p.created).Milliseconds(), "failure_code", callmedia.AudioFailureCode(err))
	cleanup, cancelCleanup := context.WithTimeout(context.Background(), callControlRollbackTimeout)
	defer cancelCleanup()
	_ = p.session.Close(cleanup)
	if err := api.communications.EndCall(cleanup, p.call.ID); err != nil {
		api.logger.Warn("outgoing media binding failed cleanup", "component", "calls", "preparation_id", p.id, "call_id", p.call.ID, "error", err)
	}
	p.mu.Lock()
	if p.state != preparationCancelled {
		p.state = preparationFailed
	}
	p.mu.Unlock()
}

func (api *API) logPreparationRejection(p *outgoingPreparation, code string) {
	api.logger.Warn("outgoing media commit rejected", "component", "calls", "preparation_id", p.id, "request_id", p.input.RequestID, "failure_code", code, "elapsed_ms", time.Since(p.created).Milliseconds())
}
