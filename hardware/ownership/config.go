package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const (
	configVersion            = 1
	defaultPollInterval      = time.Second
	defaultSettleDuration    = 750 * time.Millisecond
	defaultPortReadyTimeout  = 5 * time.Second
	defaultHostProcRoot      = "/run/host-proc"
	defaultHostSystemBusAddr = "unix:path=/run/host-dbus/system_bus_socket"
)

var (
	assignmentIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)
	hexIDPattern        = regexp.MustCompile(`^[0-9a-f]{4}$`)
)

type config struct {
	Version            int          `json:"version"`
	PollIntervalMS     int          `json:"poll_interval_ms,omitempty"`
	SettleMS           int          `json:"settle_ms,omitempty"`
	PortReadyTimeoutMS int          `json:"port_ready_timeout_ms,omitempty"`
	Host               hostConfig   `json:"host"`
	Assignments        []assignment `json:"assignments"`

	pollInterval    time.Duration
	settleDuration  time.Duration
	portReadyWindow time.Duration
}

type hostConfig struct {
	ProcRoot         string `json:"proc_root,omitempty"`
	SystemBusAddress string `json:"system_bus_address,omitempty"`
}

type assignment struct {
	ID                string    `json:"id"`
	RequiredAtStartup bool      `json:"required_at_startup,omitempty"`
	Match             matchSpec `json:"match"`
}

type matchSpec struct {
	SysfsPath string    `json:"sysfs_path,omitempty"`
	USB       *usbMatch `json:"usb,omitempty"`
}

type usbMatch struct {
	VendorID  string `json:"vendor_id"`
	ProductID string `json:"product_id"`
	Serial    string `json:"serial,omitempty"`
	PortPath  string `json:"port_path,omitempty"`
}

func loadConfig(path string) (config, error) {
	file, err := os.Open(path)
	if err != nil {
		return config{}, fmt.Errorf("open assignments config: %w", err)
	}
	defer file.Close()

	decoder := json.NewDecoder(io.LimitReader(file, 1<<20))
	decoder.DisallowUnknownFields()

	var cfg config
	if err := decoder.Decode(&cfg); err != nil {
		return config{}, fmt.Errorf("decode assignments config: %w", err)
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return config{}, err
	}
	if err := cfg.validate(); err != nil {
		return config{}, err
	}
	return cfg, nil
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("assignments config contains multiple JSON values")
		}
		return fmt.Errorf("decode trailing assignments data: %w", err)
	}
	return nil
}

func (cfg *config) validate() error {
	if cfg.Version != configVersion {
		return fmt.Errorf("unsupported assignments config version %d; expected %d", cfg.Version, configVersion)
	}
	if len(cfg.Assignments) == 0 {
		return errors.New("assignments config must contain at least one assignment")
	}

	var err error
	cfg.pollInterval, err = millisecondsOrDefault(
		"poll_interval_ms",
		cfg.PollIntervalMS,
		defaultPollInterval,
		250*time.Millisecond,
		10*time.Second,
	)
	if err != nil {
		return err
	}
	cfg.settleDuration, err = millisecondsOrDefault(
		"settle_ms",
		cfg.SettleMS,
		defaultSettleDuration,
		0,
		30*time.Second,
	)
	if err != nil {
		return err
	}
	cfg.portReadyWindow, err = millisecondsOrDefault(
		"port_ready_timeout_ms",
		cfg.PortReadyTimeoutMS,
		defaultPortReadyTimeout,
		time.Second,
		60*time.Second,
	)
	if err != nil {
		return err
	}
	if cfg.portReadyWindow < cfg.settleDuration {
		return errors.New("port_ready_timeout_ms must be greater than or equal to settle_ms")
	}

	if cfg.Host.ProcRoot == "" {
		cfg.Host.ProcRoot = defaultHostProcRoot
	}
	if !filepath.IsAbs(cfg.Host.ProcRoot) {
		return errors.New("host.proc_root must be an absolute path")
	}
	cfg.Host.ProcRoot = filepath.Clean(cfg.Host.ProcRoot)
	if cfg.Host.SystemBusAddress == "" {
		cfg.Host.SystemBusAddress = defaultHostSystemBusAddr
	}
	if !strings.HasPrefix(cfg.Host.SystemBusAddress, "unix:path=/") {
		return errors.New("host.system_bus_address must be an absolute unix:path D-Bus address")
	}

	seenIDs := make(map[string]struct{}, len(cfg.Assignments))
	for i := range cfg.Assignments {
		item := &cfg.Assignments[i]
		if !assignmentIDPattern.MatchString(item.ID) {
			return fmt.Errorf("assignments[%d].id must match %s", i, assignmentIDPattern)
		}
		if _, exists := seenIDs[item.ID]; exists {
			return fmt.Errorf("assignment id %q is duplicated", item.ID)
		}
		seenIDs[item.ID] = struct{}{}
		if err := item.Match.validate(i); err != nil {
			return err
		}
	}
	return nil
}

func millisecondsOrDefault(
	name string,
	value int,
	fallback time.Duration,
	minimum time.Duration,
	maximum time.Duration,
) (time.Duration, error) {
	if value == 0 {
		return fallback, nil
	}
	duration := time.Duration(value) * time.Millisecond
	if duration < minimum || duration > maximum {
		return 0, fmt.Errorf(
			"%s must be between %d and %d milliseconds",
			name,
			minimum.Milliseconds(),
			maximum.Milliseconds(),
		)
	}
	return duration, nil
}

func (match *matchSpec) validate(index int) error {
	hasSysfs := strings.TrimSpace(match.SysfsPath) != ""
	hasUSB := match.USB != nil
	if hasSysfs == hasUSB {
		return fmt.Errorf(
			"assignments[%d].match must set exactly one of sysfs_path or usb",
			index,
		)
	}

	if hasSysfs {
		match.SysfsPath = filepath.Clean(match.SysfsPath)
		if !filepath.IsAbs(match.SysfsPath) ||
			(match.SysfsPath != "/sys/devices" &&
				!strings.HasPrefix(match.SysfsPath, "/sys/devices/")) {
			return fmt.Errorf(
				"assignments[%d].match.sysfs_path must be an absolute physical path below /sys/devices",
				index,
			)
		}
		return nil
	}

	usb := match.USB
	usb.VendorID = strings.ToLower(strings.TrimSpace(usb.VendorID))
	usb.ProductID = strings.ToLower(strings.TrimSpace(usb.ProductID))
	usb.Serial = strings.TrimSpace(usb.Serial)
	usb.PortPath = filepath.Clean(strings.TrimSpace(usb.PortPath))
	if !hexIDPattern.MatchString(usb.VendorID) || !hexIDPattern.MatchString(usb.ProductID) {
		return fmt.Errorf(
			"assignments[%d].match.usb vendor_id and product_id must be four lowercase hexadecimal digits",
			index,
		)
	}
	hasSerial := usb.Serial != ""
	hasPort := usb.PortPath != "." && usb.PortPath != ""
	if hasSerial == hasPort {
		return fmt.Errorf(
			"assignments[%d].match.usb must set exactly one of serial or port_path",
			index,
		)
	}
	if hasPort &&
		(usb.PortPath != "/devices" && !strings.HasPrefix(usb.PortPath, "/devices/")) {
		return fmt.Errorf(
			"assignments[%d].match.usb.port_path must be a physical path below /devices",
			index,
		)
	}
	return nil
}
