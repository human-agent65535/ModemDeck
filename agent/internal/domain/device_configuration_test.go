package domain

import "testing"

func TestDeviceConfigurationRevisionIgnoresTransientRestartRequirement(t *testing.T) {
	t.Parallel()

	configuration := DeviceConfiguration{
		LineID:          "line-1",
		DataConnections: []DataConnection{},
		VoLTE: VoLTEConfiguration{
			PolicyKnown:     true,
			Policy:          "enabled",
			RestartRequired: false,
		},
	}
	before, err := RevisionDeviceConfiguration(configuration)
	if err != nil {
		t.Fatalf("RevisionDeviceConfiguration(before) error = %v", err)
	}

	configuration.VoLTE.RestartRequired = true
	after, err := RevisionDeviceConfiguration(configuration)
	if err != nil {
		t.Fatalf("RevisionDeviceConfiguration(after) error = %v", err)
	}
	if after != before {
		t.Fatalf("restart marker changed revision: before=%q after=%q", before, after)
	}
}

func TestDeviceConfigurationRevisionIgnoresHardwareTelemetry(t *testing.T) {
	t.Parallel()

	configuration := DeviceConfiguration{
		LineID:          "line-1",
		DataConnections: []DataConnection{},
		Details: DeviceHardwareDetails{
			HardwareRevision: "rev-a",
			PrimaryPort:      "cdc-wdm0",
			SNR:              float64Pointer(4.5),
		},
	}
	before, err := RevisionDeviceConfiguration(configuration)
	if err != nil {
		t.Fatalf("RevisionDeviceConfiguration(before) error = %v", err)
	}

	configuration.Details.SNR = float64Pointer(12.25)
	configuration.Details.Ports = []ModemPort{{
		Name:     "ttyUSB2",
		Type:     "at",
		TypeCode: 3,
	}}
	after, err := RevisionDeviceConfiguration(configuration)
	if err != nil {
		t.Fatalf("RevisionDeviceConfiguration(after) error = %v", err)
	}
	if after != before {
		t.Fatalf("hardware telemetry changed revision: before=%q after=%q", before, after)
	}
}

func float64Pointer(value float64) *float64 {
	return &value
}
