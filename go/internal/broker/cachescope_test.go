package broker_test

// 2026-07-28 adds ttlMs/cacheScope hints to list responses. The SDK defaults
// cacheScope to "public", which is wrong for a broker: what tools/list returns
// depends on the caller's active account, so no intermediary may share it.

import (
	"context"
	"testing"
)

func TestListToolsIsPrivatelyCacheable(t *testing.T) {
	ctx := context.Background()
	core, _ := mrtrCore(t)
	cs := connectWithElicitation(ctx, t, core, "s1")

	res, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("tools/list: %v", err)
	}
	if res.CacheScope != "private" {
		t.Fatalf("cacheScope = %q, want %q", res.CacheScope, "private")
	}
	if res.TTLMs != 0 {
		t.Fatalf("ttlMs = %d, want 0 (an account switch can invalidate the list at any time)", res.TTLMs)
	}
}
