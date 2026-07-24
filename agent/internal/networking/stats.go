package networking

import (
	"net"
	"sync"
	"sync/atomic"
)

type ProxyCounters struct {
	BytesUp           uint64
	BytesDown         uint64
	Connections       uint64
	ActiveConnections uint64
}

type trafficCounters struct {
	bytesUp     atomic.Uint64
	bytesDown   atomic.Uint64
	mu          sync.Mutex
	connections uint64
	active      uint64
}

func (counters *trafficCounters) wrap(connection net.Conn) net.Conn {
	counters.mu.Lock()
	counters.connections++
	counters.active++
	counters.mu.Unlock()
	return &countingConnection{
		Conn:     connection,
		counters: counters,
	}
}

func (counters *trafficCounters) snapshot() ProxyCounters {
	counters.mu.Lock()
	connections := counters.connections
	active := counters.active
	counters.mu.Unlock()
	return ProxyCounters{
		BytesUp:           counters.bytesUp.Load(),
		BytesDown:         counters.bytesDown.Load(),
		Connections:       connections,
		ActiveConnections: active,
	}
}

type countingConnection struct {
	net.Conn
	counters  *trafficCounters
	closeOnce sync.Once
}

func (connection *countingConnection) Read(buffer []byte) (int, error) {
	count, err := connection.Conn.Read(buffer)
	if count > 0 {
		connection.counters.bytesDown.Add(uint64(count))
	}
	return count, err
}

func (connection *countingConnection) Write(buffer []byte) (int, error) {
	count, err := connection.Conn.Write(buffer)
	if count > 0 {
		connection.counters.bytesUp.Add(uint64(count))
	}
	return count, err
}

func (connection *countingConnection) Close() error {
	connection.closeOnce.Do(func() {
		connection.counters.mu.Lock()
		connection.counters.active--
		connection.counters.mu.Unlock()
	})
	return connection.Conn.Close()
}

func (connection *countingConnection) CloseWrite() error {
	return closeWrite(connection.Conn)
}
