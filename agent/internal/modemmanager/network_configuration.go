package modemmanager

import (
	"context"
	"sort"

	"github.com/human-agent65535/modemdeck/agent/internal/domain"
)

// NetworkConfigurations returns the authoritative packet-data view for all
// routable lines from one ModemManager object observation. It deliberately
// excludes device settings and provisioning reads.
func (p *Provider) NetworkConfigurations(
	ctx context.Context,
) ([]domain.LineNetworkConfiguration, error) {
	const operation = "network_configurations"

	identity, err := p.resolveProviderIdentity(ctx, operation)
	if err != nil {
		return nil, err
	}
	objects, err := p.managedObjects(ctx, operation)
	if err != nil {
		return nil, err
	}
	parsed := ParseManagedObjects(objects, identity)
	configurations := make(
		[]domain.LineNetworkConfiguration,
		0,
		len(parsed.Lines),
	)
	for _, line := range parsed.Lines {
		if line.ID == "" {
			continue
		}
		modemPath, found := parsed.LinePaths[line.ID]
		if !found {
			return nil, domain.Internal(
				operation,
				"ModemManager line path was missing from the object snapshot",
				nil,
			)
		}
		interfaces, found := objects[modemPath]
		if !found {
			return nil, domain.Internal(
				operation,
				"ModemManager line object was missing from the object snapshot",
				nil,
			)
		}
		modemProperties, found := interfaces[modemInterface]
		if !found {
			return nil, domain.Internal(
				operation,
				"ModemManager line did not expose the modem interface",
				nil,
			)
		}
		connections, err := p.dataConnectionsFromModem(
			ctx,
			objects,
			modemProperties,
			operation,
		)
		if err != nil {
			return nil, err
		}
		configurations = append(configurations, domain.LineNetworkConfiguration{
			LineID:          line.ID,
			DataConnections: connections,
		})
	}
	sort.Slice(configurations, func(i, j int) bool {
		return configurations[i].LineID < configurations[j].LineID
	})
	return configurations, nil
}

func (p *Provider) dataConnectionsFromModem(
	ctx context.Context,
	objects ManagedObjects,
	modemProperties Properties,
	operation string,
) ([]domain.DataConnection, error) {
	bearerPaths, _ := objectPathValuesProperty(modemProperties, "Bearers")
	connections := make([]domain.DataConnection, 0, len(bearerPaths))
	for _, bearerPath := range bearerPaths {
		bearerProperties, found, err := p.referencedBearerProperties(
			ctx,
			objects,
			bearerPath,
			operation,
		)
		if err != nil {
			return nil, err
		}
		if !found {
			continue
		}
		connection, err := parseDataConnection(
			p.ids.bearerID(bearerPath),
			bearerProperties,
		)
		if err != nil {
			return nil, domain.Internal(
				operation,
				"ModemManager bearer properties were malformed",
				err,
			)
		}
		connections = append(connections, connection)
	}
	sort.Slice(connections, func(i, j int) bool {
		return connections[i].ID < connections[j].ID
	})
	return connections, nil
}
