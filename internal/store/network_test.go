package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	platformdb "github.com/human-agent65535/modemdeck/internal/platform/database"
)

func TestProxyInstanceRevisionLifecycle(t *testing.T) {
	t.Parallel()

	repository := newNetworkTestStore(t)
	created, err := repository.CreateProxyInstance(context.Background(), ProxyInstanceRecord{
		ID:                 "proxy-1",
		Name:               "Primary",
		LineID:             "line-1",
		Enabled:            true,
		Mode:               "socks5",
		ListenAddress:      "127.0.0.1",
		ListenPort:         1080,
		AuthEnabled:        true,
		Username:           "user",
		PasswordNonce:      []byte("nonce"),
		PasswordCiphertext: []byte("ciphertext"),
	})
	if err != nil {
		t.Fatalf("CreateProxyInstance() error = %v", err)
	}
	if created.Revision != 1 || len(created.PasswordCiphertext) == 0 {
		t.Fatalf("created = %+v", created)
	}
	pending, err := repository.FinalizeProxyApply(context.Background(), []ProxyApplyToken{{
		ID:       created.ID,
		Revision: created.Revision,
	}})
	if err != nil || pending {
		t.Fatalf("FinalizeProxyApply(create) pending=%v error=%v", pending, err)
	}
	created, err = repository.ProxyInstance(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("ProxyInstance(applied) error = %v", err)
	}
	if created.AppliedRevision != created.Revision {
		t.Fatalf("created applied revision = %d, want %d", created.AppliedRevision, created.Revision)
	}

	created.Name = "Updated"
	updated, err := repository.UpdateProxyInstance(context.Background(), created, created.Revision)
	if err != nil {
		t.Fatalf("UpdateProxyInstance() error = %v", err)
	}
	if updated.Revision != 2 || updated.Name != "Updated" {
		t.Fatalf("updated = %+v", updated)
	}
	if _, err := repository.UpdateProxyInstance(
		context.Background(),
		updated,
		1,
	); !errors.Is(err, ErrProxyInstanceRevisionConflict) {
		t.Fatalf("stale UpdateProxyInstance() error = %v", err)
	}
	if err := repository.DeleteProxyInstance(
		context.Background(),
		updated.ID,
		1,
	); !errors.Is(err, ErrProxyInstanceRevisionConflict) {
		t.Fatalf("stale DeleteProxyInstance() error = %v", err)
	}
	if err := repository.DeleteProxyInstance(
		context.Background(),
		updated.ID,
		updated.Revision,
	); err != nil {
		t.Fatalf("DeleteProxyInstance() error = %v", err)
	}
	pendingDelete, err := repository.ProxyInstance(context.Background(), updated.ID)
	if err != nil {
		t.Fatalf("ProxyInstance() pending delete error = %v", err)
	}
	if !pendingDelete.DesiredDeleted ||
		pendingDelete.Revision != updated.Revision+1 ||
		pendingDelete.AppliedRevision != created.Revision {
		t.Fatalf("pending delete = %+v", pendingDelete)
	}
	pending, err = repository.FinalizeProxyApply(context.Background(), []ProxyApplyToken{{
		ID:             pendingDelete.ID,
		Revision:       pendingDelete.Revision,
		DesiredDeleted: true,
	}})
	if err != nil || pending {
		t.Fatalf("FinalizeProxyApply(delete) pending=%v error=%v", pending, err)
	}
	if _, err := repository.ProxyInstance(
		context.Background(),
		updated.ID,
	); !errors.Is(err, ErrProxyInstanceNotFound) {
		t.Fatalf("ProxyInstance() after delete error = %v", err)
	}
}

func TestNetworkCountersRebaselineOnResetAndEpochChange(t *testing.T) {
	t.Parallel()

	repository := newNetworkTestStore(t)
	observedAt := time.Date(2026, 7, 24, 1, 0, 0, 0, time.UTC)
	apply := func(epoch string, rx, tx uint64, at time.Time) {
		t.Helper()
		if err := repository.ApplyNetworkCounterSamples(
			context.Background(),
			[]NetworkCounterSample{{
				ScopeKind:       NetworkScopeLine,
				ScopeID:         "line-1",
				EndpointScopeID: "endpoint-line-1",
				Epoch:           epoch,
				RXBytes:         rx,
				TXBytes:         tx,
				ObservedAt:      at,
				Location:        time.UTC,
			}},
		); err != nil {
			t.Fatalf("ApplyNetworkCounterSamples() error = %v", err)
		}
	}

	apply("boot-1|wwan0", 100, 200, observedAt)
	apply("boot-1|wwan0", 130, 250, observedAt.Add(time.Minute))
	apply("boot-1|wwan0", 5, 6, observedAt.Add(2*time.Minute))
	apply("boot-1|wwan0", 15, 16, observedAt.Add(3*time.Minute))
	apply("boot-2|wwan0", 1000, 2000, observedAt.Add(4*time.Minute))
	apply("boot-2|wwan0", 1010, 2020, observedAt.Add(5*time.Minute))

	usage, err := repository.NetworkUsage(
		context.Background(),
		"2026-07-24",
		"2026-07-24",
	)
	if err != nil {
		t.Fatalf("NetworkUsage() error = %v", err)
	}
	if len(usage) != 1 || usage[0].RXBytes != 50 || usage[0].TXBytes != 80 {
		t.Fatalf("usage = %+v, want rx=50 tx=80", usage)
	}
}

func TestNetworkCountersAggregateByDayAndScope(t *testing.T) {
	t.Parallel()

	repository := newNetworkTestStore(t)
	first := time.Date(2026, 7, 24, 23, 59, 30, 0, time.UTC)
	samples := [][]NetworkCounterSample{
		{
			{
				ScopeKind:  NetworkScopeProxy,
				ScopeID:    "proxy-1",
				Epoch:      "runtime-1",
				RXBytes:    10,
				TXBytes:    20,
				ObservedAt: first,
				Location:   time.UTC,
			},
		},
		{
			{
				ScopeKind:  NetworkScopeProxy,
				ScopeID:    "proxy-1",
				Epoch:      "runtime-1",
				RXBytes:    20,
				TXBytes:    40,
				ObservedAt: first.Add(time.Minute),
				Location:   time.UTC,
			},
		},
	}
	for _, batch := range samples {
		if err := repository.ApplyNetworkCounterSamples(context.Background(), batch); err != nil {
			t.Fatalf("ApplyNetworkCounterSamples() error = %v", err)
		}
	}

	today, err := repository.NetworkUsage(
		context.Background(),
		"2026-07-25",
		"2026-07-25",
	)
	if err != nil {
		t.Fatalf("NetworkUsage(today) error = %v", err)
	}
	if len(today) != 1 || today[0].RXBytes != 5 || today[0].TXBytes != 10 {
		t.Fatalf("today usage = %+v", today)
	}
	previousDay, err := repository.NetworkUsage(
		context.Background(),
		"2026-07-24",
		"2026-07-24",
	)
	if err != nil {
		t.Fatalf("NetworkUsage(previous day) error = %v", err)
	}
	if len(previousDay) != 1 ||
		previousDay[0].RXBytes != 5 ||
		previousDay[0].TXBytes != 10 {
		t.Fatalf("previous day usage = %+v", previousDay)
	}
	month, err := repository.NetworkUsage(
		context.Background(),
		"2026-07-01",
		"2026-07-31",
	)
	if err != nil {
		t.Fatalf("NetworkUsage(month) error = %v", err)
	}
	if len(month) != 1 || month[0].RXBytes != 10 || month[0].TXBytes != 20 {
		t.Fatalf("month usage = %+v", month)
	}
}

func TestNetworkCountersIgnoreOutOfOrderSnapshots(t *testing.T) {
	t.Parallel()

	repository := newNetworkTestStore(t)
	latest := time.Date(2026, 7, 24, 2, 0, 0, 0, time.UTC)
	apply := func(observedAt time.Time, rx uint64) {
		t.Helper()
		if err := repository.ApplyNetworkCounterSamples(
			context.Background(),
			[]NetworkCounterSample{{
				ScopeKind:       NetworkScopeLine,
				ScopeID:         "line-1",
				EndpointScopeID: "endpoint-line-1",
				Epoch:           "boot-1|wwan0",
				RXBytes:         rx,
				TXBytes:         rx,
				ObservedAt:      observedAt,
				Location:        time.UTC,
			}},
		); err != nil {
			t.Fatalf("ApplyNetworkCounterSamples() error = %v", err)
		}
	}
	apply(latest, 100)
	apply(latest.Add(time.Minute), 110)
	apply(latest.Add(-time.Minute), 1)
	apply(latest.Add(2*time.Minute), 120)

	usage, err := repository.NetworkUsage(
		context.Background(),
		"2026-07-24",
		"2026-07-24",
	)
	if err != nil {
		t.Fatalf("NetworkUsage() error = %v", err)
	}
	if len(usage) != 1 || usage[0].RXBytes != 20 || usage[0].TXBytes != 20 {
		t.Fatalf("usage = %+v, want rx=20 tx=20", usage)
	}
}

func TestNetworkCountersResetDirectionsIndependently(t *testing.T) {
	t.Parallel()

	repository := newNetworkTestStore(t)
	at := time.Date(2026, 7, 24, 3, 0, 0, 0, time.UTC)
	apply := func(rx, tx uint64, observedAt time.Time) {
		t.Helper()
		if err := repository.ApplyNetworkCounterSamples(
			context.Background(),
			[]NetworkCounterSample{{
				ScopeKind:       NetworkScopeLine,
				ScopeID:         "line-1",
				EndpointScopeID: "endpoint-line-1",
				Epoch:           "boot-1|wwan0",
				RXBytes:         rx,
				TXBytes:         tx,
				ObservedAt:      observedAt,
				Location:        time.UTC,
			}},
		); err != nil {
			t.Fatalf("ApplyNetworkCounterSamples() error = %v", err)
		}
	}
	apply(100, 100, at)
	apply(10, 150, at.Add(time.Minute))
	apply(20, 160, at.Add(2*time.Minute))

	usage, err := repository.NetworkUsage(
		context.Background(),
		"2026-07-24",
		"2026-07-24",
	)
	if err != nil {
		t.Fatalf("NetworkUsage() error = %v", err)
	}
	if len(usage) != 1 || usage[0].RXBytes != 10 || usage[0].TXBytes != 60 {
		t.Fatalf("usage = %+v, want rx=10 tx=60", usage)
	}
}

func TestNetworkCounterSnapshotRebaselinesMissingLineBeforeInterfaceReuse(t *testing.T) {
	t.Parallel()

	repository := newNetworkTestStore(t)
	at := time.Date(2026, 7, 24, 4, 0, 0, 0, time.UTC)
	apply := func(lineID string, counter uint64, observedAt time.Time) {
		t.Helper()
		if err := repository.ApplyNetworkCounterSnapshot(
			context.Background(),
			[]NetworkCounterSample{{
				ScopeKind:       NetworkScopeLine,
				ScopeID:         lineID,
				EndpointScopeID: "wwan0",
				Epoch:           "boot-1|wwan0",
				RXBytes:         counter,
				TXBytes:         counter,
				ObservedAt:      observedAt,
				Location:        time.UTC,
			}},
			[]string{lineID},
		); err != nil {
			t.Fatalf("ApplyNetworkCounterSnapshot() error = %v", err)
		}
	}

	apply("line-a", 100, at)
	apply("line-a", 150, at.Add(time.Minute))
	apply("line-b", 200, at.Add(2*time.Minute))
	apply("line-b", 250, at.Add(3*time.Minute))
	apply("line-a", 250, at.Add(4*time.Minute))
	apply("line-a", 260, at.Add(5*time.Minute))

	usage, err := repository.NetworkUsage(
		context.Background(),
		"2026-07-24",
		"2026-07-24",
	)
	if err != nil {
		t.Fatalf("NetworkUsage() error = %v", err)
	}
	byLine := make(map[string]NetworkUsage, len(usage))
	for _, item := range usage {
		byLine[item.ScopeID] = item
	}
	if byLine["line-a"].RXBytes != 60 || byLine["line-a"].TXBytes != 60 {
		t.Fatalf("line-a usage = %+v, want 60/60", byLine["line-a"])
	}
	if byLine["line-b"].RXBytes != 50 || byLine["line-b"].TXBytes != 50 {
		t.Fatalf("line-b usage = %+v, want 50/50", byLine["line-b"])
	}
}

func TestNetworkCounterSnapshotRequiresExactActiveLineSamples(t *testing.T) {
	t.Parallel()

	repository := newNetworkTestStore(t)
	sample := NetworkCounterSample{
		ScopeKind:       NetworkScopeLine,
		ScopeID:         "line-a",
		EndpointScopeID: "wwan0",
		Epoch:           "boot-1|wwan0",
		RXBytes:         100,
		TXBytes:         100,
		ObservedAt:      time.Date(2026, 7, 24, 5, 0, 0, 0, time.UTC),
		Location:        time.UTC,
	}
	if err := repository.ApplyNetworkCounterSnapshot(
		context.Background(),
		[]NetworkCounterSample{sample},
		nil,
	); err == nil {
		t.Fatal("ApplyNetworkCounterSnapshot() accepted an inactive line sample")
	}
	if err := repository.ApplyNetworkCounterSnapshot(
		context.Background(),
		nil,
		[]string{"line-a"},
	); err == nil {
		t.Fatal("ApplyNetworkCounterSnapshot() accepted a missing active line sample")
	}
	if err := repository.ApplyNetworkCounterSnapshot(
		context.Background(),
		[]NetworkCounterSample{sample},
		[]string{"line-a"},
	); err != nil {
		t.Fatalf("ApplyNetworkCounterSnapshot() exact sample error = %v", err)
	}
}

func newNetworkTestStore(t *testing.T) *Store {
	t.Helper()
	database, err := platformdb.Open(context.Background(), platformdb.Config{
		TargetPath: filepath.Join(t.TempDir(), "modemdeck.db"),
	})
	if err != nil {
		t.Fatalf("database.Open() error = %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	repository, err := New(database)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return repository
}
