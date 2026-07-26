import { describe, expect, it, vi } from "vitest";
import {
  DashAdapter,
  DEFAULT_DASH_EVENTS,
  mapDashError,
  type DashEventListener,
  type DashRepresentation,
  type DashRuntime,
  type DashRuntimePlayer
} from "./dashAdapter";
import { FakePlayerMedia, FakeTimeRanges } from "./testMedia";
import type { AdaptiveQualityBounds, PlaybackSource, PlayerMediaElement } from "./types";

const source: PlaybackSource = {
  id: "dash-main",
  type: "dash",
  url: "https://media.example/manifest.mpd",
  mimeType: "application/dash+xml",
  codecs: ["avc1.4D401F", "mp4a.40.2"],
  qualityLabel: "Auto",
  role: "adaptive",
  revision: "v2"
};

class FakeDashPlayer implements DashRuntimePlayer {
  initialized: { media: PlayerMediaElement; source: string; autoPlay: boolean; startTime: number } | null = null;
  listeners = new Map<string, Set<DashEventListener>>();
  representations: readonly DashRepresentation[] = [
    { id: "low", bitrate: 500_000, width: 640, height: 360 },
    { id: "high", bitrate: 2_000_000, width: 1280, height: 720 }
  ];
  activeId: string | null = "low";
  bufferLength = 4;
  resetCount = 0;
  autoQuality = true;
  selectedRepresentation: string | null = null;
  bounds: AdaptiveQualityBounds = {};
  playCount = 0;
  pauseCount = 0;
  seekTime = 0;

  initialize(media: PlayerMediaElement, manifest: string, autoPlay: boolean, startTime: number): void {
    this.initialized = { media, source: manifest, autoPlay, startTime };
  }

  on(eventType: string, listener: DashEventListener): void {
    const listeners = this.listeners.get(eventType) ?? new Set();
    listeners.add(listener);
    this.listeners.set(eventType, listeners);
  }

  off(eventType: string, listener: DashEventListener): void {
    this.listeners.get(eventType)?.delete(listener);
  }

  emit(eventType: string, event: unknown = {}): void {
    for (const listener of this.listeners.get(eventType) ?? []) listener(event);
  }

  play(): void {
    this.playCount += 1;
  }

  pause(): void {
    this.pauseCount += 1;
  }

  seek(time: number): void {
    this.seekTime = time;
  }

  setMuted(): void {}
  setVolume(): void {}
  setPlaybackRate(): void {}

  setAutoQuality(enabled: boolean): void {
    this.autoQuality = enabled;
  }

  setRepresentation(id: string): void {
    this.selectedRepresentation = id;
  }

  configureBounds(bounds: AdaptiveQualityBounds): void {
    this.bounds = bounds;
  }

  getRepresentations(): readonly DashRepresentation[] {
    return this.representations;
  }

  getCurrentRepresentationId(): string | null {
    return this.activeId;
  }

  getBufferLength(): number {
    return this.bufferLength;
  }

  reset(): void {
    this.resetCount += 1;
  }
}

function runtime(player: FakeDashPlayer): DashRuntime {
  return { events: DEFAULT_DASH_EVENTS, createPlayer: () => player };
}

describe("DashAdapter", () => {
  it("lazily initializes dash runtime and maps qualities, buffer, rate, seek, and lifecycle", async () => {
    const media = new FakePlayerMedia();
    media.duration = 30;
    media.buffered = new FakeTimeRanges([[0, 2]]);
    const player = new FakeDashPlayer();
    const loader = vi.fn(async () => runtime(player));
    const adapter = new DashAdapter(media, loader);

    expect(loader).not.toHaveBeenCalled();
    await adapter.load(source, {
      startTime: 5,
      muted: false,
      playbackRate: 1.25,
      adaptiveBounds: { minBitrate: 400_000, maxBitrate: 2_500_000, initialBitrate: 800_000 }
    });
    expect(loader).toHaveBeenCalledOnce();
    expect(player.initialized).toMatchObject({ source: source.url, autoPlay: false, startTime: 5 });
    expect(player.bounds).toEqual({ minBitrate: 400_000, maxBitrate: 2_500_000, initialBitrate: 800_000 });

    player.emit(DEFAULT_DASH_EVENTS.streamInitialized);
    player.emit(DEFAULT_DASH_EVENTS.playing);
    player.emit(DEFAULT_DASH_EVENTS.seeking, { seekTime: 8 });
    player.emit(DEFAULT_DASH_EVENTS.seeked, { seekTime: 9 });
    player.activeId = "high";
    player.emit(DEFAULT_DASH_EVENTS.qualityRendered, { newRepresentation: { id: "high" } });

    expect(adapter.getState()).toMatchObject({
      status: "playing",
      currentTime: 9,
      duration: 30,
      bufferedAhead: 4,
      playbackRate: 1.25,
      effectiveQualityId: "high"
    });
    expect(adapter.getState().qualities).toEqual([
      expect.objectContaining({ id: "low", label: "360p", active: false }),
      expect.objectContaining({ id: "high", label: "720p", active: true })
    ]);
  });

  it("applies manual and automatic quality changes", async () => {
    const media = new FakePlayerMedia();
    const player = new FakeDashPlayer();
    const adapter = new DashAdapter(media, async () => runtime(player));
    await adapter.load(source);
    player.emit(DEFAULT_DASH_EVENTS.streamInitialized);

    adapter.setQuality("high");
    expect(player.autoQuality).toBe(false);
    expect(player.selectedRepresentation).toBe("high");
    adapter.setQuality("auto");
    expect(player.autoQuality).toBe(true);

    adapter.setQuality("missing-representation");
    expect(player.autoQuality).toBe(true);
    expect(adapter.getState().selectedQuality).toBe("auto");
  });

  it("maps structured errors and retries with preserved state", async () => {
    const media = new FakePlayerMedia();
    const first = new FakeDashPlayer();
    const second = new FakeDashPlayer();
    let loadCount = 0;
    const adapter = new DashAdapter(media, async () => runtime(loadCount++ === 0 ? first : second));
    await adapter.load(source, { muted: false, playbackRate: 1.5, autoPlay: true });
    media.currentTime = 7;
    first.emit(DEFAULT_DASH_EVENTS.timeUpdated);
    first.emit(DEFAULT_DASH_EVENTS.error, { error: { code: 11, message: "manifest network failure" } });

    expect(adapter.getState().error).toMatchObject({ category: "manifest", recoverable: true });
    await adapter.retry();
    expect(first.resetCount).toBe(1);
    expect(second.initialized).toMatchObject({ startTime: 7 });
    expect(media.muted).toBe(false);
    expect(media.playbackRate).toBe(1.5);
  });

  it("removes every listener and resets the runtime on destroy", async () => {
    const media = new FakePlayerMedia();
    const player = new FakeDashPlayer();
    const adapter = new DashAdapter(media, async () => runtime(player));
    await adapter.load(source);
    expect([...player.listeners.values()].reduce((count, listeners) => count + listeners.size, 0)).toBeGreaterThan(0);

    adapter.destroy();
    expect([...player.listeners.values()].reduce((count, listeners) => count + listeners.size, 0)).toBe(0);
    expect(player.resetCount).toBe(1);
    expect(media.src).toBe("");
    expect(adapter.getState().status).toBe("idle");
  });

  it("cleans up stale async initialization after destruction", async () => {
    const media = new FakePlayerMedia();
    const player = new FakeDashPlayer();
    let resolveRuntime: (value: DashRuntime) => void = () => {
      throw new Error("Runtime resolver was not created.");
    };
    const pending = new Promise<DashRuntime>((resolve) => {
      resolveRuntime = resolve;
    });
    const adapter = new DashAdapter(media, () => pending);
    const loading = adapter.load(source);
    adapter.destroy();
    resolveRuntime(runtime(player));
    await loading;

    expect(player.initialized).toBeNull();
    expect(player.listeners.size).toBe(0);
  });
});

describe("mapDashError", () => {
  it("classifies network, codec, and decode failures", () => {
    expect(mapDashError({ error: { code: 17, message: "fragment failed" } }).category).toBe("network");
    expect(mapDashError({ error: { code: 23, message: "MediaSource missing" } }).category).toBe(
      "unsupported_codec"
    );
    expect(mapDashError({ error: { code: 20, message: "append decode error" } }).category).toBe("decode");
  });
});
