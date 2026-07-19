package proton

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

type Destination struct {
	ID    string `json:"id"`
	Label string `json:"label"`

	root               string
	compatibilityTools string
}

type steamRootCandidate struct {
	id    string
	label string
	path  string
}

func steamRootCandidates(userHome string) []steamRootCandidate {
	return []steamRootCandidate{
		{id: "native", label: "Steam (native)", path: filepath.Join(userHome, ".local", "share", "Steam")},
		{id: "legacy", label: "Steam (legacy location)", path: filepath.Join(userHome, ".steam", "root")},
		{id: "flatpak", label: "Steam (Flatpak)", path: filepath.Join(userHome, ".var", "app", "com.valvesoftware.Steam", "data", "Steam")},
	}
}

func DiscoverDestinations(userHome string) []Destination {
	found := make(map[string]Destination)
	for _, candidate := range steamRootCandidates(userHome) {
		root, err := validateAndResolveSteamRoot(candidate.path)
		if err != nil {
			continue
		}
		if _, duplicate := found[root]; duplicate {
			continue
		}
		found[root] = Destination{
			ID: candidate.id, Label: candidate.label,
			root: root, compatibilityTools: filepath.Join(root, "compatibilitytools.d"),
		}
	}
	destinations := make([]Destination, 0, len(found))
	for _, destination := range found {
		destinations = append(destinations, destination)
	}
	sort.Slice(destinations, func(left, right int) bool { return destinations[left].ID < destinations[right].ID })
	return destinations
}

func ValidateDestination(userHome, requestedID string) (Destination, error) {
	for _, destination := range DiscoverDestinations(userHome) {
		if destination.ID == requestedID {
			return destination, nil
		}
	}
	return Destination{}, fmt.Errorf("destination ID is unknown or no longer available")
}

func validateAndResolveSteamRoot(candidate string) (string, error) {
	if candidate == "" || !filepath.IsAbs(candidate) {
		return "", fmt.Errorf("Steam root must be an absolute path")
	}
	root, err := filepath.EvalSymlinks(filepath.Clean(candidate))
	if err != nil {
		return "", err
	}
	info, err := os.Stat(root)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("Steam root is not a directory")
	}
	steamApps, err := os.Stat(filepath.Join(root, "steamapps"))
	if err != nil || !steamApps.IsDir() {
		return "", fmt.Errorf("Steam root does not contain steamapps")
	}
	return root, nil
}
