package networking

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/human-agent65535/modemdeck/agent/internal/domain"
)

const (
	providerReadTimeout = 10 * time.Second
	managerCloseTimeout = 3 * time.Second
)

type networkConfigurationSource interface {
	NetworkConfigurations(context.Context) ([]domain.LineNetworkConfiguration, error)
}

type epochGenerator func(string) (string, error)

type managerOptions struct {
	statsReader        InterfaceStatsReader
	runnerFactory      proxyRunnerFactory
	interfaceReadiness bearerReadinessChecker
	closeTimeout       time.Duration
	now                func() time.Time
	epoch              epochGenerator
}

type managedProxy struct {
	configuration domain.ProxyConfiguration
	bearer        bearer
	runner        proxyRunner
	state         domain.ProxyState
	runtimeEpoch  string
	startedAt     *time.Time
	lastError     string
	stopResult    chan error
}

type Manager struct {
	source             networkConfigurationSource
	statsReader        InterfaceStatsReader
	runnerFactory      proxyRunnerFactory
	interfaceReadiness bearerReadinessChecker
	closeTimeout       time.Duration
	now                func() time.Time
	epoch              epochGenerator
	bootEpoch          string

	applyMu sync.Mutex
	mu      sync.Mutex
	proxies map[string]*managedProxy
	closed  bool
}

func NewManager(
	source networkConfigurationSource,
) (*Manager, error) {
	return newManager(source, managerOptions{})
}

func newManager(
	source networkConfigurationSource,
	options managerOptions,
) (*Manager, error) {
	if source == nil {
		return nil, fmt.Errorf("network configuration source is required")
	}
	if options.statsReader == nil {
		options.statsReader = SysfsStatsReader{}
	}
	if options.runnerFactory == nil {
		options.runnerFactory = newProxyServer
	}
	if options.interfaceReadiness == nil {
		options.interfaceReadiness = systemBearerReadiness
	}
	if options.closeTimeout <= 0 {
		options.closeTimeout = managerCloseTimeout
	}
	if options.now == nil {
		options.now = time.Now
	}
	if options.epoch == nil {
		options.epoch = randomEpoch
	}
	bootEpoch, err := options.epoch("network_boot")
	if err != nil {
		return nil, fmt.Errorf("generate network boot epoch: %w", err)
	}
	return &Manager{
		source:             source,
		statsReader:        options.statsReader,
		runnerFactory:      options.runnerFactory,
		interfaceReadiness: options.interfaceReadiness,
		closeTimeout:       options.closeTimeout,
		now:                options.now,
		epoch:              options.epoch,
		bootEpoch:          bootEpoch,
		proxies:            make(map[string]*managedProxy),
	}, nil
}

func (manager *Manager) ApplyProxySet(
	ctx context.Context,
	desired domain.ProxyDesiredSet,
) (domain.NetworkSnapshot, error) {
	manager.applyMu.Lock()
	defer manager.applyMu.Unlock()

	currentProxies, err := manager.currentProxies("apply_proxies")
	if err != nil {
		return domain.NetworkSnapshot{}, err
	}
	normalized, err := normalizeProxySet(desired)
	if err != nil {
		return domain.NetworkSnapshot{}, err
	}

	lineIDs := make(map[string]struct{}, len(currentProxies)+len(normalized))
	for _, current := range currentProxies {
		lineIDs[current.configuration.LineID] = struct{}{}
	}
	for _, configuration := range normalized {
		lineIDs[configuration.LineID] = struct{}{}
	}

	readContext, cancelRead := context.WithTimeout(ctx, providerReadTimeout)
	observation, err := manager.observeNetwork(readContext, "apply_proxies", lineIDs)
	cancelRead()
	if err != nil {
		return domain.NetworkSnapshot{}, err
	}
	resolutions, err := manager.resolveDesiredBearers(normalized, observation)
	if err != nil {
		return domain.NetworkSnapshot{}, err
	}
	lines := manager.lineStatuses(observation)
	if err := ctx.Err(); err != nil {
		return domain.NetworkSnapshot{}, domain.Unavailable(
			"apply_proxies",
			"proxy configuration was cancelled before runtime changes began",
			err,
		)
	}

	kept := make(map[string]*managedProxy, len(normalized))
	next := make(map[string]*managedProxy, len(normalized))
	for _, configuration := range normalized {
		if !configuration.Enabled {
			continue
		}
		current := currentProxies[configuration.ID]
		resolution := resolutions[configuration.ID]
		if resolution.connected &&
			current != nil &&
			current.state == domain.ProxyStateRunning &&
			current.configuration == configuration &&
			current.bearer.fingerprint() == resolution.bearer.fingerprint() &&
			current.runner != nil &&
			current.runner.Running() &&
			current.runner.LastError() == "" {
			kept[configuration.ID] = current
			next[configuration.ID] = current
		}
	}

	stop := make(map[string]*managedProxy)
	for id, current := range currentProxies {
		if _, preserve := kept[id]; preserve || current.runner == nil {
			continue
		}
		stop[id] = current
	}
	if err := manager.stopManagedProxies(stop); err != nil {
		return domain.NetworkSnapshot{}, domain.Unavailable(
			"apply_proxies",
			"an existing proxy could not be stopped; runtime ownership was retained",
			err,
		)
	}

	for _, configuration := range normalized {
		if _, preserve := kept[configuration.ID]; preserve {
			continue
		}
		if !configuration.Enabled {
			next[configuration.ID] = &managedProxy{
				configuration: configuration,
				state:         domain.ProxyStateDisabled,
			}
			continue
		}
		resolution := resolutions[configuration.ID]
		if !resolution.connected {
			next[configuration.ID] = &managedProxy{
				configuration: configuration,
				state:         domain.ProxyStateWaitingForBearer,
				lastError:     resolution.waitReason,
			}
			continue
		}
		next[configuration.ID] = manager.startProxy(configuration, resolution.bearer)
	}

	manager.mu.Lock()
	manager.proxies = next
	manager.mu.Unlock()
	return manager.snapshotWithLines(lines)
}

func (manager *Manager) NetworkSnapshot(
	ctx context.Context,
) (domain.NetworkSnapshot, error) {
	manager.applyMu.Lock()
	defer manager.applyMu.Unlock()
	return manager.networkSnapshot(ctx)
}

func (manager *Manager) networkSnapshot(
	ctx context.Context,
) (domain.NetworkSnapshot, error) {
	currentProxies, err := manager.currentProxies("network_snapshot")
	if err != nil {
		return domain.NetworkSnapshot{}, err
	}
	lineIDs := make(map[string]struct{}, len(currentProxies))
	for _, current := range currentProxies {
		lineIDs[current.configuration.LineID] = struct{}{}
	}

	readContext, cancelRead := context.WithTimeout(ctx, providerReadTimeout)
	observation, err := manager.observeNetwork(readContext, "network_snapshot", lineIDs)
	cancelRead()
	if err != nil {
		return domain.NetworkSnapshot{}, err
	}
	return manager.snapshotWithLines(manager.lineStatuses(observation))
}

func (manager *Manager) Close() error {
	manager.applyMu.Lock()
	defer manager.applyMu.Unlock()

	manager.mu.Lock()
	manager.closed = true
	currentProxies := make(map[string]*managedProxy, len(manager.proxies))
	for id, current := range manager.proxies {
		currentProxies[id] = current
	}
	manager.mu.Unlock()

	if err := manager.stopManagedProxies(currentProxies); err != nil {
		return err
	}
	manager.mu.Lock()
	manager.proxies = make(map[string]*managedProxy)
	manager.mu.Unlock()
	return nil
}

func (manager *Manager) currentProxies(
	operation string,
) (map[string]*managedProxy, error) {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	if manager.closed {
		return nil, domain.Unavailable(operation, "network manager is closed", nil)
	}
	current := make(map[string]*managedProxy, len(manager.proxies))
	for id, proxy := range manager.proxies {
		current[id] = proxy
	}
	return current, nil
}

type networkObservation struct {
	discovered     map[string]struct{}
	configurations map[string]domain.LineNetworkConfiguration
	lineIDs        []string
}

func (manager *Manager) observeNetwork(
	ctx context.Context,
	operation string,
	extraLineIDs map[string]struct{},
) (networkObservation, error) {
	if err := ctx.Err(); err != nil {
		return networkObservation{}, domain.Unavailable(
			operation,
			"ModemManager discovery was cancelled",
			err,
		)
	}
	observedConfigurations, err := manager.source.NetworkConfigurations(ctx)
	if err != nil {
		return networkObservation{}, domain.Unavailable(
			operation,
			"ModemManager network configuration is unavailable",
			err,
		)
	}
	if err := ctx.Err(); err != nil {
		return networkObservation{}, domain.Unavailable(
			operation,
			"ModemManager discovery was cancelled",
			err,
		)
	}

	discovered := make(map[string]struct{}, len(observedConfigurations))
	configurations := make(map[string]domain.LineNetworkConfiguration, len(observedConfigurations))
	allLineIDs := make(map[string]struct{}, len(observedConfigurations)+len(extraLineIDs))
	for _, configuration := range observedConfigurations {
		lineID := strings.TrimSpace(configuration.LineID)
		if lineID == "" {
			return networkObservation{}, domain.Unavailable(
				operation,
				"ModemManager network configuration has no line identity",
				nil,
			)
		}
		if _, duplicate := discovered[lineID]; duplicate {
			return networkObservation{}, domain.Unavailable(
				operation,
				fmt.Sprintf("ModemManager returned duplicate network line %q", lineID),
				nil,
			)
		}
		configuration.LineID = lineID
		configuration.DataConnections = append(
			[]domain.DataConnection(nil),
			configuration.DataConnections...,
		)
		discovered[lineID] = struct{}{}
		configurations[lineID] = configuration
		allLineIDs[lineID] = struct{}{}
	}
	for lineID := range extraLineIDs {
		allLineIDs[lineID] = struct{}{}
	}
	lineIDs := make([]string, 0, len(allLineIDs))
	for lineID := range allLineIDs {
		lineIDs = append(lineIDs, lineID)
	}
	sort.Strings(lineIDs)

	return networkObservation{
		discovered:     discovered,
		configurations: configurations,
		lineIDs:        lineIDs,
	}, nil
}

type bearerResolution struct {
	bearer     bearer
	connected  bool
	waitReason string
}

func (manager *Manager) resolveDesiredBearers(
	configurations []domain.ProxyConfiguration,
	observation networkObservation,
) (map[string]bearerResolution, error) {
	byLine := make(map[string]bearerResolution)
	result := make(map[string]bearerResolution, len(configurations))
	for _, configuration := range configurations {
		if !configuration.Enabled {
			continue
		}
		resolution, exists := byLine[configuration.LineID]
		if !exists {
			if _, discovered := observation.discovered[configuration.LineID]; !discovered {
				resolution.waitReason = "line is absent from authoritative ModemManager discovery"
			} else {
				current := observation.configurations[configuration.LineID]
				selected, err := resolveBearer(current)
				if errors.Is(err, errNoConnectedDataBearer) {
					resolution.waitReason = err.Error()
				} else if err != nil {
					return nil, domain.FailedPrecondition(
						"apply_proxies",
						fmt.Sprintf(
							"line %q bearer selection failed: %v",
							configuration.LineID,
							err,
						),
						err,
					)
				} else if err := manager.interfaceReadiness(selected); err != nil {
					return nil, domain.FailedPrecondition(
						"apply_proxies",
						fmt.Sprintf(
							"line %q bearer interface is not ready: %v",
							configuration.LineID,
							err,
						),
						err,
					)
				} else {
					resolution.bearer = selected
					resolution.connected = true
				}
			}
			byLine[configuration.LineID] = resolution
		}
		result[configuration.ID] = resolution
	}
	return result, nil
}

func (manager *Manager) startProxy(
	configuration domain.ProxyConfiguration,
	selected bearer,
) *managedProxy {
	current := &managedProxy{
		configuration: configuration,
		bearer:        selected,
		state:         domain.ProxyStateError,
	}
	runner, err := manager.runnerFactory(configuration, selected)
	if err != nil {
		current.lastError = err.Error()
		return current
	}
	current.runner = runner
	if err := runner.Start(); err != nil {
		manager.cleanupFailedStart(current, fmt.Errorf("start proxy: %w", err))
		return current
	}
	runtimeEpoch, err := manager.epoch("proxy_runtime")
	if err != nil {
		manager.cleanupFailedStart(
			current,
			fmt.Errorf("generate runtime epoch: %w", err),
		)
		return current
	}
	startedAt := manager.now().UTC()
	current.state = domain.ProxyStateRunning
	current.runtimeEpoch = runtimeEpoch
	current.startedAt = &startedAt
	current.lastError = ""
	return current
}

func (manager *Manager) cleanupFailedStart(
	current *managedProxy,
	startError error,
) {
	closeError := manager.stopManagedProxy(current)
	manager.mu.Lock()
	defer manager.mu.Unlock()
	if closeError == nil {
		current.runner = nil
		current.stopResult = nil
	}
	current.state = domain.ProxyStateError
	if closeError != nil {
		current.lastError = errors.Join(
			startError,
			fmt.Errorf("cleanup failed: %w", closeError),
		).Error()
	} else {
		current.lastError = startError.Error()
	}
}

func (manager *Manager) lineStatuses(
	observation networkObservation,
) []domain.LineNetworkStatus {
	statuses := make([]domain.LineNetworkStatus, 0, len(observation.lineIDs))
	for _, lineID := range observation.lineIDs {
		if _, discovered := observation.discovered[lineID]; !discovered {
			statuses = append(statuses, domain.LineNetworkStatus{
				LineID:    lineID,
				Addresses: []string{},
				DNS:       []string{},
				Error:     "line is absent from authoritative ModemManager discovery",
			})
			continue
		}
		statuses = append(
			statuses,
			manager.lineStatus(observation.configurations[lineID]),
		)
	}
	return statuses
}

func (manager *Manager) lineStatus(
	configuration domain.LineNetworkConfiguration,
) domain.LineNetworkStatus {
	status := domain.LineNetworkStatus{
		LineID:    configuration.LineID,
		Addresses: []string{},
		DNS:       []string{},
	}
	for _, connection := range configuration.DataConnections {
		if connection.Connected {
			status.Connected = true
			break
		}
	}

	connection, err := selectDefaultInternetConnection(configuration)
	if errors.Is(err, errNoConnectedDataBearer) {
		return status
	}
	if err != nil {
		status.Error = err.Error()
		return status
	}
	selected, err := bearerFromConnection(connection)
	if err != nil {
		status.Error = err.Error()
		return status
	}

	status.Interface = selected.Interface
	for _, address := range []string{
		strings.TrimSpace(connection.IPv4.Address),
		strings.TrimSpace(connection.IPv6.Address),
	} {
		if address != "" {
			status.Addresses = append(status.Addresses, address)
		}
	}
	status.DNS = selected.DNS
	var statusErrors []string
	if len(status.DNS) == 0 {
		statusErrors = append(
			statusErrors,
			fmt.Sprintf(
				"connected default Internet bearer %q has no valid DNS servers",
				connection.ID,
			),
		)
	}
	if err := manager.interfaceReadiness(selected); err != nil {
		statusErrors = append(statusErrors, err.Error())
	}
	counters, err := manager.statsReader.Read(selected.Interface)
	if err != nil {
		statusErrors = append(statusErrors, err.Error())
	} else {
		status.RXBytes = counters.RXBytes
		status.TXBytes = counters.TXBytes
	}
	status.Error = strings.Join(statusErrors, "; ")
	return status
}

func (manager *Manager) snapshotWithLines(
	lines []domain.LineNetworkStatus,
) (domain.NetworkSnapshot, error) {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	if manager.closed {
		return domain.NetworkSnapshot{}, domain.Unavailable(
			"network_snapshot",
			"network manager is closed",
			nil,
		)
	}
	if lines == nil {
		lines = []domain.LineNetworkStatus{}
	}
	return domain.NetworkSnapshot{
		BootEpoch:  manager.bootEpoch,
		ObservedAt: manager.now().UTC(),
		Lines:      lines,
		Proxies:    manager.proxyStatusesLocked(),
	}, nil
}

func (manager *Manager) proxyStatusesLocked() []domain.ProxyNetworkStatus {
	ids := make([]string, 0, len(manager.proxies))
	for id := range manager.proxies {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	statuses := make([]domain.ProxyNetworkStatus, 0, len(ids))
	for _, id := range ids {
		current := manager.proxies[id]
		status := domain.ProxyNetworkStatus{
			ID:            current.configuration.ID,
			LineID:        current.configuration.LineID,
			State:         current.state,
			Mode:          current.configuration.Mode,
			ListenAddress: current.configuration.ListenAddress,
			ListenPort:    current.configuration.ListenPort,
			Interface:     current.bearer.Interface,
			RuntimeEpoch:  current.runtimeEpoch,
			StartedAt:     current.startedAt,
			LastError:     current.lastError,
		}
		if current.runner != nil {
			counters := current.runner.Counters()
			status.BytesUp = counters.BytesUp
			status.BytesDown = counters.BytesDown
			status.Connections = counters.Connections
			status.ActiveConnections = counters.ActiveConnections
			if current.state == domain.ProxyStateRunning {
				status.Running = current.runner.Running()
				if err := current.runner.LastError(); err != "" {
					status.State = domain.ProxyStateError
					status.Running = false
					status.LastError = err
				} else if !status.Running {
					status.State = domain.ProxyStateError
					status.LastError = "proxy listener stopped unexpectedly"
				}
			} else {
				status.Running = false
				if status.State == domain.ProxyStateError && status.LastError == "" {
					status.LastError = "proxy runtime ownership is not settled"
				}
			}
		}
		statuses = append(statuses, status)
	}
	return statuses
}

func (manager *Manager) stopManagedProxies(
	proxies map[string]*managedProxy,
) error {
	ids := make([]string, 0, len(proxies))
	for id, current := range proxies {
		if current.runner != nil {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	for _, id := range ids {
		if err := manager.stopManagedProxy(proxies[id]); err != nil {
			return fmt.Errorf("stop proxy %q: %w", id, err)
		}
	}
	return nil
}

func (manager *Manager) stopManagedProxy(current *managedProxy) error {
	manager.mu.Lock()
	if current.runner == nil {
		manager.mu.Unlock()
		return nil
	}
	result := current.stopResult
	if result == nil {
		result = make(chan error, 1)
		current.stopResult = result
		current.state = domain.ProxyStateError
		current.lastError = "proxy stop is in progress"
		runner := current.runner
		go func() {
			result <- runner.Close()
		}()
	}
	manager.mu.Unlock()

	timer := time.NewTimer(manager.closeTimeout)
	defer timer.Stop()
	select {
	case err := <-result:
		if err == nil && current.runner.Running() {
			err = fmt.Errorf("proxy runner still reports running after close")
		}
		manager.mu.Lock()
		if current.stopResult == result {
			current.stopResult = nil
			current.state = domain.ProxyStateError
			if err != nil {
				current.lastError = fmt.Sprintf("proxy stop failed: %v", err)
			} else {
				current.lastError = "proxy stopped; reconciliation is pending"
			}
		}
		manager.mu.Unlock()
		return err
	case <-timer.C:
		err := fmt.Errorf("proxy stop timed out after %s", manager.closeTimeout)
		manager.mu.Lock()
		if current.stopResult == result {
			current.state = domain.ProxyStateError
			current.lastError = err.Error()
		}
		manager.mu.Unlock()
		return err
	}
}

func randomEpoch(prefix string) (string, error) {
	var content [16]byte
	if _, err := rand.Read(content[:]); err != nil {
		return "", err
	}
	return prefix + "_" + hex.EncodeToString(content[:]), nil
}

var _ domain.NetworkProvider = (*Manager)(nil)
