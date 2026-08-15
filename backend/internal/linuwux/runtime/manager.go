package runtime

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"hv-launcher/internal/model"
)

type LocalManager struct {
	UserHome string
	options  localManagerOptions
}

type netHTTPClient struct {
	client *http.Client
}

type execVersionRunner struct{}

type githubRelease struct {
	TagName    string        `json:"tag_name"`
	Draft      bool          `json:"draft"`
	Prerelease bool          `json:"prerelease"`
	Assets     []githubAsset `json:"assets"`
}

type githubAsset struct {
	Name   string `json:"name"`
	URL    string `json:"browser_download_url"`
	Digest string `json:"digest"`
	Size   int64  `json:"size"`
}

type committedFile struct {
	destination string
	temporary   string
	backup      string
	hadPrevious bool
	activated   bool
}

var versionPattern = regexp.MustCompile(`(?i)\bv?([0-9]+(?:\.[0-9]+){2,})\b`)
var releaseTagPattern = regexp.MustCompile(`^v[0-9]+(?:\.[0-9]+){2,}$`)

func NewLocalManager(userHome string) (*LocalManager, error) {
	return newLocalManager(userHome, localManagerOptions{})
}

func newLocalManager(userHome string, options localManagerOptions) (*LocalManager, error) {
	if userHome == "" || !filepath.IsAbs(userHome) {
		return nil, errors.New("Decky user home must be an absolute path")
	}
	if options.GOARCH == "" {
		options.GOARCH = runtime.GOARCH
	}
	if options.Timeout <= 0 {
		options.Timeout = 30 * time.Second
	}
	if options.Client == nil {
		options.Client = &netHTTPClient{client: &http.Client{Timeout: options.Timeout}}
	}
	if options.Runner == nil {
		options.Runner = execVersionRunner{}
	}

	return &LocalManager{UserHome: filepath.Clean(userHome), options: options}, nil
}

func (o *LocalManager) Inspect(ctx context.Context) (model.RuntimeStatus, error) {
	wrapperPath, libraryPath := o.paths()
	status := model.RuntimeStatus{
		Supported:   o.options.GOARCH == SupportedGOARCH,
		Path:        wrapperPath,
		LibraryPath: libraryPath,
		UpdateState: model.RuntimeUpdateUnknown,
	}
	if !status.Supported {
		status.State = model.RuntimeStateUnsupported
		status.Detail = fmt.Sprintf("the prebuilt LinUwUx runtime supports x86_64 only (detected %s)", o.options.GOARCH)
		return status, nil
	}

	wrapperInfo, wrapperErr := os.Lstat(wrapperPath)
	libraryInfo, libraryErr := os.Lstat(libraryPath)
	if errors.Is(wrapperErr, os.ErrNotExist) && errors.Is(libraryErr, os.ErrNotExist) {
		status.State = model.RuntimeStateAbsent
		status.Detail = "LinUwUx runtime is not installed"
		return status, nil
	}
	if wrapperErr != nil || libraryErr != nil {
		status.State = model.RuntimeStateInvalid
		status.Detail = runtimeStructureError(wrapperErr, libraryErr)
		return status, nil
	}
	if !wrapperInfo.Mode().IsRegular() || wrapperInfo.Mode()&0o111 == 0 || wrapperInfo.Size() <= 0 {
		status.State = model.RuntimeStateInvalid
		status.Detail = "the LinUwUx wrapper must be a non-empty executable regular file"
		return status, nil
	}
	if !libraryInfo.Mode().IsRegular() || libraryInfo.Size() <= 0 {
		status.State = model.RuntimeStateInvalid
		status.Detail = "the LinUwUx library must be a non-empty regular file"
		return status, nil
	}

	status.Available = true
	status.State = model.RuntimeStateAvailable
	versionContext, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	output, err := o.options.Runner.Version(versionContext, wrapperPath, o.UserHome)
	if err != nil {
		status.Detail = "runtime files are usable; installed version could not be read"
		return status, nil
	}
	version, ok := NormalizeVersion(string(output))
	if !ok {
		status.Detail = "runtime files are usable; installed version output was not recognized"
		return status, nil
	}
	status.Version = version
	status.VersionKnown = true
	status.Detail = "LinUwUx runtime is installed"
	return status, nil
}

func (o *LocalManager) Latest(ctx context.Context) (model.RuntimeRelease, error) {
	body, err := o.get(ctx, LatestReleaseURL, maxReleaseBytes)
	if err != nil {
		return model.RuntimeRelease{}, fmt.Errorf("resolve latest LinUwUx release: %w", err)
	}
	var release githubRelease
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&release); err != nil {
		// GitHub adds fields over time, so retry without rejecting unrelated
		// metadata while still validating every field we consume below.
		if err := json.Unmarshal(body, &release); err != nil {
			return model.RuntimeRelease{}, fmt.Errorf("decode latest LinUwUx release: %w", err)
		}
	}
	if release.Draft || release.Prerelease {
		return model.RuntimeRelease{}, errors.New("latest LinUwUx release is not stable")
	}
	version, ok := NormalizeVersion(release.TagName)
	if !ok || !releaseTagPattern.MatchString(release.TagName) {
		return model.RuntimeRelease{}, fmt.Errorf("latest LinUwUx release has invalid tag %q", release.TagName)
	}

	expected := map[string]int64{
		LibraryAsset: maxLibraryBytes, WrapperAsset: maxWrapperBytes, ChecksumsAsset: maxChecksumBytes,
	}
	assets := make(map[string]model.RuntimeReleaseAsset, len(expected))
	for _, asset := range release.Assets {
		limit, wanted := expected[asset.Name]
		if !wanted {
			continue
		}
		if _, duplicate := assets[asset.Name]; duplicate {
			return model.RuntimeRelease{}, fmt.Errorf("release contains duplicate %s asset", asset.Name)
		}
		if asset.Size <= 0 || asset.Size > limit {
			return model.RuntimeRelease{}, fmt.Errorf("release asset %s has unsupported size", asset.Name)
		}
		if err := validateAssetURL(asset.URL); err != nil {
			return model.RuntimeRelease{}, fmt.Errorf("release asset %s: %w", asset.Name, err)
		}
		if asset.Digest != "" && !validGitHubDigest(asset.Digest) {
			return model.RuntimeRelease{}, fmt.Errorf("release asset %s has invalid digest", asset.Name)
		}
		assets[asset.Name] = model.RuntimeReleaseAsset{Name: asset.Name, URL: asset.URL, Digest: asset.Digest}
	}
	for name := range expected {
		if _, ok := assets[name]; !ok {
			return model.RuntimeRelease{}, fmt.Errorf("release is missing required asset %s", name)
		}
	}

	return model.RuntimeRelease{Repository: Repository, Tag: release.TagName, Version: version, Assets: assets}, nil
}

func (o *LocalManager) Install(ctx context.Context, progress ProgressFunc) (model.RuntimeInstallResult, error) {
	if o.options.GOARCH != SupportedGOARCH {
		return model.RuntimeInstallResult{}, fmt.Errorf("LinUwUx runtime is unsupported on %s", o.options.GOARCH)
	}
	report(progress, Progress{Phase: "resolving-release", Progress: 2, Message: "Resolving the latest stable LinUwUx release"})
	release, err := o.Latest(ctx)
	if err != nil {
		return model.RuntimeInstallResult{}, err
	}

	stageRoot := filepath.Join(o.UserHome, ".local", "share", "hv-launcher")
	if err := os.MkdirAll(stageRoot, 0o700); err != nil {
		return model.RuntimeInstallResult{}, fmt.Errorf("create runtime staging root: %w", err)
	}
	stage, err := os.MkdirTemp(stageRoot, ".linuwux-stage-")
	if err != nil {
		return model.RuntimeInstallResult{}, fmt.Errorf("create runtime staging directory: %w", err)
	}
	defer os.RemoveAll(stage)

	order := []string{ChecksumsAsset, LibraryAsset, WrapperAsset}
	limits := map[string]int64{ChecksumsAsset: maxChecksumBytes, LibraryAsset: maxLibraryBytes, WrapperAsset: maxWrapperBytes}
	for index, name := range order {
		asset := release.Assets[name]
		report(progress, Progress{Phase: "downloading", Progress: 10 + index*15, Message: "Downloading " + name, ReleaseTag: release.Tag, Asset: name})
		payload, err := o.get(ctx, asset.URL, limits[name])
		if err != nil {
			return model.RuntimeInstallResult{}, fmt.Errorf("download %s: %w", name, err)
		}
		if asset.Digest != "" && !matchesDigest(payload, strings.TrimPrefix(asset.Digest, "sha256:")) {
			return model.RuntimeInstallResult{}, fmt.Errorf("GitHub digest verification failed for %s", name)
		}
		if err := os.WriteFile(filepath.Join(stage, name), payload, 0o600); err != nil {
			return model.RuntimeInstallResult{}, fmt.Errorf("stage %s: %w", name, err)
		}
	}

	report(progress, Progress{Phase: "verifying", Progress: 60, Message: "Verifying published checksums", ReleaseTag: release.Tag, Asset: ChecksumsAsset})
	checksums, err := readChecksums(filepath.Join(stage, ChecksumsAsset))
	if err != nil {
		return model.RuntimeInstallResult{}, err
	}
	for _, name := range []string{LibraryAsset, WrapperAsset} {
		expected, ok := checksums[name]
		if !ok {
			return model.RuntimeInstallResult{}, fmt.Errorf("published checksums do not include %s", name)
		}
		payload, err := os.ReadFile(filepath.Join(stage, name))
		if err != nil {
			return model.RuntimeInstallResult{}, fmt.Errorf("read staged %s: %w", name, err)
		}
		if !matchesDigest(payload, expected) {
			return model.RuntimeInstallResult{}, fmt.Errorf("published checksum verification failed for %s", name)
		}
	}

	wrapperPath, libraryPath := o.paths()
	report(progress, Progress{Phase: "installing", Progress: 85, Message: "Installing verified runtime files", ReleaseTag: release.Tag, Asset: LibraryAsset})
	if err := commitRuntimeFiles(filepath.Join(stage, WrapperAsset), filepath.Join(stage, LibraryAsset), wrapperPath, libraryPath); err != nil {
		return model.RuntimeInstallResult{}, err
	}
	report(progress, Progress{Phase: "complete", Progress: 100, Message: "LinUwUx runtime installed", ReleaseTag: release.Tag})
	return model.RuntimeInstallResult{
		Repository: Repository, ReleaseTag: release.Tag, Version: release.Version,
		Path: wrapperPath, LibraryPath: libraryPath,
	}, nil
}

func (o *LocalManager) Remove(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	wrapperPath, libraryPath := o.paths()
	var removeErrors []error
	for _, path := range []string{wrapperPath, libraryPath} {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			removeErrors = append(removeErrors, fmt.Errorf("remove %s: %w", path, err))
		}
	}
	if len(removeErrors) > 0 {
		return errors.Join(removeErrors...)
	}
	return nil
}

func (o *LocalManager) paths() (string, string) {
	return filepath.Join(o.UserHome, ".local", "bin", "linuwux"), filepath.Join(o.UserHome, ".local", "lib", LibraryAsset)
}

func (o *LocalManager) get(ctx context.Context, target string, maximum int64) ([]byte, error) {
	response, err := o.options.Client.Do(&httpRequest{Context: ctx, URL: target, Maximum: maximum, Header: map[string]string{
		"Accept": "application/vnd.github+json", "User-Agent": "hv-launcher",
	}})
	if err != nil {
		return nil, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("HTTP status %d", response.StatusCode)
	}
	if int64(len(response.Body)) > maximum {
		return nil, fmt.Errorf("response exceeds %d bytes", maximum)
	}
	return response.Body, nil
}

func (c *netHTTPClient) Do(request *httpRequest) (*httpResponse, error) {
	httpRequest, err := http.NewRequestWithContext(request.Context, http.MethodGet, request.URL, nil)
	if err != nil {
		return nil, err
	}
	for name, value := range request.Header {
		httpRequest.Header.Set(name, value)
	}
	response, err := c.client.Do(httpRequest)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, request.Maximum+1))
	if err != nil {
		return nil, err
	}
	return &httpResponse{StatusCode: response.StatusCode, Body: body}, nil
}

func (execVersionRunner) Version(ctx context.Context, wrapperPath, userHome string) ([]byte, error) {
	command := exec.CommandContext(ctx, wrapperPath, "--version")
	command.Env = []string{
		"HOME=" + userHome,
		"LANG=C",
		"LC_ALL=C",
		"PATH=/usr/local/bin:/usr/bin:/bin",
	}
	var output limitedBuffer
	command.Stdout = &output
	command.Stderr = &output
	err := command.Run()
	return output.Bytes(), err
}

type limitedBuffer struct {
	buffer bytes.Buffer
}

func (b *limitedBuffer) Write(payload []byte) (int, error) {
	remaining := maxVersionBytes - b.buffer.Len()
	if remaining <= 0 {
		return 0, errors.New("version output is too large")
	}
	if len(payload) > remaining {
		_, _ = b.buffer.Write(payload[:remaining])
		return remaining, errors.New("version output is too large")
	}
	return b.buffer.Write(payload)
}

func (b *limitedBuffer) Bytes() []byte { return b.buffer.Bytes() }

func NormalizeVersion(value string) (string, bool) {
	match := versionPattern.FindStringSubmatch(value)
	if len(match) != 2 {
		return "", false
	}
	parts := strings.Split(match[1], ".")
	for _, part := range parts {
		if _, err := strconv.ParseUint(part, 10, 32); err != nil {
			return "", false
		}
	}
	return strings.Join(parts, "."), true
}

func CompareVersions(left, right string) int {
	leftParts := versionNumbers(left)
	rightParts := versionNumbers(right)
	length := len(leftParts)
	if len(rightParts) > length {
		length = len(rightParts)
	}
	for index := 0; index < length; index++ {
		var leftValue, rightValue uint64
		if index < len(leftParts) {
			leftValue = leftParts[index]
		}
		if index < len(rightParts) {
			rightValue = rightParts[index]
		}
		if leftValue < rightValue {
			return -1
		}
		if leftValue > rightValue {
			return 1
		}
	}
	return 0
}

func versionNumbers(value string) []uint64 {
	version, ok := NormalizeVersion(value)
	if !ok {
		return nil
	}
	parts := strings.Split(version, ".")
	numbers := make([]uint64, 0, len(parts))
	for _, part := range parts {
		number, _ := strconv.ParseUint(part, 10, 32)
		numbers = append(numbers, number)
	}
	return numbers
}

func runtimeStructureError(wrapperErr, libraryErr error) string {
	switch {
	case errors.Is(wrapperErr, os.ErrNotExist):
		return "the LinUwUx wrapper is missing"
	case errors.Is(libraryErr, os.ErrNotExist):
		return "the LinUwUx library is missing"
	case wrapperErr != nil:
		return "the LinUwUx wrapper could not be inspected"
	default:
		return "the LinUwUx library could not be inspected"
	}
}

func validateAssetURL(value string) error {
	parsed, err := url.Parse(value)
	if err != nil {
		return errors.New("download URL is invalid")
	}
	if parsed.Scheme != "https" || parsed.Hostname() != "github.com" || !strings.HasPrefix(parsed.EscapedPath(), "/"+Repository+"/releases/download/") {
		return errors.New("download URL is outside the fixed GitHub release source")
	}
	return nil
}

func validGitHubDigest(value string) bool {
	digest := strings.TrimPrefix(value, "sha256:")
	if digest == value || len(digest) != sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(digest)
	return err == nil
}

func matchesDigest(payload []byte, expected string) bool {
	digest := sha256.Sum256(payload)
	return strings.EqualFold(hex.EncodeToString(digest[:]), strings.TrimSpace(expected))
}

func readChecksums(path string) (map[string]string, error) {
	payload, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read published checksums: %w", err)
	}
	checksums := map[string]string{}
	for _, line := range strings.Split(string(payload), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		if len(fields) != 2 || len(fields[0]) != sha256.Size*2 {
			return nil, errors.New("published checksum file has an invalid line")
		}
		if _, err := hex.DecodeString(fields[0]); err != nil {
			return nil, errors.New("published checksum file has an invalid digest")
		}
		name := strings.TrimPrefix(fields[1], "*")
		if name != LibraryAsset && name != WrapperAsset {
			continue
		}
		if _, duplicate := checksums[name]; duplicate {
			return nil, fmt.Errorf("published checksum file repeats %s", name)
		}
		checksums[name] = strings.ToLower(fields[0])
	}
	return checksums, nil
}

func commitRuntimeFiles(stagedWrapper, stagedLibrary, wrapperPath, libraryPath string) error {
	files := []struct {
		source      string
		destination string
		mode        os.FileMode
	}{
		{source: stagedLibrary, destination: libraryPath, mode: 0o644},
		{source: stagedWrapper, destination: wrapperPath, mode: 0o755},
	}
	prepared := make([]committedFile, 0, len(files))
	for _, file := range files {
		if err := os.MkdirAll(filepath.Dir(file.destination), 0o755); err != nil {
			cleanupPrepared(prepared)
			return fmt.Errorf("create runtime destination: %w", err)
		}
		temporary, err := copyToTemporary(file.source, filepath.Dir(file.destination), file.mode)
		if err != nil {
			cleanupPrepared(prepared)
			return err
		}
		prepared = append(prepared, committedFile{
			destination: file.destination,
			temporary:   temporary,
			backup:      file.destination + ".hv-launcher-backup",
		})
	}

	for index := range prepared {
		file := &prepared[index]
		_ = os.Remove(file.backup)
		if _, err := os.Lstat(file.destination); err == nil {
			if err := os.Rename(file.destination, file.backup); err != nil {
				rollbackRuntimeFiles(prepared)
				return fmt.Errorf("back up prior runtime file: %w", err)
			}
			file.hadPrevious = true
		} else if !errors.Is(err, os.ErrNotExist) {
			rollbackRuntimeFiles(prepared)
			return fmt.Errorf("inspect prior runtime file: %w", err)
		}
	}
	for index := range prepared {
		file := &prepared[index]
		if err := os.Rename(file.temporary, file.destination); err != nil {
			rollbackRuntimeFiles(prepared)
			return fmt.Errorf("activate verified runtime file: %w", err)
		}
		file.temporary = ""
		file.activated = true
	}
	for _, file := range prepared {
		if file.hadPrevious {
			_ = os.Remove(file.backup)
		}
	}
	return nil
}

func copyToTemporary(source, destinationDir string, mode os.FileMode) (string, error) {
	payload, err := os.ReadFile(source)
	if err != nil {
		return "", err
	}
	file, err := os.CreateTemp(destinationDir, ".linuwux-new-")
	if err != nil {
		return "", err
	}
	name := file.Name()
	defer func() {
		_ = file.Close()
	}()
	if _, err := file.Write(payload); err != nil {
		_ = os.Remove(name)
		return "", err
	}
	if err := file.Chmod(mode); err != nil {
		_ = os.Remove(name)
		return "", err
	}
	if err := file.Sync(); err != nil {
		_ = os.Remove(name)
		return "", err
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(name)
		return "", err
	}
	return name, nil
}

func rollbackRuntimeFiles(files []committedFile) {
	for _, file := range files {
		if file.temporary != "" {
			_ = os.Remove(file.temporary)
		}
		if file.activated {
			_ = os.Remove(file.destination)
		}
		if file.hadPrevious {
			_ = os.Rename(file.backup, file.destination)
		}
	}
}

func cleanupPrepared(files []committedFile) {
	for _, file := range files {
		if file.temporary != "" {
			_ = os.Remove(file.temporary)
		}
	}
}

func report(progress ProgressFunc, update Progress) {
	if progress != nil {
		progress(update)
	}
}

func sanitized(value string) string {
	value = strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(value, "\r", " "), "\n", " "))
	value = strings.ToValidUTF8(value, "\uFFFD")
	if len(value) > 1024 {
		value = value[:1024]
		for !utf8.ValidString(value) {
			value = value[:len(value)-1]
		}
	}
	return value
}
