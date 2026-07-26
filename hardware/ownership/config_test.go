package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeConfigFile(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "assignments.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadConfigAcceptsStablePhysicalSelectors(t *testing.T) {
	path := writeConfigFile(t, `{
		"version": 1,
		"assignments": [
			{
				"id": "rack-a",
				"required_at_startup": true,
				"match": {
					"sysfs_path": "/sys/devices/pci0000:00/0000:00:14.0/usb1/1-2"
				}
			},
			{
				"id": "rack-b",
				"match": {
					"usb": {
						"vendor_id": "2c7c",
						"product_id": "0125",
						"serial": "MODEM-SERIAL-2"
					}
				}
			}
		]
	}`)

	cfg, err := loadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Host.ProcRoot != defaultHostProcRoot ||
		cfg.Host.SystemBusAddress != defaultHostSystemBusAddr {
		t.Fatalf("host defaults = %+v", cfg.Host)
	}
	if cfg.Assignments[0].Match.SysfsPath == "" ||
		cfg.Assignments[1].Match.USB.Serial != "MODEM-SERIAL-2" {
		t.Fatalf("assignments = %+v", cfg.Assignments)
	}
}

func TestLoadConfigRejectsUnstableOrAmbiguousSelectors(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{
			name: "tty ordinal",
			body: `{
				"version": 1,
				"assignments": [{
					"id": "bad",
					"match": {"sysfs_path": "/dev/ttyUSB0"}
				}]
			}`,
			want: "below /sys/devices",
		},
		{
			name: "vid pid only",
			body: `{
				"version": 1,
				"assignments": [{
					"id": "bad",
					"match": {
						"usb": {"vendor_id": "2c7c", "product_id": "0125"}
					}
				}]
			}`,
			want: "exactly one of serial or port_path",
		},
		{
			name: "serial and port",
			body: `{
				"version": 1,
				"assignments": [{
					"id": "bad",
					"match": {
						"usb": {
							"vendor_id": "2c7c",
							"product_id": "0125",
							"serial": "serial",
							"port_path": "/devices/pci0000:00/usb1/1-2"
						}
					}
				}]
			}`,
			want: "exactly one of serial or port_path",
		},
		{
			name: "unknown field",
			body: `{
				"version": 1,
				"assignments": [{
					"id": "bad",
					"tty": "ttyUSB0",
					"match": {"sysfs_path": "/sys/devices/example"}
				}]
			}`,
			want: "unknown field",
		},
		{
			name: "non unix host bus",
			body: `{
				"version": 1,
				"host": {"system_bus_address": "tcp:host=127.0.0.1"},
				"assignments": [{
					"id": "bad",
					"match": {"sysfs_path": "/sys/devices/example"}
				}]
			}`,
			want: "absolute unix:path",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := loadConfig(writeConfigFile(t, test.body))
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want substring %q", err, test.want)
			}
		})
	}
}
