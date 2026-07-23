package modemmanager

import (
	"context"

	"github.com/godbus/dbus/v5"
)

// Caller is the complete D-Bus boundary used by Provider. Tests supply an
// in-memory implementation and never require a running system bus.
type Caller interface {
	Call(
		context.Context,
		string,
		dbus.ObjectPath,
		string,
		dbus.Flags,
		...any,
	) ([]any, error)
}

type connectionCaller struct {
	conn *dbus.Conn
}

func (c *connectionCaller) Call(
	ctx context.Context,
	destination string,
	path dbus.ObjectPath,
	method string,
	flags dbus.Flags,
	args ...any,
) ([]any, error) {
	call := c.conn.Object(destination, path).CallWithContext(ctx, method, flags, args...)
	if call.Err != nil {
		return nil, call.Err
	}
	return call.Body, nil
}
