package modemmanager

import (
	"testing"

	"github.com/godbus/dbus/v5"
)

func TestSystemBusSignalAffectsSnapshot(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		signal *dbus.Signal
		want   bool
	}{
		{
			name: "provider restart",
			signal: &dbus.Signal{
				Name: busInterface + ".NameOwnerChanged",
			},
			want: true,
		},
		{
			name: "modem added",
			signal: &dbus.Signal{
				Name: objectManagerInterface + ".InterfacesAdded",
				Body: []any{
					dbus.ObjectPath("/org/freedesktop/ModemManager1/Modem/0"),
					map[string]map[string]dbus.Variant{
						modemInterface: {},
					},
				},
			},
			want: true,
		},
		{
			name: "call removed",
			signal: &dbus.Signal{
				Name: objectManagerInterface + ".InterfacesRemoved",
				Body: []any{
					dbus.ObjectPath("/org/freedesktop/ModemManager1/Call/0"),
					[]string{callInterface},
				},
			},
			want: true,
		},
		{
			name: "bearer added",
			signal: &dbus.Signal{
				Name: objectManagerInterface + ".InterfacesAdded",
				Body: []any{
					dbus.ObjectPath("/org/freedesktop/ModemManager1/Bearer/0"),
					map[string]map[string]dbus.Variant{
						bearerInterface: {},
					},
				},
			},
			want: false,
		},
		{
			name: "extended signal telemetry",
			signal: propertyChangeSignal(signalInterface, map[string]dbus.Variant{
				"Lte": dbus.MakeVariant(map[string]dbus.Variant{
					"rsrp": dbus.MakeVariant(-98.0),
				}),
			}),
			want: false,
		},
		{
			name: "basic signal telemetry",
			signal: propertyChangeSignal(modemInterface, map[string]dbus.Variant{
				"SignalQuality": dbus.MakeVariant([]any{uint32(72), true}),
			}),
			want: false,
		},
		{
			name: "basic signal telemetry invalidated",
			signal: &dbus.Signal{
				Name: propertiesInterface + ".PropertiesChanged",
				Body: []any{
					modemInterface,
					map[string]dbus.Variant{},
					[]string{"SignalQuality"},
				},
			},
			want: false,
		},
		{
			name: "modem state with signal telemetry",
			signal: propertyChangeSignal(modemInterface, map[string]dbus.Variant{
				"SignalQuality": dbus.MakeVariant([]any{uint32(72), true}),
				"State":         dbus.MakeVariant(int32(8)),
			}),
			want: true,
		},
		{
			name: "registration state",
			signal: propertyChangeSignal(modem3GPPInterface, map[string]dbus.Variant{
				"RegistrationState": dbus.MakeVariant(uint32(1)),
			}),
			want: true,
		},
		{
			name: "sms state",
			signal: propertyChangeSignal(smsInterface, map[string]dbus.Variant{
				"State": dbus.MakeVariant(uint32(3)),
			}),
			want: true,
		},
		{
			name: "call state signal",
			signal: &dbus.Signal{
				Name: callInterface + ".StateChanged",
			},
			want: true,
		},
		{
			name: "irrelevant bearer properties",
			signal: propertyChangeSignal(bearerInterface, map[string]dbus.Variant{
				"Stats": dbus.MakeVariant(map[string]uint64{"rx-bytes": 42}),
			}),
			want: false,
		},
		{
			name: "malformed relevant properties",
			signal: &dbus.Signal{
				Name: propertiesInterface + ".PropertiesChanged",
				Body: []any{modemInterface},
			},
			want: true,
		},
		{
			name: "unknown signal",
			signal: &dbus.Signal{
				Name: "org.freedesktop.ModemManager1.Modem.Location.LocationFound",
			},
			want: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := systemBusSignalAffectsSnapshot(test.signal); got != test.want {
				t.Fatalf("systemBusSignalAffectsSnapshot() = %t, want %t", got, test.want)
			}
		})
	}
}

func propertyChangeSignal(
	interfaceName string,
	changed map[string]dbus.Variant,
) *dbus.Signal {
	return &dbus.Signal{
		Name: propertiesInterface + ".PropertiesChanged",
		Body: []any{interfaceName, changed, []string{}},
	}
}
