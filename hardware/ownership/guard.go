package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/godbus/dbus/v5"
)

const (
	modemManagerService   = "org.freedesktop.ModemManager1"
	modemManagerPath      = dbus.ObjectPath("/org/freedesktop/ModemManager1")
	modemManagerInterface = "org.freedesktop.ModemManager1.Modem"
	dbusService           = "org.freedesktop.DBus"
	dbusPath              = dbus.ObjectPath("/org/freedesktop/DBus")
	dbusInterface         = "org.freedesktop.DBus"
	objectManager         = "org.freedesktop.DBus.ObjectManager"
)

type ownershipGuard interface {
	Check(context.Context, []physicalDevice) error
}

type hostGuard struct {
	procRoot         string
	systemBusAddress string
	ownCgroup        string
}

func newHostGuard(cfg hostConfig) (*hostGuard, error) {
	if err := validateDedicatedHostProc(cfg.ProcRoot); err != nil {
		return nil, err
	}
	socketPath, err := unixSocketPath(cfg.SystemBusAddress)
	if err != nil {
		return nil, err
	}
	if info, err := os.Stat(socketPath); err != nil {
		return nil, fmt.Errorf("advanced host system bus visibility is unavailable at %s: %w", socketPath, err)
	} else if info.Mode()&os.ModeSocket == 0 {
		return nil, fmt.Errorf("advanced host system bus path %s is not a Unix socket", socketPath)
	}

	ownCgroup, err := os.ReadFile("/proc/self/cgroup")
	if err != nil {
		return nil, fmt.Errorf("read hardware container cgroup identity: %w", err)
	}
	return &hostGuard{
		procRoot:         cfg.ProcRoot,
		systemBusAddress: cfg.SystemBusAddress,
		ownCgroup:        string(ownCgroup),
	}, nil
}

func validateDedicatedHostProc(root string) error {
	info, err := os.Stat(root)
	if err != nil {
		return fmt.Errorf("advanced host proc visibility is unavailable at %s: %w", root, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("advanced host proc path %s is not a directory", root)
	}

	hostNamespace, err := os.Readlink(filepath.Join(root, "1", "ns", "pid"))
	if err != nil {
		return fmt.Errorf("read host PID namespace from %s: %w", root, err)
	}
	containerNamespace, err := os.Readlink("/proc/1/ns/pid")
	if err != nil {
		return fmt.Errorf("read container PID namespace: %w", err)
	}
	if hostNamespace == containerNamespace {
		return fmt.Errorf(
			"%s is not a dedicated host /proc view; bind-mount host /proc read-only instead of reusing container /proc",
			root,
		)
	}
	return nil
}

func unixSocketPath(address string) (string, error) {
	const prefix = "unix:path="
	if !strings.HasPrefix(address, prefix) {
		return "", fmt.Errorf("unsupported host system bus address %q", address)
	}
	path := strings.TrimPrefix(address, prefix)
	if path == "" || !filepath.IsAbs(path) || strings.Contains(path, ",") {
		return "", fmt.Errorf("host system bus address %q must contain one absolute unix:path", address)
	}
	return filepath.Clean(path), nil
}

func (guard *hostGuard) Check(ctx context.Context, devices []physicalDevice) error {
	if len(devices) == 0 {
		return guard.checkExternalManager(ctx, nil)
	}

	selectedPorts := make(map[string]physicalDevice)
	selectedPaths := make(map[string]physicalDevice)
	for _, device := range devices {
		selectedPaths[device.SysfsPath] = device
		selectedPaths[device.PortPath] = device
		for _, port := range device.Ports {
			if port.DevNode != "" {
				selectedPorts[port.DevNode] = device
			}
		}
	}

	if err := guard.checkForeignOpeners(selectedPorts); err != nil {
		return err
	}
	return guard.checkExternalManager(ctx, selectedPaths)
}

func (guard *hostGuard) checkForeignOpeners(
	selectedPorts map[string]physicalDevice,
) error {
	entries, err := os.ReadDir(guard.procRoot)
	if err != nil {
		return fmt.Errorf("scan external processes for assigned device conflicts: %w", err)
	}

	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || pid <= 0 {
			continue
		}
		processRoot := filepath.Join(guard.procRoot, entry.Name())
		cgroup, err := os.ReadFile(filepath.Join(processRoot, "cgroup"))
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return fmt.Errorf("read external process %d cgroup during assigned device conflict check: %w", pid, err)
		}
		if string(cgroup) == guard.ownCgroup {
			continue
		}

		commBytes, err := os.ReadFile(filepath.Join(processRoot, "comm"))
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return fmt.Errorf("read external process %d name during assigned device conflict check: %w", pid, err)
		}
		comm := strings.TrimSpace(string(commBytes))

		if len(selectedPorts) == 0 {
			continue
		}
		fdRoot := filepath.Join(processRoot, "fd")
		fds, err := os.ReadDir(fdRoot)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return fmt.Errorf(
				"cannot inspect external process %d (%s) descriptors during assigned device conflict check: %w",
				pid,
				comm,
				err,
			)
		}
		for _, fd := range fds {
			target, err := os.Readlink(filepath.Join(fdRoot, fd.Name()))
			if err != nil {
				if errors.Is(err, os.ErrNotExist) {
					continue
				}
				return fmt.Errorf(
					"inspect external process %d (%s) descriptor %s during assigned device conflict check: %w",
					pid,
					comm,
					fd.Name(),
					err,
				)
			}
			target = strings.TrimSuffix(target, " (deleted)")
			if device, conflict := selectedPorts[target]; conflict {
				return fmt.Errorf(
					"assigned physical device %s is externally occupied: process %d (%s) has %s open",
					device.SysfsPath,
					pid,
					comm,
					target,
				)
			}
		}
	}
	return nil
}

func (guard *hostGuard) checkExternalManager(
	ctx context.Context,
	selectedPaths map[string]physicalDevice,
) error {
	conn, err := dbus.Connect(guard.systemBusAddress)
	if err != nil {
		return fmt.Errorf("connect read-only external ownership visibility: %w", err)
	}
	defer conn.Close()

	var hasOwner bool
	call := conn.Object(dbusService, dbusPath).CallWithContext(
		ctx,
		dbusInterface+".NameHasOwner",
		dbus.FlagNoAutoStart,
		modemManagerService,
	)
	if call.Err != nil {
		return fmt.Errorf("read external ownership service state with NameHasOwner: %w", call.Err)
	}
	if err := call.Store(&hasOwner); err != nil {
		return fmt.Errorf("decode external NameHasOwner response: %w", err)
	}
	if !hasOwner {
		return nil
	}

	objects := map[dbus.ObjectPath]map[string]map[string]dbus.Variant{}
	call = conn.Object(modemManagerService, modemManagerPath).CallWithContext(
		ctx,
		objectManager+".GetManagedObjects",
		dbus.FlagNoAutoStart,
	)
	if call.Err != nil {
		return fmt.Errorf("read external managed objects with GetManagedObjects: %w", call.Err)
	}
	if err := call.Store(&objects); err != nil {
		return fmt.Errorf("decode external managed objects: %w", err)
	}
	if len(selectedPaths) == 0 {
		return nil
	}
	return externalManagedObjectConflict(selectedPaths, objects)
}

func externalManagedObjectConflict(
	selectedPaths map[string]physicalDevice,
	objects map[dbus.ObjectPath]map[string]map[string]dbus.Variant,
) error {
	selectedPortNames := make(map[string]physicalDevice)
	for _, device := range selectedPaths {
		for _, port := range device.Ports {
			selectedPortNames[port.Name] = device
		}
	}
	for path, interfaces := range objects {
		properties, ok := interfaces[modemManagerInterface]
		if !ok {
			continue
		}
		for _, propertyName := range []string{"Device", "Physdev"} {
			value, ok := variantString(properties[propertyName])
			if !ok {
				continue
			}
			if device, conflict := selectedPaths[value]; conflict {
				return fmt.Errorf(
					"assigned physical device %s is externally occupied by managed object %s",
					device.SysfsPath,
					path,
				)
			}
		}
		for _, name := range variantPortNames(properties["Ports"]) {
			if device, conflict := selectedPortNames[name]; conflict {
				return fmt.Errorf(
					"assigned physical device %s is externally occupied: port %s is exported by managed object %s",
					device.SysfsPath,
					name,
					path,
				)
			}
		}
	}
	return nil
}

func variantString(value dbus.Variant) (string, bool) {
	typed, ok := value.Value().(string)
	if !ok {
		return "", false
	}
	typed = strings.TrimSpace(typed)
	return typed, typed != ""
}

func variantPortNames(value dbus.Variant) []string {
	raw := value.Value()
	tuples, ok := raw.([][]any)
	if !ok {
		if items, itemsOK := raw.([]any); itemsOK {
			tuples = make([][]any, 0, len(items))
			for _, item := range items {
				if tuple, tupleOK := item.([]any); tupleOK {
					tuples = append(tuples, tuple)
				}
			}
		}
	}
	names := make([]string, 0, len(tuples))
	for _, tuple := range tuples {
		if len(tuple) != 2 {
			continue
		}
		if name, ok := tuple[0].(string); ok && strings.TrimSpace(name) != "" {
			names = append(names, strings.TrimSpace(name))
		}
	}
	return names
}
