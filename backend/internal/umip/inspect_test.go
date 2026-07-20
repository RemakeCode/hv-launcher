package umip

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInspectLimineRecognizesConfiguredArgumentsWithoutChangingTheFile(t *testing.T) {
	for _, argument := range []string{"clearcpuid=514", "clearcpuid=umip"} {
		t.Run(argument, func(t *testing.T) {
			paths := testPaths(t)
			contents := "ESP_PATH=\"/boot\"\nKERNEL_CMDLINE[default]+=\"quiet " + argument + "\"\n"
			writeTestFile(t, paths.LimineConfiguration, contents, 0o644)
			writeTestFile(t, paths.LimineUpdaters[0], "updater", 0o755)

			result := NewInspector(paths).Inspect(true)

			candidate := onlyCandidate(t, result)
			if candidate.Bootloader != BootloaderLimine || candidate.State != StateRestartRequired || candidate.ExistingArgument != argument {
				t.Fatalf("unexpected candidate: %+v", candidate)
			}
			unchanged, err := os.ReadFile(paths.LimineConfiguration)
			if err != nil {
				t.Fatal(err)
			}
			if string(unchanged) != contents {
				t.Fatalf("read-only inspection changed Limine configuration: %q", unchanged)
			}
		})
	}
}

func TestInspectLimineUsesOnlyActiveDefaultAssignments(t *testing.T) {
	paths := testPaths(t)
	writeTestFile(t, paths.LimineConfiguration, strings.Join([]string{
		"# KERNEL_CMDLINE[default]+=clearcpuid=514",
		"KERNEL_CMDLINE[snapshot]+=\"clearcpuid=umip\"",
		"KERNEL_CMDLINE[default]+=\"quiet xclearcpuid=514\"",
		"UNRELATED=\"clearcpuid=514\"",
	}, "\n"), 0o644)
	writeTestFile(t, paths.LimineUpdaters[0], "updater", 0o755)

	candidate := onlyCandidate(t, NewInspector(paths).Inspect(true))
	if candidate.State != StateActionRequired || candidate.ExistingArgument != "" {
		t.Fatalf("comments, unrelated values, or substrings were treated as configured: %+v", candidate)
	}
}

func TestInspectLimineHonorsAssignmentOrderAndRejectsConflicts(t *testing.T) {
	t.Run("later assignment replaces appended value", func(t *testing.T) {
		paths := testPaths(t)
		writeTestFile(t, paths.LimineConfiguration, "KERNEL_CMDLINE[default]+=clearcpuid=514\nKERNEL_CMDLINE[default]=\"quiet\"\n", 0o644)
		writeTestFile(t, paths.LimineUpdaters[0], "updater", 0o755)

		candidate := onlyCandidate(t, NewInspector(paths).Inspect(true))
		if candidate.State != StateActionRequired {
			t.Fatalf("later assignment did not replace the earlier value: %+v", candidate)
		}
	})

	t.Run("conflicting argument is manual only", func(t *testing.T) {
		paths := testPaths(t)
		writeTestFile(t, paths.LimineConfiguration, "KERNEL_CMDLINE[default]+=\"quiet clearcpuid=999\"\n", 0o644)
		writeTestFile(t, paths.LimineUpdaters[0], "updater", 0o755)

		result := NewInspector(paths).Inspect(true)
		manual := onlyManual(t, result)
		if manual.Reason != ReasonConflictingArgument || !strings.Contains(manual.Detail, "clearcpuid=999") {
			t.Fatalf("unexpected conflict result: %+v", manual)
		}
	})
}

func TestInspectGRUBAcceptsOneQuotedDefaultAssignment(t *testing.T) {
	tests := []struct {
		name     string
		contents string
		state    CandidateState
		token    string
	}{
		{name: "double quoted", contents: "GRUB_CMDLINE_LINUX_DEFAULT=\"quiet splash\"\n", state: StateActionRequired},
		{name: "single quoted configured", contents: "  GRUB_CMDLINE_LINUX_DEFAULT = 'quiet clearcpuid=umip'\n", state: StateRestartRequired, token: "clearcpuid=umip"},
		{name: "trailing comment", contents: "GRUB_CMDLINE_LINUX_DEFAULT=\"quiet clearcpuid=514\" # configured\n", state: StateRestartRequired, token: "clearcpuid=514"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			paths := testPaths(t)
			writeTestFile(t, paths.GRUBConfiguration, test.contents, 0o644)
			writeTestFile(t, paths.UpdateGRUB[0], "updater", 0o755)

			candidate := onlyCandidate(t, NewInspector(paths).Inspect(true))
			if candidate.Bootloader != BootloaderGRUB || candidate.State != test.state || candidate.ExistingArgument != test.token {
				t.Fatalf("unexpected candidate: %+v", candidate)
			}
		})
	}
}

func TestInspectGRUBRejectsUnsupportedAssignments(t *testing.T) {
	tests := []struct {
		name     string
		contents string
	}{
		{name: "missing", contents: "GRUB_TIMEOUT=5\n"},
		{name: "multiple", contents: "GRUB_CMDLINE_LINUX_DEFAULT=\"quiet\"\nGRUB_CMDLINE_LINUX_DEFAULT='splash'\n"},
		{name: "unquoted", contents: "GRUB_CMDLINE_LINUX_DEFAULT=quiet\n"},
		{name: "incomplete quote", contents: "GRUB_CMDLINE_LINUX_DEFAULT=\"quiet\n"},
		{name: "hash adjacent to quote", contents: "GRUB_CMDLINE_LINUX_DEFAULT=\"quiet\"#not-a-comment\n"},
		{name: "append assignment", contents: "GRUB_CMDLINE_LINUX_DEFAULT+=\"quiet\"\n"},
		{name: "exported assignment", contents: "export GRUB_CMDLINE_LINUX_DEFAULT=\"quiet\"\n"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			paths := testPaths(t)
			writeTestFile(t, paths.GRUBConfiguration, test.contents, 0o644)
			writeTestFile(t, paths.UpdateGRUB[0], "updater", 0o755)

			manual := onlyManual(t, NewInspector(paths).Inspect(true))
			if manual.Bootloader != BootloaderGRUB || manual.Reason != ReasonUnsupportedSyntax {
				t.Fatalf("unexpected manual outcome: %+v", manual)
			}
		})
	}
}

func TestInspectGRUBUsesOnlyTheSupportedUpdaterForms(t *testing.T) {
	t.Run("update-grub has priority", func(t *testing.T) {
		paths := testPaths(t)
		writeTestFile(t, paths.GRUBConfiguration, "GRUB_CMDLINE_LINUX_DEFAULT=\"quiet\"\n", 0o644)
		writeTestFile(t, paths.UpdateGRUB[0], "update", 0o755)
		writeTestFile(t, paths.GRUBMkconfig[0], "mkconfig", 0o755)
		writeTestFile(t, paths.GRUBOutput, "generated", 0o644)

		candidate := onlyCandidate(t, NewInspector(paths).Inspect(true))
		if candidate.Updater.Path != paths.UpdateGRUB[0] || len(candidate.Updater.Args) != 0 {
			t.Fatalf("unexpected updater: %+v", candidate.Updater)
		}
	})

	t.Run("grub-mkconfig requires the fixed existing output", func(t *testing.T) {
		paths := testPaths(t)
		writeTestFile(t, paths.GRUBConfiguration, "GRUB_CMDLINE_LINUX_DEFAULT=\"quiet\"\n", 0o644)
		writeTestFile(t, paths.GRUBMkconfig[0], "mkconfig", 0o755)

		manual := onlyManual(t, NewInspector(paths).Inspect(true))
		if manual.Reason != ReasonMissingUpdater {
			t.Fatalf("grub-mkconfig without its output was accepted: %+v", manual)
		}

		writeTestFile(t, paths.GRUBOutput, "generated", 0o644)
		candidate := onlyCandidate(t, NewInspector(paths).Inspect(true))
		if candidate.Updater.Path != paths.GRUBMkconfig[0] || strings.Join(candidate.Updater.Args, " ") != "-o "+paths.GRUBOutput {
			t.Fatalf("unexpected grub-mkconfig form: %+v", candidate.Updater)
		}
	})

	t.Run("grub-mkconfig accepts the fixed regular output regardless of mode", func(t *testing.T) {
		paths := testPaths(t)
		writeTestFile(t, paths.GRUBConfiguration, "GRUB_CMDLINE_LINUX_DEFAULT=\"quiet\"\n", 0o644)
		writeTestFile(t, paths.GRUBMkconfig[0], "mkconfig", 0o755)
		writeTestFile(t, paths.GRUBOutput, "generated", 0o666)
		if err := os.Chmod(paths.GRUBOutput, 0o666); err != nil {
			t.Fatal(err)
		}

		candidate := onlyCandidate(t, NewInspector(paths).Inspect(true))
		if candidate.Updater.Path != paths.GRUBMkconfig[0] {
			t.Fatalf("unexpected grub-mkconfig form: %+v", candidate.Updater)
		}
	})
}

func TestInspectSelectionAndManualOnlyOutcomes(t *testing.T) {
	t.Run("both candidates require a choice", func(t *testing.T) {
		paths := testPaths(t)
		writeTestFile(t, paths.LimineConfiguration, "KERNEL_CMDLINE[default]+=\"quiet\"\n", 0o644)
		writeTestFile(t, paths.LimineUpdaters[0], "limine", 0o755)
		writeTestFile(t, paths.GRUBConfiguration, "GRUB_CMDLINE_LINUX_DEFAULT=\"quiet\"\n", 0o644)
		writeTestFile(t, paths.UpdateGRUB[0], "grub", 0o755)

		result := NewInspector(paths).Inspect(true)
		if result.Selection != SelectionChoice || result.Selected != "" || len(result.Candidates) != 2 {
			t.Fatalf("unexpected selection: %+v", result)
		}
	})

	t.Run("systemd boot remains manual only", func(t *testing.T) {
		paths := testPaths(t)
		if err := os.MkdirAll(paths.SystemdEntries, 0o755); err != nil {
			t.Fatal(err)
		}

		result := NewInspector(paths).Inspect(true)
		manual := onlyManual(t, result)
		if result.Selection != SelectionManualOnly || manual.Reason != ReasonUnsupportedLoader || !strings.Contains(manual.Detail, "systemd-boot") {
			t.Fatalf("unexpected systemd-boot outcome: %+v", result)
		}
	})
}

func TestInspectAcceptsSystemManagedSymlinksAndPermissions(t *testing.T) {
	paths := testPaths(t)
	target := filepath.Join(t.TempDir(), "grub")
	writeTestFile(t, target, "GRUB_CMDLINE_LINUX_DEFAULT=\"quiet\"\n", 0o664)
	if err := os.MkdirAll(filepath.Dir(paths.GRUBConfiguration), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, paths.GRUBConfiguration); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, paths.UpdateGRUB[0], "updater", 0o775)

	candidate := onlyCandidate(t, NewInspector(paths).Inspect(true))
	if candidate.Bootloader != BootloaderGRUB {
		t.Fatalf("unexpected candidate: %+v", candidate)
	}
}

func TestInspectArgumentsUsesExactWhitespaceDelimitedTokens(t *testing.T) {
	tests := []struct {
		name  string
		value string
		state CandidateState
		token string
	}{
		{name: "numeric", value: "quiet clearcpuid=514 splash", state: StateConfigured, token: "clearcpuid=514"},
		{name: "named", value: "clearcpuid=umip", state: StateConfigured, token: "clearcpuid=umip"},
		{name: "prefix substring", value: "xclearcpuid=514", state: StateActionRequired},
		{name: "suffix substring", value: "clearcpuid=514-extra", token: "clearcpuid=514-extra"},
		{name: "conflicting", value: "clearcpuid=0", token: "clearcpuid=0"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			state, token := inspectArguments([]string{test.value})
			if state != test.state || token != test.token {
				t.Fatalf("got state=%q token=%q", state, token)
			}
		})
	}
}

func testPaths(t *testing.T) Paths {
	t.Helper()
	root := t.TempDir()
	return Paths{
		LimineConfiguration: filepath.Join(root, "etc", "default", "limine"),
		GRUBConfiguration:   filepath.Join(root, "etc", "default", "grub"),
		GRUBOutput:          filepath.Join(root, "boot", "grub", "grub.cfg"),
		SystemdEntries:      filepath.Join(root, "boot", "loader", "entries"),
		LimineUpdaters:      []string{filepath.Join(root, "usr", "bin", "limine-update")},
		UpdateGRUB:          []string{filepath.Join(root, "usr", "sbin", "update-grub")},
		GRUBMkconfig:        []string{filepath.Join(root, "usr", "bin", "grub-mkconfig")},
		RecoveryDirectory:   filepath.Join(root, "var", "lib", "hv-launcher", "recovery"),
	}
}

func writeTestFile(t *testing.T, path, contents string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), mode); err != nil {
		t.Fatal(err)
	}
}

func onlyCandidate(t *testing.T, result Inspection) Candidate {
	t.Helper()
	if result.Selection != SelectionAutomatic || len(result.Candidates) != 1 || len(result.Manual) != 0 {
		t.Fatalf("expected one automatic candidate, got %+v", result)
	}
	return result.Candidates[0]
}

func onlyManual(t *testing.T, result Inspection) ManualOutcome {
	t.Helper()
	if result.Selection != SelectionManualOnly || len(result.Candidates) != 0 || len(result.Manual) != 1 {
		t.Fatalf("expected one manual outcome, got %+v", result)
	}
	return result.Manual[0]
}
