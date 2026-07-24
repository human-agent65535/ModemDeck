package modemmanager

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/godbus/dbus/v5"
	"github.com/human-agent65535/modemdeck/agent/internal/domain"
)

const (
	networkManagerServiceName        = "org.freedesktop.NetworkManager"
	networkManagerInterface          = "org.freedesktop.NetworkManager"
	networkManagerDeviceInterface    = "org.freedesktop.NetworkManager.Device"
	networkManagerActiveInterface    = "org.freedesktop.NetworkManager.Connection.Active"
	networkManagerPath               = dbus.ObjectPath("/org/freedesktop/NetworkManager")
	networkManagerNoObject           = dbus.ObjectPath("/")
	networkManagerDeviceTypeModem    = uint32(8)
	networkManagerStateUnmanaged     = uint32(10)
	networkManagerStateActivated     = uint32(100)
	networkManagerStateFailed        = uint32(120)
	networkManagerActiveActivating   = uint32(1)
	networkManagerActiveActivated    = uint32(2)
	networkManagerActiveDeactivating = uint32(3)
	networkManagerActiveDeactivated  = uint32(4)
	networkManagerPollInterval       = 200 * time.Millisecond
	networkManagerCleanupTimeout     = 5 * time.Second
	networkManagerConnectionPrefix   = "ModemDeck "
)

type networkManagerDevice struct {
	Path             dbus.ObjectPath
	State            uint32
	StateReason      uint32
	ActiveConnection dbus.ObjectPath
}

type networkManagerActiveConnection struct {
	ID    string
	State uint32
}

func (p *Provider) activateNetworkManagerData(
	ctx context.Context,
	modemPath dbus.ObjectPath,
	lineID string,
	requestedAPN string,
	automaticAPN string,
	ipFamily uint32,
	reuseExisting bool,
) (dbus.ObjectPath, error) {
	const operation = "apply_device_configuration"
	device, err := p.findNetworkManagerDevice(ctx, modemPath, operation)
	if err != nil {
		return "", err
	}
	if device.State == networkManagerStateUnmanaged {
		return "", domain.FailedPrecondition(
			operation,
			"NetworkManager is not managing this modem",
			nil,
		)
	}

	connectionID := networkManagerConnectionID(lineID)
	if device.ActiveConnection != networkManagerNoObject {
		active, err := p.readNetworkManagerActive(
			ctx,
			device.ActiveConnection,
			operation,
		)
		if err != nil {
			return "", err
		}
		if active.ID != connectionID {
			return "", domain.Conflict(
				operation,
				"this modem already has a data connection managed outside ModemDeck",
			)
		}
		if reuseExisting {
			switch active.State {
			case networkManagerActiveActivated:
				return device.ActiveConnection, nil
			case networkManagerActiveActivating:
				if err := p.waitNetworkManagerActivated(
					ctx,
					device.Path,
					device.ActiveConnection,
					operation,
				); err != nil {
					cleanupErr := p.cleanupNetworkManagerConnection(
						device.Path,
						device.ActiveConnection,
						operation,
					)
					if cleanupErr != nil {
						return "", domain.VerificationFailed(
							operation,
							"cellular activation failed and NetworkManager cleanup also failed",
							errors.Join(err, cleanupErr),
						)
					}
					return "", err
				}
				return device.ActiveConnection, nil
			}
		}
		if err := p.deactivateNetworkManagerConnection(
			ctx,
			device.Path,
			device.ActiveConnection,
			operation,
		); err != nil {
			return "", err
		}
	}

	settings := networkManagerConnectionSettings(
		connectionID,
		requestedAPN,
		automaticAPN,
		ipFamily,
	)
	options := map[string]dbus.Variant{
		"persist": dbus.MakeVariant("volatile"),
	}
	body, err := p.callDestination(
		ctx,
		networkManagerServiceName,
		networkManagerPath,
		networkManagerInterface+".AddAndActivateConnection2",
		operation,
		"NetworkManager failed to start the cellular data connection",
		settings,
		device.Path,
		networkManagerNoObject,
		options,
	)
	if err != nil {
		return "", err
	}
	var (
		connectionPath dbus.ObjectPath
		activePath     dbus.ObjectPath
		result         map[string]dbus.Variant
	)
	if err := dbus.Store(body, &connectionPath, &activePath, &result); err != nil {
		return "", domain.Internal(
			operation,
			"NetworkManager returned a malformed activation result",
			err,
		)
	}
	if connectionPath == "" || connectionPath == networkManagerNoObject ||
		activePath == "" || activePath == networkManagerNoObject {
		return "", domain.Internal(
			operation,
			"NetworkManager returned an invalid activation path",
			nil,
		)
	}
	if err := p.waitNetworkManagerActivated(ctx, device.Path, activePath, operation); err != nil {
		cleanupErr := p.cleanupNetworkManagerConnection(device.Path, activePath, operation)
		if cleanupErr != nil {
			return "", domain.VerificationFailed(
				operation,
				"cellular activation failed and NetworkManager cleanup also failed",
				errors.Join(err, cleanupErr),
			)
		}
		return "", err
	}
	return activePath, nil
}

func (p *Provider) deactivateOwnedNetworkManagerData(
	ctx context.Context,
	modemPath dbus.ObjectPath,
	lineID string,
) (bool, error) {
	const operation = "apply_device_configuration"
	device, err := p.findNetworkManagerDevice(ctx, modemPath, operation)
	if err != nil {
		return false, err
	}
	if device.ActiveConnection == networkManagerNoObject {
		return false, nil
	}
	active, err := p.readNetworkManagerActive(ctx, device.ActiveConnection, operation)
	if err != nil {
		return false, err
	}
	if active.ID != networkManagerConnectionID(lineID) {
		return false, domain.Conflict(
			operation,
			"the active cellular connection is managed outside ModemDeck",
		)
	}
	if err := p.deactivateNetworkManagerConnection(
		ctx,
		device.Path,
		device.ActiveConnection,
		operation,
	); err != nil {
		return false, err
	}
	return true, nil
}

func (p *Provider) findNetworkManagerDevice(
	ctx context.Context,
	modemPath dbus.ObjectPath,
	operation string,
) (networkManagerDevice, error) {
	body, err := p.callDestination(
		ctx,
		networkManagerServiceName,
		networkManagerPath,
		networkManagerInterface+".GetDevices",
		operation,
		"NetworkManager device discovery is unavailable",
	)
	if err != nil {
		return networkManagerDevice{}, err
	}
	var paths []dbus.ObjectPath
	if err := dbus.Store(body, &paths); err != nil {
		return networkManagerDevice{}, domain.Internal(
			operation,
			"NetworkManager returned a malformed device list",
			err,
		)
	}
	sort.Slice(paths, func(i, j int) bool {
		return paths[i] < paths[j]
	})
	for _, path := range paths {
		properties, err := p.readNetworkManagerProperties(
			ctx,
			path,
			networkManagerDeviceInterface,
			operation,
		)
		if err != nil {
			return networkManagerDevice{}, err
		}
		udi, _ := stringProperty(properties, "Udi")
		deviceType, _ := uint32Property(properties, "DeviceType")
		if strings.TrimSpace(udi) != string(modemPath) ||
			deviceType != networkManagerDeviceTypeModem {
			continue
		}
		state, _ := uint32Property(properties, "State")
		activePath, _ := objectPathProperty(properties, "ActiveConnection")
		if activePath == "" {
			activePath = networkManagerNoObject
		}
		_, reason := networkManagerStateReason(properties)
		return networkManagerDevice{
			Path:             path,
			State:            state,
			StateReason:      reason,
			ActiveConnection: activePath,
		}, nil
	}
	return networkManagerDevice{}, domain.NotSupported(
		operation,
		"NetworkManager does not expose this ModemManager modem as a cellular device",
	)
}

func (p *Provider) readNetworkManagerActive(
	ctx context.Context,
	path dbus.ObjectPath,
	operation string,
) (networkManagerActiveConnection, error) {
	properties, err := p.readNetworkManagerProperties(
		ctx,
		path,
		networkManagerActiveInterface,
		operation,
	)
	if err != nil {
		return networkManagerActiveConnection{}, err
	}
	id, _ := stringProperty(properties, "Id")
	state, _ := uint32Property(properties, "State")
	return networkManagerActiveConnection{
		ID:    strings.TrimSpace(id),
		State: state,
	}, nil
}

func (p *Provider) readNetworkManagerProperties(
	ctx context.Context,
	path dbus.ObjectPath,
	iface string,
	operation string,
) (Properties, error) {
	body, err := p.callDestination(
		ctx,
		networkManagerServiceName,
		path,
		propertiesInterface+".GetAll",
		operation,
		"NetworkManager state could not be read",
		iface,
	)
	if err != nil {
		return nil, err
	}
	properties := Properties{}
	if err := dbus.Store(body, &properties); err != nil {
		return nil, domain.Internal(
			operation,
			"NetworkManager returned malformed properties",
			err,
		)
	}
	return properties, nil
}

func (p *Provider) waitNetworkManagerActivated(
	ctx context.Context,
	devicePath dbus.ObjectPath,
	activePath dbus.ObjectPath,
	operation string,
) error {
	ticker := time.NewTicker(networkManagerPollInterval)
	defer ticker.Stop()
	for {
		active, err := p.readNetworkManagerActive(ctx, activePath, operation)
		if err != nil {
			if operationError, ok := domain.AsOperationError(err); ok &&
				operationError.Code == domain.ErrorNotFound {
				return p.networkManagerActivationFailure(ctx, devicePath, operation)
			}
			return err
		}
		switch active.State {
		case networkManagerActiveActivated:
			return nil
		case networkManagerActiveDeactivating, networkManagerActiveDeactivated:
			return p.networkManagerActivationFailure(ctx, devicePath, operation)
		}
		select {
		case <-ctx.Done():
			return domain.Unavailable(
				operation,
				"NetworkManager cellular activation timed out",
				ctx.Err(),
			)
		case <-ticker.C:
		}
	}
}

func (p *Provider) deactivateNetworkManagerConnection(
	ctx context.Context,
	devicePath dbus.ObjectPath,
	activePath dbus.ObjectPath,
	operation string,
) error {
	if _, err := p.callDestination(
		ctx,
		networkManagerServiceName,
		networkManagerPath,
		networkManagerInterface+".DeactivateConnection",
		operation,
		"NetworkManager failed to stop the cellular data connection",
		activePath,
	); err != nil {
		if operationError, ok := domain.AsOperationError(err); ok &&
			operationError.Code == domain.ErrorNotFound {
			device, readErr := p.findNetworkManagerDeviceByPath(
				ctx,
				devicePath,
				operation,
			)
			if readErr != nil {
				return errors.Join(err, readErr)
			}
			if device.ActiveConnection == networkManagerNoObject ||
				device.ActiveConnection != activePath {
				return nil
			}
		}
		return err
	}
	ticker := time.NewTicker(networkManagerPollInterval)
	defer ticker.Stop()
	for {
		device, err := p.findNetworkManagerDeviceByPath(ctx, devicePath, operation)
		if err != nil {
			return err
		}
		if device.ActiveConnection == networkManagerNoObject {
			return nil
		}
		select {
		case <-ctx.Done():
			return domain.Unavailable(
				operation,
				"NetworkManager cellular deactivation timed out",
				ctx.Err(),
			)
		case <-ticker.C:
		}
	}
}

func (p *Provider) findNetworkManagerDeviceByPath(
	ctx context.Context,
	path dbus.ObjectPath,
	operation string,
) (networkManagerDevice, error) {
	properties, err := p.readNetworkManagerProperties(
		ctx,
		path,
		networkManagerDeviceInterface,
		operation,
	)
	if err != nil {
		return networkManagerDevice{}, err
	}
	state, _ := uint32Property(properties, "State")
	activePath, _ := objectPathProperty(properties, "ActiveConnection")
	if activePath == "" {
		activePath = networkManagerNoObject
	}
	_, reason := networkManagerStateReason(properties)
	return networkManagerDevice{
		Path:             path,
		State:            state,
		StateReason:      reason,
		ActiveConnection: activePath,
	}, nil
}

func (p *Provider) networkManagerActivationFailure(
	ctx context.Context,
	devicePath dbus.ObjectPath,
	operation string,
) error {
	device, err := p.findNetworkManagerDeviceByPath(ctx, devicePath, operation)
	if err != nil {
		return err
	}
	message := "NetworkManager cellular activation failed: " +
		networkManagerStateReasonName(device.StateReason)
	if device.StateReason == 31 {
		return domain.NetworkRejected(operation, message, nil)
	}
	return domain.FailedPrecondition(operation, message, nil)
}

func (p *Provider) cleanupNetworkManagerConnection(
	devicePath dbus.ObjectPath,
	activePath dbus.ObjectPath,
	operation string,
) error {
	ctx, cancel := context.WithTimeout(context.Background(), networkManagerCleanupTimeout)
	defer cancel()
	return p.deactivateNetworkManagerConnection(ctx, devicePath, activePath, operation)
}

func (p *Provider) cleanupOwnedNetworkManagerData(
	modemPath dbus.ObjectPath,
	lineID string,
) error {
	ctx, cancel := context.WithTimeout(context.Background(), networkManagerCleanupTimeout)
	defer cancel()
	_, err := p.deactivateOwnedNetworkManagerData(ctx, modemPath, lineID)
	return err
}

func networkManagerConnectionSettings(
	connectionID string,
	requestedAPN string,
	automaticAPN string,
	ipFamily uint32,
) map[string]map[string]dbus.Variant {
	gsm := map[string]dbus.Variant{}
	apn := strings.TrimSpace(requestedAPN)
	if apn == "" {
		apn = strings.TrimSpace(automaticAPN)
	}
	if apn == "" {
		gsm["auto-config"] = dbus.MakeVariant(true)
	} else {
		gsm["apn"] = dbus.MakeVariant(apn)
	}

	ipv4Method := "disabled"
	ipv6Method := "disabled"
	switch ipFamily {
	case bearerIPFamilyIPv4:
		ipv4Method = "auto"
	case bearerIPFamilyIPv6:
		ipv6Method = "auto"
	default:
		ipv4Method = "auto"
		ipv6Method = "auto"
	}
	return map[string]map[string]dbus.Variant{
		"connection": {
			"id":          dbus.MakeVariant(connectionID),
			"type":        dbus.MakeVariant("gsm"),
			"autoconnect": dbus.MakeVariant(false),
		},
		"gsm": gsm,
		"ipv4": {
			"method": dbus.MakeVariant(ipv4Method),
		},
		"ipv6": {
			"method": dbus.MakeVariant(ipv6Method),
		},
	}
}

func networkManagerConnectionID(lineID string) string {
	return networkManagerConnectionPrefix + strings.TrimSpace(lineID)
}

func networkManagerStateReason(properties Properties) (uint32, uint32) {
	value, ok := propertyValue(properties, "StateReason")
	if !ok {
		return 0, 0
	}
	values, ok := value.([]any)
	if !ok || len(values) != 2 {
		return 0, 0
	}
	state, stateOK := values[0].(uint32)
	reason, reasonOK := values[1].(uint32)
	if !stateOK || !reasonOK {
		return 0, 0
	}
	return state, reason
}

func networkManagerStateReasonName(reason uint32) string {
	names := map[uint32]string{
		4:  "the modem could not be prepared for configuration",
		5:  "IP configuration was unavailable or timed out",
		7:  "the connection requires credentials",
		23: "the modem is busy",
		25: "the modem could not establish a carrier",
		26: "the modem connection timed out",
		27: "the modem connection attempt failed",
		28: "modem initialization failed",
		29: "the APN was rejected",
		30: "the modem is not searching for a network",
		31: "network registration was denied",
		32: "network registration timed out",
		33: "network registration failed",
		34: "the SIM PIN check failed",
		35: "required modem firmware is missing",
		43: "the modem was not found",
		45: "the SIM is not inserted",
		46: "the SIM PIN is required",
		47: "the SIM PUK is required",
		48: "the SIM is invalid",
		52: "ModemManager is unavailable",
		57: "the modem failed or became unavailable",
		59: "the SIM PIN was incorrect",
		65: "the selected IP method is unsupported",
		79: "automatic APN selection requires an operator code",
	}
	if name, found := names[reason]; found {
		return name
	}
	if reason == 0 {
		return "no failure reason was reported"
	}
	return fmt.Sprintf("NetworkManager reason %d", reason)
}
