package main

import (
	"fmt"
	"sort"
	"strings"
)

var supportedPortSubsystems = []string{"usbmisc", "wwan", "tty", "net", "rpmsg"}

type physicalDevice struct {
	SysfsPath string
	PortPath  string
	VendorID  string
	ProductID string
	Serial    string
	Ports     []kernelPort
}

type kernelPort struct {
	Subsystem string `json:"subsystem"`
	Name      string `json:"name"`
	DevNode   string `json:"dev_node,omitempty"`
	UDevKey   string `json:"udev_key"`
}

type resolution struct {
	Assignment assignment
	Device     *physicalDevice
}

type kernelEvent struct {
	Action    string
	Subsystem string
	Name      string
	UID       string
}

func assignmentUID(id string) string {
	return "modemdeck:" + id
}

func portKey(port kernelPort) string {
	return port.Subsystem + "\x00" + port.Name
}

func sortedPorts(ports []kernelPort) []kernelPort {
	result := append([]kernelPort(nil), ports...)
	sort.Slice(result, func(i, j int) bool {
		leftPriority := subsystemPriority(result[i].Subsystem)
		rightPriority := subsystemPriority(result[j].Subsystem)
		if leftPriority != rightPriority {
			return leftPriority < rightPriority
		}
		return result[i].Name < result[j].Name
	})
	return result
}

func subsystemPriority(subsystem string) int {
	for index, candidate := range supportedPortSubsystems {
		if candidate == subsystem {
			return index
		}
	}
	return len(supportedPortSubsystems)
}

func resolveAssignments(
	assignments []assignment,
	sysfsRoot string,
	usbDevices []physicalDevice,
	physicalAt func(string) (*physicalDevice, error),
) ([]resolution, error) {
	results := make([]resolution, 0, len(assignments))
	claimedPaths := make(map[string]string)

	for _, item := range assignments {
		matches := make([]physicalDevice, 0, 1)
		if item.Match.SysfsPath != "" {
			actualPath, err := configuredSysfsPath(item.Match.SysfsPath, sysfsRoot)
			if err != nil {
				return nil, fmt.Errorf("assignment %q: %w", item.ID, err)
			}
			device, err := physicalAt(actualPath)
			if err != nil {
				return nil, fmt.Errorf("assignment %q: %w", item.ID, err)
			}
			if device != nil {
				matches = append(matches, *device)
			}
		} else {
			for _, device := range usbDevices {
				if usbMatches(*item.Match.USB, device) {
					matches = append(matches, device)
				}
			}
		}

		if len(matches) > 1 {
			paths := make([]string, 0, len(matches))
			for _, device := range matches {
				paths = append(paths, device.SysfsPath)
			}
			sort.Strings(paths)
			return nil, fmt.Errorf(
				"assignment %q is ambiguous; it matches %d physical devices: %s",
				item.ID,
				len(matches),
				strings.Join(paths, ", "),
			)
		}

		result := resolution{Assignment: item}
		if len(matches) == 1 {
			device := matches[0]
			if owner, exists := claimedPaths[device.SysfsPath]; exists {
				return nil, fmt.Errorf(
					"assignments %q and %q both match physical device %s",
					owner,
					item.ID,
					device.SysfsPath,
				)
			}
			claimedPaths[device.SysfsPath] = item.ID
			result.Device = &device
		}
		results = append(results, result)
	}
	return results, nil
}

func configuredSysfsPath(path string, sysfsRoot string) (string, error) {
	if sysfsRoot == "/sys" {
		return path, nil
	}
	if path != "/sys/devices" && !strings.HasPrefix(path, "/sys/devices/") {
		return "", fmt.Errorf("invalid configured sysfs path %q", path)
	}
	return sysfsRoot + strings.TrimPrefix(path, "/sys"), nil
}

func usbMatches(match usbMatch, device physicalDevice) bool {
	if match.VendorID != device.VendorID || match.ProductID != device.ProductID {
		return false
	}
	if match.Serial != "" {
		return match.Serial == device.Serial
	}
	return match.PortPath == device.PortPath
}
