package communication

import (
	"context"

	"github.com/human-agent65535/modemdeck/internal/agentclient"
)

// Health probes the Host Agent directly instead of consulting the cached
// communication snapshot used by user-facing API requests.
func (s *Service) Health(ctx context.Context) (agentclient.Health, error) {
	return s.agent.Health(ctx)
}
