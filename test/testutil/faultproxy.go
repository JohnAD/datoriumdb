package testutil

import (
	"io"
	"net"
	"sync"
	"sync/atomic"
	"testing"
)

// FaultyProxy is a TCP passthrough proxy that can be toggled "down" to
// simulate a network partition between a client and a real backend
// server, without needing OS-level firewall rules (which the sandboxed
// test environment may not permit). Point a server's __servers.json
// baseURL at the proxy's address instead of the real target, then call
// SetDown(true) mid-test to simulate the target becoming unreachable and
// SetDown(false) to simulate recovery.
type FaultyProxy struct {
	Addr string

	ln      net.Listener
	backend string
	down    atomic.Bool
	wg      sync.WaitGroup
	closeMu sync.Mutex
	closed  bool
}

// NewFaultyProxy starts listening on an ephemeral local port and forwards
// accepted connections to backendAddr until closed or set down.
func NewFaultyProxy(t testing.TB, backendAddr string) *FaultyProxy {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("faulty proxy listen: %v", err)
	}
	p := &FaultyProxy{
		Addr:    ln.Addr().String(),
		ln:      ln,
		backend: backendAddr,
	}
	p.wg.Add(1)
	go p.acceptLoop()
	t.Cleanup(p.Close)
	return p
}

// SetDown toggles fault injection. While down, newly accepted connections
// are closed immediately (simulating an unreachable host); existing
// connections are unaffected in-flight (recovery is observed on the next
// connection attempt, matching real network-partition test scenarios).
func (p *FaultyProxy) SetDown(down bool) {
	p.down.Store(down)
}

// IsDown reports the current fault-injection state.
func (p *FaultyProxy) IsDown() bool {
	return p.down.Load()
}

// BaseURL returns "http://{addr}" for use as a __servers.json baseURL.
func (p *FaultyProxy) BaseURL() string {
	return "http://" + p.Addr
}

func (p *FaultyProxy) acceptLoop() {
	defer p.wg.Done()
	for {
		conn, err := p.ln.Accept()
		if err != nil {
			return
		}
		if p.down.Load() {
			conn.Close()
			continue
		}
		go p.forward(conn)
	}
}

func (p *FaultyProxy) forward(client net.Conn) {
	defer client.Close()
	backend, err := net.Dial("tcp", p.backend)
	if err != nil {
		return
	}
	defer backend.Close()
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); io.Copy(backend, client) }()
	go func() { defer wg.Done(); io.Copy(client, backend) }()
	wg.Wait()
}

// Close stops accepting new connections. Safe to call multiple times.
func (p *FaultyProxy) Close() {
	p.closeMu.Lock()
	defer p.closeMu.Unlock()
	if p.closed {
		return
	}
	p.closed = true
	_ = p.ln.Close()
	p.wg.Wait()
}
