package main

import (
	"context"
	"fmt"

	"github.com/godbus/dbus/v5"
)

type eventReporter interface {
	Report(context.Context, kernelEvent) error
}

type mmReporter struct {
	conn *dbus.Conn
}

func newMMReporter() (*mmReporter, error) {
	conn, err := dbus.ConnectSystemBus()
	if err != nil {
		return nil, fmt.Errorf("connect private ModemManager D-Bus: %w", err)
	}
	return &mmReporter{conn: conn}, nil
}

func (reporter *mmReporter) Close() error {
	if reporter == nil || reporter.conn == nil {
		return nil
	}
	return reporter.conn.Close()
}

func (reporter *mmReporter) Report(ctx context.Context, event kernelEvent) error {
	properties := map[string]dbus.Variant{
		"action":    dbus.MakeVariant(event.Action),
		"subsystem": dbus.MakeVariant(event.Subsystem),
		"name":      dbus.MakeVariant(event.Name),
		"uid":       dbus.MakeVariant(event.UID),
	}
	call := reporter.conn.Object(modemManagerService, modemManagerPath).CallWithContext(
		ctx,
		modemManagerService+".ReportKernelEvent",
		dbus.FlagNoAutoStart,
		properties,
	)
	if call.Err != nil {
		return fmt.Errorf(
			"ReportKernelEvent action=%s subsystem=%s name=%s uid=%s: %w",
			event.Action,
			event.Subsystem,
			event.Name,
			event.UID,
			call.Err,
		)
	}
	return nil
}
