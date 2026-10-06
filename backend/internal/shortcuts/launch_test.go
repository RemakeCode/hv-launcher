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

func TestRuntimeParamsLaunchComposition(t *testing.T) {
	prefix := `'/wrapper' run --app-id '42' -- '/runtime' `
	for _, test := range []struct {
		name, original, want string
		params               []string
	}{
		{"multiple params before gamescope", `MANGOHUD=1 gamescope -f -- %command%`, `MANGOHUD=1 PROTON_AVX=1 LINUWUX_SYSCALL_HACK=1 ` + prefix + `gamescope -f -- %command%`, []string{"PROTON_AVX", "LINUWUX_SYSCALL_HACK"}},
		{"existing selected assignment", `PROTON_AVX='1' %command%`, `PROTON_AVX='1' ` + prefix + `%command%`, []string{"PROTON_AVX"}},
		{"manual params without selection", `LINUWUX_LEGACY_PROFILE=1 %command%`, `LINUWUX_LEGACY_PROFILE=1 ` + prefix + `%command%`, nil},
		{"shortcut arguments", `lutris:rungameid/2`, `LINUWUX_WIN32U_FREE_GUARD=1 ` + prefix + `%command% lutris:rungameid/2`, []string{"LINUWUX_WIN32U_FREE_GUARD"}},
		{"quoted existing environment", `MANGOHUD_CONFIG="fps_limit=60 30" %command%`, `MANGOHUD_CONFIG="fps_limit=60 30" LINUWUX_LEGACY_PROFILE=1 ` + prefix + `%command%`, []string{"LINUWUX_LEGACY_PROFILE"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := ManagedLaunchValueForMode(test.original, "/wrapper", "42", "/runtime", linuwux.ModeRuntime, test.params...)
			if err != nil || got != test.want {
				t.Fatalf("got %q, %v; want %q", got, err, test.want)
			}
		})
	}

	for _, test := range []struct {
		name, original string
		mode           linuwux.Mode
		params         []string
	}{
		{"conflicting assignment", `PROTON_AVX=0 %command%`, linuwux.ModeRuntime, []string{"PROTON_AVX"}},
		{"duplicate assignments with conflict", `PROTON_AVX=0 PROTON_AVX=1 %command%`, linuwux.ModeRuntime, []string{"PROTON_AVX"}},
		{"unknown param", `%command%`, linuwux.ModeRuntime, []string{"LINUWUX_DEBUG"}},
		{"shell injection", `%command%`, linuwux.ModeRuntime, []string{"PROTON_AVX; echo injected"}},
		{"duplicate selection", `%command%`, linuwux.ModeRuntime, []string{"PROTON_AVX", "PROTON_AVX"}},
		{"Proton selection", `%command%`, linuwux.ModeProton, []string{"PROTON_AVX"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := ManagedLaunchValueForMode(test.original, "/wrapper", "42", "/runtime", test.mode, test.params...); err == nil {
				t.Fatal("invalid params were accepted")
			}
		})
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
