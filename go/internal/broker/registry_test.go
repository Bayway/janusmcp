package broker_test

// The session registry used to grow without bound: Remove was never called, so
// every HTTP connection and every daemon-routed CLI invocation left a Session
// and its mcp.Server behind forever, and each global switch iterated over the
// corpses.

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/bayway/janusmcp/internal/broker"
)

func TestRegistryDropsClosedSessions(t *testing.T) {
	ctx := context.Background()
	core, _ := mrtrCore(t)

	s := broker.NewSession(ctx, core, "s1")
	ct, st := mcp.NewInMemoryTransports()
	if _, err := s.Server().Connect(ctx, st, nil); err != nil {
		t.Fatalf("server connect: %v", err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "c", Version: "0"}, nil)
	cs, err := client.Connect(ctx, ct, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	if got := core.Registry.Len(); got != 1 {
		t.Fatalf("registry size while connected = %d, want 1", got)
	}

	if err := cs.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	// Wait for the server side to observe the disconnect.
	if err := waitFor(func() bool { return core.Registry.Len() == 0 }); err != nil {
		t.Fatalf("closed session was not pruned: %v", err)
	}
}

func TestRegistryKeepsNeverConnectedSession(t *testing.T) {
	ctx := context.Background()
	core, _ := mrtrCore(t)

	// The HTTP handler registers a session before the transport attaches, so an
	// entry with no MCP session yet must survive its grace period.
	broker.NewSession(ctx, core, "pending")
	if got := core.Registry.Len(); got != 1 {
		t.Fatalf("registry size = %d, want the pending session to survive", got)
	}
}

// TestUpstreamSessionSingleflight pins that concurrent first calls share one
// upstream. Without it each caller spawns its own process and all but the last
// are orphaned — which would also break the daemon's process-reuse guarantee.
func TestUpstreamSessionSingleflight(t *testing.T) {
	ctx := context.Background()
	core, _ := mrtrCore(t)

	const callers = 20
	var wg sync.WaitGroup
	got := make([]*mcp.ClientSession, callers)
	errs := make([]error, callers)
	start := make(chan struct{})

	for i := range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			got[i], errs[i] = core.Manager.Session(ctx, "acct_a")
		}()
	}
	close(start)
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("caller %d: %v", i, err)
		}
	}
	for i := 1; i < callers; i++ {
		if got[i] != got[0] {
			t.Fatalf("caller %d got a different upstream session than caller 0", i)
		}
	}
}

// waitFor polls until cond holds, so the test does not depend on how promptly
// the SDK tears a session down.
func waitFor(cond func() bool) error {
	for range 200 {
		if cond() {
			return nil
		}
		time.Sleep(10 * time.Millisecond)
	}
	return errors.New("condition not met within 2s")
}
