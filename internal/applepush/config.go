package applepush

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/human-agent65535/modemdeck/internal/mobilepairing"
)

const defaultConfigFile = "/var/lib/modemdeck/apple-push/config.json"

type Config struct {
	TeamID         string `json:"team_id"`
	KeyID          string `json:"key_id"`
	BundleID       string `json:"bundle_id"`
	PrivateKeyFile string `json:"private_key_file"`
}

type configSource struct {
	lookupEnv   func(string) (string, bool)
	defaultFile string
}

func LoadConfigFromEnvironment() (Config, bool, error) {
	return loadConfig(configSource{
		lookupEnv:   os.LookupEnv,
		defaultFile: defaultConfigFile,
	})
}

func loadConfig(source configSource) (Config, bool, error) {
	if source.lookupEnv == nil {
		source.lookupEnv = os.LookupEnv
	}
	configuredFile, configFileSet := source.lookupEnv("MODEMDECK_APNS_CONFIG_FILE")
	configuredFile = strings.TrimSpace(configuredFile)
	if configFileSet && configuredFile == "" {
		return Config{}, false, errors.New("MODEMDECK_APNS_CONFIG_FILE is empty")
	}

	config := Config{
		TeamID:         environmentValue(source.lookupEnv, "MODEMDECK_APNS_TEAM_ID"),
		KeyID:          environmentValue(source.lookupEnv, "MODEMDECK_APNS_KEY_ID"),
		BundleID:       environmentValue(source.lookupEnv, "MODEMDECK_APNS_BUNDLE_ID"),
		PrivateKeyFile: environmentValue(source.lookupEnv, "MODEMDECK_APNS_PRIVATE_KEY_FILE"),
	}
	environmentConfigured := config != (Config{})
	if environmentConfigured {
		if configuredFile != "" {
			return Config{}, false, errors.New("APNs environment configuration cannot be combined with MODEMDECK_APNS_CONFIG_FILE")
		}
		if err := config.validate(); err != nil {
			return Config{}, false, err
		}
		return config, true, nil
	}

	path := configuredFile
	if path == "" {
		path = strings.TrimSpace(source.defaultFile)
	}
	if path == "" {
		return Config{}, false, nil
	}
	contents, err := readBoundedFile(path, 16<<10)
	if errors.Is(err, os.ErrNotExist) && !configFileSet {
		return Config{}, false, nil
	}
	if err != nil {
		return Config{}, false, fmt.Errorf("read APNs configuration: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&config); err != nil {
		return Config{}, false, fmt.Errorf("decode APNs configuration: %w", err)
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return Config{}, false, fmt.Errorf("decode APNs configuration: %w", err)
	}
	config.normalize()
	if config.PrivateKeyFile != "" && !filepath.IsAbs(config.PrivateKeyFile) {
		config.PrivateKeyFile = filepath.Join(filepath.Dir(path), config.PrivateKeyFile)
	}
	if err := config.validate(); err != nil {
		return Config{}, false, err
	}
	return config, true, nil
}

func environmentValue(lookup func(string) (string, bool), name string) string {
	value, _ := lookup(name)
	return strings.TrimSpace(value)
}

func (config *Config) normalize() {
	config.TeamID = strings.TrimSpace(config.TeamID)
	config.KeyID = strings.TrimSpace(config.KeyID)
	config.BundleID = strings.TrimSpace(config.BundleID)
	config.PrivateKeyFile = strings.TrimSpace(config.PrivateKeyFile)
}

func (config Config) validate() error {
	config.normalize()
	if !validAppleIdentifier(config.TeamID) {
		return errors.New("APNs Team ID must contain 10 uppercase letters or digits")
	}
	if !validAppleIdentifier(config.KeyID) {
		return errors.New("APNs Key ID must contain 10 uppercase letters or digits")
	}
	if !mobilepairing.ValidBundleID(config.BundleID) {
		return errors.New("APNs Bundle ID is invalid")
	}
	if config.PrivateKeyFile == "" {
		return errors.New("APNs private key file is required")
	}
	return nil
}

func validAppleIdentifier(value string) bool {
	if len(value) != 10 {
		return false
	}
	for _, character := range []byte(value) {
		if (character < 'A' || character > 'Z') && (character < '0' || character > '9') {
			return false
		}
	}
	return true
}

func readBoundedFile(path string, maximum int64) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	contents, err := io.ReadAll(io.LimitReader(file, maximum+1))
	if err != nil {
		return nil, err
	}
	if int64(len(contents)) > maximum {
		return nil, errors.New("file is too large")
	}
	return contents, nil
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); errors.Is(err, io.EOF) {
		return nil
	} else if err != nil {
		return err
	}
	return errors.New("multiple JSON values are not allowed")
}
