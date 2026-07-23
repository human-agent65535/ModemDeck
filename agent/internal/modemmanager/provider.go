package modemmanager

import (
	"context"
	"fmt"

	"github.com/godbus/dbus/v5"
	"github.com/human-agent65535/modemdeck/agent/internal/domain"
)

const (
	serviceName            = "org.freedesktop.ModemManager1"
	managerPath            = dbus.ObjectPath("/org/freedesktop/ModemManager1")
	objectManagerInterface = "org.freedesktop.DBus.ObjectManager"
)

type Provider struct {
	conn *dbus.Conn
}

func OpenSystemBus() (*Provider, error) {
	conn, err := dbus.ConnectSystemBus()
	if err != nil {
		return nil, fmt.Errorf("connect to system D-Bus: %w", err)
	}
	return &Provider{conn: conn}, nil
}

func (p *Provider) Close() error {
	if p == nil || p.conn == nil {
		return nil
	}
	return p.conn.Close()
}

func (p *Provider) Health(ctx context.Context) (domain.ProviderHealth, error) {
	health := domain.ProviderHealth{
		Name: serviceName,
		Capabilities: domain.AgentCapabilities{
			Discovery:   false,
			Dial:        false,
			AnswerCall:  false,
			HangupCall:  false,
			SendMessage: false,
		},
	}
	if p == nil || p.conn == nil {
		return health, domain.NewOperationError(
			domain.ErrorUnavailable,
			"health",
			"system D-Bus connection is unavailable",
			nil,
		)
	}

	var available bool
	call := p.conn.BusObject().CallWithContext(
		ctx,
		"org.freedesktop.DBus.NameHasOwner",
		dbus.FlagNoAutoStart,
		serviceName,
	)
	if err := call.Store(&available); err != nil {
		return health, domain.NewOperationError(
			domain.ErrorUnavailable,
			"health",
			"failed to query the ModemManager D-Bus owner",
			err,
		)
	}

	health.Available = available
	health.Capabilities.Discovery = available
	return health, nil
}

func (p *Provider) Lines(ctx context.Context) ([]domain.Line, error) {
	if p == nil || p.conn == nil {
		return nil, domain.NewOperationError(
			domain.ErrorUnavailable,
			"discover_lines",
			"system D-Bus connection is unavailable",
			nil,
		)
	}

	objects := ManagedObjects{}
	call := p.conn.Object(serviceName, managerPath).CallWithContext(
		ctx,
		objectManagerInterface+".GetManagedObjects",
		dbus.FlagNoAutoStart,
	)
	if err := call.Store(&objects); err != nil {
		return nil, domain.NewOperationError(
			domain.ErrorUnavailable,
			"discover_lines",
			"ModemManager GetManagedObjects failed",
			err,
		)
	}
	return ParseManagedObjects(objects), nil
}

func (*Provider) StartCall(context.Context, domain.StartCallRequest) (domain.Call, error) {
	return domain.Call{}, domain.NotSupported("start_call")
}

func (*Provider) AnswerCall(context.Context, string) (domain.Call, error) {
	return domain.Call{}, domain.NotSupported("answer_call")
}

func (*Provider) HangupCall(context.Context, string) (domain.Call, error) {
	return domain.Call{}, domain.NotSupported("hangup_call")
}

func (*Provider) SendMessage(context.Context, domain.SendMessageRequest) (domain.Message, error) {
	return domain.Message{}, domain.NotSupported("send_message")
}
