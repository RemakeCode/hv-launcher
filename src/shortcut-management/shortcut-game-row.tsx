import { DialogLabel, Dropdown, Field, Toggle } from '@decky/ui';
import { availableModes, effectiveGameMode, isModeAvailable, shortcutDescription } from '@/shortcut-management/management';
import type { Configuration, DisplayState, Game, LinUwUxMode, SystemStatus } from '@/types';

interface ShortcutGameRowProps {
  game: Game;
  state?: DisplayState;
  busy: boolean;
  configuration: Configuration;
  selections: Record<string, LinUwUxMode>;
  status: SystemStatus;
  onToggle(game: Game, enabled: boolean): void;
  onChangeMode(game: Game, mode: LinUwUxMode): void;
}

const shortcutGameRowStyles = `
  .hv-shortcut-row {
    margin-bottom: 8px;
  }

  .hv-shortcut-actions {
    align-items: center;
    display: flex;
    flex-shrink: 0;
    gap: 8px;
  }

  .hv-shortcut-toggle {
    flex: 0 0 auto;
  }

  .hv-shortcut-warning {
    color: #ffb4a9;
    margin-block: 4px 8px;
  }
`;

function modeLabel(mode: LinUwUxMode): string {
  return mode === 'runtime' ? 'LinUwUx runtime' : 'LinUwUx Proton';
}

export function ShortcutGameRow({
  game,
  state,
  busy,
  configuration,
  selections,
  status,
  onToggle,
  onChangeMode
}: ShortcutGameRowProps) {
  const selected = selections[game.appId] ?? (game.enabled
    ? effectiveGameMode(configuration, game.appId)
    : availableModes(status)[0] ?? 'proton');
  const modes = availableModes(status);
  const options = modes.includes(selected) ? modes : [selected, ...modes];
  const selectedAvailable = isModeAvailable(status, selected);

  return (
    <>
      <style>{shortcutGameRowStyles}</style>
      <div className='hv-shortcut-row'>
        <Field
          label={game.name}
          description={shortcutDescription(game, state ?? 'idle', busy)}
          childrenLayout='inline'
          childrenContainerWidth='min'
          verticalAlignment='center'
        >
          <div className='hv-shortcut-actions'>
            {options.length > 1 ? (
              <Dropdown
                menuLabel='LinUwUx method'
                rgOptions={options.map((mode) => ({
                  data: mode,
                  label: `${modeLabel(mode)}${isModeAvailable(status, mode) ? '' : ' · setup required'}`
                }))}
                selectedOption={selected}
                disabled={busy}
                onChange={(option) => onChangeMode(game, String(option.data) as LinUwUxMode)}
              />
            ) : (
              <DialogLabel>{modeLabel(selected)}</DialogLabel>
            )}
            <div className='hv-shortcut-toggle'>
              <Toggle
                value={game.enabled}
                disabled={busy || !game.enabled && modes.length === 0}
                onChange={(enabled) => onToggle(game, enabled)}
              />
            </div>
          </div>
        </Field>
        {!selectedAvailable && game.enabled && (
          <DialogLabel className='hv-shortcut-warning'>
            This method is unavailable. Complete setup or select another available method.
          </DialogLabel>
        )}
      </div>
    </>
  );
}
