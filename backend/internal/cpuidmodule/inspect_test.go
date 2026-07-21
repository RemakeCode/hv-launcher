package cpuidmodule

import (
	"archive/zip"
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

type zipFixtureEntry struct {
	name    string
	content string
	mode    os.FileMode
}

func TestInspectAcceptsCurrentFlatModuleLayout(t *testing.T) {
	archivePath := writeModuleZIP(t, validModuleEntries())
	inspection, err := NewInspector().ValidatePath(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	if inspection.FileName != filepath.Base(archivePath) || inspection.EntryCount != len(validModuleEntries()) ||
		inspection.Identity.PackageName != "cpuid_fault_emulation" ||
		inspection.Identity.PackageVersion != "0.1" || !inspection.Identity.AutomaticInstall ||
		!strings.Contains(inspection.Warning, "cannot verify") || !strings.Contains(inspection.Warning, "as root") {
		t.Fatalf("unexpected inspection: %+v", inspection)
	}
}

func TestInspectAcceptsUpdatedVersionAndChangedSourceLayout(t *testing.T) {
	entries := []zipFixtureEntry{
		{name: "dkms.conf", content: strings.Replace(validDKMSConfig(), `PACKAGE_VERSION="0.1"`, `PACKAGE_VERSION="1.0"`, 1), mode: 0o644},
		{name: "Makefile", content: "obj-m += cpuid_fault_emulation.o\n", mode: 0o644},
		{name: "module/main.c", content: "int module(void) { return 0; }\n", mode: 0o644},
	}
	inspection, err := NewInspector().ValidatePath(writeModuleZIP(t, entries))
	if err != nil {
		t.Fatal(err)
	}
	if inspection.Identity.PackageVersion != "1.0" || len(inspection.RequiredFiles) != 2 {
		t.Fatalf("unexpected inspection: %+v", inspection)
	}
}

func TestInspectRejectsUnsafeAndIncompleteZIPs(t *testing.T) {
	tests := []struct {
		name    string
		entries func() []zipFixtureEntry
		want    error
	}{
		{
			name: "missing required file",
			entries: func() []zipFixtureEntry {
				entries := make([]zipFixtureEntry, 0, len(validModuleEntries())-1)
				for _, entry := range validModuleEntries() {
					if entry.name != "Makefile" {
						entries = append(entries, entry)
					}
				}
				return entries
			},
			want: ErrInvalidArchive,
		},
		{
			name: "traversal",
			entries: func() []zipFixtureEntry {
				return append(validModuleEntries(), zipFixtureEntry{name: "../escape", content: "bad", mode: 0o644})
			},
			want: ErrUnsafeEntry,
		},
		{
			name: "absolute path",
			entries: func() []zipFixtureEntry {
				return append(validModuleEntries(), zipFixtureEntry{name: "/escape", content: "bad", mode: 0o644})
			},
			want: ErrUnsafeEntry,
		},
		{
			name: "symbolic link",
			entries: func() []zipFixtureEntry {
				return append(validModuleEntries(), zipFixtureEntry{name: "link", content: "Makefile", mode: os.ModeSymlink | 0o777})
			},
			want: ErrUnsafeEntry,
		},
		{
			name: "duplicate path",
			entries: func() []zipFixtureEntry {
				return append(validModuleEntries(), zipFixtureEntry{name: "Makefile", content: "duplicate", mode: 0o644})
			},
			want: ErrUnsafeEntry,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := NewInspector().ValidatePath(writeModuleZIP(t, test.entries()))
			if !errors.Is(err, test.want) {
				t.Fatalf("ValidatePath() error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestInspectEnforcesEveryResourceLimit(t *testing.T) {
	entries := validModuleEntries()
	archivePath := writeModuleZIP(t, entries)
	tests := []struct {
		name   string
		limits Limits
	}{
		{name: "compressed bytes", limits: Limits{MaxCompressedBytes: 1, MaxExpandedBytes: 1 << 20, MaxFileBytes: 1 << 20, MaxDKMSConfigBytes: 1 << 20, MaxEntries: 100, MaxPathBytes: 100}},
		{name: "expanded bytes", limits: Limits{MaxCompressedBytes: 1 << 20, MaxExpandedBytes: 8, MaxFileBytes: 1 << 20, MaxDKMSConfigBytes: 1 << 20, MaxEntries: 100, MaxPathBytes: 100}},
		{name: "file bytes", limits: Limits{MaxCompressedBytes: 1 << 20, MaxExpandedBytes: 1 << 20, MaxFileBytes: 2, MaxDKMSConfigBytes: 1 << 20, MaxEntries: 100, MaxPathBytes: 100}},
		{name: "dkms bytes", limits: Limits{MaxCompressedBytes: 1 << 20, MaxExpandedBytes: 1 << 20, MaxFileBytes: 1 << 20, MaxDKMSConfigBytes: 8, MaxEntries: 100, MaxPathBytes: 100}},
		{name: "entry count", limits: Limits{MaxCompressedBytes: 1 << 20, MaxExpandedBytes: 1 << 20, MaxFileBytes: 1 << 20, MaxDKMSConfigBytes: 1 << 20, MaxEntries: 2, MaxPathBytes: 100}},
		{name: "path bytes", limits: Limits{MaxCompressedBytes: 1 << 20, MaxExpandedBytes: 1 << 20, MaxFileBytes: 1 << 20, MaxDKMSConfigBytes: 1 << 20, MaxEntries: 100, MaxPathBytes: 5}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			inspector := &Inspector{Limits: test.limits}
			if _, err := inspector.ValidatePath(archivePath); !errors.Is(err, ErrResourceLimit) &&
				!(test.name == "path bytes" && errors.Is(err, ErrUnsafeEntry)) {
				t.Fatalf("ValidatePath() error = %v", err)
			}
		})
	}
}

func TestInspectAcceptsSelectedSymlinkAndRejectsNonZIP(t *testing.T) {
	realArchive := writeModuleZIP(t, validModuleEntries())
	symlink := filepath.Join(t.TempDir(), "module.zip")
	if err := os.Symlink(realArchive, symlink); err != nil {
		t.Fatal(err)
	}
	if _, err := NewInspector().ValidatePath(symlink); err != nil {
		t.Fatalf("selected symlink returned %v", err)
	}
	notZIP := filepath.Join(t.TempDir(), "module.zip")
	if err := os.WriteFile(notZIP, []byte("not a zip"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewInspector().ValidatePath(notZIP); !errors.Is(err, ErrInvalidArchive) {
		t.Fatalf("non-ZIP returned %v", err)
	}
}

func TestArchivePreflightChecksOnlyFileTypeAndSize(t *testing.T) {
	archivePath := writeModuleZIP(t, validModuleEntries())
	inspector := NewInspector()
	preflight, err := inspector.PreflightPath(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	if preflight.FileName != filepath.Base(archivePath) || preflight.CompressedBytes <= 0 ||
		!strings.Contains(preflight.Warning, "as root") {
		t.Fatalf("unexpected preflight: %+v", preflight)
	}

	unsafeArchive := writeModuleZIP(t, append(validModuleEntries(), zipFixtureEntry{name: "link", content: "Makefile", mode: os.ModeSymlink | 0o777}))
	if _, err := inspector.PreflightPath(unsafeArchive); err != nil {
		t.Fatalf("fast preflight performed deep validation: %v", err)
	}
	if _, err := inspector.ValidatePath(unsafeArchive); !errors.Is(err, ErrUnsafeEntry) {
		t.Fatalf("installation validation accepted unsafe archive: %v", err)
	}

	notZIP := filepath.Join(t.TempDir(), "module.zip")
	if err := os.WriteFile(notZIP, []byte("not a zip"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := inspector.PreflightPath(notZIP); !errors.Is(err, ErrInvalidArchive) {
		t.Fatalf("fast preflight accepted non-ZIP content: %v", err)
	}
}

func validModuleEntries() []zipFixtureEntry {
	entries := []zipFixtureEntry{
		{name: "dkms.conf", content: validDKMSConfig(), mode: 0o644},
		{name: "Makefile", content: "obj-m += cpuid_fault_emulation.o\n", mode: 0o644},
		{name: "inc/vmcb_layout.h", content: "#pragma once\n", mode: 0o644},
		{name: "inc/host_state.h", content: "#pragma once\n", mode: 0o644},
		{name: "src/cpuid_fault_emulation.c", content: "int module(void) { return 0; }\n", mode: 0o644},
		{name: "src/capture_context.S", content: ".text\n", mode: 0o644},
		{name: "src/run_vm.S", content: ".text\n", mode: 0o644},
	}
	sort.Slice(entries, func(left, right int) bool { return entries[left].name < entries[right].name })
	return entries
}

func validDKMSConfig() string {
	return strings.Join([]string{
		`PACKAGE_NAME="cpuid_fault_emulation"`,
		`PACKAGE_VERSION="0.1"`,
		`BUILT_MODULE_NAME="cpuid_fault_emulation"`,
		`BUILT_MODULE_NAME="cpuid_fault_emulation"`,
		`DEST_MODULE_LOCATION="/updates"`,
		`AUTOINSTALL="yes"`,
	}, "\n") + "\n"
}

func writeModuleZIP(t *testing.T, entries []zipFixtureEntry) string {
	t.Helper()
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	for _, entry := range entries {
		header := &zip.FileHeader{Name: entry.name, Method: zip.Deflate}
		header.SetMode(entry.mode)
		file, err := writer.CreateHeader(header)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := file.Write([]byte(entry.content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	archivePath := filepath.Join(t.TempDir(), "cpuid_fault_emulation.zip")
	if err := os.WriteFile(archivePath, buffer.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	return archivePath
}
