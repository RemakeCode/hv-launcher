package shortcuts

import (
	"errors"
	"path/filepath"
	"testing"

	"hv-launcher/internal/config"
	"hv-launcher/internal/linuwux"
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
		{"heroic launcher", `heroic launch legendary-game`, prefix + ` heroic launch legendary-game`},
		{"flatpak heroic", `run --branch=stable --arch=x86_64 --command=heroic com.heroicgameslauncher.hgl heroic://launch/game`, prefix + ` run --branch=stable --arch=x86_64 --command=heroic com.heroicgameslauncher.hgl heroic://launch/game`},
		{"lutris", `lutris lutris:rungame/game`, prefix + ` lutris lutris:rungame/game`},
		{"flatpak lutris", `run net.lutris.Lutris lutris:rungameid/42`, prefix + ` run net.lutris.Lutris lutris:rungameid/42`},
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
	_, err := ManagedLaunchValue("/plugin/bin/hv-launcher run --app-id 42 -- %command%", "/wrapper", "42")
	if !errors.Is(err, ErrAlreadyManaged) {
		t.Fatalf("got %v", err)
	}
}

func TestRuntimeManagedLaunchValueKeepsHVLauncherOutermost(t *testing.T) {
	wrapper := "/home/deck/homebrew/plugins/hv-launcher/bin/hv-launcher"
	runtime := "/home/deck/.local/bin/linuwux"
	prefix := `'` + wrapper + `' run --app-id '42' -- '` + runtime + `' `
	tests := []struct {
		name     string
		original string
		expected string
	}{
		{"standard", "%command%", prefix + "%command%"},
		{"gamescope", "PROTON_AVX=1 gamescope -f -- %command% --foo", "PROTON_AVX=1 " + prefix + "gamescope -f -- %command% --foo"},
		{"empty", "", prefix + "%command%"},
		{"arguments", "%command% --foo", prefix + "%command% --foo"},
		{"environment", "MANGOHUD=1 %command% --foo", "MANGOHUD=1 " + prefix + "%command% --foo"},
		{"mangohud", "mangohud %command%", prefix + "mangohud %command%"},
		{"environment and mangohud", "MANGOHUD=1 mangohud %command%", "MANGOHUD=1 " + prefix + "mangohud %command%"},
		{"quoted environment and mangohud", `MANGOHUD_CONFIG="fps_limit=60 30" mangohud %command%`, `MANGOHUD_CONFIG="fps_limit=60 30" ` + prefix + "mangohud %command%"},
		{"lutris shortcut", "lutris:rungameid/2", prefix + "%command% lutris:rungameid/2"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			actual, err := ManagedLaunchValueForMode(test.original, wrapper, "42", runtime, linuwux.ModeRuntime)
			if err != nil {
				t.Fatal(err)
			}
			if actual != test.expected {
				t.Fatalf("\ngot:  %s\nwant: %s", actual, test.expected)
			}
		})
	}
}

func TestRuntimeManagedLaunchValueRequiresAbsoluteRuntime(t *testing.T) {
	if _, err := ManagedLaunchValueForMode("%command%", "/wrapper", "42", "linuwux", linuwux.ModeRuntime); err == nil {
		t.Fatal("relative runtime path was accepted")
	}
}

func TestManagerDisablesManagement(t *testing.T) {
	store, err := config.Open(filepath.Join(t.TempDir(), "settings"))
	if err != nil {
		t.Fatal(err)
	}
	manager := &Manager{Store: store, WrapperPath: "/home/deck/homebrew/plugins/hv-launcher/bin/hv-launcher"}
	game, err := manager.Enable("42", "Heroic", true, `FOO=1 %command% "arg"`)
	if err != nil {
		t.Fatal(err)
	}
	if game.ManagedLaunch == "" {
		t.Fatal("managed launch value is empty")
	}
	if err := manager.Disable("42"); err != nil {
		t.Fatal(err)
	}
	if _, exists := store.Game("42"); exists {
		t.Fatal("disabled management record remains")
	}
}
