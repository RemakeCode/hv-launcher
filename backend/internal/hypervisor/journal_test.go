package hypervisor

import (
	"strings"
	"testing"
)

func TestNewDeckyFileJournalRequiresRuntimeDirectory(t *testing.T) {
	t.Setenv(deckyRuntimeDirectoryEnvironment, "")

	journal, err := NewDeckyFileJournal()
	if err == nil || !strings.Contains(err.Error(), deckyRuntimeDirectoryEnvironment) {
		t.Fatalf("NewDeckyFileJournal() returned journal=%v, err=%v", journal, err)
	}
}

func TestNewDeckyFileJournalUsesRuntimeDirectory(t *testing.T) {
	runtimeDir := t.TempDir()
	t.Setenv(deckyRuntimeDirectoryEnvironment, runtimeDir)

	journal, err := NewDeckyFileJournal()
	if err != nil {
		t.Fatalf("NewDeckyFileJournal() error = %v", err)
	}

	record := JournalRecord{Phase: "test"}
	if err := journal.Write(record); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	loaded, err := NewFileJournal(runtimeDir).Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if loaded == nil || loaded.Phase != record.Phase {
		t.Fatalf("Load() = %#v, want phase %q", loaded, record.Phase)
	}
}
