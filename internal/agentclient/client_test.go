package agentclient

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"path/filepath"
	"testing"
	"time"
)

func TestHealthOverUnixSocket(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "agent.sock")
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	server := &http.Server{Handler: http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/health" {
			http.NotFound(response, request)
			return
		}
		response.Header().Set("Content-Type", "application/json")
		_, _ = response.Write([]byte(`{
			"status":"ok",
			"api_version":"v1",
			"agent_version":"test",
			"provider":{
				"name":"org.freedesktop.ModemManager1",
				"available":true,
				"capabilities":{"discovery":true,"telemetry":true,"network":true,"proxy":true,"dial":false,"answer_call":false,"hangup_call":false,"send_message":false}
			}
		}`))
	})}
	serveResult := make(chan error, 1)
	go func() {
		serveResult <- server.Serve(listener)
	}()
	t.Cleanup(func() {
		_ = server.Close()
		_ = listener.Close()
		<-serveResult
	})

	client, err := New(socketPath, time.Second)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	health, err := client.Health(context.Background())
	if err != nil {
		t.Fatalf("Health() error = %v", err)
	}
	if health.APIVersion != APIVersion || !health.Provider.Available || !health.Provider.Capabilities.Discovery {
		t.Fatalf("unexpected health: %+v", health)
	}
	if !health.Provider.Capabilities.Telemetry ||
		!health.Provider.Capabilities.Network ||
		!health.Provider.Capabilities.Proxy {
		t.Fatalf("network capabilities were not decoded: %+v", health.Provider.Capabilities)
	}
	if health.Provider.Capabilities.Dial || health.Provider.Capabilities.SendMessage {
		t.Fatalf("unimplemented mutations were advertised: %+v", health.Provider.Capabilities)
	}
}

func TestHealthRejectsIncompatibleVersion(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "agent.sock")
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	server := &http.Server{Handler: http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		_, _ = response.Write([]byte(`{"api_version":"v2"}`))
	})}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() {
		_ = server.Close()
		_ = listener.Close()
	})

	client, err := New(socketPath, time.Second)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if _, err := client.Health(context.Background()); err == nil {
		t.Fatal("Health() accepted an incompatible API version")
	}
}

func TestNewRejectsRelativeSocketPath(t *testing.T) {
	if _, err := New("agent.sock", time.Second); !errors.Is(err, ErrInvalidSocketPath) {
		t.Fatalf("New() error = %v, want ErrInvalidSocketPath", err)
	}
}

func TestDeleteMessageOverUnixSocket(t *testing.T) {
	t.Parallel()
	requests := make(chan *http.Request, 1)
	client := newUnixTestClient(t, http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		requests <- request
		if request.Method != http.MethodDelete ||
			request.URL.EscapedPath() != "/v1/messages/message_boot_x" {
			http.NotFound(response, request)
			return
		}
		response.WriteHeader(http.StatusNoContent)
	}))

	if err := client.DeleteMessage(context.Background(), "message_boot_x"); err != nil {
		t.Fatalf("DeleteMessage() error = %v", err)
	}
	request := <-requests
	if request.ContentLength > 0 {
		t.Fatalf("DeleteMessage() content length = %d, want empty", request.ContentLength)
	}
}

func TestSnapshotDecodesAgentContract(t *testing.T) {
	client := newUnixTestClient(t, http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet || request.URL.Path != "/v1/snapshot" {
			http.NotFound(response, request)
			return
		}
		response.Header().Set("Content-Type", "application/json")
		_, _ = response.Write([]byte(`{
			"revision":"boot-1:42",
			"observed_at":"2026-07-23T00:00:00Z",
			"lines":[{
				"id":"line-1",
				"manufacturer":"Quectel",
				"model":"EG25-G",
				"revision":"EG25GGBR07A08M2G",
				"device_identifier":"device-1",
				"equipment_identifier":"860000000000001",
				"identity_persistent":true,
				"identity_source":"physical_device+equipment_identifier+device_identifier",
				"saved_policy_supported":true,
				"device":"/dev/cdc-wdm0",
				"physical_device":"/sys/devices/pci0000:00/usb1/1-1",
				"drivers":["qmi_wwan","option"],
				"plugin":"quectel",
				"primary_port":"cdc-wdm0",
				"audio_port":"quectel-uac:/sys/devices/pci0000:00/usb1/1-1",
				"state":"registered",
				"state_code":8,
				"power_state_code":3,
				"radio_desired_enabled":true,
				"radio_desired_enabled_known":true,
				"access_technologies":16384,
				"serving_radio":{
					"access_technology":"lte",
					"duplex_mode":"fdd",
					"band":"B1",
					"channel":100,
					"channel_type":"earfcn",
					"source":"quectel-qnwinfo"
				},
				"signal_quality_known":true,
				"signal_quality":73,
				"signal_quality_recent":true,
				"own_numbers":["+818012345678"],
				"sim_present":true,
				"sim_path":"/org/freedesktop/ModemManager1/SIM/0",
				"sim_identifier":"8986000000000000000",
				"imsi":"440510000000001",
				"home_operator_code":"44051",
				"home_operator_name":"KDDI",
				"serving_operator_code":"44010",
				"serving_operator_name":"NTT DOCOMO",
				"registration_state_known":true,
				"registration_state_code":5,
				"registration_state":"roaming",
				"roaming":true,
				"operator_identifier":"44051",
				"operator_name":"KDDI",
				"emergency_numbers":["110","119"],
				"emergency_only":false,
				"call_ids":["call-1"],
				"message_ids":["message-1"],
				"supported_message_storages":[1,2],
				"default_message_storage":2,
				"voice_verification":{
					"usb_configuration":"enabled",
					"media_routing":"enabled"
				},
				"capabilities":{
					"modem_interface":true,
					"sim_interface":true,
					"voice_interface":true,
					"messaging_interface":true,
					"dial":true,
					"answer_call":true,
					"reject_call":true,
					"hangup_call":true,
					"send_dtmf":true,
					"send_message":true,
					"media":true
				}
			}],
			"calls":[{
				"id":"call-1",
				"line_id":"line-1",
				"number":"+818012345678",
				"direction":"outgoing",
				"state":"active",
				"state_code":4,
				"state_reason":"accepted",
				"state_reason_code":3,
				"multiparty":false,
				"audio_port":"/dev/ttyUSB4",
				"audio_format":{"encoding":"pcm","resolution":"s16le","rate":8000},
				"media_available":true,
				"media_configured":true,
				"media_active":false,
				"bearer":""
			}],
			"messages":[{
				"id":"message-1",
				"line_id":"line-1",
				"number":"+818012345678",
				"text":"fixture",
				"direction":"incoming",
				"state":"received",
				"state_code":3,
				"timestamp":"2026-07-23T00:00:00Z"
			}]
		}`))
	}))

	snapshot, err := client.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}
	if snapshot.Revision != "boot-1:42" || len(snapshot.Lines) != 1 || len(snapshot.Calls) != 1 || len(snapshot.Messages) != 1 {
		t.Fatalf("unexpected snapshot: %+v", snapshot)
	}
	if !snapshot.Lines[0].IdentityPersistent ||
		!snapshot.Lines[0].SavedPolicySupported ||
		!snapshot.Lines[0].RadioDesiredEnabled ||
		!snapshot.Lines[0].RadioDesiredEnabledKnown ||
		snapshot.Lines[0].IdentitySource != "physical_device+equipment_identifier+device_identifier" ||
		snapshot.Lines[0].UnsupportedPolicyReason != "" {
		t.Fatalf("unexpected line identity: %+v", snapshot.Lines[0])
	}
	if snapshot.Lines[0].HomeOperatorName != "KDDI" ||
		snapshot.Lines[0].ServingOperatorName != "NTT DOCOMO" ||
		snapshot.Lines[0].RegistrationState != "roaming" ||
		!snapshot.Lines[0].Roaming {
		t.Fatalf("unexpected line operator state: %+v", snapshot.Lines[0])
	}
	if !snapshot.Lines[0].Capabilities.Media {
		t.Fatalf("unexpected line media capability: %+v", snapshot.Lines[0].Capabilities)
	}
	if snapshot.Lines[0].AudioPort != "quectel-uac:/sys/devices/pci0000:00/usb1/1-1" {
		t.Fatalf("unexpected line audio port: %q", snapshot.Lines[0].AudioPort)
	}
	if snapshot.Lines[0].ServingRadio == nil ||
		snapshot.Lines[0].ServingRadio.AccessTechnology != "lte" ||
		snapshot.Lines[0].ServingRadio.DuplexMode != "fdd" ||
		snapshot.Lines[0].ServingRadio.Band != "B1" ||
		snapshot.Lines[0].ServingRadio.Channel == nil ||
		*snapshot.Lines[0].ServingRadio.Channel != 100 ||
		snapshot.Lines[0].ServingRadio.ChannelType != "earfcn" {
		t.Fatalf("unexpected serving radio: %+v", snapshot.Lines[0].ServingRadio)
	}
	if snapshot.Lines[0].VoiceVerification == nil ||
		snapshot.Lines[0].VoiceVerification.USBConfiguration != "enabled" ||
		snapshot.Lines[0].VoiceVerification.MediaRouting != "enabled" {
		t.Fatalf("unexpected line voice verification: %+v", snapshot.Lines[0].VoiceVerification)
	}
	if snapshot.Calls[0].AudioFormat == nil ||
		snapshot.Calls[0].AudioFormat.Rate != 8000 ||
		!snapshot.Calls[0].MediaAvailable ||
		!snapshot.Calls[0].MediaConfigured ||
		snapshot.Calls[0].MediaActive {
		t.Fatalf("unexpected call media: %+v", snapshot.Calls[0])
	}
}

func TestTelemetryDecodesAgentContract(t *testing.T) {
	client := newUnixTestClient(t, http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet || request.URL.Path != "/v1/telemetry" {
			http.NotFound(response, request)
			return
		}
		response.Header().Set("Content-Type", "application/json")
		_, _ = response.Write([]byte(`{
			"boot_epoch":"boot-1",
			"observed_at":"2026-08-02T00:00:00Z",
			"lines":[{
				"id":"line-1",
				"access_technologies":16384,
				"access_technologies_known":true,
				"signal_quality_known":true,
				"signal_quality":73,
				"signal_quality_recent":true,
				"signal_metrics_recent":true,
				"signal_dbm":-67,
				"signal_rsrp":-92,
				"signal_rsrq":-11,
				"signal_snr":18,
				"serving_radio":{
					"access_technology":"lte",
					"duplex_mode":"fdd",
					"band":"B1",
					"channel":100,
					"channel_type":"earfcn",
					"source":"quectel-qnwinfo"
				}
			}]
		}`))
	}))

	snapshot, err := client.Telemetry(context.Background())
	if err != nil {
		t.Fatalf("Telemetry() error = %v", err)
	}
	if snapshot.BootEpoch != "boot-1" ||
		!snapshot.ObservedAt.Equal(time.Date(2026, time.August, 2, 0, 0, 0, 0, time.UTC)) ||
		len(snapshot.Lines) != 1 {
		t.Fatalf("unexpected telemetry snapshot: %+v", snapshot)
	}
	line := snapshot.Lines[0]
	if line.ID != "line-1" ||
		!line.SignalQualityKnown ||
		line.SignalQuality != 73 ||
		line.SignalDBM == nil || *line.SignalDBM != -67 ||
		line.ServingRadio == nil ||
		line.ServingRadio.Band != "B1" {
		t.Fatalf("unexpected line telemetry: %+v", line)
	}
}

func TestStartCallDecodesReceiptAndTypedError(t *testing.T) {
	callCount := 0
	client := newUnixTestClient(t, http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost || request.URL.Path != "/v1/calls" {
			http.NotFound(response, request)
			return
		}
		callCount++
		response.Header().Set("Content-Type", "application/json")
		if callCount == 2 {
			response.WriteHeader(http.StatusConflict)
			_, _ = response.Write([]byte(`{"error":{"code":"conflict","operation":"start_call","message":"line already has an active call"}}`))
			return
		}
		response.WriteHeader(http.StatusCreated)
		_, _ = response.Write([]byte(`{"request_id":"request-1","resource_id":"call-1"}`))
	}))

	receipt, err := client.StartCall(context.Background(), StartCallRequest{
		RequestID: "request-1",
		LineID:    "line-1",
		Number:    "+818012345678",
	})
	if err != nil {
		t.Fatalf("StartCall() error = %v", err)
	}
	if receipt.RequestID != "request-1" || receipt.ResourceID != "call-1" {
		t.Fatalf("unexpected receipt: %+v", receipt)
	}

	_, err = client.StartCall(context.Background(), StartCallRequest{
		RequestID: "request-2",
		LineID:    "line-1",
		Number:    "+818012345678",
	})
	var operationError *OperationError
	if !errors.As(err, &operationError) || operationError.Code != "conflict" {
		t.Fatalf("StartCall() error = %#v, want conflict OperationError", err)
	}
}

func TestDecodeOperationErrorPreservesTypedFields(t *testing.T) {
	err := decodeOperationError(http.StatusConflict, []byte(
		`{"error":{"code":"conflict","operation":"start_call","message":"line already has an active call"}}`,
	))
	var operationError *OperationError
	if !errors.As(err, &operationError) {
		t.Fatalf("error type = %T, want *OperationError", err)
	}
	if operationError.Status != http.StatusConflict ||
		operationError.Code != "conflict" ||
		operationError.Operation != "start_call" {
		t.Fatalf("unexpected operation error: %+v", operationError)
	}
}

func TestDeviceConfigurationReadsAndAppliesTypedContract(t *testing.T) {
	var applied ApplyDeviceConfigurationRequest
	client := newUnixTestClient(t, http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/lines/line-1/configuration" {
			http.NotFound(response, request)
			return
		}
		switch request.Method {
		case http.MethodGet:
		case http.MethodPatch:
			if err := json.NewDecoder(request.Body).Decode(&applied); err != nil {
				t.Fatalf("decode PATCH body: %v", err)
			}
		default:
			http.NotFound(response, request)
			return
		}
		response.Header().Set("Content-Type", "application/json")
		_, _ = response.Write([]byte(`{
			"line_id":"line-1",
			"revision":"sha256:fixture",
			"observed_at":"2026-07-23T00:00:00Z",
			"identity":{"manufacturer":"Fixture","model":"M1","firmware":"F1","equipment_identifier":"99"},
			"details":{
				"hardware_revision":"fixture-hw-1",
				"primary_port":"cdc-wdm0",
				"access_technologies":16384,
				"serving_radio":{"access_technology":"lte","duplex_mode":"fdd","band":"B1","channel":100,"channel_type":"earfcn","source":"quectel-qnwinfo"},
				"snr":8.5,
				"ports":[
					{"name":"cdc-wdm0","type":"qmi","type_code":6},
					{"name":"ttyUSB2","type":"at","type_code":3}
				]
			},
			"radio":{"enabled":true,"enabled_known":true,"power_state":"on","power_state_code":3},
			"flight_mode":false,
			"flight_mode_known":true,
			"network_enabled":false,
			"data_connections":[],
			"voice_verification":{
				"usb_configuration":"read_failed",
				"media_routing":"enabled"
			},
			"volte":{
				"policy_known":true,
				"policy":"enabled",
				"configuration_mode":"forced_enabled",
				"modem_capability_known":true,
				"modem_capability_enabled":false,
				"restart_required":true,
				"profile_id":"quectel-lte-standard-qcfg-ims",
				"provisioning":{
					"backend":"modemmanager",
					"carrier_configuration":"CU-VoLTE",
					"carrier_configuration_reported":true,
					"carrier_configuration_revision":"05011508",
					"carrier_configuration_revision_reported":true,
					"ims_profile_reported":true,
					"ims_profile_present":true
				}
			},
			"capabilities":{
				"radio":{"backend":"modemmanager","supported":true,"implemented":true,"readable":true,"writable":true},
				"data_connection":{"backend":"modemmanager","supported":true,"implemented":true,"readable":true,"writable":true},
				"flight_mode":{"backend":"modemmanager","supported":true,"implemented":true,"readable":true,"writable":true},
				"vowifi":{"backend":"vendor_extension","supported":false,"implemented":false,"readable":false,"writable":false},
				"volte":{"backend":"vendor_extension","supported":false,"implemented":false,"readable":false,"writable":false},
				"esim":{"backend":"vendor_extension","supported":false,"implemented":false,"readable":false,"writable":false},
				"at_terminal":{"backend":"vendor_extension","supported":false,"implemented":false,"readable":false,"writable":false},
				"ussd":{"backend":"modemmanager","supported":false,"implemented":false,"readable":false,"writable":false},
				"connection_profile":{"backend":"modemmanager","supported":false,"implemented":false,"readable":false,"writable":false},
				"usb_reset":{"backend":"linux_usbfs","supported":true,"implemented":true,"readable":true,"writable":true}
			}
		}`))
	}))

	configuration, err := client.DeviceConfiguration(context.Background(), "line-1")
	if err != nil {
		t.Fatalf("DeviceConfiguration() error = %v", err)
	}
	if configuration.LineID != "line-1" ||
		configuration.Revision != "sha256:fixture" ||
		len(configuration.DataConnections) != 0 ||
		configuration.Capabilities.VoLTE.Supported ||
		configuration.VoLTE.Policy != "enabled" ||
		configuration.VoLTE.ConfigurationMode != "forced_enabled" ||
		!configuration.VoLTE.ModemCapabilityKnown ||
		configuration.VoLTE.ModemCapabilityEnabled ||
		!configuration.VoLTE.RestartRequired ||
		configuration.VoLTE.Provisioning.Backend != "modemmanager" ||
		configuration.VoLTE.Provisioning.CarrierConfiguration != "CU-VoLTE" ||
		!configuration.VoLTE.Provisioning.IMSProfilePresent {
		t.Fatalf("configuration = %+v", configuration)
	}
	if configuration.VoiceVerification == nil ||
		configuration.VoiceVerification.USBConfiguration != "read_failed" ||
		configuration.VoiceVerification.MediaRouting != "enabled" {
		t.Fatalf("voice verification = %+v", configuration.VoiceVerification)
	}
	if !configuration.Capabilities.USBReset.Writable ||
		configuration.Capabilities.USBReset.Backend != "linux_usbfs" {
		t.Fatalf("USB reset capability = %+v", configuration.Capabilities.USBReset)
	}
	if configuration.Details.HardwareRevision != "fixture-hw-1" ||
		configuration.Details.PrimaryPort != "cdc-wdm0" ||
		configuration.Details.AccessTechnologies == nil ||
		*configuration.Details.AccessTechnologies != 16384 ||
		configuration.Details.ServingRadio == nil ||
		configuration.Details.ServingRadio.DuplexMode != "fdd" ||
		configuration.Details.ServingRadio.Band != "B1" ||
		configuration.Details.ServingRadio.Channel == nil ||
		*configuration.Details.ServingRadio.Channel != 100 ||
		configuration.Details.SNR == nil ||
		*configuration.Details.SNR != 8.5 ||
		len(configuration.Details.Ports) != 2 ||
		configuration.Details.Ports[1].Type != "at" {
		t.Fatalf("hardware details = %+v", configuration.Details)
	}
	enabled := false
	configuration, err = client.ApplyDeviceConfiguration(
		context.Background(),
		"line-1",
		ApplyDeviceConfigurationRequest{
			RequestID:        "config-request-1",
			ExpectedRevision: "sha256:fixture",
			Operation:        DeviceConfigurationSetRadioEnabled,
			RadioEnabled:     &enabled,
		},
	)
	if err != nil {
		t.Fatalf("ApplyDeviceConfiguration() error = %v", err)
	}
	if applied.RequestID != "config-request-1" ||
		applied.ExpectedRevision != "sha256:fixture" ||
		applied.Operation != DeviceConfigurationSetRadioEnabled ||
		applied.RadioEnabled == nil ||
		*applied.RadioEnabled {
		t.Fatalf("applied request = %+v", applied)
	}
}

func newUnixTestClient(t *testing.T, handler http.Handler) *Client {
	t.Helper()
	socketPath := filepath.Join(t.TempDir(), "agent.sock")
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	server := &http.Server{Handler: handler}
	serveResult := make(chan error, 1)
	go func() {
		serveResult <- server.Serve(listener)
	}()
	t.Cleanup(func() {
		_ = server.Close()
		_ = listener.Close()
		<-serveResult
	})
	client, err := New(socketPath, time.Second)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return client
}
