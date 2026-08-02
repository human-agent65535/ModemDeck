package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/human-agent65535/modemdeck/internal/auth"
	"github.com/human-agent65535/modemdeck/internal/networkruntime"
	"github.com/human-agent65535/modemdeck/internal/runtimeevents"
	"github.com/human-agent65535/modemdeck/internal/store"
)

const runtimeHeartbeatInterval = 5 * time.Second
const runtimeStateBuildTimeout = 5 * time.Second

type runtimeCommunicationState struct {
	Capabilities Capabilities          `json:"capabilities"`
	Lines        []lineSummaryResponse `json:"lines"`
	LineCatalog  []lineSummaryResponse `json:"line_catalog"`
	Devices      *[]store.Device       `json:"devices,omitempty"`
}

type runtimeNetworkState struct {
	Status  networkruntime.Status  `json:"status"`
	Proxies []networkruntime.Proxy `json:"proxies"`
}

type runtimeStateResponse struct {
	Epoch         string                     `json:"epoch"`
	Revision      uint64                     `json:"revision"`
	DataRevision  uint64                     `json:"data_revision"`
	ObservedAt    time.Time                  `json:"observed_at"`
	Communication *runtimeCommunicationState `json:"communication,omitempty"`
	Network       *runtimeNetworkState       `json:"network,omitempty"`
	Calls         *activeCallsResponse       `json:"calls,omitempty"`
	Recordings    *[]recordingListResponse   `json:"recordings,omitempty"`
}

type runtimeGlobalCommunicationState struct {
	Projection communicationProjection
	Devices    *[]store.Device
}

type runtimeGlobalState struct {
	Signal        runtimeevents.Signal
	Communication *runtimeGlobalCommunicationState
	Network       *runtimeNetworkState
	Calls         []store.Call
	CallsReady    bool
	Recordings    map[string]recordingListResponse
}

type runtimeStateCache struct {
	mu          sync.Mutex
	initialized bool
	epoch       string
	revision    uint64
	sections    runtimeevents.Section
	state       runtimeGlobalState
}

func (api *API) runtimeEventStream(response http.ResponseWriter, request *http.Request) {
	if api.runtimeEvents == nil {
		writeError(response, http.StatusServiceUnavailable, "runtime_events_unavailable", "Runtime events are unavailable", "")
		return
	}
	flusher, ok := response.(http.Flusher)
	if !ok {
		writeError(response, http.StatusInternalServerError, "stream_unavailable", "Streaming is unavailable", "")
		return
	}
	principal, scoped, authorized := api.authorizeEventStream(response, request, false)
	if !authorized {
		return
	}
	release, ok := api.acquireEventStream(response, request)
	if !ok {
		return
	}
	defer release()

	current, updates, cancel := api.runtimeEvents.Subscribe()
	defer cancel()

	response.Header().Set("Content-Type", "text/event-stream")
	response.Header().Set("Cache-Control", "no-store")
	response.Header().Set("Connection", "keep-alive")
	response.Header().Set("X-Accel-Buffering", "no")
	response.WriteHeader(http.StatusOK)
	controller := http.NewResponseController(response)
	_ = controller.SetWriteDeadline(time.Time{})
	if _, err := fmt.Fprint(response, "retry: 2000\n\n"); err != nil {
		return
	}
	if !api.writeRuntimeState(
		response,
		flusher,
		request,
		principal,
		scoped,
		current,
		runtimeevents.AllSections,
	) {
		return
	}
	lastWrite := time.Now()

	heartbeat := time.NewTicker(runtimeHeartbeatInterval)
	defer heartbeat.Stop()
	authentication := time.NewTicker(api.streamAuthInterval)
	defer authentication.Stop()
	for {
		select {
		case <-request.Context().Done():
			return
		case signal, open := <-updates:
			if !open {
				return
			}
			current = signal
			if !api.writeRuntimeState(
				response,
				flusher,
				request,
				principal,
				scoped,
				signal,
				signal.Sections,
			) {
				return
			}
			lastWrite = time.Now()
		case <-authentication.C:
			refreshedPrincipal, refreshedScoped, err := api.currentStreamAccess(request, false)
			if err != nil {
				return
			}
			if sameStreamAccess(principal, scoped, refreshedPrincipal, refreshedScoped) {
				continue
			}
			principal = refreshedPrincipal
			scoped = refreshedScoped
			if !api.writeRuntimeState(
				response,
				flusher,
				request,
				principal,
				scoped,
				current,
				runtimeevents.AllSections,
			) {
				return
			}
			lastWrite = time.Now()
		case observedAt := <-heartbeat.C:
			if time.Since(lastWrite) < runtimeHeartbeatInterval {
				continue
			}
			if !writeEventHeartbeat(response, flusher, observedAt) {
				return
			}
			lastWrite = time.Now()
		}
	}
}

func (api *API) writeRuntimeState(
	response http.ResponseWriter,
	flusher http.Flusher,
	request *http.Request,
	principal auth.Principal,
	scoped bool,
	signal runtimeevents.Signal,
	sections runtimeevents.Section,
) bool {
	if scoped {
		request = request.WithContext(auth.ContextWithPrincipal(request.Context(), principal))
	}
	current := api.currentRuntimeState(signal, sections)
	state := runtimeStateResponse{
		Epoch:        current.Signal.Epoch,
		Revision:     current.Signal.Revision,
		DataRevision: current.Signal.DataRevision,
		ObservedAt:   current.Signal.ObservedAt,
	}
	if state.ObservedAt.IsZero() {
		state.ObservedAt = time.Now().UTC()
	}

	if sections&runtimeevents.SectionCommunication != 0 && current.Communication != nil {
		projection := api.projectCommunicationProjection(
			current.Communication.Projection,
			request,
		)
		communication := &runtimeCommunicationState{
			Capabilities: projection.Capabilities,
			Lines:        lineSummaryResponses(projection.Lines),
			LineCatalog:  lineSummaryResponses(projection.LineCatalog),
		}
		if current.Communication.Devices != nil {
			devices := append([]store.Device(nil), (*current.Communication.Devices)...)
			devices = filterDevicesForPrincipal(request.Context(), devices)
			if devices == nil {
				devices = []store.Device{}
			}
			communication.Devices = &devices
		}
		state.Communication = communication
	}

	if sections&runtimeevents.SectionNetwork != 0 && current.Network != nil {
		status := current.Network.Status
		proxies := current.Network.Proxies
		if _, scoped := auth.PrincipalFromContext(request.Context()); scoped {
			status = filterNetworkStatusForPrincipal(request, status, proxies)
		}
		proxies = filterProxiesForPrincipal(request, proxies)
		state.Network = &runtimeNetworkState{Status: status, Proxies: proxies}
	}

	if sections&runtimeevents.SectionCalls != 0 && current.CallsReady {
		calls, err := api.projectActiveCallSnapshot(request, current.Calls)
		if err != nil {
			api.logRuntimeStateError(request, "calls", err)
		} else {
			state.Calls = &calls
			recordings := make([]recordingListResponse, 0, len(calls.Calls))
			for _, call := range calls.Calls {
				if recording, ok := current.Recordings[call.ID]; ok {
					recordings = append(recordings, recording)
				}
			}
			state.Recordings = &recordings
		}
	}
	return writeSSE(response, flusher, "state", state)
}

// currentRuntimeState lazily builds each requested section once for the newest
// signal. Browser connections only apply their authorization projection to the
// shared value, so adding viewers does not multiply hardware or SQLite reads.
func (api *API) currentRuntimeState(
	signal runtimeevents.Signal,
	sections runtimeevents.Section,
) runtimeGlobalState {
	cache := &api.runtimeState
	cache.mu.Lock()
	defer cache.mu.Unlock()
	sections &= runtimeevents.AllSections
	if !cache.initialized ||
		cache.epoch != signal.Epoch ||
		signal.Revision > cache.revision {
		if signal.ObservedAt.IsZero() {
			signal.ObservedAt = time.Now().UTC()
		}
		cache.initialized = true
		cache.epoch = signal.Epoch
		cache.revision = signal.Revision
		cache.sections = 0
		cache.state = runtimeGlobalState{Signal: signal}
	} else if signal.Revision == cache.revision {
		cache.state.Signal = signal
		if cache.state.Signal.ObservedAt.IsZero() {
			cache.state.Signal.ObservedAt = time.Now().UTC()
		}
	}

	missing := sections &^ cache.sections
	if missing == 0 {
		return cache.state
	}

	ctx, cancel := context.WithTimeout(context.Background(), runtimeStateBuildTimeout)
	defer cancel()
	state := cache.state

	if missing&runtimeevents.SectionCommunication != 0 {
		projection, err := api.loadCommunicationProjection(ctx, false)
		if err != nil {
			api.logRuntimeStateBuildError("communication", err)
		} else {
			communication := &runtimeGlobalCommunicationState{Projection: projection}
			devices, deviceErr := api.repository.Devices(ctx)
			if deviceErr != nil {
				api.logRuntimeStateBuildError("devices", deviceErr)
			} else {
				if projection.Connected {
					devices = mergeLiveDeviceNetwork(devices, projection.DeviceLines)
				}
				if devices == nil {
					devices = []store.Device{}
				}
				communication.Devices = &devices
			}
			state.Communication = communication
		}
	}

	if missing&runtimeevents.SectionNetwork != 0 && api.network != nil {
		status, statusErr := api.network.Status(ctx)
		if statusErr != nil {
			api.logRuntimeStateBuildError("network", statusErr)
		} else if proxies, proxyErr := api.network.Proxies(ctx); proxyErr != nil {
			api.logRuntimeStateBuildError("proxies", proxyErr)
		} else {
			state.Network = &runtimeNetworkState{Status: status, Proxies: proxies}
		}
	}

	if missing&runtimeevents.SectionCalls != 0 &&
		api.communications != nil &&
		api.callLeases != nil {
		calls, callsErr := api.communications.ActiveCalls(ctx)
		if callsErr != nil {
			api.logRuntimeStateBuildError("calls", callsErr)
		} else {
			state.Calls = append([]store.Call(nil), calls...)
			state.CallsReady = true
			state.Recordings = make(map[string]recordingListResponse)
			if api.recordings != nil {
				for _, call := range calls {
					recordings, recordingErr := api.recordings.CallRecordings(ctx, call.ID)
					if recordingErr != nil {
						api.logRuntimeStateBuildError("recording", recordingErr)
						continue
					}
					state.Recordings[call.ID] = recordingListResponse{
						State:    recordings.State,
						Segments: recordings.Segments,
					}
				}
			}
		}
	}

	cache.sections |= missing
	cache.state = state
	return state
}

func (api *API) logRuntimeStateBuildError(section string, err error) {
	api.logger.Warn(
		"live runtime state section is unavailable",
		"section", section,
		"error", err,
	)
}

func (api *API) logRuntimeStateError(request *http.Request, section string, err error) {
	if requestWasCanceled(request, err) {
		return
	}
	api.logger.Warn("live runtime state section is unavailable", "section", section, "error", err)
}

func (api *API) publishLiveState() {
	publisher, ok := api.runtimeEvents.(runtimeevents.Publisher)
	if !ok {
		return
	}
	publisher.Publish(runtimeevents.Change{Sections: runtimeevents.SectionCalls})
}

func (api *API) publishDurableChange() {
	publisher, ok := api.runtimeEvents.(runtimeevents.Publisher)
	if !ok {
		return
	}
	publisher.Publish(runtimeevents.Change{Durable: true})
}
