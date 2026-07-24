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
