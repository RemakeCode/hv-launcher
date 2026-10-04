package linuwux

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
