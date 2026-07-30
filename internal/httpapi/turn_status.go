package httpapi

import (
	"context"
	"sync"
	"time"

	"github.com/human-agent65535/modemdeck/internal/rtcconfig"
)

const (
	turnAvailabilitySuccessTTL = 5 * time.Minute
	turnAvailabilityFailureTTL = 30 * time.Second
	turnAvailabilityTimeout    = 5 * time.Second
)

type turnAvailabilityStatus struct {
	Configured bool `json:"configured"`
	Available  bool `json:"available"`
}

type turnAvailabilityCache struct {
	mutex     sync.RWMutex
	checkedAt time.Time
	available bool
}

func (cache *turnAvailabilityCache) current(
	now time.Time,
) (bool, bool) {
	cache.mutex.RLock()
	defer cache.mutex.RUnlock()
	if cache.checkedAt.IsZero() {
		return false, false
	}
	ttl := turnAvailabilityFailureTTL
	if cache.available {
		ttl = turnAvailabilitySuccessTTL
	}
	if now.Sub(cache.checkedAt) >= ttl {
		return false, false
	}
	return cache.available, true
}

func (cache *turnAvailabilityCache) record(
	now time.Time,
	available bool,
) {
	cache.mutex.Lock()
	cache.checkedAt = now
	cache.available = available
	cache.mutex.Unlock()
}

func (api *API) turnStatus(
	ctx context.Context,
) turnAvailabilityStatus {
	if api == nil || api.rtcConfiguration == nil {
		return turnAvailabilityStatus{}
	}
	if available, cached := api.turnAvailability.current(time.Now()); cached {
		return turnAvailabilityStatus{
			Configured: true,
			Available:  available,
		}
	}
	probeContext, cancel := context.WithTimeout(
		ctx,
		turnAvailabilityTimeout,
	)
	defer cancel()
	_, err := api.generateRTCConfiguration(probeContext)
	return turnAvailabilityStatus{
		Configured: true,
		Available:  err == nil,
	}
}

func (api *API) generateRTCConfiguration(
	ctx context.Context,
) (rtcconfig.Configuration, error) {
	configuration, err := api.rtcConfiguration.Generate(ctx)
	api.turnAvailability.record(time.Now(), err == nil)
	return configuration, err
}
