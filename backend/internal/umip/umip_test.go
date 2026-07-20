//go:build linux

package umip

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
)

type scriptedRunner struct {
	mu      sync.Mutex
	calls   []string
	outputs [][]byte
	errors  []error
	hooks   []func()
}

func (r *scriptedRunner) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, name+" "+strings.Join(args, " "))
	index := len(r.calls) - 1
	var output []byte
	var err error
	if index < len(r.outputs) {
		output = r.outputs[index]
	}
	if index < len(r.errors) {
		err = r.errors[index]
	}
	if index < len(r.hooks) && r.hooks[index] != nil {
		r.hooks[index]()
	}
	return output, err
}

func TestApplyGRUBPreservesMetadataAndRunsFixedUpdater(t *testing.T) {
	paths := testPaths(t)
	contents := "GRUB_CMDLINE_LINUX_DEFAULT=\"quiet splash\" # keep\n"
	writeTestFile(t, paths.GRUBConfiguration, contents, 0o640)
	before, err := os.Stat(paths.GRUBConfiguration)
	if err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, paths.UpdateGRUB[0], "updater", 0o755)
	xattrSupported := syscall.Setxattr(paths.GRUBConfiguration, "user.hv_launcher_test", []byte("preserved"), 0) == nil
	runner := &scriptedRunner{outputs: [][]byte{[]byte("updated")}}
	inspector := NewInspector(paths)
	inspector.Runner = runner

	result, err := inspector.Apply(context.Background(), BootloaderGRUB, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !result.RestartRequired || result.BackupRetained != "" {
		t.Fatalf("unexpected result: %+v", result)
	}
	updated, err := os.ReadFile(paths.GRUBConfiguration)
	if err != nil {
		t.Fatal(err)
	}
	want := "GRUB_CMDLINE_LINUX_DEFAULT=\"quiet splash " + FixedArgument + "\" # keep\n"
	if string(updated) != want {
		t.Fatalf("updated configuration = %q, want %q", updated, want)
	}
	info, err := os.Stat(paths.GRUBConfiguration)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o640 {
		t.Fatalf("configuration mode = %o", info.Mode().Perm())
	}
	if !os.SameFile(before, info) {
		t.Fatal("configuration inode changed during in-place update")
	}
	if xattrSupported {
		value := make([]byte, 32)
		size, err := syscall.Getxattr(paths.GRUBConfiguration, "user.hv_launcher_test", value)
		if err != nil || string(value[:size]) != "preserved" {
			t.Fatalf("extended attribute was not preserved: size=%d value=%q err=%v", size, value[:max(size, 0)], err)
		}
	}
	if len(runner.calls) != 1 || runner.calls[0] != paths.UpdateGRUB[0]+" " {
		t.Fatalf("unexpected updater calls: %v", runner.calls)
	}
	assertRecoveryDirectoryEmpty(t, paths.RecoveryDirectory)
}

func TestBuildReplacementUsesOnlyFixedBootloaderForms(t *testing.T) {
	t.Run("Limine append assignment", func(t *testing.T) {
		current := []byte("ESP_PATH=\"/boot\"\nKERNEL_CMDLINE[default]+=\"quiet\"")
		replacement, err := buildReplacement(BootloaderLimine, current)
		if err != nil {
			t.Fatal(err)
		}
		want := string(current) + "\nKERNEL_CMDLINE[default]+=" + FixedArgument + "\n"
		if string(replacement) != want {
			t.Fatalf("replacement = %q, want %q", replacement, want)
		}
	})

	t.Run("GRUB preserves syntax and comment", func(t *testing.T) {
		current := []byte("  GRUB_CMDLINE_LINUX_DEFAULT = 'quiet splash' # retained\n")
		replacement, err := buildReplacement(BootloaderGRUB, current)
		if err != nil {
			t.Fatal(err)
		}
		want := "  GRUB_CMDLINE_LINUX_DEFAULT = 'quiet splash " + FixedArgument + "' # retained\n"
		if string(replacement) != want {
			t.Fatalf("replacement = %q, want %q", replacement, want)
		}
	})
}

func TestApplyRollsBackSourceAndGeneratedConfiguration(t *testing.T) {
	paths := testPaths(t)
	contents := "KERNEL_CMDLINE[default]+=quiet\n"
	writeTestFile(t, paths.LimineConfiguration, contents, 0o644)
	writeTestFile(t, paths.LimineUpdaters[0], "updater", 0o755)
	runner := &scriptedRunner{errors: []error{errors.New("update failed"), nil}}
	inspector := NewInspector(paths)
	inspector.Runner = runner

	if _, err := inspector.Apply(context.Background(), BootloaderLimine, nil); err == nil || !strings.Contains(err.Error(), "rolled back successfully") {
		t.Fatalf("Apply() error = %v", err)
	}
	restored, _ := os.ReadFile(paths.LimineConfiguration)
	if string(restored) != contents || len(runner.calls) != 2 {
		t.Fatalf("rollback result: data=%q calls=%v", restored, runner.calls)
	}
	assertRecoveryDirectoryEmpty(t, paths.RecoveryDirectory)
}

func TestApplyRetainsBackupWhenRecoveryUpdaterFails(t *testing.T) {
	paths := testPaths(t)
	contents := "KERNEL_CMDLINE[default]+=quiet\n"
	writeTestFile(t, paths.LimineConfiguration, contents, 0o644)
	writeTestFile(t, paths.LimineUpdaters[0], "updater", 0o755)
	runner := &scriptedRunner{errors: []error{errors.New("update failed"), errors.New("recovery failed")}}
	inspector := NewInspector(paths)
	inspector.Runner = runner

	_, err := inspector.Apply(context.Background(), BootloaderLimine, nil)
	if err == nil || !strings.Contains(err.Error(), paths.RecoveryDirectory) {
		t.Fatalf("Apply() did not report retained recovery material: %v", err)
	}
	restored, _ := os.ReadFile(paths.LimineConfiguration)
	if string(restored) != contents {
		t.Fatalf("source was not restored: %q", restored)
	}
	entries, readErr := os.ReadDir(paths.RecoveryDirectory)
	if readErr != nil || len(entries) != 1 || filepath.Ext(entries[0].Name()) != ".bak" {
		t.Fatalf("recovery backup not retained: entries=%v err=%v", entries, readErr)
	}
}

func TestApplyRetainsBackupWhenSourceCannotBeRestored(t *testing.T) {
	paths := testPaths(t)
	contents := "KERNEL_CMDLINE[default]+=quiet\n"
	writeTestFile(t, paths.LimineConfiguration, contents, 0o644)
	writeTestFile(t, paths.LimineUpdaters[0], "updater", 0o755)
	runner := &scriptedRunner{
		errors: []error{errors.New("update failed")},
		hooks: []func(){func() {
			if err := os.Remove(paths.LimineConfiguration); err != nil {
				t.Error(err)
				return
			}
			if err := os.Mkdir(paths.LimineConfiguration, 0o755); err != nil {
				t.Error(err)
			}
		}},
	}
	inspector := NewInspector(paths)
	inspector.Runner = runner

	_, err := inspector.Apply(context.Background(), BootloaderLimine, nil)
	if err == nil || !strings.Contains(err.Error(), "restore original configuration") ||
		!strings.Contains(err.Error(), paths.RecoveryDirectory) {
		t.Fatalf("Apply() did not report manual recovery and retained backup: %v", err)
	}
	entries, readErr := os.ReadDir(paths.RecoveryDirectory)
	if readErr != nil || len(entries) != 1 || filepath.Ext(entries[0].Name()) != ".bak" {
		t.Fatalf("recovery backup not retained: entries=%v err=%v", entries, readErr)
	}
}

func assertRecoveryDirectoryEmpty(t *testing.T, path string) {
	t.Helper()
	entries, err := os.ReadDir(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("recovery directory contains %v", entries)
	}
}
