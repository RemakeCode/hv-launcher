package shortcuts

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"hv-launcher/internal/config"
	"hv-launcher/internal/linuwux"
	"hv-launcher/internal/model"
)

var ErrAlreadyManaged = errors.New("launch value already contains the HV Launcher wrapper")

type Manager struct {
	Store       *config.Store
	WrapperPath string
	RuntimePath string
}

func ManagedLaunchValue(original, wrapperPath, appID string) (string, error) {
	return ManagedLaunchValueForMode(original, wrapperPath, appID, "", linuwux.ModeProton)
}

func ManagedLaunchValueForMode(original, wrapperPath, appID, runtimePath string, mode linuwux.Mode) (string, error) {
	if wrapperPath == "" || appID == "" {
		return "", errors.New("wrapper path and App ID are required")
	}
	if !mode.Valid() {
		return "", fmt.Errorf("invalid LinUwUx mode %q", mode)
	}
	if mode == linuwux.ModeRuntime && (runtimePath == "" || !filepath.IsAbs(runtimePath)) {
		return "", errors.New("runtime mode requires an absolute LinUwUx path")
	}

	if strings.Contains(original, "hv-launcher run --app-id") {
		return "", ErrAlreadyManaged
	}

	prefix := shellQuote(wrapperPath) + " run --app-id " + shellQuote(appID) + " --"
	if mode == linuwux.ModeRuntime {
		prefix += " " + shellQuote(runtimePath)
	}
	if mode == linuwux.ModeRuntime && strings.Contains(original, "%command%") {
		environment, command := leadingEnvironment(original)
		return environment + prefix + " " + strings.TrimSpace(command), nil
	}
	prefix += " %command%"
	if strings.Contains(original, "%command%") {
		return strings.ReplaceAll(original, "%command%", prefix), nil
	}
	if strings.TrimSpace(original) == "" {
		return prefix, nil
	}
	return prefix + " " + original, nil
}

func (m *Manager) Enable(appID, name string, shortcut bool, currentLaunch string) (model.ManagedGame, error) {
	return m.EnableWithMode(appID, name, shortcut, currentLaunch, linuwux.ModeProton)
}

func (m *Manager) EnableWithMode(appID, name string, shortcut bool, currentLaunch string, mode linuwux.Mode) (model.ManagedGame, error) {
	if _, exists := m.Store.Game(appID); exists {
		return model.ManagedGame{}, fmt.Errorf("App ID %s is already managed", appID)
	}

	managed, err := ManagedLaunchValueForMode(currentLaunch, m.WrapperPath, appID, m.RuntimePath, mode)
	if err != nil {
		return model.ManagedGame{}, err
	}

	game := model.ManagedGame{
		AppID: appID, Name: name, Shortcut: shortcut, OriginalLaunch: currentLaunch,
		ManagedLaunch: managed, WrapperPath: m.WrapperPath, Mode: mode,
	}
	if err := m.Store.PutGame(game); err != nil {
		return model.ManagedGame{}, err
	}
	return game, nil
}

func (m *Manager) Reconfigure(appID, currentLaunch string, mode linuwux.Mode) (model.ManagedGame, error) {
	game, exists := m.Store.Game(appID)
	if !exists {
		return model.ManagedGame{}, fmt.Errorf("App ID %s is not managed", appID)
	}
	if currentLaunch != game.ManagedLaunch {
		return model.ManagedGame{}, errors.New("Steam launch options changed outside HV Launcher; disable and re-enable management before changing methods")
	}
	managed, err := ManagedLaunchValueForMode(game.OriginalLaunch, m.WrapperPath, appID, m.RuntimePath, mode)
	if err != nil {
		return model.ManagedGame{}, err
	}
	game.ManagedLaunch = managed
	game.WrapperPath = m.WrapperPath
	game.Mode = mode
	if err := m.Store.PutGame(game); err != nil {
		return model.ManagedGame{}, err
	}
	return game, nil
}

func (m *Manager) Disable(appID string) error {
	_, exists := m.Store.Game(appID)
	if !exists {
		return fmt.Errorf("App ID %s is not managed", appID)
	}

	if err := m.Store.DeleteGame(appID); err != nil {
		return err
	}

	return nil
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func leadingEnvironment(value string) (string, string) {
	prefixLength := len(value) - len(strings.TrimLeft(value, " \t"))
	offset := prefixLength
	for offset < len(value) {
		tokenStart := offset
		tokenEnd := shellTokenEnd(value, tokenStart)
		token := value[tokenStart:tokenEnd]
		name, _, assignment := strings.Cut(token, "=")
		if !assignment || !validEnvironmentName(name) {
			break
		}
		offset = tokenEnd
		for offset < len(value) && (value[offset] == ' ' || value[offset] == '\t') {
			offset++
		}
		prefixLength = offset
	}
	return value[:prefixLength], value[prefixLength:]
}

func shellTokenEnd(value string, start int) int {
	var quote byte
	escaped := false
	for index := start; index < len(value); index++ {
		character := value[index]
		if escaped {
			escaped = false
			continue
		}
		if character == '\\' && quote != '\'' {
			escaped = true
			continue
		}
		if quote != 0 {
			if character == quote {
				quote = 0
			}
			continue
		}
		if character == '\'' || character == '"' {
			quote = character
			continue
		}
		if character == ' ' || character == '\t' {
			return index
		}
	}
	return len(value)
}

func validEnvironmentName(value string) bool {
	if value == "" {
		return false
	}
	for index, character := range value {
		if character == '_' || character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || index > 0 && character >= '0' && character <= '9' {
			continue
		}
		return false
	}
	return true
}
