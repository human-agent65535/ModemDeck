package modemmanager

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/godbus/dbus/v5"
	"github.com/human-agent65535/modemdeck/agent/internal/domain"
)

const (
	bearerPollInterval   = 200 * time.Millisecond
	bearerCleanupTimeout = 10 * time.Second
)

// ReconcileDataPlane restores only network state backed by an exact
// ModemDeck-owned bearer in the current ModemManager process. Stale records
// from a previous D-Bus owner are released without touching modem objects.
func (p *Provider) ReconcileDataPlane(ctx context.Context) error {
	const operation = "reconcile_data_plane"
	if _, err := p.resolveProviderIdentity(ctx, operation); err != nil {
		return err
	}
	providerEpoch := p.ids.providerEpoch()
	ownedLines := make(map[string]struct{})
	for _, owned := range p.ownedBearers.list() {
		if owned.ProviderEpoch == providerEpoch {
			ownedLines[owned.LineID] = struct{}{}
		}
	}
	for _, lineID := range p.dataPlane.OwnedLines() {
		if _, found := ownedLines[lineID]; found {
			continue
		}
		if err := p.dataPlane.Release(ctx, lineID); err != nil {
			return domain.VerificationFailed(
				operation,
				"orphaned ModemDeck network state could not be removed",
				err,
			)
		}
	}
	for _, owned := range p.ownedBearers.list() {
		if err := ctx.Err(); err != nil {
			return domain.Unavailable(operation, "data plane reconciliation was canceled", err)
		}
		if owned.ProviderEpoch != providerEpoch {
			if err := p.releaseStaleOwnedBearer(owned.LineID); err != nil {
				return domain.VerificationFailed(
					operation,
					"stale network state could not be removed",
					err,
				)
			}
			continue
		}
		connection, found, err := p.readBearerConnection(
			ctx,
			owned.BearerPath,
			operation,
		)
		if err != nil {
			return err
		}
		if !found || !connection.Connected {
			if err := p.dataPlane.Release(ctx, owned.LineID); err != nil {
				return domain.VerificationFailed(
					operation,
					"disconnected bearer network state could not be removed",
					err,
				)
			}
			if !found {
				if err := p.ownedBearers.remove(owned.LineID); err != nil {
					return domain.VerificationFailed(
						operation,
						"missing bearer ownership state could not be removed",
						err,
					)
				}
			}
			continue
		}
		if !bearerConnectionReady(connection) {
			return domain.FailedPrecondition(
				operation,
				fmt.Sprintf(
					"owned bearer %s is connected without complete IP configuration",
					owned.BearerPath,
				),
				nil,
			)
		}
		if err := p.dataPlane.Configure(ctx, owned.LineID, connection); err != nil {
			return domain.FailedPrecondition(
				operation,
				fmt.Sprintf(
					"owned bearer %s network state could not be restored",
					owned.BearerPath,
				),
				err,
			)
		}
	}
	return nil
}

func (p *Provider) activateOwnedBearerData(
	ctx context.Context,
	modemPath dbus.ObjectPath,
	lineID string,
	requestedAPN string,
	automaticAPN string,
	ipFamily uint32,
	reuseExisting bool,
) (dbus.ObjectPath, error) {
	const operation = "apply_device_configuration"
	providerEpoch := p.ids.providerEpoch()
	if providerEpoch == "" {
		return "", domain.Unavailable(
			operation,
			"ModemManager owner identity is unavailable",
			nil,
		)
	}
	if saved, found := p.ownedBearers.getAny(lineID); found &&
		saved.ProviderEpoch != providerEpoch {
		if err := p.releaseStaleOwnedBearer(lineID); err != nil {
			return "", domain.VerificationFailed(
				operation,
				"network state from a previous ModemManager process could not be removed",
				err,
			)
		}
	}

	effectiveAPN := strings.TrimSpace(requestedAPN)
	if effectiveAPN == "" {
		effectiveAPN = strings.TrimSpace(automaticAPN)
	}
	if owned, found := p.ownedBearers.get(lineID, providerEpoch); found {
		if owned.ModemPath != modemPath {
			return "", domain.Conflict(
				operation,
				"the saved ModemDeck bearer belongs to a different modem path",
			)
		}
		connection, found, err := p.readBearerConnection(
			ctx,
			owned.BearerPath,
			operation,
		)
		if err != nil {
			return "", err
		}
		if !found {
			if err := p.releaseStaleOwnedBearer(lineID); err != nil {
				return "", domain.VerificationFailed(
					operation,
					"stale ModemDeck bearer state could not be removed",
					err,
				)
			}
		} else if reuseExisting &&
			connection.Connected &&
			dataConnectionMatches(connection, effectiveAPN, ipFamily) {
			if err := p.dataPlane.Configure(ctx, lineID, connection); err != nil {
				return "", domain.FailedPrecondition(
					operation,
					"the existing ModemDeck bearer network state could not be restored",
					err,
				)
			}
			return owned.BearerPath, nil
		} else {
			if _, err := p.deactivateOwnedBearerData(ctx, modemPath, lineID); err != nil {
				return "", err
			}
		}
	}

	objects, err := p.managedObjects(ctx, operation)
	if err != nil {
		return "", err
	}
	if conflict, found, err := p.connectedExternalDefaultBearer(
		ctx,
		objects,
		modemPath,
		operation,
	); err != nil {
		return "", err
	} else if found {
		return "", domain.Conflict(
			operation,
			fmt.Sprintf(
				"the modem already has a connected data bearer not owned by ModemDeck (%s)",
				conflict,
			),
		)
	}

	properties := bearerProperties(requestedAPN, automaticAPN, ipFamily)
	body, err := p.call(
		ctx,
		modemPath,
		modemInterface+".CreateBearer",
		operation,
		"ModemManager failed to create the packet data bearer",
		properties,
	)
	if err != nil {
		return "", err
	}
	var bearerPath dbus.ObjectPath
	if err := dbus.Store(body, &bearerPath); err != nil {
		return "", domain.Internal(
			operation,
			"ModemManager returned a malformed bearer path",
			err,
		)
	}
	if !bearerPath.IsValid() || bearerPath == "/" {
		return "", domain.Internal(
			operation,
			"ModemManager returned an invalid bearer path",
			nil,
		)
	}

	owned := ownedBearer{
		LineID:        lineID,
		ProviderEpoch: providerEpoch,
		ModemPath:     modemPath,
		BearerPath:    bearerPath,
		APN:           effectiveAPN,
		IPFamily:      ipFamily,
	}
	if err := p.ownedBearers.put(owned); err != nil {
		cleanupErr := p.deleteUnconnectedBearer(modemPath, bearerPath)
		return "", domain.VerificationFailed(
			operation,
			"the created bearer could not be recorded as ModemDeck-owned",
			errors.Join(err, cleanupErr),
		)
	}

	if _, err := p.call(
		ctx,
		bearerPath,
		bearerInterface+".Connect",
		operation,
		"ModemManager failed to connect the packet data bearer",
	); err != nil {
		if operationError, ok := domain.AsOperationError(err); ok &&
			operationError.Code == domain.ErrorNetworkRejected &&
			effectiveAPN != "" {
			err = domain.NewOperationError(
				operationError.Code,
				operation,
				fmt.Sprintf("the mobile network rejected APN %q", effectiveAPN),
				err,
			)
		}
		return "", p.activationFailureWithCleanup(modemPath, lineID, err)
	}

	connection, err := p.waitBearerReady(ctx, bearerPath, operation)
	if err != nil {
		return "", p.activationFailureWithCleanup(modemPath, lineID, err)
	}
	if !dataConnectionMatches(connection, effectiveAPN, ipFamily) {
		verificationErr := errors.New(
			"ModemManager connected a bearer that does not match the requested APN and IP family",
		)
		return "", p.activationFailureWithCleanup(
			modemPath,
			lineID,
			domain.VerificationFailed(
				operation,
				verificationErr.Error(),
				verificationErr,
			),
		)
	}
	if err := p.dataPlane.Configure(ctx, lineID, connection); err != nil {
		return "", p.activationFailureWithCleanup(
			modemPath,
			lineID,
			domain.FailedPrecondition(
				operation,
				"the connected bearer could not be configured in the host network namespace",
				err,
			),
		)
	}
	return bearerPath, nil
}

func (p *Provider) deactivateOwnedBearerData(
	ctx context.Context,
	modemPath dbus.ObjectPath,
	lineID string,
) (bool, error) {
	const operation = "apply_device_configuration"
	if saved, found := p.ownedBearers.getAny(lineID); found &&
		saved.ProviderEpoch != p.ids.providerEpoch() {
		if err := p.releaseStaleOwnedBearer(lineID); err != nil {
			return false, domain.VerificationFailed(
				operation,
				"network state from a previous ModemManager process could not be removed",
				err,
			)
		}
		return false, nil
	}
	owned, found := p.ownedBearers.get(lineID, p.ids.providerEpoch())
	if !found {
		return false, nil
	}
	if owned.ModemPath != modemPath {
		return false, domain.Conflict(
			operation,
			"the saved ModemDeck bearer belongs to a different modem path",
		)
	}
	connection, bearerFound, err := p.readBearerConnection(
		ctx,
		owned.BearerPath,
		operation,
	)
	if err != nil {
		return false, err
	}
	if err := p.dataPlane.Release(ctx, lineID); err != nil {
		return false, domain.VerificationFailed(
			operation,
			"the ModemDeck bearer network state could not be released before disconnect",
			err,
		)
	}
	if bearerFound && connection.Connected {
		if _, err := p.call(
			ctx,
			owned.BearerPath,
			bearerInterface+".Disconnect",
			operation,
			"ModemManager failed to disconnect the packet data bearer",
		); err != nil && !operationErrorIsNotFound(err) {
			return false, err
		}
	}
	if _, err := p.call(
		ctx,
		modemPath,
		modemInterface+".DeleteBearer",
		operation,
		"ModemManager failed to delete the packet data bearer",
		owned.BearerPath,
	); err != nil && !operationErrorIsNotFound(err) {
		return false, err
	}
	if err := p.ownedBearers.remove(lineID); err != nil {
		return false, domain.VerificationFailed(
			operation,
			"the disconnected bearer ownership record could not be removed",
			err,
		)
	}
	return true, nil
}

func (p *Provider) waitBearerReady(
	ctx context.Context,
	bearerPath dbus.ObjectPath,
	operation string,
) (domain.DataConnection, error) {
	ticker := time.NewTicker(bearerPollInterval)
	defer ticker.Stop()
	for {
		connection, found, err := p.readBearerConnection(ctx, bearerPath, operation)
		if err != nil {
			return domain.DataConnection{}, err
		}
		if !found {
			return domain.DataConnection{}, domain.VerificationFailed(
				operation,
				"the packet data bearer disappeared during activation",
				nil,
			)
		}
		if bearerConnectionReady(connection) {
			return connection, nil
		}
		select {
		case <-ctx.Done():
			return domain.DataConnection{}, domain.Unavailable(
				operation,
				"ModemManager bearer activation timed out before IP configuration became available",
				ctx.Err(),
			)
		case <-ticker.C:
		}
	}
}

func (p *Provider) readBearerConnection(
	ctx context.Context,
	bearerPath dbus.ObjectPath,
	operation string,
) (domain.DataConnection, bool, error) {
	properties, found, err := p.referencedBearerProperties(
		ctx,
		nil,
		bearerPath,
		operation,
	)
	if err != nil || !found {
		return domain.DataConnection{}, found, err
	}
	connection, err := parseDataConnection(p.ids.bearerID(bearerPath), properties)
	if err != nil {
		return domain.DataConnection{}, false, domain.Internal(
			operation,
			"ModemManager bearer properties were malformed",
			err,
		)
	}
	return connection, true, nil
}

func (p *Provider) connectedExternalDefaultBearer(
	ctx context.Context,
	objects ManagedObjects,
	modemPath dbus.ObjectPath,
	operation string,
) (string, bool, error) {
	interfaces, found := objects[modemPath]
	if !found {
		return "", false, domain.NotFound(operation, "modem object was not found")
	}
	modemProperties, found := interfaces[modemInterface]
	if !found {
		return "", false, domain.NotSupported(operation, "modem interface is unavailable")
	}
	paths, _ := objectPathValuesProperty(modemProperties, "Bearers")
	for _, path := range paths {
		properties, found, err := p.referencedBearerProperties(
			ctx,
			objects,
			path,
			operation,
		)
		if err != nil {
			return "", false, err
		}
		if !found {
			continue
		}
		connection, err := parseDataConnection(p.ids.bearerID(path), properties)
		if err != nil {
			return "", false, domain.Internal(
				operation,
				"ModemManager bearer properties were malformed",
				err,
			)
		}
		if connection.Connected && dataConnectionIsDefault(connection) {
			return string(path), true, nil
		}
	}
	return "", false, nil
}

func (p *Provider) activationFailureWithCleanup(
	modemPath dbus.ObjectPath,
	lineID string,
	activationErr error,
) error {
	if cleanupErr := p.cleanupOwnedBearerData(modemPath, lineID); cleanupErr != nil {
		return domain.VerificationFailed(
			"apply_device_configuration",
			"packet data activation failed and ModemDeck bearer cleanup also failed",
			errors.Join(activationErr, cleanupErr),
		)
	}
	return activationErr
}

func (p *Provider) cleanupOwnedBearerData(
	modemPath dbus.ObjectPath,
	lineID string,
) error {
	ctx, cancel := context.WithTimeout(context.Background(), bearerCleanupTimeout)
	defer cancel()
	_, err := p.deactivateOwnedBearerData(ctx, modemPath, lineID)
	return err
}

func (p *Provider) releaseStaleOwnedBearer(lineID string) error {
	ctx, cancel := context.WithTimeout(context.Background(), bearerCleanupTimeout)
	defer cancel()
	if err := p.dataPlane.Release(ctx, lineID); err != nil {
		return err
	}
	return p.ownedBearers.remove(lineID)
}

func (p *Provider) deleteUnconnectedBearer(
	modemPath dbus.ObjectPath,
	bearerPath dbus.ObjectPath,
) error {
	ctx, cancel := context.WithTimeout(context.Background(), bearerCleanupTimeout)
	defer cancel()
	_, err := p.call(
		ctx,
		modemPath,
		modemInterface+".DeleteBearer",
		"apply_device_configuration",
		"ModemManager failed to delete an unrecorded packet data bearer",
		bearerPath,
	)
	if operationErrorIsNotFound(err) {
		return nil
	}
	return err
}

func bearerProperties(
	requestedAPN string,
	automaticAPN string,
	ipFamily uint32,
) map[string]dbus.Variant {
	properties := map[string]dbus.Variant{
		"apn-type":      dbus.MakeVariant(domain.APNTypeDefault),
		"allow-roaming": dbus.MakeVariant(true),
	}
	apn := strings.TrimSpace(requestedAPN)
	if apn == "" {
		apn = strings.TrimSpace(automaticAPN)
	}
	if apn != "" {
		properties["apn"] = dbus.MakeVariant(apn)
	}
	if ipFamily != 0 {
		properties["ip-type"] = dbus.MakeVariant(ipFamily)
	}
	return properties
}

func bearerConnectionReady(connection domain.DataConnection) bool {
	if !connection.Connected || strings.TrimSpace(connection.Interface) == "" {
		return false
	}
	return ipConfigurationAvailable(connection.IPv4) ||
		ipConfigurationAvailable(connection.IPv6)
}

func dataConnectionMatches(
	connection domain.DataConnection,
	apn string,
	ipFamily uint32,
) bool {
	if strings.TrimSpace(apn) != "" &&
		!strings.EqualFold(strings.TrimSpace(connection.APN), strings.TrimSpace(apn)) {
		return false
	}
	return connectedFamilyMatches(connection, ipFamily)
}

func dataConnectionIsDefault(connection domain.DataConnection) bool {
	if connection.BearerType != 0 {
		return connection.BearerType == bearerTypeDefault
	}
	return connection.APNType&domain.APNTypeDefault != 0
}

func operationErrorIsNotFound(err error) bool {
	if err == nil {
		return false
	}
	operationError, ok := domain.AsOperationError(err)
	return ok && operationError.Code == domain.ErrorNotFound
}
