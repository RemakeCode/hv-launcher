package proton

import (
	"archive/tar"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestInstallerPreflightsArchiveWithoutDecompression(t *testing.T) {
	home, _ := installerSteamHome(t)
	archivePath := writeInstallerArchive(t, CompressionGzip, validFixture(fixtureRoot))
	installer := NewInstaller(home)

	preflight, err := installer.PreflightPath(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	if preflight.FileName != filepath.Base(archivePath) || preflight.CompressedBytes <= 0 || preflight.Compression != CompressionGzip {
		t.Fatalf("unexpected preflight: %+v", preflight)
	}
	if len(preflight.Destinations) != 1 || preflight.Destinations[0].ID != "native" {
		t.Fatalf("incomplete preflight: %+v", preflight)
	}
}

func TestInstallerDefersCompressedPayloadValidationUntilInstall(t *testing.T) {
	home, root := installerSteamHome(t)
	archivePath := filepath.Join(t.TempDir(), "claimed-LinUwUx.tar.gz")
	if err := os.WriteFile(archivePath, []byte{0x1f, 0x8b, 0x08, 0x00, 0x00, 0x00, 'x'}, 0o600); err != nil {
		t.Fatal(err)
	}
	installer := NewInstaller(home)
	if _, err := installer.PreflightPath(archivePath); err != nil {
		t.Fatalf("fast preflight decompressed the archive: %v", err)
	}
	if _, err := installer.Install(context.Background(), archivePath, "native", nil); err == nil {
		t.Fatal("invalid compressed payload passed installation validation")
	}
	assertNoFinalOrStaging(t, root)
}

func TestInstallerReportsInstallationPhases(t *testing.T) {
	home, _ := installerSteamHome(t)
	archivePath := writeInstallerArchive(t, CompressionGzip, validFixture(fixtureRoot))
	installer := NewInstaller(home)
	var phases []string
	_, err := installer.Install(context.Background(), archivePath, "native", func(phase string, _ int, _ string) {
		if len(phases) == 0 || phases[len(phases)-1] != phase {
			phases = append(phases, phase)
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"opening-archive", "extracting-and-validating", "validating-staged-tool", "committing"}
	if strings.Join(phases, ",") != strings.Join(want, ",") {
		t.Fatalf("phases = %v, want %v", phases, want)
	}
}

func TestInstallerCommitsValidGzipAndXZArchives(t *testing.T) {
	for _, compression := range []Compression{CompressionGzip, CompressionXZ} {
		t.Run(string(compression), func(t *testing.T) {
			home, root := installerSteamHome(t)
			entries := replaceEntry(validFixture(fixtureRoot), fixtureEntry{
				name: fixtureRoot + "/files/bin/tool", typeFlag: tar.TypeReg, mode: 0o6755, data: []byte("tool"),
			})
			archivePath := writeInstallerArchive(t, compression, entries)
			original, err := os.ReadFile(archivePath)
			if err != nil {
				t.Fatal(err)
			}
			installer := NewInstaller(home)
			_, err = installer.PreflightPath(archivePath)
			if err != nil {
				t.Fatal(err)
			}

			result, err := installer.Install(context.Background(), archivePath, "native", nil)
			if err != nil {
				t.Fatal(err)
			}
			finalRoot := filepath.Join(root, "compatibilitytools.d", fixtureRoot)
			if result.DestinationID != "native" || !result.RestartSteam {
				t.Fatalf("unexpected result: %+v", result)
			}
			if _, err := InspectInstalled(finalRoot); err != nil {
				t.Fatalf("installed tool is invalid: %v", err)
			}
			if info, err := os.Stat(filepath.Join(finalRoot, "files", "empty")); err != nil || !info.IsDir() {
				t.Fatalf("empty directory was not preserved: %v", err)
			}
			link, err := os.Readlink(filepath.Join(finalRoot, "files", "bin", "tool-link"))
			if err != nil || link != "tool" {
				t.Fatalf("symbolic link = %q, %v", link, err)
			}
			toolInfo, err := os.Stat(filepath.Join(finalRoot, "files", "bin", "tool"))
			if err != nil {
				t.Fatal(err)
			}
			hardInfo, err := os.Stat(filepath.Join(finalRoot, "files", "bin", "tool-hard"))
			if err != nil {
				t.Fatal(err)
			}
			if !os.SameFile(toolInfo, hardInfo) {
				t.Fatal("hard link was not preserved")
			}
			if toolInfo.Mode().Perm() != 0o755 || toolInfo.Mode()&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != 0 {
				t.Fatalf("unsafe mode was preserved: %v", toolInfo.Mode())
			}
			stat := toolInfo.Sys().(*syscall.Stat_t)
			if int(stat.Uid) != os.Getuid() || int(stat.Gid) != os.Getgid() {
				t.Fatalf("owner = %d:%d", stat.Uid, stat.Gid)
			}
			current, err := os.ReadFile(archivePath)
			if err != nil || string(current) != string(original) {
				t.Fatalf("source archive was changed: %v", err)
			}
			assertNoInstallerStaging(t, root)
		})
	}
}

func TestInstallerRevalidatesCurrentArchiveAfterPreflight(t *testing.T) {
	home, root := installerSteamHome(t)
	archivePath := writeInstallerArchive(t, CompressionGzip, validFixture(fixtureRoot))
	installer := NewInstaller(home)
	_, err := installer.PreflightPath(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	changed := append(validFixture(fixtureRoot), fixtureEntry{
		name: fixtureRoot + "/files/changed", typeFlag: tar.TypeReg, mode: 0o644, data: []byte("changed"),
	})
	if err := os.WriteFile(archivePath, buildArchive(t, CompressionGzip, changed), 0o600); err != nil {
		t.Fatal(err)
	}

	result, err := installer.Install(context.Background(), archivePath, "native", nil)
	if err != nil || result.SHA256 == "" {
		t.Fatalf("changed but valid archive was not fully validated and installed: %+v, %v", result, err)
	}
	assertNoInstallerStaging(t, root)
}

func TestInstallerRefusesExistingToolWithoutChangingIt(t *testing.T) {
	home, root := installerSteamHome(t)
	archivePath := writeInstallerArchive(t, CompressionGzip, validFixture(fixtureRoot))
	installer := NewInstaller(home)
	finalRoot := filepath.Join(root, "compatibilitytools.d", fixtureRoot)
	if err := os.MkdirAll(finalRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(finalRoot, "keep-me")
	if err := os.WriteFile(marker, []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := installer.Install(context.Background(), archivePath, "native", nil); err == nil {
		t.Fatal("existing compatibility tool was accepted")
	}
	contents, err := os.ReadFile(marker)
	if err != nil || string(contents) != "original" {
		t.Fatalf("existing tool changed: %q, %v", contents, err)
	}
	assertNoInstallerStaging(t, root)
}

func TestInstallerCleansUpAfterStagedValidationFailure(t *testing.T) {
	home, root := installerSteamHome(t)
	archivePath := writeInstallerArchive(t, CompressionGzip, validFixture(fixtureRoot))
	installer := NewInstaller(home)
	installer.beforeStageValidation = func(stagedRoot string) error {
		return os.Remove(filepath.Join(stagedRoot, "version"))
	}

	if _, err := installer.Install(context.Background(), archivePath, "native", nil); err == nil || !strings.Contains(err.Error(), "validate staged") {
		t.Fatalf("unexpected error: %v", err)
	}
	assertNoFinalOrStaging(t, root)
}

func TestInstallerCleansUpAfterCommitFailure(t *testing.T) {
	home, root := installerSteamHome(t)
	archivePath := writeInstallerArchive(t, CompressionXZ, validFixture(fixtureRoot))
	installer := NewInstaller(home)
	installer.rename = func(string, string) error { return errors.New("simulated interruption") }

	if _, err := installer.Install(context.Background(), archivePath, "native", nil); err == nil || !strings.Contains(err.Error(), "simulated interruption") {
		t.Fatalf("unexpected error: %v", err)
	}
	assertNoFinalOrStaging(t, root)
}

func TestInstallerRejectsUnsupportedSuffixAndUnknownDestination(t *testing.T) {
	home, root := installerSteamHome(t)
	archivePath := writeInstallerArchive(t, CompressionGzip, validFixture(fixtureRoot))
	installer := NewInstaller(home)
	if _, err := installer.Install(context.Background(), archivePath, "unknown", nil); err == nil {
		t.Fatal("unknown destination ID was accepted")
	}
	wrongSuffix := archivePath + ".zip"
	if err := os.Rename(archivePath, wrongSuffix); err != nil {
		t.Fatal(err)
	}
	if _, err := installer.PreflightPath(wrongSuffix); err == nil {
		t.Fatal("unsupported archive suffix was accepted")
	}
	assertNoFinalOrStaging(t, root)
}

func installerSteamHome(t *testing.T) (string, string) {
	t.Helper()
	home := t.TempDir()
	root := filepath.Join(home, ".local", "share", "Steam")
	if err := os.MkdirAll(filepath.Join(root, "steamapps"), 0o755); err != nil {
		t.Fatal(err)
	}
	return home, root
}

func writeInstallerArchive(t *testing.T, compression Compression, entries []fixtureEntry) string {
	t.Helper()
	extension := ".tar.gz"
	if compression == CompressionXZ {
		extension = ".tar.xz"
	}
	name := filepath.Join(t.TempDir(), "LinUwUx"+extension)
	if err := os.WriteFile(name, buildArchive(t, compression, entries), 0o600); err != nil {
		t.Fatal(err)
	}
	return name
}

func assertNoFinalOrStaging(t *testing.T, root string) {
	t.Helper()
	if _, err := os.Lstat(filepath.Join(root, "compatibilitytools.d", fixtureRoot)); !os.IsNotExist(err) {
		t.Fatalf("partial final tool exists: %v", err)
	}
	assertNoInstallerStaging(t, root)
}

func assertNoInstallerStaging(t *testing.T, root string) {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(root, "compatibilitytools.d"))
	if os.IsNotExist(err) {
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".hv-launcher-stage-") {
			t.Fatalf("staging directory remains: %s", entry.Name())
		}
	}
}
