package modemmanager

import (
	"context"
	"strings"

	"github.com/godbus/dbus/v5"
	"github.com/human-agent65535/modemdeck/agent/internal/domain"
)

const modemCommandTimeoutSeconds = uint32(5)

type ATTransport struct {
	provider *Provider
	lineID   string
}

func (p *Provider) ATTransport(lineID string) *ATTransport {
	return &ATTransport{
		provider: p,
		lineID:   strings.TrimSpace(lineID),
	}
}

func (transport *ATTransport) Command(ctx context.Context, command string) (string, error) {
	if transport == nil || transport.provider == nil {
		return "", domain.Unavailable("at_command", "ModemManager AT transport is unavailable", nil)
	}
	return transport.provider.commandAT(ctx, transport.lineID, command)
}

func (p *Provider) commandAT(ctx context.Context, lineID, command string) (string, error) {
	const operation = "at_command"
	lineID = strings.TrimSpace(lineID)
	command = strings.TrimSpace(command)
	if lineID == "" {
		return "", domain.InvalidArgument(operation, "line id is required")
	}
	if command == "" || len(command) > 512 || strings.ContainsAny(command, "\r\n\x00") {
		return "", domain.InvalidArgument(operation, "AT command is invalid")
	}

	parsed, err := p.snapshotContent(ctx, operation)
	if err != nil {
		return "", err
	}
	line, found := findLine(parsed.Lines, lineID)
	if !found {
		return "", domain.NotFound(operation, "line was not found")
	}
	if !line.Capabilities.ModemInterface {
		return "", domain.NotSupported(operation, "line does not expose the ModemManager Modem interface")
	}
	path, found := parsed.LinePaths[line.ID]
	if !found || !path.IsValid() {
		return "", domain.NotFound(operation, "line does not have a routable ModemManager path")
	}
	return p.commandATPath(ctx, path, operation, command)
}

func (p *Provider) commandATPath(
	ctx context.Context,
	path dbus.ObjectPath,
	operation string,
	command string,
) (string, error) {
	body, err := p.call(
		ctx,
		path,
		modemInterface+".Command",
		operation,
		"ModemManager AT command failed",
		command,
		modemCommandTimeoutSeconds,
	)
	if err != nil {
		return "", err
	}
	var response string
	if err := dbus.Store(body, &response); err != nil {
		return "", domain.Internal(operation, "ModemManager AT response was malformed", err)
	}
	return strings.TrimSpace(response), nil
}
