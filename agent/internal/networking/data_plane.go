package networking

import (
	"context"

	"github.com/human-agent65535/modemdeck/agent/internal/domain"
)

type DataPlaneOptions struct {
	StateFile string
}

type DataPlane interface {
	Configure(context.Context, string, domain.DataConnection) error
	Release(context.Context, string) error
	OwnedLines() []string
}
