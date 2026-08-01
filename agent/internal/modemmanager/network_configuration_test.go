package modemmanager

import (
	"context"
	"testing"

	"github.com/godbus/dbus/v5"
	"github.com/human-agent65535/modemdeck/agent/internal/domain"
)

func TestNetworkConfigurationsReadOnlyPacketDataFromOneObservation(t *testing.T) {
	t.Parallel()

	objects := configurationObjects()
	objects[testModemPath][profileManagerInterface] = Properties{}
	objects[testModemPath][modemInterface]["Bearers"] = dbus.MakeVariant(
		[]dbus.ObjectPath{testOwnedBearerPath},
	)
	objects[testOwnedBearerPath] = Interfaces{
		bearerInterface: testBearerProperties(
			true,
			"internet.example",
			domain.APNTypeDefault,
			bearerIPFamilyIPv4V6,
		),
	}
	caller := &configurationCaller{
		objects: objects,
		connectionProfiles: []map[string]dbus.Variant{{
			"profile-id": dbus.MakeVariant(int32(5)),
			"apn":        dbus.MakeVariant("ims"),
		}},
	}
	provider := newTestProvider(caller)

	configurations, err := provider.NetworkConfigurations(context.Background())
	if err != nil {
		t.Fatalf("NetworkConfigurations() error = %v", err)
	}
	if len(configurations) != 1 {
		t.Fatalf("NetworkConfigurations() = %+v", configurations)
	}
	configuration := configurations[0]
	if configuration.LineID != parsedLineID(objects, provider.ids) ||
		len(configuration.DataConnections) != 1 ||
		!configuration.DataConnections[0].Connected ||
		configuration.DataConnections[0].APN != "internet.example" ||
		configuration.DataConnections[0].Interface != "wwan0" {
		t.Fatalf("network configuration = %+v", configuration)
	}
	assertConfigurationMethods(
		t,
		caller.methods(),
		objectManagerInterface+".GetManagedObjects",
	)
}
