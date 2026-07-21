package shortcuts

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"hv-launcher/internal/config"
)

func TestManagedLaunchValuePreservesLaunchForms(t *testing.T) {
	wrapper := "/home/deck/.local/share/hv game/wrapper"
	prefix := `'` + wrapper + `' run --app-id '42' -- %command%`
	tests := []struct {
		name     string
		original string
		expected string
	}{
		{"empty", "", prefix},
		{"arguments only", "run com.heroicgameslauncher.hgl heroic://game", prefix + " run com.heroicgameslauncher.hgl heroic://game"},
		{"command token", "%command% --foo", prefix + " --foo"},
		{"environment prefix", "MANGOHUD=1 %command% --foo", "MANGOHUD=1 " + prefix + " --foo"},
		{"quoted arguments", `%command% --name "My Game"`, prefix + ` --name "My Game"`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			actual, err := ManagedLaunchValue(test.original, wrapper, "42")
			if err != nil {
				t.Fatal(err)
			}
			if actual != test.expected {
				t.Fatalf("\ngot:  %s\nwant: %s", actual, test.expected)
			}
		})
	}
}

func TestManagedLaunchValueRejectsNestedWrapper(t *testing.T) {
	_, err := ManagedLaunchValue("/x/hv-launcher-wrapper run -- %command%", "/wrapper", "42")
	if !errors.Is(err, ErrAlreadyManaged) {
		t.Fatalf("got %v", err)
	}
}

func TestLauncherCommandFixturesRemainInsideWrapper(t *testing.T) {
	fixtures := []string{
		`heroic launch legendary-game`,
		`run --branch=stable --arch=x86_64 --command=heroic com.heroicgameslauncher.hgl heroic://launch/game`,
		`lutris lutris:rungame/game`,
		`run net.lutris.Lutris lutris:rungameid/42`,
	}
	for _, original := range fixtures {
		managed, err := ManagedLaunchValue(original, "/persistent/hv-launcher-wrapper", "2147483714")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasSuffix(managed, original) || !strings.Contains(managed, "-- %command%") {
			t.Fatalf("launcher command was not preserved: %s", managed)
		}
	}
}

func TestManagerRestoresExactValueAndReportsConflict(t *testing.T) {
	store, err := config.Open(filepath.Join(t.TempDir(), "settings"))
	if err != nil {
		t.Fatal(err)
	}
	manager := &Manager{Store: store, WrapperPath: "/persistent/hv-launcher-wrapper"}
	game, err := manager.Enable("42", "Heroic", true, `FOO=1 %command% "arg"`)
	if err != nil {
		t.Fatal(err)
	}
	conflict, err := manager.Restore("42", game.ManagedLaunch+" user-edit")
	if err != nil {
		t.Fatal(err)
	}
	if !conflict.Conflict || !strings.Contains(conflict.Message, "changed") {
		t.Fatalf("unexpected conflict: %+v", conflict)
	}
	if _, exists := store.Game("42"); !exists {
		t.Fatal("conflicted record was removed")
	}
	restored, err := manager.Restore("42", game.ManagedLaunch)
	if err != nil {
		t.Fatal(err)
	}
	if restored.Conflict || restored.OriginalLaunch != game.OriginalLaunch {
		t.Fatalf("unexpected restore: %+v", restored)
	}
	if _, exists := store.Game("42"); exists {
		t.Fatal("restored record remains")
	}
}
