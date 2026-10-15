package callmedia

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

func TestPreparedSocketValidatesBeforeHardwareAndBindsSameSession(t *testing.T) {
	for _, rate := range []int{8000, 16000} {
		t.Run(fmt.Sprint(rate), func(t *testing.T) {
			core, opener, _ := socketTestCore(t, testFormat(rate))
			socket := newFakeSocket()
			session, err := core.PrepareSocket(context.Background(), "prepared", "owner", socket)
			if err != nil {
				t.Fatal(err)
			}
			if opener.opens.Load() != 0 {
				t.Fatal("preparation acquired hardware")
			}
			socket.inbound <- SocketFrame(0, 0, socketTestOpus(t))
			receive(t, session.UplinkReady())
			if !session.TransportReady() {
				t.Fatal("valid socket not ready")
			}
			if session.socket.receiver != nil {
				t.Fatal("pre-dial audio has a jitter queue")
			}
			if err := core.ReconcileActiveCalls(context.Background(), nil); err != nil {
				t.Fatal(err)
			}
			select {
			case <-session.Done():
				t.Fatal("snapshot closed undialed preparation")
			default:
			}
			authorizeCall(t, core, "actual")
			if err := core.BindPreparedSocket(context.Background(), "prepared", ActiveCall{ID: "actual", State: CallStateActive}, session); err != nil {
				t.Fatal(err)
			}
			if session.CallID() != "actual" || session.Format().SampleRate != rate {
				t.Fatal("binding lost identity or PCM rate")
			}
			socket.inbound <- SocketFrame(1, 320, socketTestOpus(t))
			receive(t, opener.endpoint.started)
			_ = receiveNonSilentPCM(t, opener.endpoint.writes)
			if opener.opens.Load() != 1 {
				t.Fatal("more than one hardware endpoint")
			}
			if err := core.ReleaseOwner(context.Background(), "actual", "owner"); err != nil {
				t.Fatal(err)
			}
			receive(t, session.Done())
		})
	}
}

func TestPreparedSocketInvalidAudioCancelAndBindPreconditions(t *testing.T) {
	core, opener, _ := socketTestCore(t, testFormat(16000))
	session, err := core.PrepareSocket(context.Background(), "prepared", "owner", newFakeSocket())
	if err != nil {
		t.Fatal(err)
	}
	authorizeCall(t, core, "actual")
	if err := core.BindPreparedSocket(context.Background(), "prepared", ActiveCall{ID: "actual", State: CallStateActive}, session); !errors.Is(err, ErrCanceled) {
		t.Fatal("unready socket bound", err)
	}
	if _, err := core.PrepareSocket(context.Background(), "prepared", "other", newFakeSocket()); !errors.Is(err, ErrCallInUse) {
		t.Fatal("second owner accepted", err)
	}
	if err := session.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool {
		core.mu.Lock()
		defer core.mu.Unlock()
		return core.lifetimes["prepared"] == nil && core.owners["prepared"] == nil
	})
	if opener.opens.Load() != 0 {
		t.Fatal("cancelled preparation touched hardware")
	}
	socket := newFakeSocket()
	invalid, err := core.PrepareSocket(context.Background(), "invalid", "owner", socket)
	if err != nil {
		t.Fatal(err)
	}
	socket.inbound <- SocketFrame(0, 0, []byte{0xff})
	receive(t, invalid.Done())
	if invalid.TransportReady() || !errors.Is(invalid.Err(), ErrInvalidAudio) {
		t.Fatal("invalid Opus proves readiness")
	}
}

func TestPreparedBindingDoesNotRaceCancellation(t *testing.T) {
	core, _, _ := socketTestCore(t, testFormat(16000))
	socket := newFakeSocket()
	session, err := core.PrepareSocket(context.Background(), "prepared", "owner", socket)
	if err != nil {
		t.Fatal(err)
	}
	socket.inbound <- SocketFrame(0, 0, socketTestOpus(t))
	receive(t, session.UplinkReady())
	authorizeCall(t, core, "actual")
	done := make(chan error, 1)
	go func() {
		done <- core.BindPreparedSocket(context.Background(), "prepared", ActiveCall{ID: "actual", State: CallStateActive}, session)
	}()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_ = session.Close(ctx)
	select {
	case <-done:
	case <-ctx.Done():
		t.Fatal("bind/cancel deadlock")
	}
	receive(t, session.Done())
}

func TestRepeatedPreparedSocketCancellationReleasesEveryLifetime(t *testing.T) {
	core, opener, _ := socketTestCore(t, testFormat(16000))
	for i := 0; i < 20; i++ {
		id := fmt.Sprintf("prepared-%d", i)
		session, err := core.PrepareSocket(context.Background(), id, "owner", newFakeSocket())
		if err != nil {
			t.Fatal(err)
		}
		if err := session.Close(context.Background()); err != nil {
			t.Fatal(err)
		}
		eventually(t, func() bool {
			core.mu.Lock()
			defer core.mu.Unlock()
			return core.lifetimes[id] == nil && core.owners[id] == nil
		})
	}
	if opener.opens.Load() != 0 {
		t.Fatal("cancelled preparations opened hardware")
	}
	core.mu.Lock()
	defer core.mu.Unlock()
	if len(core.lifetimes) != 0 {
		t.Fatal("preparation lifetimes leaked", len(core.lifetimes))
	}
}
