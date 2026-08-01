package broker

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/bayway/janusmcp/internal/config"
)

func TestSetGlobalPersistsSecurely(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "state.json")
	state := NewBrokerState(path, "a", config.BindingGlobal)
	if err := state.SetGlobal("client_x"); err != nil {
		t.Fatal(err)
	}
	loaded := NewBrokerState(path, "a", config.BindingGlobal)
	if got := loaded.GlobalActive(); got != "client_x" {
		t.Fatalf("active=%q", got)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got != 0o600 {
			t.Fatalf("permissions=%o", got)
		}
	}
	if err := state.SetGlobal("client_y"); err != nil {
		t.Fatalf("replace existing state: %v", err)
	}
	if got := NewBrokerState(path, "a", config.BindingGlobal).GlobalActive(); got != "client_y" {
		t.Fatalf("replaced active=%q", got)
	}
}

func TestSetGlobalDoesNotChangeMemoryWhenPersistenceFails(t *testing.T) {
	dir := t.TempDir()
	blocker := filepath.Join(dir, "not-a-directory")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	state := NewBrokerState(filepath.Join(blocker, "state.json"), "original", config.BindingGlobal)
	if err := state.SetGlobal("new"); err == nil {
		t.Fatal("expected persistence error")
	}
	if got := state.GlobalActive(); got != "original" {
		t.Fatalf("active changed after failed persistence: %q", got)
	}
}
