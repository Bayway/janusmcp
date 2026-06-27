package vault_test

import (
	"path/filepath"
	"testing"

	"github.com/bayway/janusmcp/internal/vault"
)

// Round-trips the encrypted-file backend: set, get, overwrite, delete, and verify
// the data is persisted encrypted (a fresh instance with the same key reads it back).
func TestFileVaultRoundTrip(t *testing.T) {
	dir := t.TempDir()
	encPath := filepath.Join(dir, "v.enc")
	keyPath := filepath.Join(dir, "v.key")

	v, err := vault.NewFile(encPath, keyPath)
	if err != nil {
		t.Fatal(err)
	}

	if err := v.Set("supabase_x", "pat_123"); err != nil {
		t.Fatal(err)
	}
	if got, err := v.Get("supabase_x"); err != nil || got != "pat_123" {
		t.Fatalf("get: got %q err %v", got, err)
	}

	// Overwrite.
	if err := v.Set("supabase_x", "pat_456"); err != nil {
		t.Fatal(err)
	}
	if got, _ := v.Get("supabase_x"); got != "pat_456" {
		t.Fatalf("overwrite: got %q", got)
	}

	// A new instance with the same key file must decrypt existing data.
	v2, err := vault.NewFile(encPath, keyPath)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := v2.Get("supabase_x"); err != nil || got != "pat_456" {
		t.Fatalf("reopen: got %q err %v", got, err)
	}

	// Delete.
	if err := v2.Delete("supabase_x"); err != nil {
		t.Fatal(err)
	}
	if _, err := v2.Get("supabase_x"); err == nil {
		t.Fatalf("expected error after delete")
	}
}
