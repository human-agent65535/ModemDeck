package modemmanager

import (
	"context"

	"github.com/human-agent65535/modemdeck/agent/internal/domain"
)

// DataPlane configures the kernel side of a connected ModemManager bearer.
// ModemManager remains the sole modem control plane; implementations must only
// mutate network state owned by the supplied stable line ID.
type DataPlane interface {
	Configure(context.Context, string, domain.DataConnection) error
	Release(context.Context, string) error
	OwnedLines() []string
}

type USBRecovery interface {
	Capability(string) domain.FeatureCapability
	Reset(context.Context, string) error
}

type Options struct {
	DataPlane       DataPlane
	BearerStateFile string
	RadioStateFile  string
	USBRecovery     USBRecovery
}

type noopDataPlane struct{}

func (noopDataPlane) Configure(context.Context, string, domain.DataConnection) error {
	return nil
}

func (noopDataPlane) Release(context.Context, string) error {
	return nil
}

func (noopDataPlane) OwnedLines() []string {
	return nil
}
