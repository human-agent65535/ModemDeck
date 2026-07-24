package modemmanager

import (
	"context"
	"testing"
)

func TestATTransportPreservesEmptySuccessfulResponse(t *testing.T) {
	t.Parallel()
	caller := newFakeCaller(emptyLineObjects(true, true))
	provider := newTestProvider(caller)
	snapshot, err := provider.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}
	if len(snapshot.Lines) != 1 {
		t.Fatalf("lines = %d, want 1", len(snapshot.Lines))
	}
	caller.calls = nil

	response, err := provider.ATTransport(snapshot.Lines[0].ID).Command(
		context.Background(),
		`AT+QCFG="ims",1`,
	)
	if err != nil {
		t.Fatalf("Command() error = %v", err)
	}
	if response != "" {
		t.Fatalf("response = %q, want empty successful response", response)
	}
}
