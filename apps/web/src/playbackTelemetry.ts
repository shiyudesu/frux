import type { FeedPreloadMediaResource } from "./feedPreload";
import type {
  FeedVideo,
  PlaybackSource,
  PlaybackTelemetryBatch,
  PlaybackTelemetryBrowserFamily,
  PlaybackTelemetryCodecFamily,
  PlaybackTelemetryContext,
  PlaybackTelemetryErrorCategory,
  PlaybackTelemetryEvent,
  PlaybackTelemetryEventType,
  PlaybackTelemetryMeasurementMethod,
  PlaybackTelemetryNetworkClass,
  PlaybackTelemetryOSFamily,
  PlaybackTelemetryPlayerAdapter,
  PlaybackTelemetryRecoveryOutcome,
  PlaybackTelemetrySourceType,
  PlaybackTelemetryViewportClass
} from "./types";

export const PLAYBACK_TELEMETRY_MAX_EVENTS = 50;
export const PLAYBACK_TELEMETRY_MAX_PAYLOAD_BYTES = 64 * 1024;

const MAX_FRAME_COUNT = 2_147_483_647;
const MAX_SESSION_MS = 24 * 60 * 60 * 1_000;
const MAX_FIRST_FRAME_MS = 10 * 60 * 1_000;
const DEFAULT_FLUSH_INTERVAL_MS = 10_000;
const DEFAULT_MAX_RETRIES = 2;
const MAX_QUEUED_BATCHES = 4;

export interface PlaybackTelemetrySourceContext {
  sourceType: PlaybackTelemetrySourceType;
  renditionLabel: string;
  codecFamily: PlaybackTelemetryCodecFamily;
  cdnHost: string;
  playerAdapter: PlaybackTelemetryPlayerAdapter;
}

type PlainEventType = Exclude<
  PlaybackTelemetryEventType,
  | "first_rendered_frame"
  | "play_success"
  | "play_failure"
  | "rebuffer_end"
  | "seek_end"
  | "source_change"
  | "quality_change"
  | "end"
  | "terminal_error"
>;

export type PlaybackTelemetryEventDetails =
  | { eventType: PlainEventType }
  | {
      eventType: "first_rendered_frame";
      firstFrameMs: number;
      measurementMethod: PlaybackTelemetryMeasurementMethod;
      startupRetryCount: number;
    }
  | { eventType: "play_success"; startupRetryCount: number }
  | { eventType: "play_failure"; startupRetryCount: number; errorCategory: PlaybackTelemetryErrorCategory }
  | {
      eventType: "rebuffer_end";
      intervalDurationMs: number;
      recoveryOutcome: PlaybackTelemetryRecoveryOutcome;
    }
  | { eventType: "seek_end"; intervalDurationMs: number }
  | {
      eventType: "source_change";
      sourceType: PlaybackTelemetrySourceType;
      renditionLabel: string;
      codecFamily: PlaybackTelemetryCodecFamily;
      cdnHost: string;
    }
  | { eventType: "quality_change"; renditionLabel: string }
  | {
      eventType: "end";
      droppedFrames?: number;
      totalFrames?: number;
      rebufferCount: number;
      rebufferDurationMs: number;
      maxRebufferDurationMs: number;
    }
  | {
      eventType: "terminal_error";
      errorCategory: PlaybackTelemetryErrorCategory;
      droppedFrames?: number;
      totalFrames?: number;
      rebufferCount: number;
      rebufferDurationMs: number;
      maxRebufferDurationMs: number;
    };

interface EventBase {
  eventID: string;
  offsetMs: number;
  mediaPositionMs: number;
  mediaDurationMs?: number;
}

export function buildPlaybackTelemetryEvent(
  base: EventBase,
  details: PlaybackTelemetryEventDetails
): PlaybackTelemetryEvent {
  const event: PlaybackTelemetryEvent = {
    event_id: base.eventID,
    event_type: details.eventType,
    offset_ms: boundedInteger(base.offsetMs, 0, MAX_SESSION_MS),
    media_position_ms: boundedInteger(base.mediaPositionMs, 0, MAX_SESSION_MS)
  };
  if (base.mediaDurationMs !== undefined) {
    event.media_duration_ms = boundedInteger(base.mediaDurationMs, 1, MAX_SESSION_MS);
  }

  switch (details.eventType) {
    case "first_rendered_frame":
      event.first_frame_ms = boundedInteger(details.firstFrameMs, 0, MAX_FIRST_FRAME_MS);
      event.measurement_method = details.measurementMethod;
      event.startup_retry_count = boundedInteger(details.startupRetryCount, 0, 20);
      break;
    case "play_success":
      event.startup_retry_count = boundedInteger(details.startupRetryCount, 0, 20);
      break;
    case "play_failure":
      event.startup_retry_count = boundedInteger(details.startupRetryCount, 0, 20);
      event.error_category = details.errorCategory;
      break;
    case "rebuffer_end":
      event.interval_duration_ms = boundedInteger(details.intervalDurationMs, 0, MAX_SESSION_MS);
      event.recovery_outcome = details.recoveryOutcome;
      break;
    case "seek_end":
      event.interval_duration_ms = boundedInteger(details.intervalDurationMs, 0, MAX_SESSION_MS);
      break;
    case "source_change":
      event.source_type = details.sourceType;
      event.rendition_label = normalizeLabel(details.renditionLabel);
      event.codec_family = details.codecFamily;
      event.cdn_host = normalizeHost(details.cdnHost);
      break;
    case "quality_change":
      event.rendition_label = normalizeLabel(details.renditionLabel);
      break;
    case "end":
      addFrameTotals(event, details.droppedFrames, details.totalFrames);
      addRebufferSummary(event, details);
      break;
    case "terminal_error":
      event.error_category = details.errorCategory;
      addFrameTotals(event, details.droppedFrames, details.totalFrames);
      addRebufferSummary(event, details);
      break;
    default:
      break;
  }
  return event;
}

interface PlaybackTelemetrySessionOptions {
  send: (batch: PlaybackTelemetryBatch, keepalive: boolean) => Promise<void>;
  now?: () => number;
  wallClock?: () => Date;
  createID?: (prefix: string) => string;
  flushIntervalMs?: number;
  maxRetries?: number;
  setInterval?: (callback: () => void, delayMs: number) => number;
  clearInterval?: (timerID: number) => void;
  wait?: (delayMs: number) => Promise<void>;
}

interface QueuedBatch {
  batch: PlaybackTelemetryBatch;
  keepalive: boolean;
}

export class PlaybackTelemetrySession {
  readonly playbackSessionID: string;

  private readonly send: PlaybackTelemetrySessionOptions["send"];
  private readonly now: () => number;
  private readonly wallClock: () => Date;
  private readonly createID: (prefix: string) => string;
  private readonly maxRetries: number;
  private readonly clearIntervalFn: (timerID: number) => void;
  private readonly wait: (delayMs: number) => Promise<void>;
  private readonly sessionStartedAt: number;
  private context: PlaybackTelemetryContext;
  private events: PlaybackTelemetryEvent[] = [];
  private readonly queuedBatches: QueuedBatch[] = [];
  private deliveryPromise: Promise<void> | null = null;
  private inflightBatch: PlaybackTelemetryBatch | null = null;
  private inflightKeepalive = false;
  private inflightKeepaliveResent = false;
  private flushTimerID: number | null = null;
  private lastOffsetMs = 0;
  private media: FeedPreloadMediaResource | null = null;
  private frameCallbackID: number | null = null;
  private loadStartedAt: number | null = null;
  private loadStartMediaTime = 0;
  private metadataReported = false;
  private firstFrameReported = false;
  private playAttempts = 0;
  private playSucceeded = false;
  private playbackStarted = false;
  private expectedPlayback = false;
  private seekingStartedAt: number | null = null;
  private rebufferStartedAt: number | null = null;
  private rebufferCount = 0;
  private rebufferDurationMs = 0;
  private maxRebufferDurationMs = 0;
  private terminal = false;
  private disposed = false;

  constructor(context: PlaybackTelemetryContext, options: PlaybackTelemetrySessionOptions) {
    this.context = { ...context };
    this.send = options.send;
    this.now = options.now || (() => performance.now());
    this.wallClock = options.wallClock || (() => new Date());
    this.createID = options.createID || createTelemetryID;
    this.maxRetries = boundedInteger(options.maxRetries ?? DEFAULT_MAX_RETRIES, 0, 5);
    this.clearIntervalFn =
      options.clearInterval ||
      ((timerID) => {
        if (typeof window !== "undefined") window.clearInterval(timerID);
      });
    this.wait =
      options.wait ||
      ((delayMs) =>
        new Promise((resolve) => {
          window.setTimeout(resolve, delayMs);
        }));
    this.sessionStartedAt = finiteNow(this.now());
    this.playbackSessionID = this.createID("playback");
    const setIntervalFn =
      options.setInterval ||
      ((callback: () => void, delayMs: number) =>
        typeof window === "undefined" ? 0 : window.setInterval(callback, delayMs));
    const intervalMs = boundedInteger(options.flushIntervalMs ?? DEFAULT_FLUSH_INTERVAL_MS, 1_000, 60_000);
    this.flushTimerID = setIntervalFn(() => {
      void this.flush(false);
    }, intervalMs);
  }

  sourceLoadStarted(media: FeedPreloadMediaResource, source?: PlaybackTelemetrySourceContext): void {
    if (this.disposed || this.terminal) return;
    if (this.media === media && this.loadStartedAt !== null) return;
    this.cancelFrameCallback();
    this.media = media;
    this.loadStartedAt = finiteNow(this.now());
    this.loadStartMediaTime = finiteSeconds(media.currentTime);
    this.metadataReported = false;
    this.firstFrameReported = false;
    if (source) this.applySourceContext(source);
    this.record({ eventType: "load_start" });
    const callbackID = media.requestVideoFrame?.(() => {
      this.frameCallbackID = null;
      this.recordFirstFrame("video_frame_callback");
    });
    this.frameCallbackID = callbackID === undefined ? null : callbackID;
  }

  metadataReady(): void {
    if (this.metadataReported || this.terminal) return;
    this.metadataReported = true;
    this.record({ eventType: "metadata_ready" });
  }

  playAttempted(): void {
    if (this.terminal) return;
    this.playAttempts = Math.min(21, this.playAttempts + 1);
  }

  playSucceededEvent(): void {
    if (this.terminal || this.playSucceeded) return;
    if (this.playAttempts === 0) this.playAttempts = 1;
    this.playSucceeded = true;
    this.record({ eventType: "play_success", startupRetryCount: this.startupRetryCount() });
  }

  playFailed(errorCategory: PlaybackTelemetryErrorCategory): void {
    if (this.terminal) return;
    if (this.playAttempts === 0) this.playAttempts = 1;
    this.record({
      eventType: "play_failure",
      errorCategory,
      startupRetryCount: this.startupRetryCount()
    });
  }

  playing(): void {
    if (this.terminal) return;
    this.playSucceededEvent();
    this.recordFallbackFirstFrame();
    if (this.rebufferStartedAt !== null) {
      this.closeRebuffer("resumed");
    }
    this.playbackStarted = true;
    this.expectedPlayback = true;
  }

  waiting(): void {
    if (
      this.terminal ||
      !this.playbackStarted ||
      !this.expectedPlayback ||
      this.seekingStartedAt !== null ||
      this.rebufferStartedAt !== null
    ) {
      return;
    }
    this.rebufferStartedAt = finiteNow(this.now());
    this.record({ eventType: "rebuffer_start" });
  }

  timeUpdated(): void {
    if (this.terminal || this.firstFrameReported || this.loadStartedAt === null || !this.media) return;
    if (this.media.readyState < 2 || finiteSeconds(this.media.currentTime) <= this.loadStartMediaTime + 0.001) return;
    this.recordFirstFrame("advancing_time");
  }

  pause(): void {
    if (this.terminal) return;
    if (this.rebufferStartedAt !== null) {
      this.closeRebuffer("paused");
    }
    const shouldReport = this.playbackStarted && this.expectedPlayback && !this.media?.ended;
    this.expectedPlayback = false;
    if (shouldReport) this.record({ eventType: "pause" });
  }

  seeking(): void {
    if (this.terminal || this.seekingStartedAt !== null) return;
    if (this.rebufferStartedAt !== null) {
      this.closeRebuffer("seeked");
    }
    this.seekingStartedAt = finiteNow(this.now());
    this.record({ eventType: "seek_start" });
  }

  seeked(): void {
    if (this.terminal || this.seekingStartedAt === null) return;
    const intervalDurationMs = elapsedMs(this.seekingStartedAt, this.now);
    this.seekingStartedAt = null;
    this.record({ eventType: "seek_end", intervalDurationMs });
  }

  sourceChanged(source: PlaybackTelemetrySourceContext): void {
    if (this.terminal) return;
    const changed =
      source.sourceType !== this.context.source_type ||
      source.renditionLabel !== this.context.rendition_label ||
      source.codecFamily !== this.context.codec_family ||
      source.cdnHost !== this.context.cdn_host;
    if (!changed) return;
    if (this.rebufferStartedAt !== null) {
      this.closeRebuffer("source_changed");
    }
    void this.flush(false);
    this.applySourceContext(source);
    this.record({
      eventType: "source_change",
      sourceType: source.sourceType,
      renditionLabel: source.renditionLabel,
      codecFamily: source.codecFamily,
      cdnHost: source.cdnHost
    });
  }

  qualityChanged(renditionLabel: string): void {
    const normalized = normalizeLabel(renditionLabel);
    if (this.terminal || normalized === this.context.rendition_label) return;
    void this.flush(false);
    this.context.rendition_label = normalized;
    this.record({ eventType: "quality_change", renditionLabel: normalized });
  }

  ended(): void {
    void this.finish(false);
  }

  finish(keepalive = true): Promise<void> {
    if (this.terminal) return this.flush(keepalive);
    if (this.rebufferStartedAt !== null) this.closeRebuffer("ended");
    if (this.seekingStartedAt !== null) this.seeked();
    this.terminal = true;
    this.expectedPlayback = false;
    const quality = this.readFrameQuality();
    this.record({ eventType: "end", ...quality, ...this.rebufferSummary() });
    return this.flush(keepalive);
  }

  terminalError(errorCategory: PlaybackTelemetryErrorCategory): void {
    if (this.terminal) return;
    if (this.rebufferStartedAt !== null) this.closeRebuffer("failed");
    if (this.seekingStartedAt !== null) this.seeked();
    this.terminal = true;
    this.expectedPlayback = false;
    const quality = this.readFrameQuality();
    this.record({ eventType: "terminal_error", errorCategory, ...quality, ...this.rebufferSummary() });
    void this.flush(false);
  }

  visibilityHidden(): Promise<void> {
    return this.flush(false);
  }

  pageExit(): Promise<void> {
    return this.finish(true);
  }

  flush(keepalive = false): Promise<void> {
    this.queueCurrentEvents(keepalive);
    if (
      keepalive &&
      this.deliveryPromise &&
      (this.queuedBatches.length > 1 ||
        (this.inflightBatch !== null && !this.inflightKeepalive && !this.inflightKeepaliveResent))
    ) {
      return this.deliverKeepaliveBehindInflight();
    }
    if (!this.deliveryPromise && this.queuedBatches.length > 0) {
      this.deliveryPromise = this.deliverQueued().finally(() => {
        this.deliveryPromise = null;
        if (this.queuedBatches.length > 0) void this.flush(false);
      });
    }
    return this.deliveryPromise || Promise.resolve();
  }

  dispose(): void {
    if (this.disposed) return;
    this.disposed = true;
    this.cancelFrameCallback();
    if (this.flushTimerID !== null) {
      this.clearIntervalFn(this.flushTimerID);
      this.flushTimerID = null;
    }
  }

  private record(details: PlaybackTelemetryEventDetails): void {
    if (this.disposed) return;
    const position = mediaMilliseconds(this.media?.currentTime);
    const duration = mediaMilliseconds(this.media?.duration);
    const event = buildPlaybackTelemetryEvent(
      {
        eventID: this.createID("event"),
        offsetMs: this.nextOffsetMs(),
        mediaPositionMs: duration > 0 ? Math.min(position, duration) : position,
        ...(duration > 0 ? { mediaDurationMs: duration } : {})
      },
      details
    );
    const candidate = [...this.events, event];
    if (this.events.length > 0 && this.batchBytes(candidate) > PLAYBACK_TELEMETRY_MAX_PAYLOAD_BYTES) {
      void this.flush(false);
    }
    if (this.batchBytes([...this.events, event]) <= PLAYBACK_TELEMETRY_MAX_PAYLOAD_BYTES) {
      this.events.push(event);
    }
    if (this.events.length >= PLAYBACK_TELEMETRY_MAX_EVENTS) {
      void this.flush(false);
    }
  }

  private recordFirstFrame(measurementMethod: PlaybackTelemetryMeasurementMethod): void {
    if (this.firstFrameReported || this.loadStartedAt === null || this.terminal) return;
    this.firstFrameReported = true;
    this.cancelFrameCallback();
    this.record({
      eventType: "first_rendered_frame",
      firstFrameMs: elapsedMs(this.loadStartedAt, this.now),
      measurementMethod,
      startupRetryCount: this.startupRetryCount()
    });
  }

  private recordFallbackFirstFrame(): void {
    if (this.firstFrameReported || this.loadStartedAt === null || !this.media) return;
    if (this.frameCallbackID !== null) return;
    if (this.media.readyState >= 2 && finiteSeconds(this.media.currentTime) > this.loadStartMediaTime + 0.001) {
      this.recordFirstFrame("advancing_time");
      return;
    }
    this.recordFirstFrame("playing");
  }

  private closeRebuffer(recoveryOutcome: PlaybackTelemetryRecoveryOutcome): void {
    if (this.rebufferStartedAt === null) return;
    const intervalDurationMs = elapsedMs(this.rebufferStartedAt, this.now);
    this.rebufferStartedAt = null;
    this.rebufferCount += 1;
    this.rebufferDurationMs += intervalDurationMs;
    this.maxRebufferDurationMs = Math.max(this.maxRebufferDurationMs, intervalDurationMs);
    this.record({ eventType: "rebuffer_end", intervalDurationMs, recoveryOutcome });
  }

  private rebufferSummary(): {
    rebufferCount: number;
    rebufferDurationMs: number;
    maxRebufferDurationMs: number;
  } {
    return {
      rebufferCount: this.rebufferCount,
      rebufferDurationMs: this.rebufferDurationMs,
      maxRebufferDurationMs: this.maxRebufferDurationMs
    };
  }

  private readFrameQuality(): { droppedFrames?: number; totalFrames?: number } {
    const quality = this.media?.readPlaybackQuality?.();
    if (!quality) return {};
    const totalFrames = boundedInteger(quality.totalFrames, 0, MAX_FRAME_COUNT);
    const droppedFrames = Math.min(boundedInteger(quality.droppedFrames, 0, MAX_FRAME_COUNT), totalFrames);
    return { droppedFrames, totalFrames };
  }

  private startupRetryCount(): number {
    return boundedInteger(this.playAttempts - 1, 0, 20);
  }

  private nextOffsetMs(): number {
    const measured = boundedInteger(finiteNow(this.now()) - this.sessionStartedAt, 0, MAX_SESSION_MS);
    this.lastOffsetMs = Math.max(this.lastOffsetMs, measured);
    return this.lastOffsetMs;
  }

  private applySourceContext(source: PlaybackTelemetrySourceContext): void {
    this.context.player_adapter = source.playerAdapter;
    this.context.source_type = source.sourceType;
    this.context.rendition_label = normalizeLabel(source.renditionLabel);
    this.context.codec_family = source.codecFamily;
    this.context.cdn_host = normalizeHost(source.cdnHost);
  }

  private cancelFrameCallback(): void {
    if (this.frameCallbackID !== null) {
      this.media?.cancelVideoFrameRequest?.(this.frameCallbackID);
      this.frameCallbackID = null;
    }
  }

  private queueCurrentEvents(keepalive: boolean): void {
    while (this.events.length > 0) {
      let eventCount = Math.min(this.events.length, PLAYBACK_TELEMETRY_MAX_EVENTS);
      let batch = this.createBatch(this.events.slice(0, eventCount));
      while (eventCount > 1 && byteLength(batch) > PLAYBACK_TELEMETRY_MAX_PAYLOAD_BYTES) {
        eventCount -= 1;
        batch = this.createBatch(this.events.slice(0, eventCount));
      }
      if (byteLength(batch) > PLAYBACK_TELEMETRY_MAX_PAYLOAD_BYTES) {
        this.events.shift();
        continue;
      }
      this.events.splice(0, eventCount);
      if (this.queuedBatches.length >= MAX_QUEUED_BATCHES) {
        this.queuedBatches.splice(this.deliveryPromise ? 1 : 0, 1);
      }
      this.queuedBatches.push({ batch, keepalive });
    }
    if (keepalive) {
      for (const queued of this.queuedBatches) queued.keepalive = true;
    }
  }

  private createBatch(events: PlaybackTelemetryEvent[]): PlaybackTelemetryBatch {
    return {
      schema_version: 1,
      batch_id: this.createID("batch"),
      playback_session_id: this.playbackSessionID,
      client_sent_at: this.wallClock().toISOString(),
      context: { ...this.context },
      events
    };
  }

  private batchBytes(events: PlaybackTelemetryEvent[]): number {
    return byteLength({
      schema_version: 1,
      batch_id: "batch-00000000-0000-4000-8000-000000000000",
      playback_session_id: this.playbackSessionID,
      client_sent_at: this.wallClock().toISOString(),
      context: this.context,
      events
    });
  }

  private async deliverQueued(): Promise<void> {
    while (this.queuedBatches.length > 0) {
      const queued = this.queuedBatches[0];
      const deliveryKeepalive = queued.keepalive;
      this.inflightBatch = queued.batch;
      this.inflightKeepalive = deliveryKeepalive;
      this.inflightKeepaliveResent = false;
      let delivered = false;
      for (let attempt = 0; attempt <= this.maxRetries; attempt += 1) {
        try {
          await this.send(queued.batch, deliveryKeepalive);
          delivered = true;
          break;
        } catch {
          if (attempt < this.maxRetries) {
            await this.wait(500 * 2 ** attempt);
          }
        }
      }
      this.queuedBatches.shift();
      this.inflightBatch = null;
      this.inflightKeepalive = false;
      this.inflightKeepaliveResent = false;
      if (!delivered) continue;
    }
  }

  private deliverKeepaliveBehindInflight(): Promise<void> {
    const urgent = this.queuedBatches.splice(1);
    const candidates = urgent.map(({ batch }) => ({ batch, inflight: false }));
    if (this.inflightBatch && !this.inflightKeepalive && !this.inflightKeepaliveResent) {
      candidates.push({ batch: this.inflightBatch, inflight: true });
    }
    const reservedKeepaliveBytes =
      this.inflightBatch && this.inflightKeepalive ? byteLength(this.inflightBatch) : 0;
    const selectedBatches = selectPageExitKeepaliveBatches(
      candidates.map((candidate) => candidate.batch),
      reservedKeepaliveBytes
    );
    const selected = candidates.filter((candidate) => selectedBatches.includes(candidate.batch));
    if (selected.some((candidate) => candidate.inflight)) {
      this.inflightKeepaliveResent = true;
    }
    return Promise.all(
      selected.map(({ batch }) =>
        this.send(batch, true).catch(() => {
          // Page-exit delivery is a final best-effort attempt and never blocks navigation.
        })
      )
    ).then(() => undefined);
  }
}

function batchIsTerminal(batch: PlaybackTelemetryBatch): boolean {
  return batch.events.some((event) => event.event_type === "end" || event.event_type === "terminal_error");
}

export function selectPageExitKeepaliveBatches(
  batches: PlaybackTelemetryBatch[],
  reservedBytes = 0
): PlaybackTelemetryBatch[] {
  const prioritized = [...batches].sort(
    (left, right) => Number(batchIsTerminal(right)) - Number(batchIsTerminal(left))
  );
  const selected: PlaybackTelemetryBatch[] = [];
  let selectedBytes = Math.max(0, reservedBytes);
  for (const batch of prioritized) {
    const batchBytes = byteLength(batch);
    if (selectedBytes + batchBytes > PLAYBACK_TELEMETRY_MAX_PAYLOAD_BYTES) continue;
    selected.push(batch);
    selectedBytes += batchBytes;
  }
  return selected;
}

export function buildPlaybackTelemetryContext(
  item: FeedVideo,
  mediaURL: string,
  nav: Navigator | undefined = typeof navigator === "undefined" ? undefined : navigator,
  viewportWidth: number | undefined = typeof window === "undefined" ? undefined : window.innerWidth
): PlaybackTelemetryContext {
  const source = playbackTelemetrySource(item, mediaURL);
  const browser = detectBrowser(nav?.userAgent || "");
  const connection = readConnection(nav);
  return {
    video_id: Math.max(1, Math.round(item.video_id)),
    scene: normalizeToken(item.feed_scene, 32, "unknown"),
    request_id: item.request_id.trim().slice(0, 64),
    player_adapter: source.playerAdapter,
    source_type: source.sourceType,
    rendition_label: source.renditionLabel,
    codec_family: source.codecFamily,
    network_class: detectNetworkClass(nav, connection),
    save_data: Boolean(connection?.saveData),
    browser_family: browser.family,
    browser_major: browser.major,
    os_family: detectOS(nav?.userAgent || ""),
    viewport_class: detectViewport(viewportWidth),
    cdn_host: source.cdnHost
  };
}

export function playbackTelemetrySource(item: FeedVideo, mediaURL: string): PlaybackTelemetrySourceContext {
  const selected = findPlaybackSource(item.playback_sources, mediaURL);
  const sourceType = detectSourceType(selected?.type, mediaURL);
  return {
    sourceType,
    renditionLabel: normalizeLabel(selected?.quality || "unknown"),
    codecFamily: detectCodecFamily(selected?.codec || ""),
    cdnHost: hostFromURL(mediaURL),
    playerAdapter: sourceType === "dash" ? "dash" : sourceType === "mp4" ? "native_mp4" : "unknown"
  };
}

export function playbackErrorCategory(code: number | undefined): PlaybackTelemetryErrorCategory {
  switch (code) {
    case 1:
      return "aborted";
    case 2:
      return "network";
    case 3:
      return "decode";
    case 4:
      return "unsupported";
    default:
      return "unknown";
  }
}

function findPlaybackSource(sources: PlaybackSource[] | undefined, mediaURL: string): PlaybackSource | undefined {
  return sources?.find((source) => source.url === mediaURL) || sources?.find((source) => source.type !== "image");
}

function detectSourceType(type: PlaybackSource["type"] | undefined, mediaURL: string): PlaybackTelemetrySourceType {
  if (type === "dash" || /\.mpd(?:$|[?#])/i.test(mediaURL)) return "dash";
  if (type === "mp4" || /\.mp4(?:$|[?#])/i.test(mediaURL) || mediaURL.startsWith("/uploads/")) return "mp4";
  return "unknown";
}

function detectCodecFamily(codec: string): PlaybackTelemetryCodecFamily {
  const value = codec.toLowerCase();
  if (/(avc1|avc3|h\.?264)/.test(value)) return "h264";
  if (/(hev1|hvc1|h\.?265|hevc)/.test(value)) return "h265";
  if (/vp0?8/.test(value)) return "vp8";
  if (/vp0?9/.test(value)) return "vp9";
  if (/av01|av1/.test(value)) return "av1";
  return value ? "other" : "unknown";
}

interface BrowserConnection {
  effectiveType?: string;
  type?: string;
  saveData?: boolean;
}

type NavigatorWithConnection = Navigator & {
  connection?: BrowserConnection;
  mozConnection?: BrowserConnection;
  webkitConnection?: BrowserConnection;
};

function readConnection(nav: Navigator | undefined): BrowserConnection | undefined {
  const connected = nav as NavigatorWithConnection | undefined;
  return connected?.connection || connected?.mozConnection || connected?.webkitConnection;
}

function detectNetworkClass(
  nav: Navigator | undefined,
  connection: BrowserConnection | undefined
): PlaybackTelemetryNetworkClass {
  if (nav?.onLine === false) return "offline";
  const type = `${connection?.type || ""} ${connection?.effectiveType || ""}`.toLowerCase();
  if (type.includes("wifi")) return "wifi";
  if (type.includes("ethernet")) return "ethernet";
  if (type.includes("slow-2g")) return "slow_2g";
  if (/(^|\s)2g(\s|$)/.test(type)) return "2g";
  if (/(^|\s)3g(\s|$)/.test(type)) return "3g";
  if (/(^|\s)4g(\s|$)/.test(type)) return "4g";
  if (/(^|\s)5g(\s|$)/.test(type)) return "5g";
  return "unknown";
}

function detectBrowser(userAgent: string): { family: PlaybackTelemetryBrowserFamily; major: number } {
  const patterns: Array<[PlaybackTelemetryBrowserFamily, RegExp]> = [
    ["edge", /Edg\/(\d+)/],
    ["chrome", /(?:Chrome|CriOS)\/(\d+)/],
    ["firefox", /(?:Firefox|FxiOS)\/(\d+)/],
    ["safari", /Version\/(\d+).+Safari/]
  ];
  for (const [family, pattern] of patterns) {
    const match = userAgent.match(pattern);
    if (match) return { family, major: boundedInteger(Number(match[1]), 0, 999) };
  }
  return { family: userAgent ? "other" : "unknown", major: 0 };
}

function detectOS(userAgent: string): PlaybackTelemetryOSFamily {
  if (/CrOS/i.test(userAgent)) return "chromeos";
  if (/Android/i.test(userAgent)) return "android";
  if (/(iPhone|iPad|iPod)/i.test(userAgent)) return "ios";
  if (/Windows/i.test(userAgent)) return "windows";
  if (/Mac OS X/i.test(userAgent)) return "macos";
  if (/Linux/i.test(userAgent)) return "linux";
  return userAgent ? "other" : "unknown";
}

function detectViewport(width: number | undefined): PlaybackTelemetryViewportClass {
  if (!Number.isFinite(width) || Number(width) <= 0) return "unknown";
  if (Number(width) < 640) return "small";
  if (Number(width) < 1024) return "medium";
  return "large";
}

function hostFromURL(value: string): string {
  if (!value) return "";
  try {
    const base = typeof window === "undefined" ? "https://local.invalid" : window.location.href;
    const host = new URL(value, base).hostname;
    return host === "local.invalid" ? "" : normalizeHost(host);
  } catch {
    return "";
  }
}

function normalizeLabel(value: string): string {
  return normalizeToken(value, 32, "unknown");
}

function normalizeHost(value: string): string {
  return value.trim().toLowerCase().slice(0, 253);
}

function normalizeToken(value: string, maxLength: number, fallback: string): string {
  const normalized = value
    .trim()
    .toLowerCase()
    .replace(/[^a-z0-9._-]+/g, "_")
    .replace(/^_+|_+$/g, "")
    .slice(0, maxLength);
  return normalized || fallback;
}

function createTelemetryID(prefix: string): string {
  const id =
    typeof globalThis.crypto?.randomUUID === "function"
      ? globalThis.crypto.randomUUID()
      : `${Date.now()}-${Math.random().toString(36).slice(2, 12)}`;
  return `${prefix}-${id}`;
}

function addFrameTotals(event: PlaybackTelemetryEvent, droppedFrames?: number, totalFrames?: number): void {
  if (droppedFrames === undefined || totalFrames === undefined) return;
  const boundedTotal = boundedInteger(totalFrames, 0, MAX_FRAME_COUNT);
  event.total_frames = boundedTotal;
  event.dropped_frames = Math.min(boundedInteger(droppedFrames, 0, MAX_FRAME_COUNT), boundedTotal);
}

function addRebufferSummary(
  event: PlaybackTelemetryEvent,
  summary: { rebufferCount: number; rebufferDurationMs: number; maxRebufferDurationMs: number }
): void {
  event.rebuffer_count = boundedInteger(summary.rebufferCount, 0, 10_000);
  event.rebuffer_duration_ms = boundedInteger(summary.rebufferDurationMs, 0, MAX_SESSION_MS);
  event.max_rebuffer_duration_ms = Math.min(
    boundedInteger(summary.maxRebufferDurationMs, 0, MAX_SESSION_MS),
    event.rebuffer_duration_ms
  );
}

function mediaMilliseconds(value: number | undefined): number {
  if (!Number.isFinite(value) || Number(value) <= 0) return 0;
  return boundedInteger(Number(value) * 1_000, 0, MAX_SESSION_MS);
}

function finiteSeconds(value: number | undefined): number {
  return Number.isFinite(value) && Number(value) >= 0 ? Number(value) : 0;
}

function finiteNow(value: number): number {
  return Number.isFinite(value) && value >= 0 ? value : 0;
}

function elapsedMs(startedAt: number, now: () => number): number {
  return boundedInteger(finiteNow(now()) - startedAt, 0, MAX_SESSION_MS);
}

function boundedInteger(value: number, minimum: number, maximum: number): number {
  if (!Number.isFinite(value)) return minimum;
  return Math.max(minimum, Math.min(maximum, Math.round(value)));
}

function byteLength(value: unknown): number {
  return new TextEncoder().encode(JSON.stringify(value)).byteLength;
}
