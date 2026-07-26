package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
)

const (
	posixReadAccess  = 4
	posixWriteAccess = 2
)

type sysfsScanner struct {
	sysfsRoot    string
	devRoot      string
	udevDataRoot string
}

func newSysfsScanner(sysfsRoot string, devRoot string, udevDataRoot string) (*sysfsScanner, error) {
	for name, path := range map[string]string{
		"sysfs root":     sysfsRoot,
		"device root":    devRoot,
		"udev data root": udevDataRoot,
	} {
		if !filepath.IsAbs(path) {
			return nil, fmt.Errorf("%s must be absolute", name)
		}
		if info, err := os.Stat(path); err != nil {
			return nil, fmt.Errorf("inspect %s %s: %w", name, path, err)
		} else if !info.IsDir() {
			return nil, fmt.Errorf("%s %s is not a directory", name, path)
		}
	}
	return &sysfsScanner{
		sysfsRoot:    filepath.Clean(sysfsRoot),
		devRoot:      filepath.Clean(devRoot),
		udevDataRoot: filepath.Clean(udevDataRoot),
	}, nil
}

func (scanner *sysfsScanner) resolve(assignments []assignment) ([]resolution, error) {
	usbDevices, err := scanner.scanUSBDevices()
	if err != nil {
		return nil, err
	}
	return resolveAssignments(
		assignments,
		scanner.sysfsRoot,
		usbDevices,
		scanner.physicalDeviceAt,
	)
}

func (scanner *sysfsScanner) scanUSBDevices() ([]physicalDevice, error) {
	usbRoot := filepath.Join(scanner.sysfsRoot, "bus", "usb", "devices")
	entries, err := os.ReadDir(usbRoot)
	if err != nil {
		return nil, fmt.Errorf("read USB sysfs inventory %s: %w", usbRoot, err)
	}

	seenPaths := make(map[string]struct{})
	devices := make([]physicalDevice, 0)
	for _, entry := range entries {
		name := entry.Name()
		if strings.Contains(name, ":") || strings.HasPrefix(name, "usb") {
			continue
		}
		path, err := filepath.EvalSymlinks(filepath.Join(usbRoot, name))
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return nil, fmt.Errorf("resolve USB sysfs entry %s: %w", name, err)
		}
		if _, exists := seenPaths[path]; exists {
			continue
		}
		seenPaths[path] = struct{}{}

		device, err := scanner.physicalDeviceAt(path)
		if err != nil {
			return nil, err
		}
		if device == nil || device.VendorID == "" || device.ProductID == "" {
			continue
		}
		devices = append(devices, *device)
	}

	sort.Slice(devices, func(i, j int) bool {
		return devices[i].SysfsPath < devices[j].SysfsPath
	})
	return devices, nil
}

func (scanner *sysfsScanner) physicalDeviceAt(path string) (*physicalDevice, error) {
	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("inspect physical sysfs path %s: %w", path, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("physical sysfs path %s is not a directory", path)
	}

	actualPath, err := filepath.EvalSymlinks(path)
	if err != nil {
		return nil, fmt.Errorf("resolve physical sysfs path %s: %w", path, err)
	}
	devicesRoot := filepath.Join(scanner.sysfsRoot, "devices")
	if !pathWithin(actualPath, devicesRoot) {
		return nil, fmt.Errorf("physical path %s resolves outside %s", path, devicesRoot)
	}

	logicalPath, err := scanner.logicalSysfsPath(actualPath)
	if err != nil {
		return nil, err
	}
	ports, err := scanner.collectPorts(actualPath)
	if err != nil {
		return nil, err
	}

	return &physicalDevice{
		SysfsPath: logicalPath,
		PortPath:  strings.TrimPrefix(logicalPath, "/sys"),
		VendorID:  readTrimmedFile(filepath.Join(actualPath, "idVendor")),
		ProductID: readTrimmedFile(filepath.Join(actualPath, "idProduct")),
		Serial:    readTrimmedFile(filepath.Join(actualPath, "serial")),
		Ports:     sortedPorts(ports),
	}, nil
}

func (scanner *sysfsScanner) collectPorts(physicalPath string) ([]kernelPort, error) {
	ports := make([]kernelPort, 0)
	seen := make(map[string]struct{})

	for _, subsystem := range supportedPortSubsystems {
		classRoot := filepath.Join(scanner.sysfsRoot, "class", subsystem)
		entries, err := os.ReadDir(classRoot)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return nil, fmt.Errorf("read %s class: %w", subsystem, err)
		}
		for _, entry := range entries {
			name := entry.Name()
			if name == "" || name == "." || name == ".." || strings.ContainsRune(name, '/') {
				continue
			}
			classPath := filepath.Join(classRoot, name)
			actualPath, err := filepath.EvalSymlinks(classPath)
			if err != nil {
				if errors.Is(err, os.ErrNotExist) {
					continue
				}
				return nil, fmt.Errorf("resolve %s port %s: %w", subsystem, name, err)
			}
			if !pathWithin(actualPath, physicalPath) {
				continue
			}

			port := kernelPort{
				Subsystem: subsystem,
				Name:      name,
				UDevKey:   udevDatabaseKey(classPath, subsystem),
			}
			if subsystem != "net" {
				port.DevNode = filepath.Join(scanner.devRoot, name)
			}
			key := portKey(port)
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}
			ports = append(ports, port)
		}
	}
	return ports, nil
}

func (scanner *sysfsScanner) checkPortReady(port kernelPort) error {
	if port.UDevKey == "" {
		return fmt.Errorf("%s/%s has no kernel device identifier for the host udev database", port.Subsystem, port.Name)
	}
	udevRecord := filepath.Join(scanner.udevDataRoot, port.UDevKey)
	if info, err := os.Stat(udevRecord); err != nil {
		return fmt.Errorf("host udev record %s is unavailable: %w", udevRecord, err)
	} else if info.IsDir() {
		return fmt.Errorf("host udev record %s is not a file", udevRecord)
	}

	if port.DevNode == "" {
		return nil
	}
	info, err := os.Stat(port.DevNode)
	if err != nil {
		return fmt.Errorf("device node %s is unavailable: %w", port.DevNode, err)
	}
	if info.Mode()&os.ModeDevice == 0 || info.Mode()&os.ModeCharDevice == 0 {
		return fmt.Errorf("device node %s is not a character device", port.DevNode)
	}

	if err := syscall.Access(port.DevNode, posixReadAccess|posixWriteAccess); err != nil {
		return fmt.Errorf("device node %s is not readable and writable by the private hardware runtime: %w", port.DevNode, err)
	}
	return nil
}

func (scanner *sysfsScanner) logicalSysfsPath(actualPath string) (string, error) {
	relative, err := filepath.Rel(scanner.sysfsRoot, actualPath)
	if err != nil {
		return "", fmt.Errorf("make sysfs path relative: %w", err)
	}
	if relative == ".." || strings.HasPrefix(relative, "../") {
		return "", fmt.Errorf("sysfs path %s is outside %s", actualPath, scanner.sysfsRoot)
	}
	return filepath.Join("/sys", relative), nil
}

func pathWithin(path string, root string) bool {
	cleanPath := filepath.Clean(path)
	cleanRoot := filepath.Clean(root)
	return cleanPath == cleanRoot || strings.HasPrefix(cleanPath, cleanRoot+string(os.PathSeparator))
}

func readTrimmedFile(path string) string {
	value, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(value))
}

func udevDatabaseKey(classPath string, subsystem string) string {
	if deviceNumber := readTrimmedFile(filepath.Join(classPath, "dev")); deviceNumber != "" {
		parts := strings.Split(deviceNumber, ":")
		if len(parts) == 2 {
			if _, err := strconv.ParseUint(parts[0], 10, 32); err == nil {
				if _, err := strconv.ParseUint(parts[1], 10, 32); err == nil {
					return "c" + deviceNumber
				}
			}
		}
	}
	if subsystem == "net" {
		if index := readTrimmedFile(filepath.Join(classPath, "ifindex")); index != "" {
			if _, err := strconv.ParseUint(index, 10, 32); err == nil {
				return "n" + index
			}
		}
	}
	return ""
}
