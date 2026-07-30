package mediaapp

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/human-agent65535/modemdeck/internal/callmedia"
	"github.com/human-agent65535/modemdeck/internal/communication"
	"github.com/human-agent65535/modemdeck/internal/rtcconfig"
	"github.com/human-agent65535/modemdeck/internal/store"
)

var (
	ErrInvalidArgument = errors.New("invalid media request")
	ErrNotFound        = errors.New("media call not found")
	ErrNotActive       = errors.New("media call is not active")
	ErrUnavailable     = errors.New("call media is unavailable")
	ErrConflict        = errors.New("call media is already attached")
	ErrNegotiation     = errors.New("WebRTC negotiation failed")
)

type Refresher interface {
	Refresh(context.Context) (communication.Status, error)
}

type CallStore interface {
	CallByID(context.Context, string) (store.Call, error)
}

type Core interface {
	Exchange(context.Context, callmedia.Offer) (callmedia.ExchangeResult, error)
	ReleaseOwner(context.Context, string, string) error
	CloseCall(context.Context, string) error
	ReconcileActiveCalls(context.Context, []string) error
	Close(context.Context) error
}

type Service struct {
	refresher Refresher
	calls     CallStore
	core      Core
	rtc       rtcconfig.Provider
}

type Options struct {
	RTCProvider rtcconfig.Provider
}

func New(
	refresher Refresher,
	calls CallStore,
	core Core,
	options ...Options,
) (*Service, error) {
	if refresher == nil || calls == nil || core == nil {
		return nil, errors.New("media service requires refresher, call store, and media core")
	}
	var configuration Options
	if len(options) > 1 {
		return nil, errors.New("media service accepts at most one options value")
	}
	if len(options) == 1 {
		configuration = options[0]
	}
	return &Service{
		refresher: refresher,
		calls:     calls,
		core:      core,
		rtc:       configuration.RTCProvider,
	}, nil
}

func (s *Service) Exchange(
	ctx context.Context,
	callID, ownerToken, offerSDP string,
	relayOnly bool,
) (string, error) {
	callID = strings.TrimSpace(callID)
	ownerToken = strings.TrimSpace(ownerToken)
	if callID == "" || ownerToken == "" || strings.TrimSpace(offerSDP) == "" {
		return "", fmt.Errorf(
			"%w: call id, owner token, and SDP offer are required",
			ErrInvalidArgument,
		)
	}
	if _, err := s.refresher.Refresh(ctx); err != nil {
		return "", fmt.Errorf("%w: authoritative call state could not be refreshed: %v", ErrUnavailable, err)
	}
	call, err := s.calls.CallByID(ctx, callID)
	if errors.Is(err, store.ErrCallNotFound) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("%w: call state could not be read: %v", ErrUnavailable, err)
	}
	if call.Phase != "active" {
		return "", ErrNotActive
	}
	if !call.MediaAvailable {
		return "", ErrUnavailable
	}
	var rtcConfiguration rtcconfig.Configuration
	if relayOnly {
		if s.rtc == nil {
			return "", fmt.Errorf(
				"%w: relay configuration is not configured",
				ErrUnavailable,
			)
		}
		rtcConfiguration, err = s.rtc.Generate(ctx)
		if err != nil {
			return "", fmt.Errorf(
				"%w: relay configuration could not be generated",
				ErrUnavailable,
			)
		}
	}
	result, err := s.core.Exchange(ctx, callmedia.Offer{
		Call: callmedia.ActiveCall{
			ID:    call.ID,
			State: callmedia.CallStateActive,
		},
		OwnerToken:       ownerToken,
		SDP:              offerSDP,
		RTCConfiguration: rtcConfiguration,
	})
	if err != nil {
		return "", classifyCoreError(err)
	}
	return result.AnswerSDP, nil
}

func (s *Service) ReleaseOwner(ctx context.Context, callID, ownerToken string) error {
	if strings.TrimSpace(callID) == "" || strings.TrimSpace(ownerToken) == "" {
		return fmt.Errorf("%w: call id and owner token are required", ErrInvalidArgument)
	}
	if err := s.core.ReleaseOwner(ctx, callID, ownerToken); err != nil {
		return classifyCoreError(err)
	}
	return nil
}

func (s *Service) CloseCall(ctx context.Context, callID string) error {
	if strings.TrimSpace(callID) == "" {
		return nil
	}
	return s.core.CloseCall(ctx, callID)
}

func (s *Service) ReconcileAuthoritativeCalls(
	ctx context.Context,
	activeCalls []store.Call,
) error {
	callIDs := make([]string, 0, len(activeCalls))
	for _, call := range activeCalls {
		callIDs = append(callIDs, call.ID)
	}
	return s.core.ReconcileActiveCalls(ctx, callIDs)
}

func (s *Service) Close(ctx context.Context) error {
	return s.core.Close(ctx)
}

func classifyCoreError(err error) error {
	switch {
	case errors.Is(err, callmedia.ErrInvalidArgument),
		errors.Is(err, callmedia.ErrInvalidOffer),
		errors.Is(err, callmedia.ErrUnsupportedMedia),
		errors.Is(err, callmedia.ErrUnsupportedCodec),
		errors.Is(err, callmedia.ErrUnsupportedDirection):
		return fmt.Errorf("%w: %v", ErrInvalidArgument, err)
	case errors.Is(err, callmedia.ErrCallNotActive):
		return ErrNotActive
	case errors.Is(err, callmedia.ErrCallInUse):
		return ErrConflict
	case errors.Is(err, callmedia.ErrNotMediaOwner):
		return ErrConflict
	case errors.Is(err, callmedia.ErrNegotiation),
		errors.Is(err, callmedia.ErrGatheringTimeout):
		return fmt.Errorf("%w: %v", ErrNegotiation, err)
	case errors.Is(err, callmedia.ErrEndpointUnavailable),
		errors.Is(err, callmedia.ErrUnsupported),
		errors.Is(err, callmedia.ErrCodec):
		return fmt.Errorf("%w: %v", ErrUnavailable, err)
	default:
		return fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
}
