package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"
)

var version = "dev"

func main() {
	if err := runMain(os.Args[1:]); err != nil {
		slog.Error("device owner stopped", "error", err)
		os.Exit(1)
	}
}

func runMain(arguments []string) error {
	if len(arguments) == 0 {
		return errors.New("expected a command: run, validate, inventory, or health")
	}
	switch arguments[0] {
	case "run":
		return runOwner(arguments[1:])
	case "validate":
		return validateConfig(arguments[1:])
	case "inventory":
		return printInventory(arguments[1:])
	case "health":
		return healthStatus(arguments[1:])
	case "version", "--version":
		fmt.Println(version)
		return nil
	default:
		return fmt.Errorf("unknown command %q; expected run, validate, inventory, or health", arguments[0])
	}
}

func runOwner(arguments []string) error {
	flags := flag.NewFlagSet("run", flag.ContinueOnError)
	configPath := flags.String("config", "", "absolute device assignments JSON path")
	statusPath := flags.String(
		"status-file",
		"/run/modemdeck/device-owner-status.json",
		"absolute owner status JSON path",
	)
	sysfsRoot := flags.String("sysfs-root", "/sys", "sysfs mount root")
	devRoot := flags.String("dev-root", "/dev", "device node root")
	udevDataRoot := flags.String("udev-data-root", "/run/udev/data", "host udev database root")
	startupTimeoutSeconds := flags.Int("startup-timeout-seconds", 20, "required assignment startup timeout")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected run arguments: %v", flags.Args())
	}
	if err := requireAbsoluteFile("config", *configPath); err != nil {
		return err
	}
	if err := requireAbsolutePath("status-file", *statusPath); err != nil {
		return err
	}
	if *startupTimeoutSeconds <= 0 || *startupTimeoutSeconds > 300 {
		return errors.New("startup-timeout-seconds must be between 1 and 300")
	}

	cfg, err := loadConfig(*configPath)
	if err != nil {
		return err
	}
	scanner, err := newSysfsScanner(*sysfsRoot, *devRoot, *udevDataRoot)
	if err != nil {
		return err
	}
	guard, err := newHostGuard(cfg.Host)
	if err != nil {
		return err
	}
	reporter, err := newMMReporter()
	if err != nil {
		return err
	}
	defer reporter.Close()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	slog.Info(
		"starting advanced device owner",
		"version",
		version,
		"assignments",
		len(cfg.Assignments),
		"poll_interval",
		cfg.pollInterval,
	)
	return newCoordinator(
		cfg,
		scanner,
		guard,
		reporter,
		*statusPath,
		time.Duration(*startupTimeoutSeconds)*time.Second,
	).Run(ctx)
}

func validateConfig(arguments []string) error {
	flags := flag.NewFlagSet("validate", flag.ContinueOnError)
	configPath := flags.String("config", "", "absolute device assignments JSON path")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected validate arguments: %v", flags.Args())
	}
	if err := requireAbsoluteFile("config", *configPath); err != nil {
		return err
	}
	_, err := loadConfig(*configPath)
	return err
}

func printInventory(arguments []string) error {
	flags := flag.NewFlagSet("inventory", flag.ContinueOnError)
	sysfsRoot := flags.String("sysfs-root", "/sys", "sysfs mount root")
	devRoot := flags.String("dev-root", "/dev", "device node root")
	udevDataRoot := flags.String("udev-data-root", "/run/udev/data", "host udev database root")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected inventory arguments: %v", flags.Args())
	}
	scanner, err := newSysfsScanner(*sysfsRoot, *devRoot, *udevDataRoot)
	if err != nil {
		return err
	}
	devices, err := scanner.scanUSBDevices()
	if err != nil {
		return err
	}
	type inventoryDevice struct {
		SysfsPath string       `json:"sysfs_path"`
		PortPath  string       `json:"port_path"`
		VendorID  string       `json:"vendor_id"`
		ProductID string       `json:"product_id"`
		Serial    string       `json:"serial,omitempty"`
		Ports     []kernelPort `json:"ports"`
	}
	result := make([]inventoryDevice, 0, len(devices))
	for _, device := range devices {
		result = append(result, inventoryDevice{
			SysfsPath: device.SysfsPath,
			PortPath:  device.PortPath,
			VendorID:  device.VendorID,
			ProductID: device.ProductID,
			Serial:    device.Serial,
			Ports:     device.Ports,
		})
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	return encoder.Encode(map[string]any{
		"devices": result,
	})
}

func healthStatus(arguments []string) error {
	flags := flag.NewFlagSet("health", flag.ContinueOnError)
	statusPath := flags.String(
		"status-file",
		"/run/modemdeck/device-owner-status.json",
		"absolute owner status JSON path",
	)
	maxAgeSeconds := flags.Int("max-age-seconds", 10, "maximum accepted status age")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected health arguments: %v", flags.Args())
	}
	if err := requireAbsolutePath("status-file", *statusPath); err != nil {
		return err
	}
	if *maxAgeSeconds <= 0 || *maxAgeSeconds > 300 {
		return errors.New("max-age-seconds must be between 1 and 300")
	}
	return checkOwnerStatus(
		*statusPath,
		time.Duration(*maxAgeSeconds)*time.Second,
		time.Now(),
	)
}

func requireAbsoluteFile(name string, path string) error {
	if err := requireAbsolutePath(name, path); err != nil {
		return err
	}
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("%s %s is unavailable: %w", name, path, err)
	}
	if info.IsDir() {
		return fmt.Errorf("%s %s is a directory", name, path)
	}
	return nil
}

func requireAbsolutePath(name string, path string) error {
	if path == "" || !filepath.IsAbs(path) {
		return fmt.Errorf("%s must be an absolute path", name)
	}
	return nil
}
