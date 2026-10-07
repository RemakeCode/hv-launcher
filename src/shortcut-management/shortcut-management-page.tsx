import {
  DialogBody,
  DialogHeader,
  DialogControlsSection,
  DialogControlsSectionHeader,
  DialogLabel,
  Dropdown,
  Field,
  Focusable,
  NavEntryPositionPreferences,
  Toggle
} from '@decky/ui';
import { useCallback, useEffect, useState } from 'react';
import { getConfiguration, getStatus } from '@/api';
import {
  availableModes,
  effectiveGameMode,
  shortcuts,
  isModeAvailable,
  shortcutActionError,
  shortcutDescription
} from '@/shortcut-management/management';
import { logger } from '@/shared/logger';
import { LoadingSpinner } from '@/shared/loading-spinner';
import { disableManagedGame, discoverGames, displayState, enableManagedGame } from '@/steam';
import type { Configuration, Game, LinUwUxMode, LinUwUxParam, SystemStatus } from '@/types';

const EMPTY_CONFIGURATION: Configuration = { version: 1, games: {} };
const LINUWUX_PARAMS: { name: LinUwUxParam; tooltip: string }[] = [
  { name: 'PROTON_AVX', tooltip: 'Enables AVX flags in the runtime’s applicable CPU profile.' },
  { name: 'LINUWUX_SYSCALL_HACK', tooltip: 'Enables the direct syscall workaround required by some games.' },
  { name: 'LINUWUX_LEGACY_PROFILE', tooltip: 'Uses the legacy CPU profile and selector-dispatch handling.' },
  { name: 'LINUWUX_WIN32U_FREE_GUARD', tooltip: 'Protects against duplicate memory frees in win32u.' }
];

//language=css
const shortcutManagementStyles = `
  .hv-shortcut-page {
    padding: 54px 2.4vw;
  }

  .hv-shortcut-item {
    border-radius: var(--round-radius-size);
    margin-block-end: 6px;
  }

  .hv-shortcut-page-title {
    margin-block-end: 24px;
  }

  .hv-shortcut-section-title {
    font-size: 14px;
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.5px;
    margin-block: 24px 12px;
  }

  .hv-shortcut-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 16px;
    width: 100%;
  }

  .hv-shortcut-name {
    min-width: 0;
  }

  .hv-shortcut-guidance {
    margin-block-start: 16px;
  }

  .hv-shortcut-summary {
    font-size: 95%;
    opacity: 0.65;
    margin-block-start: 4px;
  }

  .hv-shortcut-error {
    color: #ffb4a9;
    margin-block: 8px;
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

  .hv-shortcut-actions > :first-child:not(.hv-shortcut-toggle) {
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
    gap: 24px;
  }

  .hv-shortcut-param {
    display: flex;
    align-items: center;
    gap: 8px;
  }

  .hv-shortcut-param > :last-child {
    width: 40px;
    flex-shrink: 0;
  }
`;

function modeLabel(mode: LinUwUxMode): string {
  return mode === 'runtime' ? 'LinUwUx Runtime' : 'LinUwUx Proton';
}

export function ShortcutManagementPage() {
  const [status, setStatus] = useState<SystemStatus>();
  const [games, setGames] = useState<Game[]>([]);
  const [busy, setBusy] = useState<string>();
  const [error, setError] = useState('');
  const [configuration, setConfiguration] = useState<Configuration>(EMPTY_CONFIGURATION);
  const [selections, setSelections] = useState<Record<string, LinUwUxMode>>({});
  const [params, setParams] = useState<Record<string, LinUwUxParam[]>>({});

  const updateGamesFromSteam = useCallback((configuration: Configuration) => {
    setGames(discoverGames(configuration));
  }, []);

  const reloadBackendAndLibrary = useCallback(async () => {
    try {
      const [nextStatus, configuration] = await Promise.all([getStatus(), getConfiguration()]);
      setConfiguration(configuration);
      setStatus(nextStatus);
      updateGamesFromSteam(configuration);
      setError('');
    } catch (reason) {
      logger.error('Failed to refresh Shortcut management', reason);
      setError(reason instanceof Error ? reason.message : String(reason));
    }
  }, [updateGamesFromSteam]);

  useEffect(() => {
    void reloadBackendAndLibrary();
  }, [reloadBackendAndLibrary]);

  const defaultMode = status ? (availableModes(status)[0] ?? 'proton') : 'proton';
  const methodOptions = (['proton', 'runtime'] as const).map((mode) => ({
    data: mode,
    label: `${modeLabel(mode)}${status && isModeAvailable(status, mode) ? '' : ' · setup required'}`
  }));

  const toggle = async (game: Game, enabled: boolean) => {
    if (!status || busy !== undefined) return;
    setBusy(game.appId);
    setError('');
    try {
      const mode = selections[game.appId] ?? defaultMode;
      if (enabled && !isModeAvailable(status, mode)) {
        throw new Error(`${modeLabel(mode)} setup is required before enabling this shortcut.`);
      }
      if (enabled) {
        await enableManagedGame(game, mode, mode === 'runtime' ? (params[game.appId] ?? []) : []);
      } else {
        await disableManagedGame(game);
        setSelections((current) => {
          const next = { ...current };
          delete next[game.appId];
          return next;
        });
        setParams((current) => {
          const next = { ...current };
          delete next[game.appId];
          return next;
        });
      }
      await reloadBackendAndLibrary();
    } catch (reason) {
      logger.error(`Failed to ${enabled ? 'enable' : 'disable'} ${game.name}`, reason);
      setError(shortcutActionError(game, enabled, reason));
    } finally {
      setBusy(undefined);
    }
  };

  const changeMode = (game: Game, mode: LinUwUxMode) => {
    if (!status || game.enabled || busy !== undefined) return;
    if (!isModeAvailable(status, mode)) {
      setError(`${modeLabel(mode)} setup is required before it can be selected.`);
      return;
    }
    setError('');
    setSelections((current) => ({ ...current, [game.appId]: mode }));
  };

  const changeParam = (appId: string, param: LinUwUxParam, enabled: boolean) => {
    setParams((current) => {
      const selectedParams = current[appId] ?? [];
      return {
        ...current,
        [appId]: enabled ? [...selectedParams, param] : selectedParams.filter((name) => name !== param)
      };
    });
  };

  const sections = shortcuts(games);

  const renderManagedRow = (game: Game) => {
    if (!status) return null;
    const mode = effectiveGameMode(configuration, game.appId);
    const savedParams = configuration.games[game.appId]?.params ?? [];
    const description = shortcutDescription(game, displayState(game.appId), busy === game.appId);

    return (
      <Field key={game.appId} className='hv-shortcut-item' childrenLayout='below' childrenContainerWidth='max'>
        <div className='hv-shortcut-header'>
          <div className='hv-shortcut-name'>
            {game.name}
            <div className='hv-shortcut-summary'>
              {modeLabel(mode)}
              {mode === 'runtime' &&
                savedParams.length > 0 &&
                `: ${savedParams.map((param) => `${param}=1`).join(', ')}`}
            </div>
            {description && <div className='hv-shortcut-summary'>{description}</div>}
            {!isModeAvailable(status, mode) && (
              <div className='hv-shortcut-summary'>Selected method requires setup.</div>
            )}
          </div>
          <div className='hv-shortcut-actions'>
            <div className='hv-shortcut-toggle'>
              <Toggle value={true} disabled={busy !== undefined} onChange={(enabled) => void toggle(game, enabled)} />
            </div>
          </div>
        </div>
      </Field>
    );
  };

  const renderAvailableRow = (game: Game) => {
    if (!status) return null;
    const selected = selections[game.appId] ?? defaultMode;
    const selectedParams = params[game.appId] ?? [];
    const available = isModeAvailable(status, selected);
    const description = shortcutDescription(game, displayState(game.appId), busy === game.appId);
    return (
      <Field
        key={game.appId}
        className='hv-shortcut-item'
        childrenLayout='below'
        childrenContainerWidth='max'
        focusable={false}
      >
        <Focusable noFocusRing flow-children='column'>
          <div className='hv-shortcut-header'>
            <div className='hv-shortcut-name'>
              {game.name}
              {description && <div className='hv-shortcut-summary'>{description}</div>}
              {!available && <div className='hv-shortcut-summary'>Selected method requires setup.</div>}
            </div>
            <Focusable
              className='hv-shortcut-actions'
              noFocusRing
              flow-children='row'
              navEntryPreferPosition={NavEntryPositionPreferences.MAINTAIN_X}
            >
              <Dropdown
                menuLabel='LinUwUx method'
                rgOptions={methodOptions}
                selectedOption={selected}
                disabled={busy !== undefined || game.missing}
                onChange={(option) => changeMode(game, String(option.data) as LinUwUxMode)}
              />
              <div className='hv-shortcut-toggle'>
                <Toggle
                  value={false}
                  disabled={busy !== undefined || !available}
                  onChange={(enabled) => void toggle(game, enabled)}
                />
              </div>
            </Focusable>
          </div>
          {selected === 'runtime' && (
            <>
              <DialogLabel className='hv-shortcut-guidance'>
                Optional runtime variables. Only enable these if you understand what they do or your game’s
                compatibility instructions require them. Leave them off otherwise.
              </DialogLabel>
              <Focusable
                className='hv-shortcut-params'
                noFocusRing
                flow-children='row'
                navEntryPreferPosition={NavEntryPositionPreferences.MAINTAIN_X}
              >
                {LINUWUX_PARAMS.map((param) => (
                  <div key={param.name} className='hv-shortcut-param'>
                    <span title={param.tooltip}>{param.name}</span>
                    <Toggle
                      value={selectedParams.includes(param.name)}
                      disabled={busy !== undefined || game.missing}
                      onChange={(enabled) => changeParam(game.appId, param.name, enabled)}
                    />
                  </div>
                ))}
              </Focusable>
            </>
          )}
        </Focusable>
      </Field>
    );
  };

  return (
    <>
      <style>{shortcutManagementStyles}</style>
      <DialogBody className='hv-shortcut-page'>
        <DialogHeader className='hv-shortcut-page-title'>Shortcut management</DialogHeader>
        {error && (
          <Field label='Shortcut management needs attention' description={error} className='hv-shortcut-error' />
        )}
        {!status ? (
          !error && <LoadingSpinner />
        ) : (
          <>
            {status.status !== 'hypervisor-ready' && status.status !== 'native-ready' && (
              <Field
                label='System readiness'
                description='Readiness affects game launch, not shortcut configuration.'
              />
            )}
            <DialogControlsSection>
              <DialogControlsSectionHeader className='hv-shortcut-section-title'>
                Managed shortcuts
              </DialogControlsSectionHeader>
              {sections.managed.length > 0 ? (
                sections.managed.map(renderManagedRow)
              ) : (
                <DialogLabel className='hv-shortcut-empty'>No managed shortcuts.</DialogLabel>
              )}
            </DialogControlsSection>

            <DialogControlsSection>
              <DialogControlsSectionHeader className='hv-shortcut-section-title'>
                Available shortcuts
              </DialogControlsSectionHeader>
              {sections.available.length > 0 ? (
                sections.available.map(renderAvailableRow)
              ) : (
                <DialogLabel>No available installed shortcuts.</DialogLabel>
              )}
            </DialogControlsSection>
          </>
        )}
      </DialogBody>
    </>
  );
}
