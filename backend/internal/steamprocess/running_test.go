package steamprocess

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"hv-launcher/internal/system"
)

func TestNormalizeRunningShortcutID(t *testing.T) {
	appID := uint64(0x80000042)
	gameID := (appID << 32) | 0x02000000
	if got, ok := NormalizeRunningID(strconv.FormatUint(gameID, 10)); !ok || got != "2147483714" {
		t.Fatalf("got %q, %v", got, ok)
	}
	if _, ok := NormalizeRunningID(strconv.FormatUint(uint64(10)<<32, 10)); ok {
		t.Fatal("ordinary 64-bit Steam game ID must not normalize as a shortcut")
	}
}

func TestResolveRunningShortcutIDsUsesSupportedEnvironmentKeys(t *testing.T) {
	proc := t.TempDir()
	appID := uint64(0x80000042)
	gameID := (appID << 32) | 0x02000000
	writeRunningTestFile(t, filepath.Join(proc, "100", "environ"), []byte("PATH=/bin\x00SteamAppId="+strconv.FormatUint(gameID, 10)+"\x00"))
	writeRunningTestFile(t, filepath.Join(proc, "101", "environ"), []byte("PRESSURE_VESSEL_APP_ID=2147483714\x00"))
	ids := ResolveRunningShortcutIDs(system.OSReader{}, proc, map[string]bool{"2147483714": true})
	if len(ids) != 1 || ids[0] != "2147483714" {
		t.Fatalf("got %v", ids)
	}
}

func writeRunningTestFile(t *testing.T, path string, contents []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, contents, 0o644); err != nil {
		t.Fatal(err)
	}
}
