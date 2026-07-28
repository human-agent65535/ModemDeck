package callmedia

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/pion/webrtc/v4"
)

func TestDefaultRecoveryWindowIsFifteenSeconds(t *testing.T) {
	core, _, _ := testCore(t, testFormat(8000))
	if core.recoveryTime != 15*time.Second {
		t.Fatalf("default recovery timeout = %s, want 15s", core.recoveryTime)
	}
}

func TestConnectionRecoveryWindowAllowsTransientDisconnect(t *testing.T) {
	session, cancel := testConnectionSession(30 * time.Millisecond)
	defer cancel()
	result := make(chan error, 1)
	go func() { result <- session.connectionLoop() }()

	session.events.updateState(webrtc.PeerConnectionStateDisconnected)
	time.Sleep(10 * time.Millisecond)
	session.events.updateState(webrtc.PeerConnectionStateConnected)

	select {
	case err := <-result:
		t.Fatalf("connectionLoop() returned during recovered disconnect: %v", err)
	case <-time.After(40 * time.Millisecond):
	}
	cancel()
	if err := receive(t, result); err != nil {
		t.Fatalf("connectionLoop() after cancellation = %v", err)
	}
}

func TestConnectionRecoveryWindowExpiresOnce(t *testing.T) {
	session, cancel := testConnectionSession(15 * time.Millisecond)
	defer cancel()
	result := make(chan error, 1)
	go func() { result <- session.connectionLoop() }()

	session.events.updateState(webrtc.PeerConnectionStateFailed)
	if err := receive(t, result); !errors.Is(err, ErrTransportClosed) {
		t.Fatalf("connectionLoop() error = %v, want ErrTransportClosed", err)
	}
}

func TestClosedConnectionEndsWithoutRecoveryDelay(t *testing.T) {
	session, cancel := testConnectionSession(time.Second)
	defer cancel()
	result := make(chan error, 1)
	go func() { result <- session.connectionLoop() }()

	session.events.updateState(webrtc.PeerConnectionStateClosed)
	if err := receive(t, result); !errors.Is(err, ErrTransportClosed) {
		t.Fatalf("connectionLoop() error = %v, want ErrTransportClosed", err)
	}
}

func testConnectionSession(recoveryTime time.Duration) (*Session, context.CancelFunc) {
	ctx, cancel := context.WithCancel(context.Background())
	return &Session{
		ctx:          ctx,
		cancel:       cancel,
		events:       newPeerEvents(),
		recoveryTime: recoveryTime,
	}, cancel
}
