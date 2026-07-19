package proton

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDiscoverDestinationsCanonicalizesAndDeduplicatesSteamRoots(t *testing.T) {
	home := t.TempDir()
	native := filepath.Join(home, ".local", "share", "Steam")
	if err := os.MkdirAll(filepath.Join(native, "steamapps"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(home, ".steam"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(native, filepath.Join(home, ".steam", "root")); err != nil {
		t.Fatal(err)
	}
	flatpak := filepath.Join(home, ".var", "app", "com.valvesoftware.Steam", "data", "Steam")
	if err := os.MkdirAll(filepath.Join(flatpak, "steamapps"), 0o755); err != nil {
		t.Fatal(err)
	}

	destinations := DiscoverDestinations(home)
	if len(destinations) != 2 {
		t.Fatalf("destinations = %+v", destinations)
	}
	for _, destination := range destinations {
		if destination.compatibilityTools != filepath.Join(destination.root, "compatibilitytools.d") || destination.ID == "" || destination.Label == "" {
			t.Fatalf("unexpected destination: %+v", destination)
		}
		encoded, err := json.Marshal(destination)
		if err != nil {
			t.Fatal(err)
		}
		if string(encoded) == "" || strings.Contains(string(encoded), destination.root) {
			t.Fatalf("destination JSON exposed filesystem path: %s", encoded)
		}
	}
}

func TestValidateDestinationRejectsArbitraryAndIncompleteRoots(t *testing.T) {
	home := t.TempDir()
	native := filepath.Join(home, ".local", "share", "Steam")
	if err := os.MkdirAll(filepath.Join(native, "steamapps"), 0o755); err != nil {
		t.Fatal(err)
	}
	destination, err := ValidateDestination(home, "native")
	if err != nil || destination.root == "" {
		t.Fatalf("valid destination failed: %+v %v", destination, err)
	}

	arbitrary := filepath.Join(home, "arbitrary")
	if err := os.MkdirAll(filepath.Join(arbitrary, "steamapps"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := ValidateDestination(home, arbitrary); err == nil {
		t.Fatal("accepted an arbitrary Steam-looking root")
	}
	if _, err := ValidateDestination(home, "unknown"); err == nil {
		t.Fatal("accepted an unknown destination ID")
	}

	incomplete := filepath.Join(home, ".var", "app", "com.valvesoftware.Steam", "data", "Steam")
	if err := os.MkdirAll(incomplete, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := ValidateDestination(home, "flatpak"); err == nil {
		t.Fatal("accepted a root without steamapps")
	}
}
