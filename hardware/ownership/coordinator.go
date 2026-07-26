package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"time"
)

type deviceScanner interface {
	resolve([]assignment) ([]resolution, error)
	checkPortReady(kernelPort) error
}

type activeAssignment struct {
	PhysicalPath string
	Ports        map[string]kernelPort
}

type pendingObservation struct {
	FirstSeen time.Time
	LastError string
}

type coordinator struct {
	cfg            config
	scanner        deviceScanner
	guard          ownershipGuard
	reporter       eventReporter
	statusPath     string
	startupTimeout time.Duration
	now            func() time.Time

	active  map[string]*activeAssignment
	pending map[string]pendingObservation
	ready   bool
}

func newCoordinator(
	cfg config,
	scanner deviceScanner,
	guard ownershipGuard,
	reporter eventReporter,
	statusPath string,
	startupTimeout time.Duration,
) *coordinator {
	return &coordinator{
		cfg:            cfg,
		scanner:        scanner,
		guard:          guard,
		reporter:       reporter,
		statusPath:     statusPath,
		startupTimeout: startupTimeout,
		now:            time.Now,
		active:         make(map[string]*activeAssignment),
		pending:        make(map[string]pendingObservation),
	}
}

func (coordinator *coordinator) Run(ctx context.Context) error {
	startedAt := coordinator.now()
	startupDeadline := startedAt.Add(coordinator.startupTimeout)
	ticker := time.NewTicker(coordinator.cfg.pollInterval)
	defer ticker.Stop()

	for {
		now := coordinator.now()
		statuses, startupComplete, err := coordinator.reconcile(ctx, now)
		if err != nil {
			return err
		}
		if !coordinator.ready && startupComplete {
			coordinator.ready = true
			slog.Info("device ownership initial reconciliation complete")
		}
		if err := writeOwnerStatus(coordinator.statusPath, ownerStatus{
			Ready:       coordinator.ready,
			UpdatedAt:   now,
			Assignments: statuses,
		}); err != nil {
			return err
		}
		if !coordinator.ready && !now.Before(startupDeadline) {
			return errors.New("required device assignments did not become ready before startup timeout")
		}

		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

func (coordinator *coordinator) reconcile(
	ctx context.Context,
	now time.Time,
) ([]assignmentStatus, bool, error) {
	resolutions, err := coordinator.scanner.resolve(coordinator.cfg.Assignments)
	if err != nil {
		return nil, false, err
	}

	matchedDevices := make([]physicalDevice, 0, len(resolutions))
	for _, resolved := range resolutions {
		if resolved.Device != nil {
			matchedDevices = append(matchedDevices, *resolved.Device)
		}
	}
	if err := coordinator.guard.Check(ctx, matchedDevices); err != nil {
		return nil, false, fmt.Errorf("advanced ownership guard failed: %w", err)
	}

	currentAssignments := make(map[string]resolution, len(resolutions))
	observedPending := make(map[string]struct{})
	for _, resolved := range resolutions {
		currentAssignments[resolved.Assignment.ID] = resolved
	}

	for id, active := range coordinator.active {
		current := currentAssignments[id]
		if current.Device == nil || current.Device.SysfsPath != active.PhysicalPath {
			if err := coordinator.removePorts(ctx, id, active.Ports); err != nil {
				return nil, false, err
			}
			delete(coordinator.active, id)
			continue
		}
		currentPorts := make(map[string]kernelPort, len(current.Device.Ports))
		for _, port := range current.Device.Ports {
			currentPorts[portKey(port)] = port
		}
		removed := make(map[string]kernelPort)
		for key, port := range active.Ports {
			if _, exists := currentPorts[key]; !exists {
				removed[key] = port
			}
		}
		if err := coordinator.removePorts(ctx, id, removed); err != nil {
			return nil, false, err
		}
		for key := range removed {
			delete(active.Ports, key)
		}
	}

	statuses := make([]assignmentStatus, 0, len(resolutions))
	startupComplete := true
	for _, resolved := range resolutions {
		status := assignmentStatus{
			ID:       resolved.Assignment.ID,
			State:    "absent",
			Required: resolved.Assignment.RequiredAtStartup,
		}
		if resolved.Device == nil {
			if resolved.Assignment.RequiredAtStartup {
				startupComplete = false
				status.Detail = "required physical device is not present"
			}
			statuses = append(statuses, status)
			continue
		}

		device := *resolved.Device
		status.SysfsPath = device.SysfsPath
		status.USBSerial = device.Serial
		status.USBVendorID = device.VendorID
		status.USBProductID = device.ProductID
		status.Ports = append([]kernelPort(nil), device.Ports...)
		status.State = "settling"

		if len(device.Ports) == 0 {
			key := resolved.Assignment.ID + "\x00physical"
			pending := coordinator.observePending(key, now, "physical device exposes no ModemManager candidate ports")
			observedPending[key] = struct{}{}
			status.Detail = pending.LastError
			startupComplete = false
			if now.Sub(pending.FirstSeen) >= coordinator.cfg.portReadyWindow {
				return nil, false, fmt.Errorf(
					"assignment %q physical device %s exposes no ModemManager candidate ports after %s",
					resolved.Assignment.ID,
					device.SysfsPath,
					coordinator.cfg.portReadyWindow,
				)
			}
			statuses = append(statuses, status)
			continue
		}

		active := coordinator.active[resolved.Assignment.ID]
		if active == nil {
			active = &activeAssignment{
				PhysicalPath: device.SysfsPath,
				Ports:        make(map[string]kernelPort),
			}
			coordinator.active[resolved.Assignment.ID] = active
		}

		for _, port := range device.Ports {
			key := portKey(port)
			if _, exists := active.Ports[key]; exists {
				continue
			}
			pendingKey := resolved.Assignment.ID + "\x00" + key
			readinessError := coordinator.scanner.checkPortReady(port)
			detail := ""
			if readinessError != nil {
				detail = readinessError.Error()
			}
			pending := coordinator.observePending(pendingKey, now, detail)
			observedPending[pendingKey] = struct{}{}
			if readinessError != nil {
				status.Detail = readinessError.Error()
				startupComplete = false
				if now.Sub(pending.FirstSeen) >= coordinator.cfg.portReadyWindow {
					return nil, false, fmt.Errorf(
						"assignment %q port %s/%s did not become exclusively usable after %s: %w",
						resolved.Assignment.ID,
						port.Subsystem,
						port.Name,
						coordinator.cfg.portReadyWindow,
						readinessError,
					)
				}
				continue
			}
			if now.Sub(pending.FirstSeen) < coordinator.cfg.settleDuration {
				status.Detail = "waiting for stable sysfs and host udev state"
				startupComplete = false
				continue
			}

			event := kernelEvent{
				Action:    "add",
				Subsystem: port.Subsystem,
				Name:      port.Name,
				UID:       assignmentUID(resolved.Assignment.ID),
			}
			if err := coordinator.reporter.Report(ctx, event); err != nil {
				return nil, false, err
			}
			active.Ports[key] = port
			delete(coordinator.pending, pendingKey)
			slog.Info(
				"reported assigned kernel port",
				"assignment",
				resolved.Assignment.ID,
				"action",
				"add",
				"subsystem",
				port.Subsystem,
				"name",
				port.Name,
				"physical_path",
				device.SysfsPath,
			)
		}

		if len(active.Ports) == len(device.Ports) {
			status.State = "active"
			status.Detail = ""
		} else {
			startupComplete = false
		}
		statuses = append(statuses, status)
	}

	for key := range coordinator.pending {
		if _, observed := observedPending[key]; !observed {
			delete(coordinator.pending, key)
		}
	}
	sort.Slice(statuses, func(i, j int) bool {
		return statuses[i].ID < statuses[j].ID
	})
	return statuses, startupComplete, nil
}

func (coordinator *coordinator) observePending(
	key string,
	now time.Time,
	detail string,
) pendingObservation {
	pending, exists := coordinator.pending[key]
	if !exists || pending.LastError != detail {
		pending = pendingObservation{FirstSeen: now}
	}
	pending.LastError = detail
	coordinator.pending[key] = pending
	return pending
}

func (coordinator *coordinator) removePorts(
	ctx context.Context,
	assignmentID string,
	ports map[string]kernelPort,
) error {
	if len(ports) == 0 {
		return nil
	}
	ordered := make([]kernelPort, 0, len(ports))
	for _, port := range ports {
		ordered = append(ordered, port)
	}
	ordered = sortedPorts(ordered)
	for left, right := 0, len(ordered)-1; left < right; left, right = left+1, right-1 {
		ordered[left], ordered[right] = ordered[right], ordered[left]
	}
	for _, port := range ordered {
		event := kernelEvent{
			Action:    "remove",
			Subsystem: port.Subsystem,
			Name:      port.Name,
			UID:       assignmentUID(assignmentID),
		}
		if err := coordinator.reporter.Report(ctx, event); err != nil {
			return err
		}
		slog.Info(
			"reported assigned kernel port",
			"assignment",
			assignmentID,
			"action",
			"remove",
			"subsystem",
			port.Subsystem,
			"name",
			port.Name,
		)
	}
	return nil
}
