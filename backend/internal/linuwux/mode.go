package linuwux

type Mode string

type Availability struct {
	Proton  bool
	Runtime bool
}

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

func (a Availability) Resolve() (Mode, bool) {
	switch {
	case a.Proton && !a.Runtime:
		return ModeProton, true
	case a.Runtime && !a.Proton:
		return ModeRuntime, true
	case a.Proton && a.Runtime:
		return ModeProton, true
	default:
		return "", false
	}
}
