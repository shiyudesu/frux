import {
  feedPreloadResourceKey,
  type AcquiredFeedPreloadResource,
  type EffectiveFeedPreloadPolicy,
  type FeedPreloadCandidate,
  type FeedPreloadMediaEvent,
  type FeedPreloadMediaResource,
  type FeedPreloadMode,
  type FeedPreloadReadiness,
  type FeedPreloadResourceKey
} from "./feedPreload";

export interface FeedPreloadDebugState {
  attempts: number;
  ready: number;
  reused: number;
  cancellations: number;
  failures: number;
  activeResources: number;
  acquiredResources: number;
}

export interface FeedPreloadControllerOptions {
  createMedia?: () => FeedPreloadMediaResource;
  prepareCover?: (url: string) => () => void;
  now?: () => number;
  setTimer?: (callback: () => void, delayMs: number) => number;
  clearTimer?: (timerID: number) => void;
}

interface ResourceEntry {
  registryKey: string;
  candidate: FeedPreloadCandidate;
  media: FeedPreloadMediaResource;
  readiness: FeedPreloadReadiness;
  bufferedMs: number;
  acquired: boolean;
  retained: boolean;
  lastUsedAt: number;
  timerID: number | null;
  unsubscribe: () => void;
}

export class FeedPreloadController {
  private readonly createMedia: () => FeedPreloadMediaResource;
  private readonly prepareCover: (url: string) => () => void;
  private readonly now: () => number;
  private readonly setTimer: (callback: () => void, delayMs: number) => number;
  private readonly clearTimer: (timerID: number) => void;
  private readonly entries = new Map<string, ResourceEntry>();
  private readonly coverCleanups = new Map<string, () => void>();
  private readonly retryAfter = new Map<string, number>();
  private readonly debugSubscribers = new Set<(state: FeedPreloadDebugState) => void>();
  private policy: EffectiveFeedPreloadPolicy | null = null;
  private generation = "";
  private debug: FeedPreloadDebugState = emptyDebugState();
  private destroyed = false;

  constructor(options: FeedPreloadControllerOptions = {}) {
    this.createMedia = options.createMedia || (() => new NativeFeedPreloadMedia());
    this.prepareCover = options.prepareCover || prepareNativeCover;
    this.now = options.now || (() => Date.now());
    this.setTimer = options.setTimer || ((callback, delayMs) => window.setTimeout(callback, delayMs));
    this.clearTimer = options.clearTimer || ((timerID) => window.clearTimeout(timerID));
  }

  sync(candidates: FeedPreloadCandidate[], policy: EffectiveFeedPreloadPolicy): void {
    if (this.destroyed) return;
    this.policy = policy;
    const nextGeneration = candidates[0]?.key.generation || "";
    if (nextGeneration !== this.generation) {
      this.generation = nextGeneration;
      this.retryAfter.clear();
    }
    const orderedCandidates = [...candidates].sort(compareCandidatePriority);
    const retainedKeys = new Set(orderedCandidates.map((candidate) => feedPreloadResourceKey(candidate.key)));
    const retainedCovers = new Set(orderedCandidates.map((candidate) => candidate.item.cover_url).filter(Boolean));

    for (const [url, cleanup] of this.coverCleanups) {
      if (retainedCovers.has(url)) continue;
      cleanup();
      this.coverCleanups.delete(url);
    }
    for (const url of retainedCovers) {
      if (!this.coverCleanups.has(url)) {
        this.coverCleanups.set(url, this.prepareCover(url));
      }
    }

    for (const entry of this.entries.values()) {
      entry.retained = retainedKeys.has(entry.registryKey);
      if (!entry.retained && !entry.acquired) {
        this.removeEntry(entry, true);
      }
    }

    for (const candidate of orderedCandidates) {
      const registryKey = feedPreloadResourceKey(candidate.key);
      const existing = this.entries.get(registryKey);
      if (existing) {
        const previousMode = existing.candidate.mode;
        existing.candidate = candidate;
        existing.retained = true;
        existing.lastUsedAt = this.now();
        existing.media.setPreloadMode(candidate.mode);
        if (previousMode !== "buffer" && candidate.mode === "buffer") {
          existing.readiness = "loading";
          existing.bufferedMs = 0;
          this.updateReadiness(existing);
          if (!this.isReady(existing) && !existing.acquired) {
            this.scheduleEntryTimeout(existing, policy.timeoutMs);
            existing.media.load();
          }
          continue;
        }
        this.updateReadiness(existing);
        continue;
      }
      if (!candidate.source || candidate.mode === "cover" || this.isCoolingDown(candidate)) continue;
      if (this.entries.size >= policy.maxResources && !this.evictLeastRecentlyUsed()) continue;
      this.createEntry(candidate, policy);
    }

    while (this.entries.size > policy.maxResources && this.evictLeastRecentlyUsed()) {
      // Keep trimming non-acquired entries until the absolute cap is restored.
    }
    this.updateDebugCounts();
  }

  acquire(key: FeedPreloadResourceKey): AcquiredFeedPreloadResource | undefined {
    if (this.destroyed) return undefined;
    const entry = this.entries.get(feedPreloadResourceKey(key));
    if (!entry || entry.acquired) return undefined;
    entry.acquired = true;
    entry.lastUsedAt = this.now();
    this.clearEntryTimer(entry);
    this.debug.reused += 1;
    this.updateDebugCounts();

    let released = false;
    return {
      key: entry.candidate.key,
      media: entry.media,
      get readiness() {
        return entry.readiness;
      },
      get bufferedMs() {
        return entry.bufferedMs;
      },
      release: () => {
        if (released) return;
        released = true;
        entry.media.unmount();
        entry.acquired = false;
        entry.lastUsedAt = this.now();
        if (!entry.retained || entry.readiness === "failed") {
          this.removeEntry(entry, false);
        }
        this.updateDebugCounts();
      }
    };
  }

  acquireCandidate(
    candidate: FeedPreloadCandidate,
    policy: EffectiveFeedPreloadPolicy
  ): AcquiredFeedPreloadResource | undefined {
    if (this.destroyed) return undefined;
    const registryKey = feedPreloadResourceKey(candidate.key);
    if (
      !this.entries.has(registryKey) &&
      candidate.source &&
      candidate.mode !== "cover" &&
      !this.isCoolingDown(candidate) &&
      (this.entries.size < policy.maxResources || this.evictLeastRecentlyUsed())
    ) {
      this.policy = policy;
      this.createEntry(candidate, policy);
      this.updateDebugCounts();
    }
    return this.acquire(candidate.key);
  }

  getDebugState(): FeedPreloadDebugState {
    return {
      ...this.debug,
      activeResources: this.entries.size,
      acquiredResources: [...this.entries.values()].filter((entry) => entry.acquired).length
    };
  }

  subscribeDebug(listener: (state: FeedPreloadDebugState) => void): () => void {
    this.debugSubscribers.add(listener);
    listener(this.getDebugState());
    return () => this.debugSubscribers.delete(listener);
  }

  destroy(): void {
    if (this.destroyed) return;
    this.destroyed = true;
    for (const entry of [...this.entries.values()]) {
      this.removeEntry(entry, entry.readiness === "loading" || entry.readiness === "metadata");
    }
    for (const cleanup of this.coverCleanups.values()) {
      cleanup();
    }
    this.coverCleanups.clear();
    this.retryAfter.clear();
    this.updateDebugCounts();
    this.debugSubscribers.clear();
  }

  private createEntry(candidate: FeedPreloadCandidate, policy: EffectiveFeedPreloadPolicy): void {
    if (!candidate.source) return;
    const registryKey = feedPreloadResourceKey(candidate.key);
    const media = this.createMedia();
    const entry: ResourceEntry = {
      registryKey,
      candidate,
      media,
      readiness: "loading",
      bufferedMs: 0,
      acquired: false,
      retained: true,
      lastUsedAt: this.now(),
      timerID: null,
      unsubscribe: () => {}
    };
    entry.unsubscribe = media.subscribe((event) => this.handleMediaEvent(entry, event));
    media.configure(candidate.source.url, candidate.item.cover_url, candidate.mode);
    this.entries.set(registryKey, entry);
    this.debug.attempts += 1;
    this.scheduleEntryTimeout(entry, policy.timeoutMs);
    media.load();
  }

  private handleMediaEvent(entry: ResourceEntry, event: FeedPreloadMediaEvent): void {
    if (this.destroyed || this.entries.get(entry.registryKey) !== entry) return;
    if (event === "error") {
      this.failEntry(entry);
      return;
    }
    if (event === "loadedmetadata" && entry.readiness === "loading") {
      entry.readiness = "metadata";
    }
    if (event === "loadedmetadata" || event === "canplay" || event === "progress" || event === "loadeddata") {
      this.updateReadiness(entry, event === "canplay");
    }
  }

  private updateReadiness(entry: ResourceEntry, canPlayFallback = false): void {
    const mode = entry.candidate.mode;
    if (mode === "metadata") {
      if (entry.media.readyState >= 1) {
        this.markReady(entry, "metadata");
      }
      return;
    }
    if (mode !== "buffer") return;

    const bufferedMs = entry.media.bufferedAheadMs();
    entry.bufferedMs = bufferedMs || 0;
    const targetMs = entry.candidate.bufferTargetMs;
    if ((targetMs > 0 && entry.bufferedMs >= targetMs) || (targetMs <= 0 && entry.media.readyState >= 3)) {
      this.markReady(entry, "ready");
      return;
    }
    if (canPlayFallback && bufferedMs === undefined && entry.media.readyState >= 3) {
      this.markReady(entry, "ready");
    }
  }

  private markReady(entry: ResourceEntry, readiness: "metadata" | "ready"): void {
    const wasReady = entry.readiness === "ready" || entry.readiness === "metadata";
    entry.readiness = readiness;
    if (!wasReady) {
      this.debug.ready += 1;
    }
    this.clearEntryTimer(entry);
    this.updateDebugCounts();
  }

  private handleTimeout(entry: ResourceEntry): void {
    entry.timerID = null;
    if (this.destroyed || entry.acquired || this.entries.get(entry.registryKey) !== entry) return;
    if (
      entry.readiness === "ready" ||
      (entry.candidate.mode === "metadata" && entry.readiness === "metadata")
    ) {
      return;
    }
    this.failEntry(entry);
  }

  private failEntry(entry: ResourceEntry): void {
    if (entry.readiness === "failed") return;
    entry.readiness = "failed";
    this.debug.failures += 1;
    this.retryAfter.set(retryKey(entry.candidate), this.now() + (this.policy?.retryCooldownMs || 0));
    this.clearEntryTimer(entry);
    if (!entry.acquired) {
      this.removeEntry(entry, false);
    }
    this.updateDebugCounts();
  }

  private isCoolingDown(candidate: FeedPreloadCandidate): boolean {
    const key = retryKey(candidate);
    const retryAt = this.retryAfter.get(key);
    if (!retryAt) return false;
    if (retryAt <= this.now()) {
      this.retryAfter.delete(key);
      return false;
    }
    return true;
  }

  private evictLeastRecentlyUsed(): boolean {
    const evictable = [...this.entries.values()]
      .filter((entry) => !entry.acquired)
      .sort((left, right) => left.lastUsedAt - right.lastUsedAt)[0];
    if (!evictable) return false;
    this.removeEntry(evictable, evictable.readiness === "loading" || evictable.readiness === "metadata");
    return true;
  }

  private removeEntry(entry: ResourceEntry, cancellation: boolean): void {
    if (this.entries.get(entry.registryKey) !== entry) return;
    this.clearEntryTimer(entry);
    entry.unsubscribe();
    entry.media.destroy();
    this.entries.delete(entry.registryKey);
    if (cancellation) {
      this.debug.cancellations += 1;
    }
  }

  private clearEntryTimer(entry: ResourceEntry): void {
    if (entry.timerID === null) return;
    this.clearTimer(entry.timerID);
    entry.timerID = null;
  }

  private isReady(entry: ResourceEntry): boolean {
    return entry.readiness === "ready";
  }

  private scheduleEntryTimeout(entry: ResourceEntry, timeoutMs: number): void {
    this.clearEntryTimer(entry);
    entry.timerID = this.setTimer(() => this.handleTimeout(entry), timeoutMs);
  }

  private updateDebugCounts(): void {
    this.debug.activeResources = this.entries.size;
    this.debug.acquiredResources = [...this.entries.values()].filter((entry) => entry.acquired).length;
    const snapshot = { ...this.debug };
    for (const subscriber of this.debugSubscribers) {
      subscriber(snapshot);
    }
  }
}

class NativeFeedPreloadMedia implements FeedPreloadMediaResource {
  private static readonly eventTypes: FeedPreloadMediaEvent[] = [
    "loadedmetadata",
    "loadeddata",
    "durationchange",
    "canplay",
    "progress",
    "playing",
    "pause",
    "waiting",
    "stalled",
    "timeupdate",
    "seeking",
    "seeked",
    "ended",
    "volumechange",
    "error"
  ];

  private readonly element = document.createElement("video");
  private readonly subscribers = new Set<(event: FeedPreloadMediaEvent) => void>();
  private readonly eventHandlers = new Map<FeedPreloadMediaEvent, EventListener>();

  constructor() {
    this.element.muted = true;
    this.element.loop = true;
    this.element.playsInline = true;
    for (const eventType of NativeFeedPreloadMedia.eventTypes) {
      const handler = () => {
        for (const subscriber of this.subscribers) {
          subscriber(eventType);
        }
      };
      this.eventHandlers.set(eventType, handler);
      this.element.addEventListener(eventType, handler);
    }
  }

  get currentTime(): number {
    return this.element.currentTime;
  }

  set currentTime(value: number) {
    this.element.currentTime = value;
  }

  get duration(): number {
    return this.element.duration;
  }

  get paused(): boolean {
    return this.element.paused;
  }

  get ended(): boolean {
    return this.element.ended;
  }

  get muted(): boolean {
    return this.element.muted;
  }

  set muted(value: boolean) {
    this.element.muted = value;
  }

  get readyState(): number {
    return this.element.readyState;
  }

  requestVideoFrame(callback: () => void): number | undefined {
    if (typeof this.element.requestVideoFrameCallback !== "function") return undefined;
    return this.element.requestVideoFrameCallback(() => callback());
  }

  cancelVideoFrameRequest(callbackID: number): void {
    this.element.cancelVideoFrameCallback?.(callbackID);
  }

  readPlaybackQuality(): { droppedFrames: number; totalFrames: number } | undefined {
    if (typeof this.element.getVideoPlaybackQuality !== "function") return undefined;
    const quality = this.element.getVideoPlaybackQuality();
    return {
      droppedFrames: quality.droppedVideoFrames,
      totalFrames: quality.totalVideoFrames
    };
  }

  mediaErrorCode(): number {
    return this.element.error?.code || 0;
  }

  currentSource(): string {
    return this.element.currentSrc;
  }

  configure(url: string, poster: string, mode: FeedPreloadMode): void {
    this.element.src = url;
    this.element.poster = poster;
    this.setPreloadMode(mode);
  }

  setPreloadMode(mode: FeedPreloadMode): void {
    this.element.preload = mode === "buffer" ? "auto" : "metadata";
  }

  load(): void {
    this.element.load();
  }

  play(): Promise<void> {
    return this.element.play();
  }

  pause(): void {
    this.element.pause();
  }

  bufferedAheadMs(): number | undefined {
    const currentTime = Math.max(0, this.element.currentTime || 0);
    for (let index = 0; index < this.element.buffered.length; index += 1) {
      const start = this.element.buffered.start(index);
      const end = this.element.buffered.end(index);
      if (start <= currentTime + 0.05 && end >= currentTime) {
        return Math.max(0, Math.round((end - currentTime) * 1000));
      }
    }
    return undefined;
  }

  mount(host: HTMLElement, className: string): void {
    this.element.className = className;
    if (this.element.parentElement !== host) {
      host.replaceChildren(this.element);
    }
  }

  unmount(): void {
    this.element.remove();
  }

  subscribe(listener: (event: FeedPreloadMediaEvent) => void): () => void {
    this.subscribers.add(listener);
    return () => this.subscribers.delete(listener);
  }

  destroy(): void {
    this.unmount();
    this.subscribers.clear();
    for (const [eventType, handler] of this.eventHandlers) {
      this.element.removeEventListener(eventType, handler);
    }
    this.eventHandlers.clear();
    this.element.removeAttribute("src");
    this.element.load();
  }
}

function prepareNativeCover(url: string): () => void {
  const cover = new Image();
  cover.src = url;
  return () => {
    cover.src = "";
  };
}

function compareCandidatePriority(left: FeedPreloadCandidate, right: FeedPreloadCandidate): number {
  return candidatePriority(left) - candidatePriority(right);
}

function candidatePriority(candidate: FeedPreloadCandidate): number {
  if (candidate.role === "active") return 0;
  if (candidate.role === "forward") return candidate.feedIndex + 1;
  return Number.MAX_SAFE_INTEGER - candidate.feedIndex;
}

function retryKey(candidate: FeedPreloadCandidate): string {
  return `${candidate.key.generation}:${candidate.item.video_id}:${candidate.key.sourceRevision}`;
}

function emptyDebugState(): FeedPreloadDebugState {
  return {
    attempts: 0,
    ready: 0,
    reused: 0,
    cancellations: 0,
    failures: 0,
    activeResources: 0,
    acquiredResources: 0
  };
}

export function createNativeFeedPreloadMediaResource(): FeedPreloadMediaResource {
  return new NativeFeedPreloadMedia();
}
