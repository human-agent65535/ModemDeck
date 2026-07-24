package networking

import (
	"io"
	"net"
	"sync"
	"sync/atomic"
	"testing"
)

func TestCountingConnectionTracksDirectionAndLifecycle(t *testing.T) {
	local, peer := net.Pipe()
	defer peer.Close()

	var counters trafficCounters
	connection := counters.wrap(local)
	if snapshot := counters.snapshot(); snapshot.Connections != 1 ||
		snapshot.ActiveConnections != 1 {
		t.Fatalf("initial counters = %+v", snapshot)
	}

	downDone := make(chan error, 1)
	go func() {
		_, err := peer.Write([]byte("down"))
		downDone <- err
	}()
	buffer := make([]byte, 4)
	if _, err := io.ReadFull(connection, buffer); err != nil {
		t.Fatalf("read downstream: %v", err)
	}
	if err := <-downDone; err != nil {
		t.Fatalf("write downstream: %v", err)
	}

	upDone := make(chan error, 1)
	go func() {
		_, err := io.ReadFull(peer, make([]byte, 2))
		upDone <- err
	}()
	if _, err := connection.Write([]byte("up")); err != nil {
		t.Fatalf("write upstream: %v", err)
	}
	if err := <-upDone; err != nil {
		t.Fatalf("read upstream: %v", err)
	}

	if err := connection.Close(); err != nil {
		t.Fatalf("close connection: %v", err)
	}
	if err := connection.Close(); err != nil {
		t.Fatalf("close connection twice: %v", err)
	}
	snapshot := counters.snapshot()
	if snapshot.BytesDown != 4 ||
		snapshot.BytesUp != 2 ||
		snapshot.Connections != 1 ||
		snapshot.ActiveConnections != 0 {
		t.Fatalf("final counters = %+v", snapshot)
	}
}

func TestTrafficCountersSnapshotMaintainsConnectionInvariant(t *testing.T) {
	var counters trafficCounters
	var workers sync.WaitGroup
	var finished atomic.Bool

	for range 8 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for range 2_000 {
				local, peer := net.Pipe()
				connection := counters.wrap(local)
				_ = connection.Close()
				_ = peer.Close()
			}
		}()
	}
	go func() {
		workers.Wait()
		finished.Store(true)
	}()

	for !finished.Load() {
		snapshot := counters.snapshot()
		if snapshot.ActiveConnections > snapshot.Connections {
			t.Fatalf("inconsistent counters = %+v", snapshot)
		}
	}
	snapshot := counters.snapshot()
	if snapshot.ActiveConnections != 0 ||
		snapshot.Connections != 16_000 {
		t.Fatalf("final counters = %+v", snapshot)
	}
}

func TestCountingConnectionForwardsCloseWrite(t *testing.T) {
	local, peer := net.Pipe()
	defer peer.Close()
	underlying := &closeWriteTrackingConnection{Conn: local}

	var counters trafficCounters
	connection := counters.wrap(underlying)
	writer, ok := connection.(interface{ CloseWrite() error })
	if !ok {
		t.Fatal("counting connection does not expose CloseWrite")
	}
	if err := writer.CloseWrite(); err != nil {
		t.Fatalf("close write: %v", err)
	}
	if underlying.closeWriteCalls.Load() != 1 {
		t.Fatalf(
			"underlying CloseWrite calls = %d",
			underlying.closeWriteCalls.Load(),
		)
	}
	_ = connection.Close()
}

type closeWriteTrackingConnection struct {
	net.Conn
	closeWriteCalls atomic.Int32
}

func (connection *closeWriteTrackingConnection) CloseWrite() error {
	connection.closeWriteCalls.Add(1)
	return nil
}
