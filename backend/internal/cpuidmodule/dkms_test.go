package cpuidmodule

import (
	"errors"
	"strings"
	"testing"
)

func TestParseDKMSConfigAcceptsTheFixedModuleIdentityAndArchiveVersion(t *testing.T) {
	identity, err := ParseDKMSConfig([]byte(validDKMSConfig()))
	if err != nil {
		t.Fatal(err)
	}
	if identity.PackageName != "cpuid_fault_emulation" || identity.PackageVersion != "0.1" ||
		identity.BuiltModuleName != "cpuid_fault_emulation" || identity.Destination != "/updates" ||
		!identity.AutomaticInstall {
		t.Fatalf("unexpected identity: %+v", identity)
	}
}

func TestParseDKMSConfigAcceptsFutureSafeVersion(t *testing.T) {
	config := strings.Replace(validDKMSConfig(), `PACKAGE_VERSION="0.1"`, `PACKAGE_VERSION="0.2-rc1"`, 1)
	identity, err := ParseDKMSConfig([]byte(config))
	if err != nil {
		t.Fatal(err)
	}
	if identity.PackageVersion != "0.2-rc1" {
		t.Fatalf("package version = %q", identity.PackageVersion)
	}
}

func TestParseDKMSConfigRejectsMissingConflictingAndExecutableSyntax(t *testing.T) {
	tests := map[string]string{
		"missing":              strings.Replace(validDKMSConfig(), `AUTOINSTALL="yes"`+"\n", "", 1),
		"conflicting":          validDKMSConfig() + `PACKAGE_VERSION="0.2"` + "\n",
		"unsafe version":       strings.Replace(validDKMSConfig(), `PACKAGE_VERSION="0.1"`, `PACKAGE_VERSION="../../evil"`, 1),
		"command substitution": strings.Replace(validDKMSConfig(), `AUTOINSTALL="yes"`, `AUTOINSTALL="$(id)"`, 1),
		"function":             validDKMSConfig() + "evil() { id; }\n",
		"hook":                 validDKMSConfig() + `POST_BUILD="/tmp/evil"` + "\n",
		"adjacent comment":     strings.Replace(validDKMSConfig(), `AUTOINSTALL="yes"`, `AUTOINSTALL="yes"#hidden`, 1),
	}
	for name, data := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseDKMSConfig([]byte(data)); !errors.Is(err, ErrInvalidDKMSConfig) {
				t.Fatalf("ParseDKMSConfig() error = %v", err)
			}
		})
	}
}
