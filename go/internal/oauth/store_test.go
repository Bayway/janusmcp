package oauth_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bayway/janusmcp/internal/oauth"
	"github.com/bayway/janusmcp/internal/vault"
)

// Verifies the refresh path: an expired token is refreshed via the token endpoint,
// the new access token is returned, and a subsequent call serves it without refreshing.
func TestAccessTokenRefresh(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if r.Form.Get("grant_type") != "refresh_token" || r.Form.Get("refresh_token") != "r1" {
			http.Error(w, "bad grant", http.StatusBadRequest)
			return
		}
		atomic.AddInt32(&hits, 1)
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"newtok","token_type":"bearer","expires_in":3600}`))
	}))
	defer srv.Close()

	dir := t.TempDir()
	v, err := vault.NewFile(filepath.Join(dir, "v.enc"), filepath.Join(dir, "v.key"))
	if err != nil {
		t.Fatal(err)
	}
	providers := map[string]oauth.Provider{"test": {TokenURL: srv.URL, ClientID: "cid"}}
	store := oauth.NewStore(v, providers)

	// Seed an already-expired token that carries a refresh token.
	if err := store.Put("acme", "test", oauth.Token{
		AccessToken:  "old",
		RefreshToken: "r1",
		ExpiresIn:    1,
		ObtainedAt:   time.Now().Add(-time.Hour),
	}); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	got, err := store.AccessToken(ctx, "acme")
	if err != nil {
		t.Fatalf("AccessToken: %v", err)
	}
	if got != "newtok" {
		t.Fatalf("got %q, want newtok", got)
	}

	// Second call: token is now valid → no extra refresh hit.
	if _, err := store.AccessToken(ctx, "acme"); err != nil {
		t.Fatal(err)
	}
	if h := atomic.LoadInt32(&hits); h != 1 {
		t.Fatalf("expected exactly 1 refresh, got %d", h)
	}
}
