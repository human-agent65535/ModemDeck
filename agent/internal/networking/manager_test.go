package networking

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/human-agent65535/modemdeck/agent/internal/domain"
)

func TestManagerReconcilesOnlyChangesAndRotatesRuntimeEpoch(t *testing.T) {
	source := &fakeNetworkSource{
		lines: []domain.Line{{ID: "line-main"}},
		configurations: map[string]domain.DeviceConfiguration{
			"line-main": connectedConfiguration("line-main", "wwan0", "8.8.8.8"),
		},
	}
	factory := &fakeRunnerFactory{}
	now := time.Date(2026, 7, 24, 1, 2, 3, 0, time.UTC)
	epochSequence := 0
	manager, err := newManager(source, source, managerOptions{
		statsReader: fakeStatsReader{
			"wwan0": {RXBytes: 100, TXBytes: 200},
		},
		runnerFactory:      factory.new,
		interfaceReadiness: alwaysReadyBearer,
		now: func() time.Time {
			return now
		},
		epoch: func(prefix string) (string, error) {
			epochSequence++
			return fmt.Sprintf("%s-%d", prefix, epochSequence), nil
		},
	})
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	t.Cleanup(func() { _ = manager.Close() })

	configuration := validProxyConfiguration()
	first, err := manager.ApplyProxySet(context.Background(), domain.ProxyDesiredSet{
		Proxies: []domain.ProxyConfiguration{configuration},
	})
	if err != nil {
		t.Fatalf("first apply: %v", err)
	}
	assertRunningProxy(t, first, "proxy_runtime-2", "wwan0")
	if first.BootEpoch != "network_boot-1" ||
		len(first.Lines) != 1 ||
		first.Lines[0].RXBytes != 100 ||
		first.Lines[0].TXBytes != 200 {
		t.Fatalf("unexpected first snapshot: %+v", first)
	}
	if factory.created != 1 || factory.runners[0].starts != 1 {
		t.Fatalf("first runner counts = created %d, starts %d", factory.created, factory.runners[0].starts)
	}

	second, err := manager.ApplyProxySet(context.Background(), domain.ProxyDesiredSet{
		Proxies: []domain.ProxyConfiguration{configuration},
	})
	if err != nil {
		t.Fatalf("idempotent apply: %v", err)
	}
	assertRunningProxy(t, second, "proxy_runtime-2", "wwan0")
	if factory.created != 1 ||
		factory.runners[0].starts != 1 ||
		factory.runners[0].closes != 0 {
		t.Fatalf("unchanged proxy restarted: %+v", factory.runners[0])
	}
	factory.runners[0].counters = ProxyCounters{
		BytesUp:           10,
		BytesDown:         20,
		Connections:       3,
		ActiveConnections: 1,
	}
	counted, err := manager.NetworkSnapshot(context.Background())
	if err != nil {
		t.Fatalf("counted snapshot: %v", err)
	}
	if counted.Proxies[0].BytesUp != 10 ||
		counted.Proxies[0].BytesDown != 20 ||
		counted.Proxies[0].Connections != 3 ||
		counted.Proxies[0].ActiveConnections != 1 {
		t.Fatalf("proxy counters = %+v", counted.Proxies[0])
	}

	configuration.ListenPort = 1081
	third, err := manager.ApplyProxySet(context.Background(), domain.ProxyDesiredSet{
		Proxies: []domain.ProxyConfiguration{configuration},
	})
	if err != nil {
		t.Fatalf("changed apply: %v", err)
	}
	assertRunningProxy(t, third, "proxy_runtime-3", "wwan0")
	if factory.created != 2 ||
		factory.runners[0].closes != 1 ||
		factory.runners[1].starts != 1 {
		t.Fatalf("changed proxy counts = created %d, first %+v, second %+v", factory.created, factory.runners[0], factory.runners[1])
	}

	configuration.Enabled = false
	disabled, err := manager.ApplyProxySet(context.Background(), domain.ProxyDesiredSet{
		Proxies: []domain.ProxyConfiguration{configuration},
	})
	if err != nil {
		t.Fatalf("disable apply: %v", err)
	}
	if len(disabled.Proxies) != 1 ||
		disabled.Proxies[0].State != domain.ProxyStateDisabled ||
		disabled.Proxies[0].Running ||
		disabled.Proxies[0].RuntimeEpoch != "" {
		t.Fatalf("unexpected disabled status: %+v", disabled.Proxies)
	}
	if factory.runners[1].closes != 1 {
		t.Fatalf("enabled runner close count = %d", factory.runners[1].closes)
	}

	empty, err := manager.ApplyProxySet(context.Background(), domain.ProxyDesiredSet{
		Proxies: []domain.ProxyConfiguration{},
	})
	if err != nil {
		t.Fatalf("delete apply: %v", err)
	}
	if len(empty.Proxies) != 0 {
		t.Fatalf("removed proxies remain: %+v", empty.Proxies)
	}
}

func TestManagerWaitsForBearerUntilExplicitReapply(t *testing.T) {
	source := &fakeNetworkSource{
		lines: []domain.Line{{ID: "line-main"}},
		configurations: map[string]domain.DeviceConfiguration{
			"line-main": {
				LineID: "line-main",
				DataConnections: []domain.DataConnection{{
					ID:        "bearer-1",
					Connected: false,
				}},
			},
		},
	}
	factory := &fakeRunnerFactory{}
	manager, err := newManager(source, source, managerOptions{
		statsReader:        fakeStatsReader{},
		runnerFactory:      factory.new,
		interfaceReadiness: alwaysReadyBearer,
		epoch:              deterministicEpoch(),
	})
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	t.Cleanup(func() { _ = manager.Close() })

	configuration := validProxyConfiguration()
	waiting, err := manager.ApplyProxySet(context.Background(), domain.ProxyDesiredSet{
		Proxies: []domain.ProxyConfiguration{configuration},
	})
	if err != nil {
		t.Fatalf("waiting apply: %v", err)
	}
	if len(waiting.Proxies) != 1 ||
		waiting.Proxies[0].State != domain.ProxyStateWaitingForBearer ||
		waiting.Proxies[0].Running ||
		waiting.Proxies[0].LastError == "" ||
		factory.created != 0 {
		t.Fatalf("unexpected waiting status: %+v, created %d", waiting.Proxies, factory.created)
	}

	snapshot, err := manager.NetworkSnapshot(context.Background())
	if err != nil {
		t.Fatalf("network snapshot: %v", err)
	}
	if snapshot.Proxies[0].State != domain.ProxyStateWaitingForBearer ||
		factory.created != 0 {
		t.Fatalf("GET implicitly retried waiting proxy: %+v", snapshot.Proxies[0])
	}
	if len(snapshot.Lines) != 1 ||
		snapshot.Lines[0].Connected ||
		snapshot.Lines[0].Interface != "" ||
		snapshot.Lines[0].Error != "" {
		t.Fatalf("disconnected line was reported as unhealthy: %+v", snapshot.Lines)
	}

	source.setConfiguration(
		"line-main",
		connectedConfiguration("line-main", "wwan7", "1.1.1.1"),
	)
	running, err := manager.ApplyProxySet(context.Background(), domain.ProxyDesiredSet{
		Proxies: []domain.ProxyConfiguration{configuration},
	})
	if err != nil {
		t.Fatalf("reapply connected bearer: %v", err)
	}
	if len(running.Proxies) != 1 ||
		running.Proxies[0].State != domain.ProxyStateRunning ||
		running.Proxies[0].Interface != "wwan7" ||
		factory.created != 1 {
		t.Fatalf("unexpected running status: %+v, created %d", running.Proxies, factory.created)
	}
	if err := manager.Close(); err != nil {
		t.Fatalf("close manager: %v", err)
	}
	if factory.runners[0].closes != 1 {
		t.Fatalf("Close stopped runner %d times", factory.runners[0].closes)
	}
	if err := manager.Close(); err != nil {
		t.Fatalf("close manager twice: %v", err)
	}
}

func TestManagerRejectsInvalidDesiredSetWithoutChangingRuntime(t *testing.T) {
	source := &fakeNetworkSource{
		lines: []domain.Line{{ID: "line-main"}},
		configurations: map[string]domain.DeviceConfiguration{
			"line-main": connectedConfiguration("line-main", "wwan0", "8.8.8.8"),
		},
	}
	factory := &fakeRunnerFactory{}
	manager, err := newManager(source, source, managerOptions{
		runnerFactory:      factory.new,
		statsReader:        fakeStatsReader{},
		interfaceReadiness: alwaysReadyBearer,
		epoch:              deterministicEpoch(),
	})
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	t.Cleanup(func() { _ = manager.Close() })

	valid := validProxyConfiguration()
	if _, err := manager.ApplyProxySet(context.Background(), domain.ProxyDesiredSet{
		Proxies: []domain.ProxyConfiguration{valid},
	}); err != nil {
		t.Fatalf("valid apply: %v", err)
	}
	invalid := valid
	invalid.ListenPort = 80
	if _, err := manager.ApplyProxySet(context.Background(), domain.ProxyDesiredSet{
		Proxies: []domain.ProxyConfiguration{invalid},
	}); err == nil {
		t.Fatal("invalid apply succeeded")
	}
	invalid = valid
	invalid.AuthEnabled = true
	invalid.Username = "user"
	invalid.Password = strings.Repeat("p", 256)
	if _, err := manager.ApplyProxySet(context.Background(), domain.ProxyDesiredSet{
		Proxies: []domain.ProxyConfiguration{invalid},
	}); err == nil {
		t.Fatal("protocol-invalid apply succeeded")
	}
	if factory.created != 1 ||
		factory.runners[0].closes != 0 ||
		!factory.runners[0].running {
		t.Fatalf("invalid apply changed runtime: %+v", factory.runners[0])
	}
}

func TestManagerPreservesRuntimeOnDiscoveryReadAndCancellationErrors(t *testing.T) {
	source := &fakeNetworkSource{
		lines: []domain.Line{{ID: "line-main"}},
		configurations: map[string]domain.DeviceConfiguration{
			"line-main": connectedConfiguration("line-main", "wwan0", "8.8.8.8"),
		},
	}
	factory := &fakeRunnerFactory{}
	manager, err := newManager(source, source, managerOptions{
		statsReader:        fakeStatsReader{},
		runnerFactory:      factory.new,
		interfaceReadiness: alwaysReadyBearer,
		epoch:              deterministicEpoch(),
	})
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	t.Cleanup(func() { _ = manager.Close() })

	configuration := validProxyConfiguration()
	if _, err := manager.ApplyProxySet(context.Background(), domain.ProxyDesiredSet{
		Proxies: []domain.ProxyConfiguration{configuration},
	}); err != nil {
		t.Fatalf("initial apply: %v", err)
	}
	configuration.ListenPort++

	source.setSnapshotError(errors.New("D-Bus discovery unavailable"))
	if _, err := manager.ApplyProxySet(context.Background(), domain.ProxyDesiredSet{
		Proxies: []domain.ProxyConfiguration{configuration},
	}); operationErrorCode(err) != domain.ErrorUnavailable {
		t.Fatalf("discovery apply error = %v, want unavailable", err)
	}
	if _, err := manager.NetworkSnapshot(context.Background()); operationErrorCode(err) != domain.ErrorUnavailable {
		t.Fatalf("discovery snapshot error = %v, want unavailable", err)
	}
	source.setSnapshotError(nil)

	source.setConfigurationError("line-main", errors.New("configuration read timed out"))
	if _, err := manager.ApplyProxySet(context.Background(), domain.ProxyDesiredSet{
		Proxies: []domain.ProxyConfiguration{configuration},
	}); operationErrorCode(err) != domain.ErrorUnavailable {
		t.Fatalf("configuration apply error = %v, want unavailable", err)
	}
	source.setConfigurationError("line-main", nil)

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := manager.ApplyProxySet(cancelled, domain.ProxyDesiredSet{
		Proxies: []domain.ProxyConfiguration{configuration},
	}); operationErrorCode(err) != domain.ErrorUnavailable {
		t.Fatalf("cancelled apply error = %v, want unavailable", err)
	}

	starts, closes, running := factory.runners[0].runtimeState()
	if factory.created != 1 || starts != 1 || closes != 0 || !running {
		t.Fatalf(
			"read failures changed runtime: created=%d starts=%d closes=%d running=%t",
			factory.created,
			starts,
			closes,
			running,
		)
	}
}

func TestManagerStopsRunningProxyAfterAuthoritativeDisconnect(t *testing.T) {
	source := &fakeNetworkSource{
		lines: []domain.Line{{ID: "line-main"}},
		configurations: map[string]domain.DeviceConfiguration{
			"line-main": connectedConfiguration("line-main", "wwan0", "8.8.8.8"),
		},
	}
	factory := &fakeRunnerFactory{}
	manager, err := newManager(source, source, managerOptions{
		statsReader:        fakeStatsReader{},
		runnerFactory:      factory.new,
		interfaceReadiness: alwaysReadyBearer,
		epoch:              deterministicEpoch(),
	})
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	t.Cleanup(func() { _ = manager.Close() })

	configuration := validProxyConfiguration()
	if _, err := manager.ApplyProxySet(context.Background(), domain.ProxyDesiredSet{
		Proxies: []domain.ProxyConfiguration{configuration},
	}); err != nil {
		t.Fatalf("initial apply: %v", err)
	}
	source.setConfiguration("line-main", domain.DeviceConfiguration{
		LineID: "line-main",
		DataConnections: []domain.DataConnection{{
			ID:        "bearer-default",
			Connected: false,
			APNType:   apnTypeDefault,
		}},
	})

	snapshot, err := manager.ApplyProxySet(context.Background(), domain.ProxyDesiredSet{
		Proxies: []domain.ProxyConfiguration{configuration},
	})
	if err != nil {
		t.Fatalf("disconnected apply: %v", err)
	}
	if len(snapshot.Proxies) != 1 ||
		snapshot.Proxies[0].State != domain.ProxyStateWaitingForBearer ||
		snapshot.Proxies[0].Running {
		t.Fatalf("unexpected disconnected proxy: %+v", snapshot.Proxies)
	}
	_, closes, running := factory.runners[0].runtimeState()
	if closes != 1 || running {
		t.Fatalf("disconnected runner closes=%d running=%t", closes, running)
	}
}

func TestManagerRetainsRunnerOwnershipUntilFailedCloseCanBeRetried(t *testing.T) {
	source := &fakeNetworkSource{
		lines: []domain.Line{{ID: "line-main"}},
		configurations: map[string]domain.DeviceConfiguration{
			"line-main": connectedConfiguration("line-main", "wwan0", "8.8.8.8"),
		},
	}
	factory := &fakeRunnerFactory{
		configure: func(index int, runner *fakeRunner) {
			if index == 0 {
				runner.closeErrors = []error{
					errors.New("listener close failed"),
					nil,
				}
			}
		},
	}
	manager, err := newManager(source, source, managerOptions{
		statsReader:        fakeStatsReader{},
		runnerFactory:      factory.new,
		interfaceReadiness: alwaysReadyBearer,
		epoch:              deterministicEpoch(),
	})
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	t.Cleanup(func() { _ = manager.Close() })

	configuration := validProxyConfiguration()
	if _, err := manager.ApplyProxySet(context.Background(), domain.ProxyDesiredSet{
		Proxies: []domain.ProxyConfiguration{configuration},
	}); err != nil {
		t.Fatalf("initial apply: %v", err)
	}
	configuration.ListenPort++

	if _, err := manager.ApplyProxySet(context.Background(), domain.ProxyDesiredSet{
		Proxies: []domain.ProxyConfiguration{configuration},
	}); operationErrorCode(err) != domain.ErrorUnavailable {
		t.Fatalf("failed close apply error = %v, want unavailable", err)
	}
	if factory.created != 1 {
		t.Fatalf("replacement started after failed close: created=%d", factory.created)
	}
	status, err := manager.NetworkSnapshot(context.Background())
	if err != nil {
		t.Fatalf("snapshot after failed close: %v", err)
	}
	if len(status.Proxies) != 1 ||
		status.Proxies[0].State != domain.ProxyStateError ||
		status.Proxies[0].Running ||
		status.Proxies[0].LastError == "" {
		t.Fatalf("failed close status = %+v", status.Proxies)
	}

	retried, err := manager.ApplyProxySet(context.Background(), domain.ProxyDesiredSet{
		Proxies: []domain.ProxyConfiguration{configuration},
	})
	if err != nil {
		t.Fatalf("retry apply: %v", err)
	}
	assertRunningProxy(t, retried, "proxy_runtime-3", "wwan0")
	_, closes, running := factory.runners[0].runtimeState()
	if factory.created != 2 || closes != 2 || running {
		t.Fatalf(
			"retry ownership = created %d, closes %d, old running %t",
			factory.created,
			closes,
			running,
		)
	}
}

func TestManagerResumesTimedOutCloseWithoutStartingAConflictingRunner(t *testing.T) {
	closeGate := make(chan struct{})
	closeStarted := make(chan struct{})
	source := &fakeNetworkSource{
		lines: []domain.Line{{ID: "line-main"}},
		configurations: map[string]domain.DeviceConfiguration{
			"line-main": connectedConfiguration("line-main", "wwan0", "8.8.8.8"),
		},
	}
	factory := &fakeRunnerFactory{
		configure: func(index int, runner *fakeRunner) {
			if index == 0 {
				runner.closeGate = closeGate
				runner.closeStart = closeStarted
			}
		},
	}
	manager, err := newManager(source, source, managerOptions{
		statsReader:        fakeStatsReader{},
		runnerFactory:      factory.new,
		interfaceReadiness: alwaysReadyBearer,
		closeTimeout:       20 * time.Millisecond,
		epoch:              deterministicEpoch(),
	})
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	t.Cleanup(func() { _ = manager.Close() })

	configuration := validProxyConfiguration()
	if _, err := manager.ApplyProxySet(context.Background(), domain.ProxyDesiredSet{
		Proxies: []domain.ProxyConfiguration{configuration},
	}); err != nil {
		t.Fatalf("initial apply: %v", err)
	}
	configuration.ListenPort++
	if _, err := manager.ApplyProxySet(context.Background(), domain.ProxyDesiredSet{
		Proxies: []domain.ProxyConfiguration{configuration},
	}); operationErrorCode(err) != domain.ErrorUnavailable {
		t.Fatalf("timed out close error = %v, want unavailable", err)
	}
	select {
	case <-closeStarted:
	default:
		t.Fatal("runner close did not start")
	}
	if factory.created != 1 {
		t.Fatalf("replacement started during timed out close: created=%d", factory.created)
	}

	close(closeGate)
	retried, err := manager.ApplyProxySet(context.Background(), domain.ProxyDesiredSet{
		Proxies: []domain.ProxyConfiguration{configuration},
	})
	if err != nil {
		t.Fatalf("retry after close completion: %v", err)
	}
	assertRunningProxy(t, retried, "proxy_runtime-3", "wwan0")
	_, closes, _ := factory.runners[0].runtimeState()
	if factory.created != 2 || closes != 1 {
		t.Fatalf("close was restarted instead of resumed: created=%d closes=%d", factory.created, closes)
	}
}

func TestManagerTreatsInterfaceReadinessFailureAsConfigurationError(t *testing.T) {
	source := &fakeNetworkSource{
		lines: []domain.Line{{ID: "line-main"}},
		configurations: map[string]domain.DeviceConfiguration{
			"line-main": connectedConfiguration("line-main", "wwan0", "8.8.8.8"),
		},
	}
	var readinessError error
	factory := &fakeRunnerFactory{}
	manager, err := newManager(source, source, managerOptions{
		statsReader:   fakeStatsReader{},
		runnerFactory: factory.new,
		interfaceReadiness: func(_ bearer) error {
			return readinessError
		},
		epoch: deterministicEpoch(),
	})
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	t.Cleanup(func() { _ = manager.Close() })

	configuration := validProxyConfiguration()
	if _, err := manager.ApplyProxySet(context.Background(), domain.ProxyDesiredSet{
		Proxies: []domain.ProxyConfiguration{configuration},
	}); err != nil {
		t.Fatalf("initial apply: %v", err)
	}
	readinessError = errors.New("wwan0 has no usable address")
	configuration.ListenPort++
	if _, err := manager.ApplyProxySet(context.Background(), domain.ProxyDesiredSet{
		Proxies: []domain.ProxyConfiguration{configuration},
	}); operationErrorCode(err) != domain.ErrorFailedPrecondition {
		t.Fatalf("readiness apply error = %v, want failed_precondition", err)
	}
	_, closes, running := factory.runners[0].runtimeState()
	if factory.created != 1 || closes != 0 || !running {
		t.Fatalf(
			"readiness error changed runtime: created=%d closes=%d running=%t",
			factory.created,
			closes,
			running,
		)
	}
}

func TestNetworkSnapshotKeepsInterfaceCountersWhenDNSIsMissing(t *testing.T) {
	source := &fakeNetworkSource{
		lines: []domain.Line{{ID: "line-main"}},
		configurations: map[string]domain.DeviceConfiguration{
			"line-main": {
				LineID: "line-main",
				DataConnections: []domain.DataConnection{{
					ID:        "bearer-1",
					Connected: true,
					APNType:   apnTypeDefault,
					Interface: "wwan0",
				}},
			},
		},
	}
	manager, err := newManager(source, source, managerOptions{
		statsReader: fakeStatsReader{
			"wwan0": {RXBytes: 700, TXBytes: 800},
		},
		runnerFactory:      (&fakeRunnerFactory{}).new,
		interfaceReadiness: alwaysReadyBearer,
		epoch:              deterministicEpoch(),
	})
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	t.Cleanup(func() { _ = manager.Close() })

	snapshot, err := manager.NetworkSnapshot(context.Background())
	if err != nil {
		t.Fatalf("network snapshot: %v", err)
	}
	if len(snapshot.Lines) != 1 {
		t.Fatalf("line count = %d", len(snapshot.Lines))
	}
	line := snapshot.Lines[0]
	if !line.Connected ||
		line.Interface != "wwan0" ||
		line.RXBytes != 700 ||
		line.TXBytes != 800 ||
		line.Error == "" {
		t.Fatalf("unexpected line status: %+v", line)
	}
}

func TestManagerReportsListenerStartFailureAsRuntimeError(t *testing.T) {
	source := &fakeNetworkSource{
		lines: []domain.Line{{ID: "line-main"}},
		configurations: map[string]domain.DeviceConfiguration{
			"line-main": connectedConfiguration("line-main", "wwan0", "8.8.8.8"),
		},
	}
	factory := &fakeRunnerFactory{
		startError: errors.New("listener unavailable"),
	}
	manager, err := newManager(source, source, managerOptions{
		statsReader:        fakeStatsReader{},
		runnerFactory:      factory.new,
		interfaceReadiness: alwaysReadyBearer,
		epoch:              deterministicEpoch(),
	})
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	t.Cleanup(func() { _ = manager.Close() })

	snapshot, err := manager.ApplyProxySet(context.Background(), domain.ProxyDesiredSet{
		Proxies: []domain.ProxyConfiguration{validProxyConfiguration()},
	})
	if err != nil {
		t.Fatalf("apply proxy: %v", err)
	}
	if len(snapshot.Proxies) != 1 ||
		snapshot.Proxies[0].State != domain.ProxyStateError ||
		snapshot.Proxies[0].Running ||
		snapshot.Proxies[0].RuntimeEpoch != "" ||
		snapshot.Proxies[0].LastError == "" ||
		factory.runners[0].closes != 1 {
		t.Fatalf("unexpected error status: %+v, runner %+v", snapshot.Proxies, factory.runners[0])
	}
}

func assertRunningProxy(
	t *testing.T,
	snapshot domain.NetworkSnapshot,
	epoch string,
	interfaceName string,
) {
	t.Helper()
	if len(snapshot.Proxies) != 1 {
		t.Fatalf("proxy count = %d", len(snapshot.Proxies))
	}
	status := snapshot.Proxies[0]
	if status.State != domain.ProxyStateRunning ||
		!status.Running ||
		status.RuntimeEpoch != epoch ||
		status.Interface != interfaceName ||
		status.StartedAt == nil ||
		status.LastError != "" {
		t.Fatalf("unexpected running proxy: %+v", status)
	}
}

func connectedConfiguration(
	lineID string,
	interfaceName string,
	dns string,
) domain.DeviceConfiguration {
	return domain.DeviceConfiguration{
		LineID: lineID,
		DataConnections: []domain.DataConnection{{
			ID:        "bearer-1",
			Connected: true,
			APNType:   apnTypeDefault,
			Interface: interfaceName,
			IPv4: domain.IPConfiguration{
				Address: "10.0.0.2",
				DNS:     []string{dns},
			},
		}},
	}
}

type fakeNetworkSource struct {
	mu                  sync.Mutex
	lines               []domain.Line
	configurations      map[string]domain.DeviceConfiguration
	snapshotError       error
	configurationErrors map[string]error
}

func (source *fakeNetworkSource) Snapshot(context.Context) (domain.Snapshot, error) {
	source.mu.Lock()
	defer source.mu.Unlock()
	if source.snapshotError != nil {
		return domain.Snapshot{}, source.snapshotError
	}
	return domain.Snapshot{
		Lines: append([]domain.Line(nil), source.lines...),
	}, nil
}

func (source *fakeNetworkSource) DeviceConfiguration(
	_ context.Context,
	lineID string,
) (domain.DeviceConfiguration, error) {
	source.mu.Lock()
	defer source.mu.Unlock()
	if err := source.configurationErrors[lineID]; err != nil {
		return domain.DeviceConfiguration{}, err
	}
	configuration, exists := source.configurations[lineID]
	if !exists {
		return domain.DeviceConfiguration{}, domain.NotFound(
			"device_configuration",
			"line was not found",
		)
	}
	return configuration, nil
}

func (source *fakeNetworkSource) setConfiguration(
	lineID string,
	configuration domain.DeviceConfiguration,
) {
	source.mu.Lock()
	source.configurations[lineID] = configuration
	source.mu.Unlock()
}

func (source *fakeNetworkSource) setSnapshotError(err error) {
	source.mu.Lock()
	source.snapshotError = err
	source.mu.Unlock()
}

func (source *fakeNetworkSource) setConfigurationError(lineID string, err error) {
	source.mu.Lock()
	if source.configurationErrors == nil {
		source.configurationErrors = make(map[string]error)
	}
	source.configurationErrors[lineID] = err
	source.mu.Unlock()
}

type fakeStatsReader map[string]InterfaceCounters

func (reader fakeStatsReader) Read(interfaceName string) (InterfaceCounters, error) {
	value, exists := reader[interfaceName]
	if !exists {
		return InterfaceCounters{}, fmt.Errorf("statistics unavailable for %s", interfaceName)
	}
	return value, nil
}

type fakeRunnerFactory struct {
	created    int
	runners    []*fakeRunner
	startError error
	configure  func(int, *fakeRunner)
}

func (factory *fakeRunnerFactory) new(
	_ domain.ProxyConfiguration,
	_ bearer,
) (proxyRunner, error) {
	runner := &fakeRunner{startError: factory.startError}
	if factory.configure != nil {
		factory.configure(factory.created, runner)
	}
	factory.created++
	factory.runners = append(factory.runners, runner)
	return runner, nil
}

type fakeRunner struct {
	mu          sync.Mutex
	starts      int
	closes      int
	running     bool
	lastError   string
	startError  error
	counters    ProxyCounters
	closeErrors []error
	closeGate   <-chan struct{}
	closeStart  chan struct{}
	closeOnce   sync.Once
}

func (runner *fakeRunner) Start() error {
	runner.mu.Lock()
	defer runner.mu.Unlock()
	runner.starts++
	if runner.startError != nil {
		return runner.startError
	}
	runner.running = true
	return nil
}

func (runner *fakeRunner) Close() error {
	runner.mu.Lock()
	runner.closes++
	gate := runner.closeGate
	started := runner.closeStart
	var err error
	if len(runner.closeErrors) > 0 {
		err = runner.closeErrors[0]
		runner.closeErrors = runner.closeErrors[1:]
	}
	runner.mu.Unlock()

	if started != nil {
		runner.closeOnce.Do(func() {
			close(started)
		})
	}
	if gate != nil {
		<-gate
	}

	runner.mu.Lock()
	defer runner.mu.Unlock()
	if err == nil {
		runner.running = false
	}
	return err
}

func (runner *fakeRunner) Running() bool {
	runner.mu.Lock()
	defer runner.mu.Unlock()
	return runner.running
}

func (runner *fakeRunner) LastError() string {
	runner.mu.Lock()
	defer runner.mu.Unlock()
	return runner.lastError
}

func (runner *fakeRunner) Counters() ProxyCounters {
	runner.mu.Lock()
	defer runner.mu.Unlock()
	return runner.counters
}

func (runner *fakeRunner) runtimeState() (int, int, bool) {
	runner.mu.Lock()
	defer runner.mu.Unlock()
	return runner.starts, runner.closes, runner.running
}

func alwaysReadyBearer(_ bearer) error {
	return nil
}

func operationErrorCode(err error) domain.ErrorCode {
	operationError, ok := domain.AsOperationError(err)
	if !ok {
		return ""
	}
	return operationError.Code
}

func deterministicEpoch() epochGenerator {
	sequence := 0
	return func(prefix string) (string, error) {
		sequence++
		return fmt.Sprintf("%s-%d", prefix, sequence), nil
	}
}
