package steam

import (
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"hv-launcher/internal/system"
)

var runningIDKeys = []string{"SteamAppId", "SteamGameId", "PRESSURE_VESSEL_APP_ID"}

func ResolveRunningShortcutIDs(reader system.Reader, procRoot string, enabled map[string]bool) []string {
	entries, err := reader.ReadDir(procRoot)
	if err != nil {
		return nil
	}
	found := map[string]bool{}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if _, err := strconv.Atoi(entry.Name()); err != nil {
			continue
		}
		data, err := reader.ReadFile(filepath.Join(procRoot, entry.Name(), "environ"))
		if err != nil {
			continue
		}
		for _, field := range strings.Split(string(data), "\x00") {
			key, value, ok := strings.Cut(field, "=")
			if !ok || !contains(runningIDKeys, key) {
				continue
			}
			if appID, ok := NormalizeRunningID(value); ok && enabled[appID] {
				found[appID] = true
			}
		}
	}
	result := make([]string, 0, len(found))
	for id := range found {
		result = append(result, id)
	}
	sort.Strings(result)
	return result
}

func NormalizeRunningID(value string) (string, bool) {
	parsed, err := strconv.ParseUint(strings.TrimSpace(value), 10, 64)
	if err != nil || parsed == 0 {
		return "", false
	}
	if parsed <= 0xffffffff {
		return strconv.FormatUint(parsed, 10), true
	}
	appID := uint32(parsed >> 32)
	if appID&0x80000000 == 0 {
		return "", false
	}
	return strconv.FormatUint(uint64(appID), 10), true
}

func contains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}
