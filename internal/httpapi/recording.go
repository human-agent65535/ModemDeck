package httpapi

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/human-agent65535/modemdeck/internal/recording"
	"github.com/human-agent65535/modemdeck/internal/store"
)

type recordingSettingsRequest struct {
	DefaultEnabled *bool `json:"default_enabled"`
	Revision       int64 `json:"revision"`
}

type recordingToggleRequest struct {
	Enabled *bool `json:"enabled"`
}

type recordingSettingsResponse struct {
	Settings store.RecordingSettings `json:"settings"`
}

type recordingStateResponse struct {
	State store.CallRecordingState `json:"state"`
}

type recordingListResponse struct {
	State    store.CallRecordingState `json:"state"`
	Segments []store.RecordingSegment `json:"segments"`
}

type recordingEntriesResponse struct {
	Recordings []store.RecordingEntry `json:"recordings"`
	Meta       responseMeta           `json:"meta"`
}

type recordingMutationErrorResponse struct {
	State store.CallRecordingState `json:"state"`
	Error errorResponse            `json:"error"`
}

type recordingResourceKind uint8

const (
	recordingResourceToggle recordingResourceKind = iota + 1
	recordingResourceList
	recordingResourceDownload
)

type recordingResourcePath struct {
	Kind      recordingResourceKind
	CallID    string
	SegmentID string
}

func (api *API) recordingSettings(response http.ResponseWriter, request *http.Request) {
	if api.recordings == nil {
		writeError(response, http.StatusServiceUnavailable, "recording_unavailable", "Call recording is unavailable", "")
		return
	}
	switch request.Method {
	case http.MethodGet:
		settings, err := api.recordings.Settings(request.Context())
		if err != nil {
			api.writeRecordingError(response, request, "load recording settings", err, nil)
			return
		}
		writeJSON(response, http.StatusOK, recordingSettingsResponse{Settings: settings})
	case http.MethodPut:
		var input recordingSettingsRequest
		if !decodeJSONBody(response, request, &input) {
			return
		}
		if input.DefaultEnabled == nil || input.Revision <= 0 {
			writeError(response, http.StatusBadRequest, "invalid_argument", "default_enabled and a positive revision are required", "")
			return
		}
		settings, err := api.recordings.UpdateSettings(
			request.Context(),
			*input.DefaultEnabled,
			input.Revision,
		)
		if err != nil {
			api.writeRecordingError(response, request, "update recording settings", err, nil)
			return
		}
		api.logger.Info("recording defaults updated", "enabled", settings.DefaultEnabled)
		writeJSON(response, http.StatusOK, recordingSettingsResponse{Settings: settings})
	default:
		response.Header().Set("Allow", http.MethodGet+", "+http.MethodPut)
		writeError(response, http.StatusMethodNotAllowed, "method_not_allowed", "Only GET and PUT are supported", "")
	}
}

func (api *API) recordingEntries(response http.ResponseWriter, request *http.Request) {
	limit, ok := requestLimit(response, request)
	if !ok {
		return
	}
	search, ok := requestSearch(response, request)
	if !ok {
		return
	}
	entries, err := api.repository.RecordingEntries(
		request.Context(),
		store.RecordingQuery{Search: search, Limit: limit},
	)
	if err != nil {
		api.writeInternalError(response, request, "list recordings", err)
		return
	}
	writeJSON(response, http.StatusOK, recordingEntriesResponse{
		Recordings: entries,
		Meta:       responseMeta{Limit: limit},
	})
}

func (api *API) recordingResource(
	response http.ResponseWriter,
	request *http.Request,
	resource recordingResourcePath,
) {
	if api.recordings == nil {
		writeError(response, http.StatusServiceUnavailable, "recording_unavailable", "Call recording is unavailable", "")
		return
	}
	switch resource.Kind {
	case recordingResourceToggle:
		api.toggleRecording(response, request, resource.CallID)
	case recordingResourceList:
		api.callRecordings(response, request, resource.CallID)
	case recordingResourceDownload:
		api.downloadRecording(response, request, resource.CallID, resource.SegmentID)
	default:
		writeError(response, http.StatusNotFound, "not_found", "API endpoint was not found", "")
	}
}

func (api *API) toggleRecording(response http.ResponseWriter, request *http.Request, callID string) {
	if request.Method != http.MethodPut {
		response.Header().Set("Allow", http.MethodPut)
		writeError(response, http.StatusMethodNotAllowed, "method_not_allowed", "Only PUT is supported", "")
		return
	}
	var input recordingToggleRequest
	if !decodeJSONBody(response, request, &input) {
		return
	}
	if input.Enabled == nil {
		writeError(response, http.StatusBadRequest, "invalid_argument", "enabled is required", "enabled")
		return
	}
	state, err := api.recordings.SetEnabled(request.Context(), callID, *input.Enabled)
	if err != nil {
		api.writeRecordingError(response, request, "toggle call recording", err, &state)
		return
	}
	api.logger.Info(
		"call recording updated",
		"call_id",
		callID,
		"enabled",
		state.Enabled,
		"status",
		state.Status,
	)
	writeJSON(response, http.StatusOK, recordingStateResponse{State: state})
}

func (api *API) callRecordings(response http.ResponseWriter, request *http.Request, callID string) {
	if request.Method != http.MethodGet {
		response.Header().Set("Allow", http.MethodGet)
		writeError(response, http.StatusMethodNotAllowed, "method_not_allowed", "Only GET is supported", "")
		return
	}
	result, err := api.recordings.CallRecordings(request.Context(), callID)
	if err != nil {
		api.writeRecordingError(response, request, "list call recordings", err, nil)
		return
	}
	writeJSON(response, http.StatusOK, recordingListResponse{
		State:    result.State,
		Segments: result.Segments,
	})
}

func (api *API) downloadRecording(
	response http.ResponseWriter,
	request *http.Request,
	callID, segmentID string,
) {
	if request.Method != http.MethodGet {
		response.Header().Set("Allow", http.MethodGet)
		writeError(response, http.StatusMethodNotAllowed, "method_not_allowed", "Only GET is supported", "")
		return
	}
	download, err := api.recordings.Download(request.Context(), callID, segmentID)
	if err != nil {
		api.writeRecordingError(response, request, "download call recording", err, nil)
		return
	}
	defer download.File.Close()
	modified := time.Time{}
	if download.Segment.EndedAt != nil {
		modified, _ = time.Parse(time.RFC3339Nano, *download.Segment.EndedAt)
	}
	response.Header().Set("Content-Type", "audio/ogg")
	response.Header().Set("Content-Disposition", `attachment; filename="`+download.Segment.ID+`.ogg"`)
	response.Header().Set("Cache-Control", "private, no-store")
	http.ServeContent(response, request, download.Segment.ID+".ogg", modified, download.File)
}

func parseRecordingResource(path string) (recordingResourcePath, bool) {
	const prefix = "/api/v1/calls/"
	if !strings.HasPrefix(path, prefix) {
		return recordingResourcePath{}, false
	}
	parts := strings.Split(strings.TrimPrefix(path, prefix), "/")
	switch {
	case len(parts) == 2 && validRecordingPathID(parts[0]) && parts[1] == "recording":
		return recordingResourcePath{Kind: recordingResourceToggle, CallID: parts[0]}, true
	case len(parts) == 2 && validRecordingPathID(parts[0]) && parts[1] == "recordings":
		return recordingResourcePath{Kind: recordingResourceList, CallID: parts[0]}, true
	case len(parts) == 4 &&
		validRecordingPathID(parts[0]) &&
		parts[1] == "recordings" &&
		validRecordingPathID(parts[2]) &&
		parts[3] == "download":
		return recordingResourcePath{
			Kind:      recordingResourceDownload,
			CallID:    parts[0],
			SegmentID: parts[2],
		}, true
	default:
		return recordingResourcePath{}, false
	}
}

func validRecordingPathID(value string) bool {
	if value == "" || len(value) > maxIdentifierLength {
		return false
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') ||
			(r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') ||
			r == '_' || r == '-' {
			continue
		}
		return false
	}
	return true
}

func (api *API) writeRecordingError(
	response http.ResponseWriter,
	request *http.Request,
	operation string,
	err error,
	state *store.CallRecordingState,
) {
	status := http.StatusInternalServerError
	code := "recording_failed"
	message := "Call recording request could not be completed"
	switch {
	case errors.Is(err, recording.ErrInvalidArgument):
		status = http.StatusBadRequest
		code = "invalid_argument"
		message = "Recording request is invalid"
	case errors.Is(err, recording.ErrNotFound):
		status = http.StatusNotFound
		code = "not_found"
		message = "Recording resource was not found"
	case errors.Is(err, recording.ErrRevisionConflict):
		status = http.StatusConflict
		code = "revision_conflict"
		message = "Recording settings changed since they were loaded"
	case errors.Is(err, recording.ErrRequestConflict):
		status = http.StatusConflict
		code = "request_conflict"
		message = "The recording override conflicts with this request id"
	case errors.Is(err, recording.ErrNotReady):
		status = http.StatusConflict
		code = "recording_not_ready"
		message = "The recording is not ready for download"
	case errors.Is(err, recording.ErrConflict):
		status = http.StatusConflict
		code = "recording_conflict"
		message = "Recording state conflicts with another request"
	case errors.Is(err, recording.ErrCallNotActive):
		status = http.StatusConflict
		code = "call_not_active"
		message = "Recording can only be changed during an active call"
	case errors.Is(err, recording.ErrMediaUnavailable):
		status = http.StatusServiceUnavailable
		code = "media_unavailable"
		message = "The modem media endpoint is unavailable"
	case errors.Is(err, recording.ErrBackpressure):
		status = http.StatusServiceUnavailable
		code = "recording_backpressure"
		message = "The recording pipeline could not keep up with live audio"
	case errors.Is(err, recording.ErrCodec):
		status = http.StatusServiceUnavailable
		code = "recording_codec_unavailable"
		message = "The recording codec is unavailable"
	default:
		api.logger.Error(operation, "method", request.Method, "path", request.URL.Path, "error", err)
	}
	if status < http.StatusInternalServerError {
		api.logger.Warn(
			operation,
			"method",
			request.Method,
			"path",
			request.URL.Path,
			"error_class",
			code,
			"error",
			err,
		)
	}
	payload := errorResponse{Code: code, Message: message}
	if state != nil && state.CallID != "" {
		writeJSON(response, status, recordingMutationErrorResponse{State: *state, Error: payload})
		return
	}
	writeJSON(response, status, payload)
}
