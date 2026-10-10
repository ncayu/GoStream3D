package orchestrator

import (
	"errors"
	"sync"
	"testing"
)

func TestPortAllocatorExhaustionAndRelease(t *testing.T) {
	a, err := NewPortAllocator(PortRange{10000, 10001}, PortRange{11000, 11001}, PortRange{20000, 20003}, 2)
	if err != nil {
		t.Fatal(err)
	}
	if got := a.Capacity(); got != 2 {
		t.Fatalf("capacity = %d, want 2 independent workers", got)
	}
	first, err := a.Allocate()
	if err != nil {
		t.Fatal(err)
	}
	second, err := a.Allocate()
	if err != nil {
		t.Fatal(err)
	}
	if first.HTTPPort == second.HTTPPort || first.CDPPort == second.CDPPort || first.UDPEnd >= second.UDPStart {
		t.Fatalf("overlapping leases: %+v and %+v", first, second)
	}
	if _, err := a.Allocate(); !errors.Is(err, ErrPortsExhausted) {
		t.Fatalf("want ErrPortsExhausted, got %v", err)
	}
	first.Release()
	first.Release()
	third, err := a.Allocate()
	if err != nil {
		t.Fatal(err)
	}
	if third.HTTPPort != first.HTTPPort || third.CDPPort != first.CDPPort || third.UDPStart != first.UDPStart {
		t.Fatalf("released ports were not reusable: %+v", third)
	}
	second.Release()
	third.Release()
}

func TestPortAllocatorConcurrentUnique(t *testing.T) {
	a, err := NewPortAllocator(PortRange{10000, 10031}, PortRange{11000, 11031}, PortRange{20000, 20063}, 2)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	results := make(chan PortLease, 32)
	errorsCh := make(chan error, 32)
	for range 32 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			lease, err := a.Allocate()
			if err != nil {
				errorsCh <- err
				return
			}
			results <- lease
		}()
	}
	wg.Wait()
	close(results)
	close(errorsCh)
	for err := range errorsCh {
		t.Fatal(err)
	}
	seenHTTP := map[int]bool{}
	seenCDP := map[int]bool{}
	seenUDP := map[int]bool{}
	for lease := range results {
		if seenHTTP[lease.HTTPPort] {
			t.Fatalf("duplicate HTTP port %d", lease.HTTPPort)
		}
		seenHTTP[lease.HTTPPort] = true
		if seenCDP[lease.CDPPort] {
			t.Fatalf("duplicate CDP port %d", lease.CDPPort)
		}
		seenCDP[lease.CDPPort] = true
		for port := lease.UDPStart; port <= lease.UDPEnd; port++ {
			if seenUDP[port] {
				t.Fatalf("duplicate UDP port %d", port)
			}
			seenUDP[port] = true
		}
		lease.Release()
	}
	if len(seenHTTP) != 32 || len(seenCDP) != 32 || len(seenUDP) != 64 {
		t.Fatalf("unexpected allocation counts: HTTP=%d CDP=%d UDP=%d", len(seenHTTP), len(seenCDP), len(seenUDP))
	}
}
