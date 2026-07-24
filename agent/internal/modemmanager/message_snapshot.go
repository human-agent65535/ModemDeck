package modemmanager

import (
	"context"
	"sort"
	"strings"

	"github.com/godbus/dbus/v5"
	"github.com/human-agent65535/modemdeck/agent/internal/domain"
)

func (p *Provider) hydrateMessages(
	ctx context.Context,
	operation string,
	objects ManagedObjects,
) (ManagedObjects, error) {
	modemPaths := make([]dbus.ObjectPath, 0)
	for path, interfaces := range objects {
		if _, found := interfaces[modemInterface]; !found {
			continue
		}
		if _, found := interfaces[messagingInterface]; found {
			modemPaths = append(modemPaths, path)
		}
	}
	sort.Slice(modemPaths, func(i, j int) bool {
		return modemPaths[i] < modemPaths[j]
	})

	for _, modemPath := range modemPaths {
		body, err := p.call(
			ctx,
			modemPath,
			messagingInterface+".List",
			operation,
			"ModemManager failed to list SMS messages",
		)
		if err != nil {
			return nil, err
		}
		var listedPaths []dbus.ObjectPath
		if err := dbus.Store(body, &listedPaths); err != nil {
			return nil, domain.Internal(
				operation,
				"ModemManager SMS list response was malformed",
				err,
			)
		}

		listedPaths, err = validMessagePaths(operation, listedPaths)
		if err != nil {
			return nil, err
		}
		hydratedPaths := make([]dbus.ObjectPath, 0, len(listedPaths))
		for _, messagePath := range listedPaths {
			if interfaces, found := objects[messagePath]; found {
				if _, found := interfaces[smsInterface]; found {
					hydratedPaths = append(hydratedPaths, messagePath)
					continue
				}
			}

			properties, found, err := p.readMessageProperties(
				ctx,
				operation,
				messagePath,
			)
			if err != nil {
				return nil, err
			}
			if !found {
				continue
			}
			interfaces := objects[messagePath]
			if interfaces == nil {
				interfaces = Interfaces{}
			}
			interfaces[smsInterface] = properties
			objects[messagePath] = interfaces
			hydratedPaths = append(hydratedPaths, messagePath)
		}

		messagingProperties := objects[modemPath][messagingInterface]
		if messagingProperties == nil {
			messagingProperties = Properties{}
			objects[modemPath][messagingInterface] = messagingProperties
		}
		messagingProperties["Messages"] = dbus.MakeVariant(hydratedPaths)
	}
	return objects, nil
}

func (p *Provider) readMessageProperties(
	ctx context.Context,
	operation string,
	path dbus.ObjectPath,
) (Properties, bool, error) {
	body, err := p.call(
		ctx,
		path,
		propertiesInterface+".GetAll",
		operation,
		"ModemManager failed to read an SMS message",
		smsInterface,
	)
	if err != nil {
		if operationError, ok := domain.AsOperationError(err); ok &&
			operationError.Code == domain.ErrorNotFound {
			return nil, false, nil
		}
		return nil, false, err
	}
	properties := Properties{}
	if err := dbus.Store(body, &properties); err != nil {
		return nil, false, domain.Internal(
			operation,
			"ModemManager SMS properties response was malformed",
			err,
		)
	}
	return properties, true, nil
}

func validMessagePaths(
	operation string,
	paths []dbus.ObjectPath,
) ([]dbus.ObjectPath, error) {
	result := make([]dbus.ObjectPath, 0, len(paths))
	seen := make(map[dbus.ObjectPath]struct{}, len(paths))
	for _, path := range paths {
		if !path.IsValid() ||
			!strings.HasPrefix(string(path), "/org/freedesktop/ModemManager1/SMS/") {
			return nil, domain.Internal(
				operation,
				"ModemManager returned an invalid SMS object path",
				nil,
			)
		}
		if _, duplicate := seen[path]; duplicate {
			continue
		}
		seen[path] = struct{}{}
		result = append(result, path)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i] < result[j]
	})
	return result, nil
}
