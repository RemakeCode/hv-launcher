import { BASE_URL, getActiveSetupJob, getSetupJob } from "./api";
import { logger } from "./shared/logger";
import type { SetupJobEvent, SetupJobSnapshot } from "./types";

type SetupListener = (job: SetupJobSnapshot) => void;
type TerminalListener = (job: SetupJobSnapshot) => void;

interface EventStream {
  close(): void;
  addEventListener(type: string, listener: (event: MessageEvent<string>) => void): void;
  onopen: ((event: Event) => void) | null;
  onerror: ((event: Event) => void) | null;
}

type EventStreamFactory = (url: string) => EventStream;

export class SetupEventStore {
  private source?: EventStream;
  private listeners = new Set<SetupListener>();
  private terminalListener?: TerminalListener;
  private latestByKind = new Map<string, SetupJobSnapshot>();
  private notified = new Set<string>();

  constructor(
    private readonly createStream: EventStreamFactory = (url) => new EventSource(url),
  ) {}

  start(onTerminal?: TerminalListener) {
    if (this.source) return;
    this.terminalListener = onTerminal;
    const source = this.createStream(`${BASE_URL}/setup/events`);
    this.source = source;
    source.addEventListener("setup-job", (event) => {
      try {
        const update = JSON.parse(event.data) as SetupJobEvent;
        if (update.type === "setup-job") this.accept(update.job);
      } catch (reason) {
        logger.error("Failed to read a setup event", reason);
      }
    });
    source.onopen = () => void this.reconcile();
    source.onerror = () => logger.warn("Setup event stream disconnected; Decky will reconnect it");
    void this.reconcile();
  }

  stop() {
    this.source?.close();
    this.source = undefined;
    this.terminalListener = undefined;
    this.listeners.clear();
    this.latestByKind.clear();
    this.notified.clear();
  }

  subscribe(listener: SetupListener): () => void {
    this.listeners.add(listener);
    for (const snapshot of this.latestByKind.values()) listener(snapshot);
    return () => this.listeners.delete(listener);
  }

  current(kind: string): SetupJobSnapshot | undefined {
    return this.latestByKind.get(kind);
  }

  dismiss(kind: string) {
    this.latestByKind.delete(kind);
  }

  async reconcile() {
    try {
      const active = await getActiveSetupJob();
      if (!active.job) return;
      const snapshot = await getSetupJob(active.job.id);
      this.accept(snapshot);
    } catch (reason) {
      logger.error("Failed to reconcile setup job state", reason);
    }
  }

  private accept(snapshot: SetupJobSnapshot) {
    this.latestByKind.set(snapshot.kind, snapshot);
    for (const listener of this.listeners) listener(snapshot);
    if (snapshot.state === "running") return;
    const terminalKey = `${snapshot.id}:${snapshot.state}`;
    if (this.notified.has(terminalKey)) return;
    this.notified.add(terminalKey);
    this.terminalListener?.(snapshot);
  }
}

export const setupEventStore = new SetupEventStore();
