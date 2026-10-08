package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/human-agent65535/modemdeck/internal/auth"
	"github.com/human-agent65535/modemdeck/internal/mobilepairing"
)

type mobileDiagnosticEvent struct {
	ID       string            `json:"id"`
	TimeMS   int64             `json:"time_ms"`
	Category string            `json:"category"`
	Name     string            `json:"name"`
	Network  string            `json:"network"`
	CallID   string            `json:"call_id,omitempty"`
	Fields   map[string]string `json:"fields"`
}

type mobileDiagnosticBatch struct {
	Schema     int                     `json:"schema"`
	AppVersion string                  `json:"app_version"`
	AppBuild   string                  `json:"app_build"`
	OSVersion  string                  `json:"os_version"`
	Dropped    int                     `json:"dropped"`
	Events     []mobileDiagnosticEvent `json:"events"`
}

var (
	diagnosticUUID    = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	diagnosticCallID  = regexp.MustCompile(`^(call_[A-Za-z0-9_-]{24}|call_[0-9a-f]{32}|(test-|call-)?[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})$`)
	diagnosticName    = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)
	diagnosticVersion = regexp.MustCompile(`^[0-9]+(\.[0-9]+)*$`)
)

// Paired devices append bounded, structured diagnostics to the existing operator
// log sink. Ownership comes exclusively from authentication, never the payload.
func (api *API) mobileDiagnostics(response http.ResponseWriter, request *http.Request) {
	mobile, isMobile := mobileAuthenticationFromContext(request.Context())
	principal, authenticated := auth.PrincipalFromContext(request.Context())
	if !isMobile || !authenticated {
		writeError(response, http.StatusForbidden, "mobile_api_required", "Use the paired iOS app to send diagnostics", "")
		return
	}
	var batch mobileDiagnosticBatch
	if !decodeJSONBodyWithLimit(response, request, &batch, 32<<10) {
		return
	}
	if !validMobileDiagnosticBatch(batch) {
		writeError(response, http.StatusBadRequest, "invalid_diagnostics", "Unsupported diagnostic fields or values", "")
		return
	}
	repository, ok := api.repository.(interface {
		IOSPairingCredentialIDByTokenDigest(context.Context, mobilepairing.TokenDigest) (string, bool, error)
	})
	if !ok {
		writeError(response, http.StatusServiceUnavailable, "diagnostics_unavailable", "Mobile diagnostics are unavailable", "")
		return
	}
	deviceID, found, err := repository.IOSPairingCredentialIDByTokenDigest(request.Context(), mobile.Digest)
	if err != nil {
		api.writeInternalError(response, request, "read diagnostic device", err)
		return
	}
	if !found {
		writeError(response, http.StatusUnauthorized, "authentication_required", "Device pairing is no longer active", "")
		return
	}
	for index, event := range batch.Events {
		fields := []any{"source", "ios", "component", "ios." + event.Category,
			"user_id", principal.UserID, "device_id", deviceID,
			"app_version", batch.AppVersion, "app_build", batch.AppBuild, "os_version", batch.OSVersion,
			"event_id", event.ID, "client_time_ms", event.TimeMS, "network", event.Network}
		if index == 0 && batch.Dropped > 0 {
			fields = append(fields, "dropped_events", batch.Dropped)
		}
		if event.CallID != "" {
			fields = append(fields, "call_id", event.CallID)
		}
		for key, value := range event.Fields {
			fields = append(fields, key, value)
		}
		api.logger.Log(request.Context(), slog.LevelInfo, "iOS "+event.Name, fields...)
	}
	response.Header().Set("Cache-Control", "no-store")
	response.WriteHeader(http.StatusNoContent)
}

func validMobileDiagnosticBatch(batch mobileDiagnosticBatch) bool {
	if batch.Schema != 1 || len(batch.Events) == 0 || len(batch.Events) > 24 || batch.Dropped < 0 || batch.Dropped > 1_000_000_000 {
		return false
	}
	for _, version := range []string{batch.AppVersion, batch.AppBuild, batch.OSVersion} {
		if len(version) > 32 || !diagnosticVersion.MatchString(version) {
			return false
		}
	}
	for _, event := range batch.Events {
		if !diagnosticUUID.MatchString(event.ID) || !diagnosticName.MatchString(event.Name) || event.TimeMS <= 0 || event.TimeMS > 100_000_000_000_000 ||
			!diagnosticChoice(event.Category, "app network api pairing push callkit audio storage permissions") ||
			!diagnosticChoice(event.Network, "unknown offline cellular wifi wired other") ||
			(event.CallID != "" && !diagnosticCallID.MatchString(event.CallID)) || len(event.Fields) > 32 {
			return false
		}
		for key, value := range event.Fields {
			if !validMobileDiagnosticField(key, value) {
				return false
			}
		}
	}
	return true
}

func diagnosticChoice(value, choices string) bool {
	for _, choice := range strings.Fields(choices) {
		if value == choice {
			return true
		}
	}
	return false
}

func validMobileDiagnosticField(key, value string) bool {
	if len(value) > 256 {
		return false
	}
	switch key {
	case "elapsed_ms", "stage_elapsed_ms", "dns_ms", "connect_ms", "tls_ms", "error_code", "http_status", "attempt", "delay_ms", "sample_rate", "channels", "input_gain_percent", "output_volume_percent", "microphone_dbfs", "captured_frames", "dropped_frames", "server_received_packets", "sent_packets", "sent_bytes", "received_packets", "received_bytes", "lost_packets", "playback_pending", "playback_underruns", "playback_resets",
		"capture_dropped_frames", "send_dropped_frames", "receive_stale_frames", "receive_invalid_frames", "playback_dropped_frames",
		"server_dropped_packets", "server_dropped_source_early_packets", "server_dropped_source_late_packets",
		"server_dropped_queue_overflow_packets", "server_dropped_reanchor_packets", "server_dropped_playout_packets", "server_dropped_rebuffer_packets",
		"server_clock_reanchors", "server_playout_underruns", "server_playout_silence_frames", "server_playout_missed_ticks":
		number, err := strconv.ParseInt(value, 10, 64)
		return err == nil && number >= -1_000_000_000 && number <= 1_000_000_000
	case "expensive", "constrained", "ipv4", "ipv6", "dns", "enabled", "test_call", "input_available", "input_gain_settable", "audio_enabled", "reused_connection":
		return value == "true" || value == "false"
	case "input_route", "output_route":
		return diagnosticChoice(value, "none microphone receiver speaker bluetooth headphones external")
	case "http_protocol":
		return diagnosticChoice(value, "h2 h3 http/1.1 other")
	case "stage":
		return diagnosticChoice(value, "idle waiting_for_active_call connecting_wss reconnecting_wss connected")
	case "method":
		return diagnosticChoice(value, "GET POST PUT PATCH DELETE HEAD")
	case "error_domain":
		return diagnosticChoice(value, "url cocoa osstatus callkit call_audio api other")
	case "reason":
		return diagnosticChoice(value, "dns tls timeout authentication address_family unreachable other")
	case "status":
		return diagnosticChoice(value, "online offline checking paired unpaired launching loading unavailable authorized denied notDetermined not_determined provisional ephemeral restricted unknown granted undetermined")
	case "route":
		if !strings.HasPrefix(value, "/api/v1/") {
			return false
		}
		parts := strings.Split(strings.TrimPrefix(value, "/"), "/")
		if len(parts) > 12 {
			return false
		}
		for _, part := range parts {
			if !diagnosticChoice(part, ":id api v1 mobile session bootstrap pairing push call-tests test-call calls active media ws lease answer hangup reject hold resume mute dtmf recording recordings audio segments contacts batch messages threads state read read-all unread-summary events runtime settings system lines devices users account sessions password profile telegram units diagnostics logs health external-access status favorite contact") {
				return false
			}
		}
		return true
	default:
		return false
	}
}
