package modemmanager

import (
	"context"
	"errors"
	"strings"

	"github.com/godbus/dbus/v5"
	"github.com/human-agent65535/modemdeck/agent/internal/domain"
)

func mapCallError(operation, message string, err error) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return domain.Unavailable(operation, message, err)
	}

	name := dbusErrorName(err)
	switch {
	case name == "org.freedesktop.DBus.Error.InvalidArgs",
		strings.HasSuffix(name, ".Error.Core.InvalidArgs"):
		return domain.NewOperationError(domain.ErrorInvalidArgument, operation, message, err)
	case name == "org.freedesktop.DBus.Error.UnknownObject",
		strings.HasSuffix(name, ".Error.Core.NotFound"):
		return domain.NewOperationError(domain.ErrorNotFound, operation, message, err)
	case name == "org.freedesktop.DBus.Error.UnknownMethod",
		name == "org.freedesktop.DBus.Error.UnknownInterface",
		name == "org.freedesktop.DBus.Error.UnknownProperty",
		strings.HasSuffix(name, ".Error.Core.Unsupported"):
		return domain.NewOperationError(domain.ErrorNotSupported, operation, message, err)
	case name == "org.freedesktop.DBus.Error.AccessDenied",
		name == "org.freedesktop.DBus.Error.AuthFailed",
		strings.HasSuffix(name, ".Error.Core.Unauthorized"):
		return domain.PermissionDenied(operation, message, err)
	case name == "org.freedesktop.DBus.Error.ServiceUnknown",
		name == "org.freedesktop.DBus.Error.NameHasNoOwner",
		name == "org.freedesktop.DBus.Error.NoReply",
		name == "org.freedesktop.DBus.Error.Timeout",
		name == "org.freedesktop.DBus.Error.Disconnected",
		strings.HasSuffix(name, ".Error.Core.Timeout"),
		strings.HasSuffix(name, ".Error.Core.Retry"),
		strings.HasSuffix(name, ".Error.Core.ResetAndRetry"),
		strings.HasSuffix(name, ".Error.Core.NoPlugins"):
		return domain.Unavailable(operation, message, err)
	case strings.HasSuffix(name, ".Error.Core.InProgress"),
		strings.HasSuffix(name, ".Error.Core.WrongState"),
		strings.HasSuffix(name, ".Error.Core.Connected"),
		strings.HasSuffix(name, ".Error.Core.TooMany"),
		strings.HasSuffix(name, ".Error.Core.Exists"),
		strings.HasSuffix(name, ".Error.MobileEquipment.Busy"):
		return domain.NewOperationError(domain.ErrorConflict, operation, message, err)
	default:
		return domain.Internal(operation, message, err)
	}
}

func dbusErrorName(err error) string {
	var pointer *dbus.Error
	if errors.As(err, &pointer) && pointer != nil {
		return pointer.Name
	}
	var value dbus.Error
	if errors.As(err, &value) {
		return value.Name
	}
	return ""
}
