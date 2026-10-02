package mediaapp

import (
	"context"

	"github.com/human-agent65535/modemdeck/internal/calllease"
	"github.com/human-agent65535/modemdeck/internal/calllifecycle"
	"github.com/human-agent65535/modemdeck/internal/callmedia"
	"github.com/human-agent65535/modemdeck/internal/rtcconfig"
)

// Runtime wires the same media, ownership, and lifecycle services for hardware
// calls and temporary audio tests. Only the authoritative state and PCM source
// differ; neither caller may bypass media registration or ownership checks.
type Runtime struct {
	Core   *callmedia.Core
	Media  *Service
	Leases *calllease.Manager
}

type RuntimeOptions struct {
	Calls          calllease.CallStore
	Refresher      Refresher
	Controller     calllease.CallController
	EndpointOpener callmedia.MediaEndpointOpener
	RTCProvider    rtcconfig.Provider
	LeaseOptions   calllease.Options
}

func NewRuntime(options RuntimeOptions) (*Runtime, error) {
	leases, err := calllease.New(options.Calls, options.Controller, options.LeaseOptions)
	if err != nil {
		return nil, err
	}
	core, err := callmedia.New(callmedia.Options{
		EndpointOpener: options.EndpointOpener,
		OnOwnerStateChange: func(callID string, connected bool) {
			if connected {
				leases.MediaConnected(callID)
			} else {
				leases.MediaDisconnected(callID)
			}
		},
	})
	if err != nil {
		return nil, err
	}
	media, err := New(options.Refresher, options.Calls, core, Options{RTCProvider: options.RTCProvider})
	if err != nil {
		_ = core.Close(context.Background())
		return nil, err
	}
	return &Runtime{Core: core, Media: media, Leases: leases}, nil
}

// Lifecycle always registers media before optional consumers (e.g. recording)
// and ownership reconciliation. Call sources publish their snapshots here.
func (r *Runtime) Lifecycle(consumers ...calllifecycle.Reconciler) (*calllifecycle.Coordinator, error) {
	reconcilers := []calllifecycle.Reconciler{r.Media}
	reconcilers = append(reconcilers, consumers...)
	reconcilers = append(reconcilers, r.Leases)
	return calllifecycle.New(reconcilers...)
}
