package linuwux

import "fmt"

type Mode string

const (
	ModeProton  Mode = "proton"
	ModeRuntime Mode = "runtime"
)

func (m Mode) Valid() bool {
	return m == ModeProton || m == ModeRuntime
}

func (m Mode) ResolveLegacyMode() Mode {
	if m == "" {
		return ModeProton
	}

	return m
}

func ValidateParams(mode Mode, params []string) error {
	if len(params) > 0 && mode != ModeRuntime {
		return fmt.Errorf("LinUwUx params require runtime mode")
	}
	seen := make(map[string]bool, len(params))
	for _, param := range params {
		switch param {
		case "PROTON_AVX", "LINUWUX_SYSCALL_HACK", "LINUWUX_LEGACY_PROFILE", "LINUWUX_WIN32U_FREE_GUARD":
		default:
			return fmt.Errorf("unsupported LinUwUx param %q", param)
		}
		if seen[param] {
			return fmt.Errorf("duplicate LinUwUx param %q", param)
		}
		seen[param] = true
	}
	return nil
}
