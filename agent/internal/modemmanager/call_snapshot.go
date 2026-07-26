package modemmanager

import (
	"context"
	"sort"
	"strings"

	"github.com/godbus/dbus/v5"
	"github.com/human-agent65535/modemdeck/agent/internal/domain"
)

// hydrateCalls resolves Voice.Calls references into complete Call objects.
// ModemManager exposes call paths through the Voice interface, but those call
// objects are not guaranteed to be included in GetManagedObjects.
func (p *Provider) hydrateCalls(
	ctx context.Context,
	operation string,
	objects ManagedObjects,
) (ManagedObjects, error) {
	modemPaths := make([]dbus.ObjectPath, 0)
	for path, interfaces := range objects {
		if _, found := interfaces[modemInterface]; !found {
			continue
		}
		if _, found := interfaces[voiceInterface]; found {
			modemPaths = append(modemPaths, path)
		}
	}
	sort.Slice(modemPaths, func(i, j int) bool {
		return modemPaths[i] < modemPaths[j]
	})

	for _, modemPath := range modemPaths {
		voiceProperties := objects[modemPath][voiceInterface]
		if !modemServiceHydrationReady(objects[modemPath]) {
			if voiceProperties == nil {
				voiceProperties = Properties{}
				objects[modemPath][voiceInterface] = voiceProperties
			}
			voiceProperties["Calls"] = dbus.MakeVariant([]dbus.ObjectPath{})
			continue
		}
		listedPaths, known := objectPathValuesProperty(voiceProperties, "Calls")
		if !known {
			body, err := p.call(
				ctx,
				modemPath,
				voiceInterface+".ListCalls",
				operation,
				"ModemManager failed to list calls",
			)
			if err != nil {
				if serviceHydrationUnavailable(err) {
					listedPaths = []dbus.ObjectPath{}
				} else {
					return nil, err
				}
			} else {
				if err := dbus.Store(body, &listedPaths); err != nil {
					return nil, domain.Internal(
						operation,
						"ModemManager call list response was malformed",
						err,
					)
				}
			}
		}

		listedPaths, err := validCallPaths(operation, listedPaths)
		if err != nil {
			return nil, err
		}
		hydratedPaths := make([]dbus.ObjectPath, 0, len(listedPaths))
		for _, callPath := range listedPaths {
			if interfaces, found := objects[callPath]; found {
				if _, found := interfaces[callInterface]; found {
					hydratedPaths = append(hydratedPaths, callPath)
					continue
				}
			}

			properties, found, err := p.readCallProperties(
				ctx,
				operation,
				callPath,
			)
			if err != nil {
				return nil, err
			}
			if !found {
				continue
			}
			interfaces := objects[callPath]
			if interfaces == nil {
				interfaces = Interfaces{}
			}
			interfaces[callInterface] = properties
			objects[callPath] = interfaces
			hydratedPaths = append(hydratedPaths, callPath)
		}

		if voiceProperties == nil {
			voiceProperties = Properties{}
			objects[modemPath][voiceInterface] = voiceProperties
		}
		voiceProperties["Calls"] = dbus.MakeVariant(hydratedPaths)
	}
	return objects, nil
}

func (p *Provider) readCallProperties(
	ctx context.Context,
	operation string,
	path dbus.ObjectPath,
) (Properties, bool, error) {
	body, err := p.call(
		ctx,
		path,
		propertiesInterface+".GetAll",
		operation,
		"ModemManager failed to read a call",
		callInterface,
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
			"ModemManager call properties response was malformed",
			err,
		)
	}
	return properties, true, nil
}

func validCallPaths(
	operation string,
	paths []dbus.ObjectPath,
) ([]dbus.ObjectPath, error) {
	result := make([]dbus.ObjectPath, 0, len(paths))
	seen := make(map[dbus.ObjectPath]struct{}, len(paths))
	for _, path := range paths {
		if !path.IsValid() ||
			!strings.HasPrefix(string(path), "/org/freedesktop/ModemManager1/Call/") {
			return nil, domain.Internal(
				operation,
				"ModemManager returned an invalid call object path",
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
