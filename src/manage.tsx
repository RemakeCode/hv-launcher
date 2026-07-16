import {
    DialogBody,
    DialogControlsSection,
    DialogControlsSectionHeader,
    DialogLabel,
    SidebarNavigation,
    Spinner,
    ToggleField
} from '@decky/ui';
import { useCallback, useEffect, useRef, useState } from 'react';
import { getConfiguration, getStatus } from './api';
import { canChangeShortcut, groupShortcuts, shortcutActionError, shortcutDescription } from './management';
import { logger } from './shared/logger';
import {
    disableManagedGame,
    discoverGames,
    displayState,
    enableManagedGame,
    observeSteamOverviews,
    SteamLibraryLoadingError
} from './steam';
import type { Configuration, DisplayState, Game, SystemStatus } from './types';
import { GiGamepad } from 'react-icons/gi';

const EMPTY_CONFIGURATION: Configuration = { version: 1, games: {} };

export function ShortcutManagementPage() {
    const [status, setStatus] = useState<SystemStatus>();
    const [games, setGames] = useState<Game[]>([]);
    const [states, setStates] = useState<Record<string, DisplayState>>({});
    const [busy, setBusy] = useState<string>();
    const [error, setError] = useState('');
    const [libraryMessage, setLibraryMessage] = useState('');
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
            let conflict: string | undefined;
            if (enabled) await enableManagedGame(game);
            else conflict = await disableManagedGame(game);
            await refresh();
            if (conflict) setError(`${game.name}: ${conflict}`);
        } catch (reason) {
            logger.error(`Failed to ${enabled ? 'enable' : 'restore'} ${game.name}`, reason);
            setError(shortcutActionError(game, enabled, reason));
        } finally {
            setBusy(undefined);
        }
    };

    const sections = groupShortcuts(games);
    const row = (game: Game) => (
        <ToggleField
            key={game.appId}
            label={game.name}
            description={shortcutDescription(game, states[game.appId] ?? 'idle', busy === game.appId)}
            checked={game.enabled}
            disabled={busy !== undefined || !status || !canChangeShortcut(status.status, game)}
            onChange={(enabled) => void toggle(game, enabled)}
        />
    );

    return (
        <SidebarNavigation
            pages={[
                {
                    title: 'ShortCut Management',
                    icon: <GiGamepad />,
                    content: (
                        <DialogBody>
                            {!status && !error && <Spinner />}
                            {status && status.status !== 'hypervisor-ready' && (
                                <DialogLabel>
                                    New shortcuts cannot be enabled in the current host state. Managed shortcuts can
                                    still be restored.
                                </DialogLabel>
                            )}
                            {libraryMessage && <DialogLabel style={{ marginBottom: 16 }}>{libraryMessage}</DialogLabel>}

                            <DialogControlsSection>
                                {error && (
                                    <DialogLabel style={{ color: '#ffb4a9', marginBlock: '8px' }}>{error}</DialogLabel>
                                )}
                                <DialogControlsSectionHeader>Managed shortcuts</DialogControlsSectionHeader>
                                {sections.managed.length > 0 ? (
                                    sections.managed.map(row)
                                ) : (
                                    <DialogLabel style={{ marginBlockStart: '8px' }}>No managed shortcuts.</DialogLabel>
                                )}
                            </DialogControlsSection>

                            <DialogControlsSection>
                                <DialogControlsSectionHeader>Available shortcuts</DialogControlsSectionHeader>
                                {sections.available.length > 0 ? (
                                    sections.available.map(row)
                                ) : (
                                    <DialogLabel>No available installed shortcuts.</DialogLabel>
                                )}
                            </DialogControlsSection>
                        </DialogBody>
                    )
                }
            ]}
        ></SidebarNavigation>
    );
}
