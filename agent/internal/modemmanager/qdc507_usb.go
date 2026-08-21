package modemmanager

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/human-agent65535/modemdeck/agent/internal/domain"
)

type qdc507DeviceInhibitor struct {
	provider *Provider
}

func (i qdc507DeviceInhibitor) Inhibit(
	ctx context.Context,
	uid string,
) (func(context.Context) error, error) {
	const operation = "provision_qdc507_usb"
	if i.provider == nil {
		return nil, domain.Unavailable(operation, "ModemManager provider is unavailable", nil)
	}
	if _, err := i.provider.call(
		ctx,
		managerPath,
		serviceName+".InhibitDevice",
		operation,
		"ModemManager could not inhibit the QDC507",
		uid,
		true,
	); err != nil {
		return nil, err
	}
	return func(releaseContext context.Context) error {
		_, err := i.provider.call(
			releaseContext,
			managerPath,
			serviceName+".InhibitDevice",
			operation,
			"ModemManager could not release the QDC507 inhibition",
			uid,
			false,
		)
		return err
	}, nil
}

// EnsureQDC507VoiceUSB serializes the persistent USB change with every call
// and configuration transaction. It rejects a busy line before handing the
// exact physical device to the direct-serial provisioner.
func (p *Provider) EnsureQDC507VoiceUSB(
	ctx context.Context,
	lines []domain.Line,
) (bool, error) {
	const operation = "provision_qdc507_usb"
	if p == nil || p.qdc507USB == nil {
		return false, nil
	}
	if ctx == nil {
		return false, domain.InvalidArgument(operation, "request context is required")
	}
	desired := make([]domain.Line, 0, len(lines))
	for _, line := range lines {
		if isQDC507Line(line) {
			desired = append(desired, line)
		}
	}
	if len(desired) == 0 {
		return false, nil
	}
	sort.Slice(desired, func(i, j int) bool {
		if desired[i].PhysicalDevice != desired[j].PhysicalDevice {
			return desired[i].PhysicalDevice < desired[j].PhysicalDevice
		}
		return desired[i].ID < desired[j].ID
	})

	p.configMu.Lock()
	defer p.configMu.Unlock()
	p.callMu.Lock()
	defer p.callMu.Unlock()

	identity, err := p.resolveProviderIdentity(ctx, operation)
	if err != nil {
		return false, err
	}
	objects, err := p.managedObjects(ctx, operation)
	if err != nil {
		return false, err
	}
	objects, err = p.hydrateCalls(ctx, operation, objects)
	if err != nil {
		return false, err
	}
	parsed := ParseManagedObjects(objects, identity)

	changed := false
	var failures []error
	for _, requested := range desired {
		line, found := findLine(parsed.Lines, requested.ID)
		if !found || normalizePhysicalDevice(line.PhysicalDevice) != normalizePhysicalDevice(requested.PhysicalDevice) {
			failures = append(failures, fmt.Errorf("%s: QDC507 inventory changed before USB provisioning", requested.ID))
			continue
		}
		if lineHasCall(parsed.Calls, line.ID, "") {
			failures = append(failures, fmt.Errorf("%s: QDC507 USB provisioning is blocked by an active call", line.ID))
			continue
		}
		modemPath, found := parsed.LinePaths[line.ID]
		if !found || !modemPath.IsValid() {
			failures = append(failures, fmt.Errorf("%s: QDC507 ModemManager path is unavailable", line.ID))
			continue
		}
		callList, callErr := p.commandATPath(ctx, modemPath, operation, quectelCallListQuery)
		if callErr != nil {
			failures = append(failures, fmt.Errorf("%s: confirm QDC507 call state: %w", line.ID, callErr))
			continue
		}
		callRecords, parseErr := parseQuectelCLCC(callList)
		if parseErr != nil {
			failures = append(failures, fmt.Errorf("%s: QDC507 call state was malformed: %w", line.ID, parseErr))
			continue
		}
		if len(callRecords) != 0 {
			failures = append(failures, fmt.Errorf("%s: QDC507 USB provisioning is blocked by an AT call", line.ID))
			continue
		}
		interfaces, found := objects[modemPath]
		if !found {
			failures = append(failures, fmt.Errorf("%s: QDC507 object disappeared before USB provisioning", line.ID))
			continue
		}
		modemProperties, found := interfaces[modemInterface]
		if !found {
			failures = append(failures, fmt.Errorf("%s: QDC507 modem interface is unavailable", line.ID))
			continue
		}
		connections, connectionErr := p.dataConnectionsFromModem(
			ctx,
			objects,
			modemProperties,
			operation,
		)
		if connectionErr != nil {
			failures = append(failures, fmt.Errorf("%s: confirm QDC507 data state: %w", line.ID, connectionErr))
			continue
		}
		busyData := false
		for _, connection := range connections {
			if connection.Connected {
				busyData = true
				break
			}
		}
		if busyData {
			failures = append(failures, fmt.Errorf("%s: QDC507 USB provisioning is blocked by a connected data bearer", line.ID))
			continue
		}
		lineChanged, ensureErr := p.qdc507USB.Ensure(ctx, line)
		changed = changed || lineChanged
		if ensureErr != nil {
			// QADBKEY responses are deliberately excluded by the provisioner;
			// keep the provider wrapper free of serial response text as well.
			failures = append(failures, fmt.Errorf("%s: %w", line.ID, ensureErr))
		}
	}
	if changed {
		p.clearVoiceProbes()
	}
	return changed, errors.Join(failures...)
}
