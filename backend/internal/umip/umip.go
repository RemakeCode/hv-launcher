//go:build linux

package umip

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type configurationSnapshot struct {
	data []byte
	info os.FileInfo
}

func (i *Inspector) Apply(ctx context.Context, bootloader Bootloader, progress ProgressFunc) (ApplyResult, error) {
	if i.Runner == nil {
		return ApplyResult{}, errors.New("bootloader updater runner is unavailable")
	}
	configuration, updater, err := i.selectedTargets(bootloader)
	if err != nil {
		return ApplyResult{}, err
	}
	reportProgress(progress, "validating", 5, "Validating the reviewed boot configuration")
	snapshot, err := readConfigurationSnapshot(configuration)
	if err != nil {
		return ApplyResult{}, err
	}
	replacement, err := buildReplacement(bootloader, snapshot.data)
	if err != nil {
		return ApplyResult{}, err
	}

	reportProgress(progress, "backing-up", 15, "Saving a private recovery copy")
	backup, err := createRecoveryBackup(i.Paths.RecoveryDirectory, bootloader, snapshot.data)
	if err != nil {
		return ApplyResult{}, err
	}

	keepBackup := true
	defer func() {
		if !keepBackup {
			_ = removeRecoveryBackup(backup)
		}
	}()

	reportProgress(progress, "updating-configuration", 35, "Applying the reviewed UMIP argument")
	if err := writeConfiguration(configuration, replacement, snapshot.info); err != nil {
		if restoreErr := writeConfiguration(configuration, snapshot.data, snapshot.info); restoreErr != nil {
			return ApplyResult{}, recoveryError(err, backup, fmt.Errorf("restore original configuration: %w", restoreErr))
		}
		keepBackup = false
		return ApplyResult{}, fmt.Errorf("configuration update failed and was rolled back successfully: %w", err)
	}

	reportProgress(progress, "regenerating-boot-configuration", 60, "Updating bootloader configuration")
	output, updateErr := i.Runner.Run(ctx, updater.Path, updater.Args...)
	reportUpdaterOutput(progress, "regenerating-boot-configuration", 65, output)
	if updateErr == nil {
		result := ApplyResult{Bootloader: bootloader, RestartRequired: true}
		if err := removeRecoveryBackup(backup); err != nil {
			result.BackupRetained = backup
		} else {
			keepBackup = false
		}
		return result, nil
	}

	reportProgress(progress, "rolling-back", 75, "Bootloader update failed; restoring the original configuration")
	if err := writeConfiguration(configuration, snapshot.data, snapshot.info); err != nil {
		return ApplyResult{}, recoveryError(updateErr, backup, fmt.Errorf("restore original configuration: %w", err))
	}

	reportProgress(progress, "restoring-generated-configuration", 90, "Regenerating boot configuration from the restored source")
	recoveryOutput, recoveryErr := i.Runner.Run(context.WithoutCancel(ctx), updater.Path, updater.Args...)
	reportUpdaterOutput(progress, "restoring-generated-configuration", 95, recoveryOutput)
	if recoveryErr != nil {
		return ApplyResult{}, recoveryError(updateErr, backup, fmt.Errorf("recovery updater failed: %w", recoveryErr))
	}

	if err := removeRecoveryBackup(backup); err != nil {
		return ApplyResult{}, fmt.Errorf("bootloader update failed and was rolled back successfully; recovery backup remains at %s: %w", backup, updateErr)
	}
	keepBackup = false
	return ApplyResult{}, fmt.Errorf("bootloader update failed and was rolled back successfully: %w", updateErr)
}

func buildReplacement(bootloader Bootloader, data []byte) ([]byte, error) {
	switch bootloader {
	case BootloaderLimine:
		values, err := parseLimine(data)
		if err != nil {
			return nil, err
		}
		if err := requireAction(values); err != nil {
			return nil, err
		}

		replacement := append([]byte(nil), data...)
		if len(replacement) > 0 && replacement[len(replacement)-1] != '\n' {
			replacement = append(replacement, '\n')
		}
		replacement = append(replacement, []byte(limineVariable+"+="+FixedArgument+"\n")...)
		return replacement, nil
	case BootloaderGRUB:
		values, err := parseGRUB(data)
		if err != nil {
			return nil, err
		}
		if err := requireAction(values); err != nil {
			return nil, err
		}
		return replaceGRUBValue(data, appendArgument(values[0]))
	default:
		return nil, fmt.Errorf("%w: %q", ErrUnsupportedBootloader, bootloader)
	}
}

func requireAction(values []string) error {
	state, token := inspectArguments(values)
	if state == "" {
		return fmt.Errorf("%w: conflicting kernel argument %q", ErrCandidateUnavailable, token)
	}

	if state == StateConfigured {
		return fmt.Errorf("%w: %s is already configured", ErrNoChangeRequired, token)
	}
	return nil
}

func replaceGRUBValue(data []byte, proposed string) ([]byte, error) {
	offset := 0
	for _, lineWithEnding := range bytes.SplitAfter(data, []byte{'\n'}) {
		line := strings.TrimSuffix(string(lineWithEnding), "\n")
		line = strings.TrimSuffix(line, "\r")
		_, matched, err := parseAssignment(line, grubVariable, false, true)
		if err != nil {
			return nil, err
		}
		if !matched {
			offset += len(lineWithEnding)
			continue
		}

		valueStart, valueEnd, err := quotedValueBounds(line)
		if err != nil {
			return nil, err
		}
		start := offset + valueStart
		end := offset + valueEnd
		replacement := make([]byte, 0, len(data)+len(proposed)-(end-start))
		replacement = append(replacement, data[:start]...)
		replacement = append(replacement, proposed...)
		replacement = append(replacement, data[end:]...)
		return replacement, nil
	}
	return nil, fmt.Errorf("%s is missing", grubVariable)
}

func quotedValueBounds(line string) (int, int, error) {
	trimmedStart := len(line) - len(strings.TrimLeftFunc(line, func(character rune) bool {
		return character == ' ' || character == '\t'
	}))
	position := trimmedStart + len(grubVariable)
	for position < len(line) && (line[position] == ' ' || line[position] == '\t') {
		position++
	}
	if position >= len(line) || line[position] != '=' {
		return 0, 0, fmt.Errorf("%s is not a supported assignment", grubVariable)
	}

	position++
	for position < len(line) && (line[position] == ' ' || line[position] == '\t') {
		position++
	}
	if position >= len(line) || (line[position] != '\'' && line[position] != '"') {
		return 0, 0, fmt.Errorf("%s value is not quoted", grubVariable)
	}

	quoted := line[position:]
	closing := closingQuote(quoted, quoted[0])
	if closing < 0 {
		return 0, 0, fmt.Errorf("%s value is incompletely quoted", grubVariable)
	}
	return position + 1, position + closing, nil
}

func (i *Inspector) selectedTargets(bootloader Bootloader) (string, Updater, error) {
	switch bootloader {
	case BootloaderLimine:
		updater, err := i.limineUpdater()
		return i.Paths.LimineConfiguration, updater, err
	case BootloaderGRUB:
		updater, err := i.grubUpdater()
		return i.Paths.GRUBConfiguration, updater, err
	default:
		return "", Updater{}, fmt.Errorf("%w: %q", ErrUnsupportedBootloader, bootloader)
	}
}

func readConfigurationSnapshot(path string) (configurationSnapshot, error) {
	initial, err := os.Stat(path)
	if err != nil {
		return configurationSnapshot{}, fmt.Errorf("inspect trusted configuration %s: %w", path, err)
	}

	if err := validateConfigurationInfo(path, initial); err != nil {
		return configurationSnapshot{}, err
	}
	file, err := os.Open(path)
	if err != nil {
		return configurationSnapshot{}, fmt.Errorf("open trusted configuration %s: %w", path, err)
	}

	defer file.Close()
	opened, err := file.Stat()
	if err != nil {
		return configurationSnapshot{}, fmt.Errorf("inspect opened configuration %s: %w", path, err)
	}

	if !os.SameFile(initial, opened) {
		return configurationSnapshot{}, ErrConfigurationChanged
	}
	if err := validateConfigurationInfo(path, opened); err != nil {
		return configurationSnapshot{}, err
	}
	data, err := io.ReadAll(io.LimitReader(file, MaxConfigurationBytes+1))
	if err != nil {
		return configurationSnapshot{}, fmt.Errorf("read trusted configuration %s: %w", path, err)
	}

	if int64(len(data)) > MaxConfigurationBytes {
		return configurationSnapshot{}, fmt.Errorf("%s exceeds the %d-byte inspection limit", path, MaxConfigurationBytes)
	}
	return configurationSnapshot{data: data, info: opened}, nil
}

func validateConfigurationInfo(path string, info os.FileInfo) error {
	if !info.Mode().IsRegular() {
		return fmt.Errorf("%s must be a regular file", path)
	}
	if info.Size() > MaxConfigurationBytes {
		return fmt.Errorf("%s exceeds the %d-byte inspection limit", path, MaxConfigurationBytes)
	}
	return nil
}

func writeConfiguration(path string, data []byte, expected os.FileInfo) error {
	current, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("inspect configuration before writing: %w", err)
	}

	if err := validateConfigurationInfo(path, current); err != nil {
		return err
	}
	if !os.SameFile(expected, current) {
		return ErrConfigurationChanged
	}

	file, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		return fmt.Errorf("open configuration for writing: %w", err)
	}

	defer file.Close()
	opened, err := file.Stat()
	if err != nil {
		return fmt.Errorf("inspect opened configuration: %w", err)
	}
	if !os.SameFile(expected, opened) {
		return ErrConfigurationChanged
	}
	written, err := file.WriteAt(data, 0)
	if err != nil {
		return fmt.Errorf("write configuration: %w", err)
	}
	if written != len(data) {
		return io.ErrShortWrite
	}
	if err := file.Truncate(int64(len(data))); err != nil {
		return fmt.Errorf("truncate configuration: %w", err)
	}
	if err := file.Sync(); err != nil {
		return fmt.Errorf("sync configuration: %w", err)
	}
	return nil
}

func createRecoveryBackup(directory string, bootloader Bootloader, data []byte) (string, error) {
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return "", fmt.Errorf("create recovery directory: %w", err)
	}

	if err := os.Chmod(directory, 0o700); err != nil {
		return "", fmt.Errorf("make recovery directory private: %w", err)
	}
	info, err := os.Lstat(directory)
	if err != nil || !info.IsDir() {
		return "", fmt.Errorf("recovery path must be a directory")
	}

	random := make([]byte, 12)
	if _, err := rand.Read(random); err != nil {
		return "", fmt.Errorf("create recovery backup name: %w", err)
	}

	name := "umip-" + string(bootloader) + "-" + base64.RawURLEncoding.EncodeToString(random) + ".bak"
	path := filepath.Join(directory, name)
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return "", fmt.Errorf("create recovery backup: %w", err)
	}

	remove := true
	defer func() {
		file.Close()
		if remove {
			_ = os.Remove(path)
		}
	}()
	if _, err := file.Write(data); err != nil {
		return "", fmt.Errorf("write recovery backup: %w", err)
	}
	if err := file.Sync(); err != nil {
		return "", fmt.Errorf("sync recovery backup: %w", err)
	}
	if err := file.Close(); err != nil {
		return "", fmt.Errorf("close recovery backup: %w", err)
	}
	if err := syncDirectory(directory); err != nil {
		return "", err
	}
	remove = false
	return path, nil
}

func removeRecoveryBackup(path string) error {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return syncDirectory(filepath.Dir(path))
}

func syncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}

func reportProgress(progress ProgressFunc, phase string, percent int, message string) {
	if progress != nil {
		progress(phase, percent, message)
	}
}

func reportUpdaterOutput(progress ProgressFunc, phase string, percent int, output []byte) {
	if progress != nil && len(output) > 0 {
		progress(phase, percent, string(output))
	}
}

func recoveryError(updateErr error, backup string, recoveryErr error) error {
	return fmt.Errorf("bootloader update failed (%v); recovery requires %s: %w", updateErr, backup, recoveryErr)
}
