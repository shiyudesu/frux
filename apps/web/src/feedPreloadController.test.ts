import { describe, expect, it } from "vitest";
import {
  deriveEffectiveFeedPreloadPolicy,
  deriveFeedPreloadCandidates,
  type FeedPreloadEnvironment,
  type FeedPreloadGeneration,
  type FeedPreloadMediaEvent,
  type FeedPreloadMediaResource,
  type FeedPreloadMode
} from "./feedPreload";
import { FeedPreloadController } from "./feedPreloadController";
import type { FeedVideo, PlaybackConfig } from "./types";

const config: PlaybackConfig = {
  platform: "Web",
  network_type: "DEFAULT",
  preload_count: 3,
  buffer_ms: 1200
};

const generation: FeedPreloadGeneration = {
  scene: "recommend",
  requestID: "request-1",
  requestGeneration: 1,
  authGeneration: 1
};

describe("FeedPreloadController", () => {
  it("prepares bounded active, previous, and forward resources", () => {
    const harness = createHarness();
    const candidates = candidateWindow([11, 22, 33, 44, 55], 1);

    harness.controller.sync(candidates, harness.policy);

    expect(harness.media).toHaveLength(5);
    expect(harness.controller.getDebugState()).toMatchObject({
      attempts: 5,
      activeResources: 5,
      acquiredResources: 0
    });
  });

  it("marks buffered and canplay fallback resources ready", () => {
    const harness = createHarness();
    const candidates = candidateWindow([11, 22], 0);
    harness.controller.sync(candidates, harness.policy);

    harness.media[1].bufferedMs = 1500;
    harness.media[1].readyStateValue = 3;
    harness.media[1].emit("progress");
    expect(harness.controller.acquire(candidates[1].key)?.readiness).toBe("ready");

    const fallbackHarness = createHarness();
    const fallbackCandidates = candidateWindow([11, 22], 0);
    fallbackHarness.controller.sync(fallbackCandidates, fallbackHarness.policy);
    fallbackHarness.media[1].readyStateValue = 3;
    fallbackHarness.media[1].emit("canplay");
    expect(fallbackHarness.controller.acquire(fallbackCandidates[1].key)?.readiness).toBe("ready");
  });

  it("reuses a retained resource after release", () => {
    const harness = createHarness();
    const candidates = candidateWindow([11, 22, 33], 0);
    harness.controller.sync(candidates, harness.policy);

    const first = harness.controller.acquire(candidates[1].key);
    first?.release();
    const second = harness.controller.acquire(candidates[1].key);

    expect(second?.media).toBe(first?.media);
    expect(harness.controller.getDebugState().reused).toBe(2);
  });

  it("reprioritizes matching resources without recreating them", () => {
    const harness = createHarness();
    const items = [feedVideo(11), feedVideo(22), feedVideo(33), feedVideo(44)];
    const first = deriveFeedPreloadCandidates(items, 1, generation, harness.policy);
    harness.controller.sync(first, harness.policy);
    const created = [...harness.media];

    const second = deriveFeedPreloadCandidates(items, 2, generation, harness.policy);
    harness.controller.sync(second, harness.policy);

    expect(harness.media).toEqual(created);
    expect(harness.controller.getDebugState().attempts).toBe(created.length);
  });

  it("restarts metadata resources when they become buffered candidates", () => {
    const harness = createHarness();
    const slowPolicy = deriveEffectiveFeedPreloadPolicy(config, environment({ effectiveType: "3g" }));
    const items = [feedVideo(11), feedVideo(22)];
    const metadataCandidates = deriveFeedPreloadCandidates(items, 0, generation, slowPolicy);
    harness.controller.sync(metadataCandidates, slowPolicy);
    const nextMedia = harness.media[1];
    nextMedia.readyStateValue = 1;
    nextMedia.emit("loadedmetadata");

    const bufferedCandidates = deriveFeedPreloadCandidates(items, 0, generation, harness.policy);
    harness.controller.sync(bufferedCandidates, harness.policy);

    expect(nextMedia.mode).toBe("buffer");
    expect(nextMedia.loadCount).toBe(2);
    nextMedia.bufferedMs = 1500;
    nextMedia.readyStateValue = 3;
    nextMedia.emit("progress");
    expect(harness.controller.acquire(bufferedCandidates[1].key)?.readiness).toBe("ready");
  });

  it("cancels stale generations and source revisions", () => {
    const harness = createHarness();
    const first = candidateWindow([11, 22], 0);
    harness.controller.sync(first, harness.policy);
    const oldMedia = [...harness.media];

    const nextGeneration = { ...generation, requestGeneration: 2, requestID: "request-2" };
    const second = deriveFeedPreloadCandidates([feedVideo(11), feedVideo(22)], 0, nextGeneration, harness.policy);
    harness.controller.sync(second, harness.policy);

    expect(oldMedia.every((media) => media.destroyed)).toBe(true);
    expect(harness.controller.getDebugState().cancellations).toBe(2);

    const revised = deriveFeedPreloadCandidates([feedVideo(11, "b"), feedVideo(22, "b")], 0, nextGeneration, harness.policy);
    const priorRevision = harness.media.slice(-2);
    harness.controller.sync(revised, harness.policy);
    expect(priorRevision.every((media) => media.destroyed)).toBe(true);
  });

  it("isolates failures and suppresses immediate retries", () => {
    const harness = createHarness();
    const candidates = candidateWindow([11, 22], 0);
    harness.controller.sync(candidates, harness.policy);
    const failedMedia = harness.media[1];

    failedMedia.emit("error");
    harness.controller.sync(candidates, harness.policy);
    expect(harness.media).toHaveLength(2);
    expect(harness.controller.getDebugState().failures).toBe(1);

    harness.advance(harness.policy.retryCooldownMs + 1);
    harness.controller.sync(candidates, harness.policy);
    expect(harness.media).toHaveLength(3);
    expect(harness.media[2]).not.toBe(failedMedia);
  });

  it("does not carry retry cooldown into a new generation", () => {
    const harness = createHarness();
    const first = candidateWindow([11, 22], 0);
    harness.controller.sync(first, harness.policy);
    harness.media[1].emit("error");

    const nextGeneration = { ...generation, requestGeneration: 2, requestID: "request-2" };
    const second = deriveFeedPreloadCandidates([feedVideo(11), feedVideo(22)], 0, nextGeneration, harness.policy);
    harness.controller.sync(second, harness.policy);

    expect(harness.media).toHaveLength(4);
  });

  it("times out buffering work that never reaches the target", () => {
    const harness = createHarness();
    const candidates = candidateWindow([11, 22], 0);
    harness.controller.sync(candidates, harness.policy);
    const active = harness.controller.acquire(candidates[0].key);
    harness.media[1].readyStateValue = 1;
    harness.media[1].emit("loadedmetadata");

    harness.runTimers();

    expect(harness.controller.getDebugState().failures).toBe(1);
    active?.release();
  });

  it("releases resources outside the retained window", () => {
    const harness = createHarness();
    const first = candidateWindow([11, 22, 33, 44], 1);
    harness.controller.sync(first, harness.policy);
    const removed = harness.media.find((media) => media.source.includes("/11."))!;

    const second = deriveFeedPreloadCandidates([feedVideo(22), feedVideo(33), feedVideo(44)], 1, generation, harness.policy);
    harness.controller.sync(second, harness.policy);

    expect(removed.destroyed).toBe(true);
    expect(harness.controller.getDebugState().activeResources).toBeLessThanOrEqual(harness.policy.maxResources);
  });

  it("does not create media after destruction", () => {
    const harness = createHarness();
    const candidates = candidateWindow([11], 0);
    harness.controller.destroy();

    expect(harness.controller.acquireCandidate(candidates[0], harness.policy)).toBeUndefined();
    expect(harness.media).toHaveLength(0);
  });
});

function createHarness() {
  let now = 100;
  let nextTimerID = 1;
  const timers = new Map<number, () => void>();
  const media: FakeMedia[] = [];
  const policy = deriveEffectiveFeedPreloadPolicy(config, environment({ connectionType: "wifi" }));
  const controller = new FeedPreloadController({
    createMedia: () => {
      const resource = new FakeMedia();
      media.push(resource);
      return resource;
    },
    prepareCover: () => () => {},
    now: () => now,
    setTimer: (callback) => {
      const timerID = nextTimerID++;
      timers.set(timerID, callback);
      return timerID;
    },
    clearTimer: (timerID) => {
      timers.delete(timerID);
    }
  });
  return {
    controller,
    media,
    policy,
    advance: (milliseconds: number) => {
      now += milliseconds;
    },
    runTimers: () => {
      const callbacks = [...timers.values()];
      timers.clear();
      callbacks.forEach((callback) => callback());
    }
  };
}

class FakeMedia implements FeedPreloadMediaResource {
  currentTime = 0;
  duration = 0;
  paused = true;
  ended = false;
  muted = true;
  readyStateValue = 0;
  bufferedMs: number | undefined;
  source = "";
  mode: FeedPreloadMode = "metadata";
  destroyed = false;
  loadCount = 0;
  private readonly subscribers = new Set<(event: FeedPreloadMediaEvent) => void>();

  get readyState(): number {
    return this.readyStateValue;
  }

  configure(url: string, _poster: string, mode: FeedPreloadMode): void {
    this.source = url;
    this.mode = mode;
  }

  setPreloadMode(mode: FeedPreloadMode): void {
    this.mode = mode;
  }

  load(): void {
    this.loadCount += 1;
  }

  play(): Promise<void> {
    this.paused = false;
    return Promise.resolve();
  }

  pause(): void {
    this.paused = true;
  }

  bufferedAheadMs(): number | undefined {
    return this.bufferedMs;
  }

  mount(_host: HTMLElement, _className: string): void {}

  unmount(): void {}

  subscribe(listener: (event: FeedPreloadMediaEvent) => void): () => void {
    this.subscribers.add(listener);
    return () => this.subscribers.delete(listener);
  }

  destroy(): void {
    this.destroyed = true;
    this.subscribers.clear();
  }

  emit(event: FeedPreloadMediaEvent): void {
    for (const subscriber of this.subscribers) {
      subscriber(event);
    }
  }
}

function candidateWindow(videoIDs: number[], activeIndex: number) {
  const harnessPolicy = deriveEffectiveFeedPreloadPolicy(config, environment({ connectionType: "wifi" }));
  return deriveFeedPreloadCandidates(videoIDs.map((videoID) => feedVideo(videoID)), activeIndex, generation, harnessPolicy);
}

function environment(overrides: Partial<FeedPreloadEnvironment>): FeedPreloadEnvironment {
  return {
    online: true,
    saveData: false,
    effectiveType: "",
    connectionType: "",
    ...overrides
  };
}

function feedVideo(videoID: number, revision = "a"): FeedVideo {
  return {
    video_id: videoID,
    author_id: videoID + 100,
    title: `video-${videoID}`,
    media_url: `https://media.example/${videoID}.mp4?revision=${revision}`,
    cover_url: `https://media.example/${videoID}.jpg`,
    like_count: 0,
    comment_count: 0,
    favorite_count: 0,
    liked: false,
    favorited: false,
    author: `author-${videoID}`,
    avatar_url: "",
    description: "",
    feed_scene: generation.scene,
    request_id: generation.requestID
  };
}
