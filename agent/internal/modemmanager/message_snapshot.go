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
	epoch := ""
	if p.ids != nil {
		epoch = p.ids.providerEpoch()
	}
	err := p.messageProperties.synchronize(
		epoch,
		func(cache *messagePropertyCache) error {
			return p.hydrateMessagesLocked(ctx, operation, objects, cache)
		},
	)
	return objects, err
}

func (p *Provider) hydrateMessagesLocked(
	ctx context.Context,
	operation string,
	objects ManagedObjects,
	cache *messagePropertyCache,
) error {
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

	listedKeys := make(map[messagePropertyCacheKey]struct{})
	for _, modemPath := range modemPaths {
		if !modemServiceHydrationReady(objects[modemPath]) {
			messagingProperties := objects[modemPath][messagingInterface]
			if messagingProperties == nil {
				messagingProperties = Properties{}
				objects[modemPath][messagingInterface] = messagingProperties
			}
			messagingProperties["Messages"] = dbus.MakeVariant([]dbus.ObjectPath{})
			continue
		}
		body, err := p.call(
			ctx,
			modemPath,
			messagingInterface+".List",
			operation,
			"ModemManager failed to list SMS messages",
		)
		if err != nil {
			if serviceHydrationUnavailable(err) {
				messagingProperties := objects[modemPath][messagingInterface]
				if messagingProperties == nil {
					messagingProperties = Properties{}
					objects[modemPath][messagingInterface] = messagingProperties
				}
				messagingProperties["Messages"] = dbus.MakeVariant([]dbus.ObjectPath{})
				continue
			}
			return err
		}
		var listedPaths []dbus.ObjectPath
		if err := dbus.Store(body, &listedPaths); err != nil {
			return domain.Internal(
				operation,
				"ModemManager SMS list response was malformed",
				err,
			)
		}

		listedPaths, err = validMessagePaths(operation, listedPaths)
		if err != nil {
			return err
		}
		hydratedPaths := make([]dbus.ObjectPath, 0, len(listedPaths))
		for _, messagePath := range listedPaths {
			key := messagePropertyCacheKey{
				modemPath:   modemPath,
				messagePath: messagePath,
			}
			listedKeys[key] = struct{}{}

			if interfaces, found := objects[messagePath]; found {
				if properties, found := interfaces[smsInterface]; found {
					cache.storeLocked(key, properties)
					hydratedPaths = append(hydratedPaths, messagePath)
					continue
				}
			}

			if properties, found := cache.getLocked(key); found {
				interfaces := objects[messagePath]
				if interfaces == nil {
					interfaces = Interfaces{}
				}
				interfaces[smsInterface] = properties
				objects[messagePath] = interfaces
				hydratedPaths = append(hydratedPaths, messagePath)
				continue
			}

			properties, found, err := p.readMessageProperties(
				ctx,
				operation,
				messagePath,
			)
			if err != nil {
				return err
			}
			if !found {
				cache.deleteLocked(key)
				continue
			}
			cache.storeLocked(key, properties)
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
	cache.reconcileLocked(listedKeys)
	return nil
}

func modemServiceHydrationReady(interfaces Interfaces) bool {
	state, known := modemServiceHydrationState(interfaces)
	return known && state >= modemStateEnabled
}

func modemServiceHydrationState(interfaces Interfaces) (int32, bool) {
	return int32Property(interfaces[modemInterface], "State")
}

func serviceHydrationUnavailable(err error) bool {
	operationError, ok := domain.AsOperationError(err)
	return ok && operationError.Code == domain.ErrorFailedPrecondition
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
