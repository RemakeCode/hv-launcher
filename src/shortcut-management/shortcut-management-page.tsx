import {
  DialogBody,
  DialogControlsSection,
  DialogControlsSectionHeader,
  DialogLabel,
  Dropdown,
  Field,
  Toggle,
  SidebarNavigation
} from '@decky/ui';
import { useCallback, useEffect, useRef, useState } from 'react';
import { getConfiguration, getStatus } from '@/api';
import {
  availableModes,
  effectiveGameMode,
  groupShortcuts,
  isModeAvailable,
  shortcutActionError,
  shortcutDescription
} from '@/shortcut-management/management';
import { logger } from '@/shared/logger';
import { LoadingSpinner } from '@/shared/loading-spinner';
import {
  disableManagedGame,
  discoverGames,
  displayState,
  enableManagedGame,
  observeSteamOverviews,
  SteamLibraryLoadingError
} from '@/steam';
import type { Configuration, DisplayState, Game, LinUwUxMode, LinUwUxParam, SystemStatus } from '@/types';
import { GiGamepad } from 'react-icons/gi';

const EMPTY_CONFIGURATION: Configuration = { version: 1, games: {} };
const LINUWUX_PARAMS: { name: LinUwUxParam; label: string }[] = [
  { name: 'PROTON_AVX', label: 'AVX' },
  { name: 'LINUWUX_SYSCALL_HACK', label: 'Syscall workaround' },
  { name: 'LINUWUX_LEGACY_PROFILE', label: 'Legacy profile' },
  { name: 'LINUWUX_WIN32U_FREE_GUARD', label: 'Free guard' }
];

const shortcutManagementStyles = `
  .hv-shortcut-error {
    color: #ffb4a9;
    margin-block: 8px;
  }

  .hv-shortcut-library-message {
    margin-bottom: 16px;
  }

  .hv-shortcut-actions {
    display: flex;
    align-items: center;
    flex-shrink: 0;
    gap: 8px;
  }

  .hv-shortcut-actions > * {
    flex-shrink: 0;
  }

  .hv-shortcut-actions > :first-child {
    width: 220px;
  }

  .hv-shortcut-toggle {
    display: flex;
    align-items: center;
    flex: 0 0 40px;
  }

  .hv-shortcut-toggle > * {
    width: 40px;
    flex-shrink: 0;
  }

  .hv-shortcut-empty {
    margin-block-start: 8px;
  }

  .hv-shortcut-params {
    display: flex;
    flex-wrap: wrap;
    gap: 8px;
  }

  .hv-shortcut-param {
    flex: 1 1 180px;
  }
`;

function modeLabel(mode: LinUwUxMode): string {
  return mode === 'runtime' ? 'LinUwUx runtime' : 'LinUwUx Proton';
}

export function ShortcutManagementPage() {
  const [status, setStatus] = useState<SystemStatus>();
  const [games, setGames] = useState<Game[]>([]);
  const [states, setStates] = useState<Record<string, DisplayState>>({});
  const [busy, setBusy] = useState<string>();
  const [error, setError] = useState('');
  const [libraryMessage, setLibraryMessage] = useState('');
  const [configuration, setConfiguration] = useState<Configuration>(EMPTY_CONFIGURATION);
  const [selections, setSelections] = useState<Record<string, LinUwUxMode>>({});
  const [params, setParams] = useState<Record<string, LinUwUxParam[]>>({});
  const configurationRef = useRef<Configuration>(EMPTY_CONFIGURATION);

  const refreshLibrary = useCallback((configuration: Configuration) => {
    try {
      const nextGames = discoverGames(configuration).filter((game) => game.shortcut);
      setGames(nextGames);
      setStates(Object.fromEntries(nextGames.map((game) => [game.appId, displayState(game.appId)])));
      setLibraryMessage('');
    } catch (reason) {
      if (reason instanceof SteamLibraryLoadingError) {
        setGames([]);
        setStates({});
        setLibraryMessage(reason.message);
        return;
      }
      throw reason;
    }
  }, []);

  const refresh = useCallback(async () => {
    try {
      const [nextStatus, configuration] = await Promise.all([getStatus(), getConfiguration()]);
      configurationRef.current = configuration;
      setConfiguration(configuration);
      setStatus(nextStatus);
      refreshLibrary(configuration);
      setError('');
    } catch (reason) {
      logger.error('Failed to refresh Shortcut management', reason);
      setError(reason instanceof Error ? reason.message : String(reason));
    }
  }, [refreshLibrary]);

  useEffect(() => {
    void refresh();
  }, [refresh]);

  useEffect(
    () =>
      observeSteamOverviews(() => {
        try {
          refreshLibrary(configurationRef.current);
        } catch (reason) {
          logger.error('Failed to refresh Steam shortcut state', reason);
          setError(reason instanceof Error ? reason.message : String(reason));
        }
      }),
    [refreshLibrary]
  );

  const toggle = async (game: Game, enabled: boolean) => {
    setBusy(game.appId);
    setError('');
    try {
      const mode = selections[game.appId] ?? availableModes(status!)[0] ?? 'proton';
      if (enabled && !isModeAvailable(status!, mode)) {
        throw new Error(`${modeLabel(mode)} setup is required before enabling this shortcut.`);
      }
      if (enabled) {
        await enableManagedGame(game, mode, mode === 'runtime' ? (params[game.appId] ?? []) : []);
      } else {
        await disableManagedGame(game);
        setSelections((current) => ({ ...current, [game.appId]: effectiveGameMode(configuration, game.appId) }));
        setParams((current) => ({ ...current, [game.appId]: configuration.games[game.appId]?.params ?? [] }));
      }
      await refresh();
    } catch (reason) {
      logger.error(`Failed to ${enabled ? 'enable' : 'disable'} ${game.name}`, reason);
      setError(shortcutActionError(game, enabled, reason));
    } finally {
      setBusy(undefined);
    }
  };

  const changeMode = (game: Game, mode: LinUwUxMode) => {
    if (game.enabled || busy !== undefined) return;
    if (!isModeAvailable(status!, mode)) {
      setError(`${modeLabel(mode)} setup is required before it can be selected.`);
      return;
    }
    setError('');
    setSelections((current) => ({ ...current, [game.appId]: mode }));
  };

  const sections = groupShortcuts(games);
  const renderRow = (game: Game) => {
    const selected = game.enabled
      ? effectiveGameMode(configuration, game.appId)
      : (selections[game.appId] ?? availableModes(status!)[0] ?? 'proton');
    const available = isModeAvailable(status!, selected);
    const selectedParams = game.enabled ? (configuration.games[game.appId]?.params ?? []) : (params[game.appId] ?? []);
    const options: LinUwUxMode[] = ['proton', 'runtime'];
    const description = [
      shortcutDescription(game, states[game.appId] ?? 'idle', busy === game.appId),
      game.enabled && selected === 'runtime' && selectedParams.length > 0
        ? `LinUwUx params: ${selectedParams.map((param) => `${param}=1`).join(', ')}`
        : undefined,
      !available ? 'Selected method requires setup.' : undefined
    ]
      .filter(Boolean)
      .join(' ');
    return (
      <div key={game.appId}>
        <Field
          label={game.name}
          description={description}
          childrenLayout='inline'
          childrenContainerWidth='min'
          verticalAlignment='center'
        >
          <div className='hv-shortcut-actions'>
            <Dropdown
              menuLabel='LinUwUx method'
              rgOptions={options.map((mode) => ({
                data: mode,
                label: `${modeLabel(mode)}${isModeAvailable(status!, mode) ? '' : ' · setup required'}`
              }))}
              selectedOption={selected}
              disabled={game.enabled || busy !== undefined || game.missing}
              onChange={(option) => changeMode(game, String(option.data) as LinUwUxMode)}
            />
            <div className='hv-shortcut-toggle'>
              <Toggle
                value={game.enabled}
                disabled={busy !== undefined || (!game.enabled && !available)}
                onChange={(enabled) => void toggle(game, enabled)}
              />
            </div>
          </div>
        </Field>
        {!game.enabled && selected === 'runtime' && (
          <Field
            label='LinUwUx params'
            description='Optional runtime variables. Only enable these if you understand what they do or your game’s compatibility instructions require them. Leave them off otherwise.'
            childrenLayout='below'
          >
            <div className='hv-shortcut-params'>
              {LINUWUX_PARAMS.map((param) => (
                <Field key={param.name} label={param.label} className='hv-shortcut-param' childrenContainerWidth='min'>
                  <div className='hv-shortcut-toggle'>
                    <Toggle
                      value={selectedParams.includes(param.name)}
                      disabled={busy !== undefined || game.missing}
                      onChange={(enabled) => {
                        setParams((current) => {
                          const currentParams = current[game.appId] ?? [];
                          return {
                            ...current,
                            [game.appId]: LINUWUX_PARAMS.map(({ name }) => name).filter((name) =>
                              name === param.name ? enabled : currentParams.includes(name)
                            )
                          };
                        });
                      }}
                    />
                  </div>
                </Field>
              ))}
            </div>
          </Field>
        )}
      </div>
    );
  };

  return (
    <SidebarNavigation
      pages={[
        {
          title: 'ShortCut Management',
          icon: <GiGamepad />,
          content: (
            <DialogBody>
              <style>{shortcutManagementStyles}</style>
              {!status ? (
                error ? (
                  <DialogLabel className='hv-shortcut-error'>{error}</DialogLabel>
                ) : (
                  <LoadingSpinner />
                )
              ) : (
                <>
                  {status.status !== 'hypervisor-ready' && (
                    <DialogLabel>Readiness affects game launch, not shortcut configuration.</DialogLabel>
                  )}
                  {libraryMessage && (
                    <DialogLabel className='hv-shortcut-library-message'>{libraryMessage}</DialogLabel>
                  )}

                  <DialogControlsSection>
                    {error && <DialogLabel className='hv-shortcut-error'>{error}</DialogLabel>}
                    <DialogControlsSectionHeader>Managed shortcuts</DialogControlsSectionHeader>
                    {sections.managed.length > 0 ? (
                      sections.managed.map(renderRow)
                    ) : (
                      <DialogLabel className='hv-shortcut-empty'>No managed shortcuts.</DialogLabel>
                    )}
                  </DialogControlsSection>

                  <DialogControlsSection>
                    <DialogControlsSectionHeader>Available shortcuts</DialogControlsSectionHeader>
                    {sections.available.length > 0 ? (
                      sections.available.map(renderRow)
                    ) : (
                      <DialogLabel>No available installed shortcuts.</DialogLabel>
                    )}
                  </DialogControlsSection>
                </>
              )}
            </DialogBody>
          )
        }
      ]}
    ></SidebarNavigation>
  );
}
