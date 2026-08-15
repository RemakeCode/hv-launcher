package runtime

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"hv-launcher/internal/model"
)

type fakeHTTPClient struct {
	responses map[string]httpResponse
	errors    map[string]error
	requests  []string
}

type fakeVersionRunner struct {
	output []byte
	err    error
}

func (c *fakeHTTPClient) Do(request *httpRequest) (*httpResponse, error) {
	c.requests = append(c.requests, request.URL)
	if err := c.errors[request.URL]; err != nil {
		return nil, err
	}
	response, ok := c.responses[request.URL]
	if !ok {
		return nil, errors.New("unexpected URL: " + request.URL)
	}
	return &response, nil
}

func (r fakeVersionRunner) Version(context.Context, string, string) ([]byte, error) {
	return r.output, r.err
}

func TestInspectRuntimeStatesAndVersionFallback(t *testing.T) {
	t.Run("unsupported", func(t *testing.T) {
		manager := testManager(t, t.TempDir(), localManagerOptions{GOARCH: "arm64"})
		status, err := manager.Inspect(context.Background())
		if err != nil || status.State != model.RuntimeStateUnsupported || status.Available {
			t.Fatalf("status = %+v, error = %v", status, err)
		}
	})

	t.Run("absent", func(t *testing.T) {
		manager := testManager(t, t.TempDir(), localManagerOptions{GOARCH: SupportedGOARCH})
		status, err := manager.Inspect(context.Background())
		if err != nil || status.State != model.RuntimeStateAbsent || status.Available {
			t.Fatalf("status = %+v, error = %v", status, err)
		}
	})

	t.Run("incomplete", func(t *testing.T) {
		home := t.TempDir()
		writeRuntimeFile(t, filepath.Join(home, ".local", "bin", "linuwux"), "wrapper", 0o755)
		manager := testManager(t, home, localManagerOptions{GOARCH: SupportedGOARCH})
		status, err := manager.Inspect(context.Background())
		if err != nil || status.State != model.RuntimeStateInvalid || status.Available || !strings.Contains(status.Detail, "library") {
			t.Fatalf("status = %+v, error = %v", status, err)
		}
	})

	t.Run("valid", func(t *testing.T) {
		home := writeRuntimeFixture(t, "old-wrapper", "old-library")
		manager := testManager(t, home, localManagerOptions{
			GOARCH: SupportedGOARCH, Runner: fakeVersionRunner{output: []byte("linuwux v26.08.14.1\n")},
		})
		status, err := manager.Inspect(context.Background())
		if err != nil || !status.Available || !status.VersionKnown || status.Version != "26.08.14.1" {
			t.Fatalf("status = %+v, error = %v", status, err)
		}
	})

	t.Run("unreadable version remains available", func(t *testing.T) {
		home := writeRuntimeFixture(t, "old-wrapper", "old-library")
		manager := testManager(t, home, localManagerOptions{
			GOARCH: SupportedGOARCH, Runner: fakeVersionRunner{err: errors.New("failed")},
		})
		status, err := manager.Inspect(context.Background())
		if err != nil || !status.Available || status.VersionKnown || status.UpdateState != model.RuntimeUpdateUnknown {
			t.Fatalf("status = %+v, error = %v", status, err)
		}
	})
}

func TestInstallUsesOneExactReleaseAndVerifiesChecksums(t *testing.T) {
	home := writeRuntimeFixture(t, "old-wrapper", "old-library")
	wrapper := []byte("#!/bin/sh\necho linuwux v26.08.14.1\n")
	library := []byte("new-library")
	client := releaseClient(t, "v26.08.14.1", wrapper, library, false)
	manager := testManager(t, home, localManagerOptions{GOARCH: SupportedGOARCH, Client: client})
	var progress []Progress
	result, err := manager.Install(context.Background(), func(update Progress) { progress = append(progress, update) })
	if err != nil {
		t.Fatal(err)
	}
	if result.ReleaseTag != "v26.08.14.1" || result.Version != "26.08.14.1" {
		t.Fatalf("result = %+v", result)
	}
	installedWrapper, _ := os.ReadFile(filepath.Join(home, ".local", "bin", "linuwux"))
	installedLibrary, _ := os.ReadFile(filepath.Join(home, ".local", "lib", LibraryAsset))
	if !bytes.Equal(installedWrapper, wrapper) || !bytes.Equal(installedLibrary, library) {
		t.Fatal("verified payloads were not installed")
	}
	info, err := os.Stat(filepath.Join(home, ".local", "bin", "linuwux"))
	if err != nil || info.Mode()&0o111 == 0 {
		t.Fatal("installed wrapper is not executable")
	}
	if len(client.requests) != 4 || client.requests[0] != LatestReleaseURL {
		t.Fatalf("requests = %v", client.requests)
	}
	for _, request := range client.requests[1:] {
		if !strings.Contains(request, "/releases/download/v26.08.14.1/") {
			t.Fatalf("asset was not bound to resolved release: %s", request)
		}
	}
	if !progressContains(progress, ChecksumsAsset) || !progressContains(progress, LibraryAsset) || !progressContains(progress, WrapperAsset) {
		t.Fatalf("asset progress = %+v", progress)
	}
}

func TestRemoveDeletesOnlyInstalledRuntimeFiles(t *testing.T) {
	home := writeRuntimeFixture(t, "wrapper", "library")
	writeRuntimeFile(t, filepath.Join(home, ".local", "bin", "keep"), "keep", 0o755)
	writeRuntimeFile(t, filepath.Join(home, ".local", "lib", "keep.so"), "keep", 0o644)
	manager := testManager(t, home, localManagerOptions{GOARCH: SupportedGOARCH})

	if err := manager.Remove(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{
		filepath.Join(home, ".local", "bin", "linuwux"),
		filepath.Join(home, ".local", "lib", LibraryAsset),
	} {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("runtime file %s still exists: %v", path, err)
		}
	}
	for _, path := range []string{
		filepath.Join(home, ".local", "bin", "keep"),
		filepath.Join(home, ".local", "lib", "keep.so"),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("unrelated file %s was removed: %v", path, err)
		}
	}
	if err := manager.Remove(context.Background()); err != nil {
		t.Fatalf("removing an absent runtime should be idempotent: %v", err)
	}
}

func TestFailedInstallPreservesExistingRuntime(t *testing.T) {
	tests := []struct {
		name   string
		client func(*testing.T, []byte, []byte) *fakeHTTPClient
	}{
		{
			name: "checksum mismatch",
			client: func(t *testing.T, wrapper, library []byte) *fakeHTTPClient {
				return releaseClient(t, "v26.08.14.1", wrapper, library, true)
			},
		},
		{
			name: "offline after release resolution",
			client: func(t *testing.T, wrapper, library []byte) *fakeHTTPClient {
				client := releaseClient(t, "v26.08.14.1", wrapper, library, false)
				client.errors[assetURL("v26.08.14.1", ChecksumsAsset)] = context.Canceled
				return client
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			home := writeRuntimeFixture(t, "old-wrapper", "old-library")
			wrapper := []byte("new-wrapper")
			library := []byte("new-library")
			manager := testManager(t, home, localManagerOptions{
				GOARCH: SupportedGOARCH, Client: test.client(t, wrapper, library),
			})
			if _, err := manager.Install(context.Background(), nil); err == nil {
				t.Fatal("failed installation unexpectedly succeeded")
			}
			assertRuntimeContents(t, home, "old-wrapper", "old-library")
		})
	}
}

func TestLatestRejectsMissingDuplicateOrUntrustedAssets(t *testing.T) {
	base := githubRelease{
		TagName: "v26.08.14.1",
		Assets: []githubAsset{
			{Name: LibraryAsset, URL: assetURL("v26.08.14.1", LibraryAsset), Size: 10},
			{Name: WrapperAsset, URL: assetURL("v26.08.14.1", WrapperAsset), Size: 10},
			{Name: ChecksumsAsset, URL: assetURL("v26.08.14.1", ChecksumsAsset), Size: 10},
		},
	}
	tests := []struct {
		name   string
		mutate func(*githubRelease)
	}{
		{"missing", func(release *githubRelease) { release.Assets = release.Assets[:2] }},
		{"duplicate", func(release *githubRelease) { release.Assets = append(release.Assets, release.Assets[0]) }},
		{"untrusted URL", func(release *githubRelease) { release.Assets[0].URL = "https://example.com/liblinuwux.so" }},
		{"prerelease", func(release *githubRelease) { release.Prerelease = true }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			release := base
			release.Assets = append([]githubAsset(nil), base.Assets...)
			test.mutate(&release)
			payload, _ := json.Marshal(release)
			client := &fakeHTTPClient{responses: map[string]httpResponse{LatestReleaseURL: {StatusCode: 200, Body: payload}}}
			manager := testManager(t, t.TempDir(), localManagerOptions{GOARCH: SupportedGOARCH, Client: client})
			if _, err := manager.Latest(context.Background()); err == nil {
				t.Fatal("invalid release was accepted")
			}
		})
	}
}

func TestVersionNormalizationAndComparison(t *testing.T) {
	if version, ok := NormalizeVersion("LinUwUx v26.08.14.1"); !ok || version != "26.08.14.1" {
		t.Fatalf("version = %q, %v", version, ok)
	}
	if CompareVersions("26.08.14", "v26.08.14.1") >= 0 || CompareVersions("26.09.1", "26.08.99") <= 0 || CompareVersions("v26.08.14.1", "26.08.14.1") != 0 {
		t.Fatal("version comparison is incorrect")
	}
}

func TestWorkerRequiresUnprivilegedIdentityAndBoundedTypedRequest(t *testing.T) {
	if WorkerCommand != "linuwux-runtime-worker" {
		t.Fatalf("runtime worker command = %q", WorkerCommand)
	}
	if _, err := NewWorkerClient("/plugin/hv-launcher", "/home/deck", 0, 1000); err == nil {
		t.Fatal("root LinUwUx runtime worker identity was accepted")
	}
	for _, request := range []string{
		`{"operation":"install","userHome":"relative"}`,
		`{"operation":"shell","userHome":"/home/deck"}`,
		`{"operation":"inspect","userHome":"/home/deck","url":"https://example.com"}`,
	} {
		var output bytes.Buffer
		if err := RunWorker(context.Background(), strings.NewReader(request), &output); err != nil {
			t.Fatal(err)
		}
		var response workerResponse
		if err := json.Unmarshal(output.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		if response.Error == "" {
			t.Fatalf("request was accepted: %s", request)
		}
	}
}

func testManager(t *testing.T, home string, options localManagerOptions) *LocalManager {
	t.Helper()
	manager, err := newLocalManager(home, options)
	if err != nil {
		t.Fatal(err)
	}
	return manager
}

func writeRuntimeFixture(t *testing.T, wrapper, library string) string {
	t.Helper()
	home := t.TempDir()
	writeRuntimeFile(t, filepath.Join(home, ".local", "bin", "linuwux"), wrapper, 0o755)
	writeRuntimeFile(t, filepath.Join(home, ".local", "lib", LibraryAsset), library, 0o644)
	return home
}

func writeRuntimeFile(t *testing.T, path, contents string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), mode); err != nil {
		t.Fatal(err)
	}
}

func releaseClient(t *testing.T, tag string, wrapper, library []byte, corruptChecksum bool) *fakeHTTPClient {
	t.Helper()
	checksums := digest(library) + "  " + LibraryAsset + "\n" + digest(wrapper) + "  " + WrapperAsset + "\n"
	if corruptChecksum {
		checksums = strings.Repeat("0", 64) + "  " + LibraryAsset + "\n" + digest(wrapper) + "  " + WrapperAsset + "\n"
	}
	assets := []githubAsset{
		{Name: LibraryAsset, URL: assetURL(tag, LibraryAsset), Size: int64(len(library)), Digest: "sha256:" + digest(library)},
		{Name: WrapperAsset, URL: assetURL(tag, WrapperAsset), Size: int64(len(wrapper)), Digest: "sha256:" + digest(wrapper)},
		{Name: ChecksumsAsset, URL: assetURL(tag, ChecksumsAsset), Size: int64(len(checksums)), Digest: "sha256:" + digest([]byte(checksums))},
	}
	releasePayload, err := json.Marshal(githubRelease{TagName: tag, Assets: assets})
	if err != nil {
		t.Fatal(err)
	}
	return &fakeHTTPClient{
		responses: map[string]httpResponse{
			LatestReleaseURL:              {StatusCode: 200, Body: releasePayload},
			assetURL(tag, LibraryAsset):   {StatusCode: 200, Body: library},
			assetURL(tag, WrapperAsset):   {StatusCode: 200, Body: wrapper},
			assetURL(tag, ChecksumsAsset): {StatusCode: 200, Body: []byte(checksums)},
		},
		errors: map[string]error{},
	}
}

func assetURL(tag, name string) string {
	return "https://github.com/" + Repository + "/releases/download/" + tag + "/" + name
}

func digest(payload []byte) string {
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

func progressContains(progress []Progress, asset string) bool {
	for _, update := range progress {
		if update.Asset == asset {
			return true
		}
	}
	return false
}

func assertRuntimeContents(t *testing.T, home, wrapper, library string) {
	t.Helper()
	wrapperPayload, err := os.ReadFile(filepath.Join(home, ".local", "bin", "linuwux"))
	if err != nil {
		t.Fatal(err)
	}
	libraryPayload, err := os.ReadFile(filepath.Join(home, ".local", "lib", LibraryAsset))
	if err != nil {
		t.Fatal(err)
	}
	if string(wrapperPayload) != wrapper || string(libraryPayload) != library {
		t.Fatalf("runtime changed: wrapper=%q library=%q", wrapperPayload, libraryPayload)
	}
}
