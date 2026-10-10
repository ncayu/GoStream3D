package orchestrator

import (
	"errors"
	"fmt"
	"sync"
)

var ErrPortsExhausted = errors.New("no free port block")

// PortRange uses inclusive endpoints.
type PortRange struct {
	Start int
	End   int
}

func (r PortRange) validate() error {
	if r.Start < 1 || r.End > 65535 || r.Start > r.End {
		return fmt.Errorf("invalid port range %d-%d", r.Start, r.End)
	}
	return nil
}

// PortLease owns HTTP and CDP/TCP ports plus a contiguous WebRTC/UDP block.
// It remains reserved until Release is called, including during container startup.
type PortLease struct {
	HTTPPort int
	CDPPort  int
	UDPStart int
	UDPEnd   int
	id       uint64
	owner    *PortAllocator
}

func (l PortLease) Release() {
	if l.owner != nil {
		l.owner.release(l.id)
	}
}

type PortAllocator struct {
	mu       sync.Mutex
	http     PortRange
	cdp      PortRange
	udp      PortRange
	udpWidth int
	nextID   uint64
	leasing  map[uint64]PortLease
	usedHTTP map[int]bool
	usedCDP  map[int]bool
	usedUDP  map[int]bool
}

// Capacity is the maximum number of independent workers this port pool can host.
func (a *PortAllocator) Capacity() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.capacityLocked()
}

func (a *PortAllocator) Available() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.capacityLocked() - len(a.leasing)
}

func (a *PortAllocator) capacityLocked() int {
	capacity := a.http.End - a.http.Start + 1
	if n := a.cdp.End - a.cdp.Start + 1; n < capacity {
		capacity = n
	}
	if n := (a.udp.End - a.udp.Start + 1) / a.udpWidth; n < capacity {
		capacity = n
	}
	return capacity
}

func NewPortAllocator(httpPorts, cdpPorts, udpPorts PortRange, udpPortsPerWorker int) (*PortAllocator, error) {
	if err := httpPorts.validate(); err != nil {
		return nil, err
	}
	if err := cdpPorts.validate(); err != nil {
		return nil, err
	}
	if httpPorts.Start <= cdpPorts.End && cdpPorts.Start <= httpPorts.End {
		return nil, errors.New("HTTP and CDP TCP port ranges must not overlap")
	}
	if err := udpPorts.validate(); err != nil {
		return nil, err
	}
	if udpPortsPerWorker < 1 || udpPortsPerWorker > udpPorts.End-udpPorts.Start+1 {
		return nil, fmt.Errorf("invalid UDP block size %d", udpPortsPerWorker)
	}
	return &PortAllocator{
		http: httpPorts, cdp: cdpPorts, udp: udpPorts, udpWidth: udpPortsPerWorker,
		leasing: make(map[uint64]PortLease), usedHTTP: make(map[int]bool), usedCDP: make(map[int]bool), usedUDP: make(map[int]bool),
	}, nil
}

// Allocate reserves both kinds of ports atomically. Docker remains the final
// arbiter of host port availability when other processes use this same range.
func (a *PortAllocator) Allocate() (PortLease, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	httpPort := 0
	for port := a.http.Start; port <= a.http.End; port++ {
		if !a.usedHTTP[port] {
			httpPort = port
			break
		}
	}
	if httpPort == 0 {
		return PortLease{}, ErrPortsExhausted
	}
	cdpPort := 0
	for port := a.cdp.Start; port <= a.cdp.End; port++ {
		if !a.usedCDP[port] {
			cdpPort = port
			break
		}
	}
	if cdpPort == 0 {
		return PortLease{}, ErrPortsExhausted
	}

	udpStart := 0
	for start := a.udp.Start; start <= a.udp.End-a.udpWidth+1; start++ {
		free := true
		for port := start; port < start+a.udpWidth; port++ {
			if a.usedUDP[port] {
				free = false
				break
			}
		}
		if free {
			udpStart = start
			break
		}
	}
	if udpStart == 0 {
		return PortLease{}, ErrPortsExhausted
	}

	a.nextID++
	lease := PortLease{HTTPPort: httpPort, CDPPort: cdpPort, UDPStart: udpStart, UDPEnd: udpStart + a.udpWidth - 1, id: a.nextID, owner: a}
	a.leasing[lease.id] = lease
	a.usedHTTP[httpPort] = true
	a.usedCDP[cdpPort] = true
	for port := lease.UDPStart; port <= lease.UDPEnd; port++ {
		a.usedUDP[port] = true
	}
	return lease, nil
}

func (a *PortAllocator) release(id uint64) {
	a.mu.Lock()
	defer a.mu.Unlock()
	lease, ok := a.leasing[id]
	if !ok {
		return
	}
	delete(a.leasing, id)
	delete(a.usedHTTP, lease.HTTPPort)
	delete(a.usedCDP, lease.CDPPort)
	for port := lease.UDPStart; port <= lease.UDPEnd; port++ {
		delete(a.usedUDP, port)
	}
}
