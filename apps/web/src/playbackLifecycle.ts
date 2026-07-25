import type { CreateViewEventRequest, FeedVideo, ViewEventType } from "./types";

const DEFAULT_PROGRESS_INTERVAL_MS = 10_000;
const DEFAULT_COMPLETION_RATIO = 0.95;
const DEFAULT_COMPLETION_REMAINING_MS = 2_000;

interface PlaybackLifecycleOptions {
  progressIntervalMs?: number;
  completionRatio?: number;
  completionRemainingMs?: number;
  createID?: (prefix: string) => string;
  occurredAt?: () => string;
}

export class PlaybackLifecycle {
  private readonly videoID: number;
  private readonly scene: string;
  private readonly requestID: string;
  private readonly playbackSessionID: string;
  private readonly progressIntervalMs: number;
  private readonly completionRatio: number;
  private readonly completionRemainingMs: number;
  private readonly createID: (prefix: string) => string;
  private readonly occurredAt: () => string;
  private sequence = 0;
  private exposed = false;
  private playReported = false;
  private completed = false;
  private terminal = false;
  private mediaPlaying = false;
  private visible = true;
  private watchStartedAt = 0;
  private watchMs = 0;
  private positionMs = 0;
  private durationMs = 0;
  private lastReportedWatchMs = 0;
  private lastReportedPositionMs = 0;

  constructor(item: FeedVideo, options: PlaybackLifecycleOptions = {}) {
    this.videoID = item.video_id;
    this.scene = item.feed_scene;
    this.requestID = item.request_id;
    this.progressIntervalMs = positiveNumber(options.progressIntervalMs, DEFAULT_PROGRESS_INTERVAL_MS);
    this.completionRatio = boundedRatio(options.completionRatio, DEFAULT_COMPLETION_RATIO);
    this.completionRemainingMs = positiveNumber(options.completionRemainingMs, DEFAULT_COMPLETION_REMAINING_MS);
    this.createID = options.createID || createPlaybackEventID;
    this.occurredAt = options.occurredAt || (() => new Date().toISOString());
    this.playbackSessionID = this.createID("playback");
  }

  activate(now: number): CreateViewEventRequest[] {
    if (this.terminal || this.exposed) return [];
    this.updateWatch(now);
    this.exposed = true;
    return [this.createEvent("exposed")];
  }

  playing(now: number, positionSeconds: number, durationSeconds: number): CreateViewEventRequest[] {
    if (this.terminal) return [];
    const events = this.ensureExposed(now);
    this.updatePosition(positionSeconds, durationSeconds);
    this.mediaPlaying = true;
    this.startWatch(now);
    if (!this.playReported) {
      this.playReported = true;
      events.push(this.createEvent("play"));
    }
    return events;
  }

  timeUpdate(now: number, positionSeconds: number, durationSeconds: number): CreateViewEventRequest[] {
    if (this.terminal || !this.exposed) return [];
    this.updateWatch(now);
    this.updatePosition(positionSeconds, durationSeconds);
    const completed = this.completeIfNeeded();
    if (completed) return [completed];
    if (this.completed || !this.shouldReportProgress()) return [];
    return [this.createEvent("progress")];
  }

  pause(now: number, positionSeconds: number, durationSeconds: number): CreateViewEventRequest[] {
    if (this.terminal || !this.exposed) return [];
    this.updateWatch(now);
    this.updatePosition(positionSeconds, durationSeconds);
    this.mediaPlaying = false;
    this.watchStartedAt = 0;
    const completed = this.completeIfNeeded();
    if (completed) return [completed];
    return this.flushProgress();
  }

  waiting(now: number, positionSeconds: number, durationSeconds: number): void {
    if (this.terminal) return;
    this.updateWatch(now);
    this.updatePosition(positionSeconds, durationSeconds);
    this.mediaPlaying = false;
    this.watchStartedAt = 0;
  }

  seeking(now: number, positionSeconds: number, durationSeconds: number): void {
    if (this.terminal) return;
    this.updateWatch(now);
    this.updatePosition(positionSeconds, durationSeconds);
    this.watchStartedAt = 0;
  }

  seeked(
    now: number,
    positionSeconds: number,
    durationSeconds: number,
    resumePlaying = this.mediaPlaying
  ): CreateViewEventRequest[] {
    if (this.terminal || !this.exposed) return [];
    this.updateWatch(now);
    this.updatePosition(positionSeconds, durationSeconds);
    this.mediaPlaying = resumePlaying;
    if (this.mediaPlaying) {
      this.startWatch(now);
    }
    const completed = this.completeIfNeeded();
    if (completed) return [completed];
    return this.flushProgress();
  }

  flush(now: number, positionSeconds: number, durationSeconds: number): CreateViewEventRequest[] {
    if (this.terminal || !this.exposed) return [];
    this.updateWatch(now);
    this.updatePosition(positionSeconds, durationSeconds);
    const completed = this.completeIfNeeded();
    if (completed) return [completed];
    return this.flushProgress();
  }

  setVisibility(now: number, visible: boolean, positionSeconds: number, durationSeconds: number): CreateViewEventRequest[] {
    if (this.terminal || this.visible === visible) return [];
    this.updateWatch(now);
    this.updatePosition(positionSeconds, durationSeconds);
    this.visible = visible;
    if (visible) {
      this.startWatch(now);
      return [];
    }
    this.watchStartedAt = 0;
    return this.flushProgress();
  }

  finish(now: number, positionSeconds?: number, durationSeconds?: number): CreateViewEventRequest[] {
    if (this.terminal || !this.exposed) return [];
    this.updateWatch(now);
    if (positionSeconds !== undefined || durationSeconds !== undefined) {
      this.updatePosition(positionSeconds || 0, durationSeconds || 0);
    }
    this.mediaPlaying = false;
    this.watchStartedAt = 0;
    this.terminal = true;
    const completed = this.completeIfNeeded();
    if (completed || this.completed) return completed ? [completed] : [];
    return [this.createEvent("skip")];
  }

  private ensureExposed(now: number): CreateViewEventRequest[] {
    return this.exposed ? [] : this.activate(now);
  }

  private startWatch(now: number): void {
    if (!this.mediaPlaying || !this.visible || this.watchStartedAt > 0) return;
    this.watchStartedAt = finiteNow(now);
  }

  private updateWatch(now: number): void {
    if (this.watchStartedAt <= 0) return;
    const current = finiteNow(now);
    this.watchMs += Math.max(0, current - this.watchStartedAt);
    this.watchStartedAt = this.mediaPlaying && this.visible ? current : 0;
  }

  private updatePosition(positionSeconds: number, durationSeconds: number): void {
    this.positionMs = secondsToMs(positionSeconds);
    this.durationMs = secondsToMs(durationSeconds);
  }

  private completeIfNeeded(): CreateViewEventRequest | null {
    if (this.completed || this.durationMs <= 0) return null;
    const remainingMs = Math.max(0, this.durationMs - this.positionMs);
    const ratio = this.positionMs / this.durationMs;
    if (ratio < this.completionRatio || remainingMs > this.completionRemainingMs) {
      return null;
    }
    this.completed = true;
    this.terminal = true;
    return this.createEvent("complete");
  }

  private shouldReportProgress(): boolean {
    return (
      this.watchMs - this.lastReportedWatchMs >= this.progressIntervalMs ||
      Math.abs(this.positionMs - this.lastReportedPositionMs) >= this.progressIntervalMs
    );
  }

  private flushProgress(): CreateViewEventRequest[] {
    if (this.completed) return [];
    const watchAdvanced = this.watchMs > this.lastReportedWatchMs;
    const positionAdvanced = Math.abs(this.positionMs - this.lastReportedPositionMs) >= 1_000;
    return watchAdvanced || positionAdvanced ? [this.createEvent("progress")] : [];
  }

  private createEvent(eventType: ViewEventType): CreateViewEventRequest {
    this.sequence += 1;
    const event: CreateViewEventRequest = {
      video_id: this.videoID,
      scene: this.scene,
      request_id: this.requestID,
      event_type: eventType,
      watch_ms: Math.max(0, Math.round(this.watchMs)),
      completed: eventType === "complete",
      event_id: this.createID("event"),
      playback_session_id: this.playbackSessionID,
      sequence: this.sequence,
      occurred_at: this.occurredAt(),
      position_ms: this.positionMs
    };
    if (this.durationMs > 0) {
      event.duration_ms = this.durationMs;
    }
    this.lastReportedWatchMs = this.watchMs;
    this.lastReportedPositionMs = this.positionMs;
    return event;
  }
}

export function createPlaybackEventID(prefix: string): string {
  const randomID =
    typeof globalThis.crypto?.randomUUID === "function"
      ? globalThis.crypto.randomUUID()
      : `${Date.now()}-${Math.random().toString(36).slice(2, 12)}`;
  return `${prefix}-${randomID}`;
}

function secondsToMs(value: number): number {
  if (!Number.isFinite(value) || value <= 0) return 0;
  return Math.max(0, Math.round(value * 1_000));
}

function finiteNow(value: number): number {
  return Number.isFinite(value) && value >= 0 ? value : 0;
}

function positiveNumber(value: number | undefined, fallback: number): number {
  return Number.isFinite(value) && Number(value) > 0 ? Number(value) : fallback;
}

function boundedRatio(value: number | undefined, fallback: number): number {
  return Number.isFinite(value) && Number(value) > 0 && Number(value) <= 1 ? Number(value) : fallback;
}
