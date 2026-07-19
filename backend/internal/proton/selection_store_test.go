package proton

import (
	"errors"
	"testing"
	"time"
)

func TestSelectionStoreExpiresAndDoesNotSurviveRestart(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	store := NewSelectionStore()
	store.now = func() time.Time { return now }
	store.ttl = time.Minute
	record, err := store.Put("/tmp/tool.tar.xz", Preflight{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Get(record.ID); err != nil {
		t.Fatal(err)
	}
	now = now.Add(2 * time.Minute)
	if _, err := store.Get(record.ID); !errors.Is(err, ErrSelectionUnavailable) {
		t.Fatalf("expired record error = %v", err)
	}
	if _, err := NewSelectionStore().Get(record.ID); !errors.Is(err, ErrSelectionUnavailable) {
		t.Fatalf("restarted store retained record: %v", err)
	}
}
