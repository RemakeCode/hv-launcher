package shortcuts

import (
	"errors"
	"fmt"
	"strings"

	"hv-launcher/internal/config"
	"hv-launcher/internal/model"
)

var ErrAlreadyManaged = errors.New("launch value already contains the HV Launcher wrapper")

type Manager struct {
	Store       *config.Store
	WrapperPath string
}

func ManagedLaunchValue(original, wrapperPath, appID string) (string, error) {
	if wrapperPath == "" || appID == "" {
		return "", errors.New("wrapper path and App ID are required")
	}

	if strings.Contains(original, "hv-launcher run --app-id") {
		return "", ErrAlreadyManaged
	}

	prefix := shellQuote(wrapperPath) + " run --app-id " + shellQuote(appID) + " -- %command%"
	if strings.Contains(original, "%command%") {
		return strings.ReplaceAll(original, "%command%", prefix), nil
	}
	if strings.TrimSpace(original) == "" {
		return prefix, nil
	}
	return prefix + " " + original, nil
}

func (m *Manager) Enable(appID, name string, shortcut bool, currentLaunch string) (model.ManagedGame, error) {
	if _, exists := m.Store.Game(appID); exists {
		return model.ManagedGame{}, fmt.Errorf("App ID %s is already managed", appID)
	}

	managed, err := ManagedLaunchValue(currentLaunch, m.WrapperPath, appID)
	if err != nil {
		return model.ManagedGame{}, err
	}

	game := model.ManagedGame{
		AppID: appID, Name: name, Shortcut: shortcut, OriginalLaunch: currentLaunch,
		ManagedLaunch: managed, WrapperPath: m.WrapperPath,
	}
	if err := m.Store.PutGame(game); err != nil {
		return model.ManagedGame{}, err
	}
	return game, nil
}

func (m *Manager) Restore(appID, currentLaunch string) (model.RestoreResponse, error) {
	game, exists := m.Store.Game(appID)
	if !exists {
		return model.RestoreResponse{}, fmt.Errorf("App ID %s is not managed", appID)
	}

	if currentLaunch != game.ManagedLaunch && currentLaunch != game.OriginalLaunch {
		return model.RestoreResponse{AppID: appID, Conflict: true, Message: "Steam launch value was changed after management was enabled"}, nil
	}

	if err := m.Store.DeleteGame(appID); err != nil {
		return model.RestoreResponse{}, err
	}
	return model.RestoreResponse{AppID: appID, OriginalLaunch: game.OriginalLaunch}, nil
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}
