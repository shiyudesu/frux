import { describe, expect, it } from "vitest";
import type {
  FeedPreloadMediaEvent,
  FeedPreloadMediaResource,
  FeedPreloadMode
} from "./feedPreload";
import {
  PLAYBACK_TELEMETRY_MAX_EVENTS,
  PLAYBACK_TELEMETRY_MAX_PAYLOAD_BYTES,
  PlaybackTelemetrySession,
  selectPageExitKeepaliveBatches
} from "./playbackTelemetry";
import type { PlaybackTelemetryBatch, PlaybackTelemetryContext } from "./types";

const context: PlaybackTelemetryContext = {
  video_id: 42,
  scene: "recommend",
  request_id: "request-1",
  player_adapter: "native_mp4",
  source_type: "mp4",
  rendition_label: "720p",
  codec_family: "h264",
  network_class: "wifi",
  save_data: false,
  browser_family: "chrome",
  browser_major: 126,
  os_family: "windows",
  viewport_class: "large",
  cdn_host: "media.example"
};

describe("PlaybackTelemetrySession", () => {
  it("measures the first rendered frame with the exact callback", async () => {
    const harness = createHarness();
    harness.media.supportsFrameCallback = true;

    harness.session.sourceLoadStarted(harness.media);
    harness.advance(48);
    harness.session.playing();
    harness.media.renderFrame();
    await harness.session.flush();

    const event = allEvents(harness.sent).find((candidate) => candidate.event_type === "first_rendered_frame");
    expect(event).toMatchObject({
      first_frame_ms: 48,
      measurement_method: "video_frame_callback"
    });

    describe("selectPageExitKeepaliveBatches", () => {
      it("prioritizes terminal data and keeps aggregate bodies within the browser keepalive quota", () => {
        const regularA = largeBatch("regular-a", false);
        const regularB = largeBatch("regular-b", false);
        const terminal = largeBatch("terminal", true);

        const selected = selectPageExitKeepaliveBatches([regularA, regularB, terminal]);
        const totalBytes = selected.reduce(
          (total, batch) => total + new TextEncoder().encode(JSON.stringify(batch)).byteLength,
          0
        );

        expect(selected).toContain(terminal);
        expect(totalBytes).toBeLessThanOrEqual(PLAYBACK_TELEMETRY_MAX_PAYLOAD_BYTES);
      });

      it("reserves quota already consumed by an in-flight keepalive request", () => {
        const terminal = largeBatch("terminal-reserved", true);
        const terminalBytes = new TextEncoder().encode(JSON.stringify(terminal)).byteLength;

        const selected = selectPageExitKeepaliveBatches(
          [terminal],
          PLAYBACK_TELEMETRY_MAX_PAYLOAD_BYTES - terminalBytes + 1
        );

        expect(selected).toEqual([]);
      });
    });
  });

  it("uses advancing-time and playing fallbacks when frame callbacks are unavailable", async () => {
    const advancing = createHarness();
    advancing.session.sourceLoadStarted(advancing.media);
    advancing.media.readyStateValue = 2;
    advancing.media.currentTime = 0.05;
    advancing.advance(30);
    advancing.session.timeUpdated();
    await advancing.session.flush();

    expect(
      allEvents(advancing.sent).find((candidate) => candidate.event_type === "first_rendered_frame")
    ).toMatchObject({
      first_frame_ms: 30,
      measurement_method: "advancing_time"
    });

    const playing = createHarness();
    playing.session.sourceLoadStarted(playing.media);
    playing.media.readyStateValue = 2;
    playing.advance(35);
    playing.session.playing();
    await playing.session.flush();

    expect(allEvents(playing.sent).find((candidate) => candidate.event_type === "first_rendered_frame")).toMatchObject(
      {
        first_frame_ms: 35,
        measurement_method: "playing"
      }
    );
  });

  it("classifies only expected-playback stalls as rebuffering", async () => {
    const harness = createHarness();
    harness.session.sourceLoadStarted(harness.media);
    harness.session.playing();
    harness.advance(100);
    harness.session.waiting();
    harness.advance(250);
    harness.session.playing();
    harness.session.pause();
    harness.advance(100);
    harness.session.waiting();
    await harness.session.flush();

    const rebufferEvents = allEvents(harness.sent).filter((event) => event.event_type.startsWith("rebuffer"));
    expect(rebufferEvents.map((event) => event.event_type)).toEqual(["rebuffer_start", "rebuffer_end"]);
    expect(rebufferEvents[1]).toMatchObject({
      interval_duration_ms: 250,
      recovery_outcome: "resumed"
    });
  });

  it("records seek intervals without treating seek buffering as rebuffering", async () => {
    const harness = createHarness();
    harness.session.sourceLoadStarted(harness.media);
    harness.session.playing();
    harness.advance(100);
    harness.session.seeking();
    harness.advance(50);
    harness.session.waiting();
    harness.advance(150);
    harness.session.seeked();
    await harness.session.flush();

    const events = allEvents(harness.sent);
    expect(events.filter((event) => event.event_type.startsWith("rebuffer"))).toEqual([]);
    expect(events.filter((event) => event.event_type.startsWith("seek"))).toMatchObject([
      { event_type: "seek_start" },
      { event_type: "seek_end", interval_duration_ms: 200 }
    ]);
  });

  it("retries a failed batch with stable batch and event IDs", async () => {
    const attempts: PlaybackTelemetryBatch[] = [];
    const harness = createHarness(async (batch) => {
      attempts.push(batch);
      if (attempts.length === 1) throw new Error("offline");
    });
    harness.session.sourceLoadStarted(harness.media);

    await harness.session.flush();

    expect(attempts).toHaveLength(2);
    expect(attempts[1]).toEqual(attempts[0]);
    expect(attempts[1].events[0].event_id).toBe(attempts[0].events[0].event_id);
  });

  it("uses keepalive delivery on page exit", async () => {
    const keepaliveValues: boolean[] = [];
    const harness = createHarness(async (_batch, keepalive) => {
      keepaliveValues.push(keepalive);
    });
    harness.session.sourceLoadStarted(harness.media);

    await harness.session.pageExit();

    expect(keepaliveValues).toEqual([true]);
  });

  it("keeps the telemetry session active after a BFCache-style keepalive flush", async () => {
    const harness = createHarness();
    harness.session.sourceLoadStarted(harness.media);
    await harness.session.flush(true);

    harness.session.playing();
    harness.session.pause();
    await harness.session.flush(false);

    expect(allEvents(harness.sent).map((event) => event.event_type)).toEqual(
      expect.arrayContaining(["play_success", "pause"])
    );
  });

  it("sends terminal page-exit data immediately when a normal flush is in flight", async () => {
    let releaseFirst: (() => void) | undefined;
    const attempts: Array<{ batch: PlaybackTelemetryBatch; keepalive: boolean }> = [];
    const harness = createHarness(async (batch, keepalive) => {
      attempts.push({ batch, keepalive });
      if (attempts.length === 1) {
        await new Promise<void>((resolve) => {
          releaseFirst = resolve;
        });
      }
    });
    harness.session.sourceLoadStarted(harness.media);
    const normalFlush = harness.session.flush(false);
    harness.session.playing();

    await harness.session.pageExit();

    expect(attempts).toHaveLength(3);
    expect(attempts[0].keepalive).toBe(false);
    expect(attempts[1].keepalive).toBe(true);
    expect(attempts[2].keepalive).toBe(true);
    expect(attempts.some((attempt) => attempt.batch.events.some((event) => event.event_type === "end"))).toBe(true);
    releaseFirst?.();
    await normalFlush;
  });

  it("resends an in-flight terminal batch with keepalive on page exit", async () => {
    let releaseFirst: (() => void) | undefined;
    const attempts: Array<{ batch: PlaybackTelemetryBatch; keepalive: boolean }> = [];
    const harness = createHarness(async (batch, keepalive) => {
      attempts.push({ batch, keepalive });
      if (attempts.length === 1) {
        await new Promise<void>((resolve) => {
          releaseFirst = resolve;
        });
      }
    });
    harness.session.sourceLoadStarted(harness.media);
    harness.session.ended();

    await harness.session.pageExit();

    expect(attempts).toHaveLength(2);
    expect(attempts[0].keepalive).toBe(false);
    expect(attempts[1].keepalive).toBe(true);
    expect(attempts[1].batch.batch_id).toBe(attempts[0].batch.batch_id);
    expect(attempts[1].batch.events.map((event) => event.event_type)).toContain("end");
    releaseFirst?.();
  });

  it("finalizes active rebuffer state with a terminal summary on page exit", async () => {
    const harness = createHarness();
    harness.session.sourceLoadStarted(harness.media);
    harness.session.playing();
    harness.advance(100);
    harness.session.waiting();
    harness.advance(250);

    await harness.session.pageExit();

    const events = allEvents(harness.sent);
    expect(events.map((event) => event.event_type)).toContain("end");
    expect(events.find((event) => event.event_type === "rebuffer_end")).toMatchObject({
      interval_duration_ms: 250,
      recovery_outcome: "ended"
    });
    expect(events.find((event) => event.event_type === "end")).toMatchObject({
      rebuffer_count: 1,
      rebuffer_duration_ms: 250,
      max_rebuffer_duration_ms: 250
    });
  });

  it("keeps event counts, payload bytes, offsets, and terminal frame totals bounded", async () => {
    const harness = createHarness();
    harness.media.totalFrames = Number.MAX_SAFE_INTEGER;
    harness.media.droppedFrames = Number.MAX_SAFE_INTEGER;
    harness.session.sourceLoadStarted(harness.media);
    for (let index = 0; index < 30; index += 1) {
      harness.advance(index % 2 === 0 ? 1 : -1);
      harness.session.seeking();
      harness.advance(2);
      harness.session.seeked();
    }
    harness.session.ended();
    await harness.session.flush();

    expect(harness.sent.every((batch) => batch.events.length <= PLAYBACK_TELEMETRY_MAX_EVENTS)).toBe(true);
    expect(
      harness.sent.every(
        (batch) => new TextEncoder().encode(JSON.stringify(batch)).byteLength <= PLAYBACK_TELEMETRY_MAX_PAYLOAD_BYTES
      )
    ).toBe(true);
    const offsets = allEvents(harness.sent).map((event) => event.offset_ms);
    expect(offsets).toEqual([...offsets].sort((left, right) => left - right));
    expect(allEvents(harness.sent).find((event) => event.event_type === "end")).toMatchObject({
      dropped_frames: 2_147_483_647,
      total_frames: 2_147_483_647,
      rebuffer_count: 0,
      rebuffer_duration_ms: 0,
      max_rebuffer_duration_ms: 0
    });
  });
});

function createHarness(
  send: (batch: PlaybackTelemetryBatch, keepalive: boolean) => Promise<void> = async () => {}
) {
  let now = 1_000;
  let id = 0;
  const sent: PlaybackTelemetryBatch[] = [];
  const media = new FakeMedia();
  const session = new PlaybackTelemetrySession(context, {
    send: async (batch, keepalive) => {
      sent.push(batch);
      await send(batch, keepalive);
    },
    now: () => now,
    wallClock: () => new Date("2026-07-26T13:00:00.000Z"),
    createID: (prefix) => `${prefix}-${++id}`,
    setInterval: () => 1,
    clearInterval: () => {},
    wait: async () => {}
  });
  return {
    media,
    sent,
    session,
    advance: (milliseconds: number) => {
      now += milliseconds;
    }
  };
}

function allEvents(batches: PlaybackTelemetryBatch[]) {
  return batches.flatMap((batch) => batch.events);
}

function largeBatch(batchID: string, terminal: boolean): PlaybackTelemetryBatch {
  const events = Array.from({ length: PLAYBACK_TELEMETRY_MAX_EVENTS }, (_, index) => ({
    event_id: `${batchID}-${index}-${"x".repeat(96)}`,
    event_type: terminal && index === PLAYBACK_TELEMETRY_MAX_EVENTS - 1 ? ("end" as const) : ("source_change" as const),
    offset_ms: index,
    media_position_ms: index,
    source_type: "mp4" as const,
    rendition_label: "x".repeat(32),
    codec_family: "h264" as const,
    cdn_host: `${"a".repeat(63)}.${"b".repeat(63)}.${"c".repeat(63)}.com`
  }));
  return {
    schema_version: 1,
    batch_id: batchID,
    playback_session_id: `playback-${batchID}`,
    client_sent_at: "2026-07-26T13:00:00.000Z",
    context,
    events
  };
}

class FakeMedia implements FeedPreloadMediaResource {
  currentTime = 0;
  duration = 120;
  paused = false;
  ended = false;
  muted = true;
  readyStateValue = 0;
  supportsFrameCallback = false;
  droppedFrames = 0;
  totalFrames = 0;
  private frameCallback: (() => void) | null = null;

  get readyState(): number {
    return this.readyStateValue;
  }

  requestVideoFrame(callback: () => void): number | undefined {
    if (!this.supportsFrameCallback) return undefined;
    this.frameCallback = callback;
    return 1;
  }

  cancelVideoFrameRequest(): void {
    this.frameCallback = null;
  }

  readPlaybackQuality(): { droppedFrames: number; totalFrames: number } {
    return {
      droppedFrames: this.droppedFrames,
      totalFrames: this.totalFrames
    };
  }

  configure(_url: string, _poster: string, _mode: FeedPreloadMode): void {}

  setPreloadMode(_mode: FeedPreloadMode): void {}

  load(): void {}

  play(): Promise<void> {
    return Promise.resolve();
  }

  pause(): void {}

  bufferedAheadMs(): number | undefined {
    return undefined;
  }

  mount(_host: HTMLElement, _className: string): void {}

  unmount(): void {}

  subscribe(_listener: (event: FeedPreloadMediaEvent) => void): () => void {
    return () => {};
  }

  destroy(): void {}

  renderFrame(): void {
    this.frameCallback?.();
  }
}
