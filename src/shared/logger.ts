const BADGE = '%cHV Launcher%c';
const BADGE_STYLE =
  'background:#9d00ff;color:#ffffff;padding:1px 5px;border-radius:3px;font-weight:bold;margin-right:4px;';
const RESET_STYLE = '';

type LogMethod = (...args: unknown[]) => void;

export interface Logger {
  log: LogMethod;
  info: LogMethod;
  warn: LogMethod;
  error: LogMethod;
  debug: LogMethod;
}

function makeLogMethod(consoleMethod: LogMethod): LogMethod {
  return (...args: unknown[]) => consoleMethod(BADGE, BADGE_STYLE, RESET_STYLE, ...args);
}

export const logger: Logger = {
  log: makeLogMethod(console.log),
  info: makeLogMethod(console.info),
  warn: makeLogMethod(console.warn),
  error: makeLogMethod(console.error),
  debug: makeLogMethod(console.debug),
};
