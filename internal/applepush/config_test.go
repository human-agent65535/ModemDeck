package applepush

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfigIsDisabledWithoutLocalConfiguration(t *testing.T) {
	t.Parallel()
	config, configured, err := loadConfig(configSource{
		lookupEnv:   func(string) (string, bool) { return "", false },
		defaultFile: filepath.Join(t.TempDir(), "missing.json"),
	})
	if err != nil || configured || config != (Config{}) {
		t.Fatalf("loadConfig() = %+v, %t, %v", config, configured, err)
	}
}

func TestLoadConfigReadsRelativePrivateKeyPath(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	path := filepath.Join(directory, "config.json")
	if err := os.WriteFile(path, []byte(`{
		"team_id":"ABCDEFGHIJ",
		"key_id":"KLMNOPQRST",
		"bundle_id":"com.example.modemdeck",
		"private_key_file":"AuthKey_example.p8"
	}`), 0o600); err != nil {
		t.Fatal(err)
	}
	config, configured, err := loadConfig(configSource{
		lookupEnv: func(name string) (string, bool) {
			if name == "MODEMDECK_APNS_CONFIG_FILE" {
				return path, true
			}
			return "", false
		},
	})
	if err != nil || !configured {
		t.Fatalf("loadConfig() = %+v, %t, %v", config, configured, err)
	}
	if config.PrivateKeyFile != filepath.Join(directory, "AuthKey_example.p8") ||
		config.BundleID != "com.example.modemdeck" {
		t.Fatalf("config = %+v", config)
	}
}

func TestLoadConfigRejectsPartialEnvironment(t *testing.T) {
	t.Parallel()
	_, _, err := loadConfig(configSource{
		lookupEnv: func(name string) (string, bool) {
			if name == "MODEMDECK_APNS_TEAM_ID" {
				return "ABCDEFGHIJ", true
			}
			return "", false
		},
	})
	if err == nil {
		t.Fatal("loadConfig() error = nil")
	}
}
