package linuwux

import "testing"

func TestAvailabilityResolve(t *testing.T) {
	tests := []struct {
		name         string
		availability Availability
		want         Mode
		wantResolved bool
	}{
		{name: "proton only", availability: Availability{Proton: true}, want: ModeProton, wantResolved: true},
		{name: "runtime only", availability: Availability{Runtime: true}, want: ModeRuntime, wantResolved: true},
		{name: "both prefer proton", availability: Availability{Proton: true, Runtime: true}, want: ModeProton, wantResolved: true},
		{name: "neither"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, resolved := test.availability.Resolve()
			if got != test.want || resolved != test.wantResolved {
				t.Fatalf("Availability.Resolve() = %q, %t; want %q, %t", got, resolved, test.want, test.wantResolved)
			}
		})
	}
}

func TestModeValidationAndLegacyResolution(t *testing.T) {
	if Mode("automatic").Valid() {
		t.Fatal("unsupported mode was accepted")
	}
	if got := Mode("").ResolveLegacyMode(); got != ModeProton {
		t.Fatalf("missing mode resolved to %q", got)
	}
}
