import { describe, expect, it } from "vitest";
import {
  deriveEffectiveFeedPreloadPolicy,
  deriveFeedPreloadCandidates,
  feedPreloadResourceKey,
  type AcquiredFeedPreloadResource,
  type EffectiveFeedPreloadPolicy,
  type FeedPreloadCandidate,
  type FeedPreloadGeneration,
  type FeedPreloadMediaEvent,
  type FeedPreloadMediaResource,
  type FeedPreloadMode,
  type FeedPreloadReadiness
} from "../feedPreload";
import type { FeedVideo, PlaybackConfig } from "../types";
import {
  FeedPlayerPool,
  MAX_FEED_PLAYER_POOL_SLOTS,
  type FeedPlayerPoolController
} from "./feedPlayerPool";

const config: PlaybackConfig = {
  platform: "Web",
  network_type: "WiFi",
  preload_count: 4,
  buffer_ms: 1200
};

const generationOne: FeedPreloadGeneration = {
  scene: "recommend",
  requestID: "request-1",
  requestGeneration: 1,
  authGeneration: 1
};

const generationTwo: FeedPreloadGeneration = {
  ...generationOne,
  requestID: "request-2",
  requestGeneration: 2
};

const policy = deriveEffectiveFeedPreloadPolicy(config, {
  online: true,
  saveData: false,
  effectiveType: "",
  connectionType: "wifi"
});

class FakeMedia implements FeedPreloadMediaResource {
  currentTime = 0;
  duration = 0;
  paused = true;
  ended = false;
  muted = true;
  readyState = 0;

  configure(): void {}
  setPreloadMode(_mode: FeedPreloadMode): void {}
  load(): void {}
  play(): Promise<void> {
    this.paused = false;
    return Promise.resolve();
  }
  pause(): void {
    this.paused = true;
  }
  bufferedAheadMs(): number | undefined {
    return undefined;
  }
  mount(_host: HTMLElement, _className: string): void {}
  unmount(): void {}
  subscribe(_listener: (event: FeedPreloadMediaEvent) => void): () => void {
    return () => {};
  }
  destroy(): void {}
}

interface FakeHandleRecord {
  media: FakeMedia;
  readiness: FeedPreloadReadiness;
  bufferedMs: number;
  releaseCount: number;
}

class FakeController implements FeedPlayerPoolController {
  readonly syncCalls: { candidates: FeedPreloadCandidate[]; policy: EffectiveFeedPreloadPolicy }[] = [];
  readonly acquisitionCount = new Map<string, number>();
  readonly records: FakeHandleRecord[] = [];
  destroyCount = 0;

  sync(candidates: FeedPreloadCandidate[], effectivePolicy: EffectiveFeedPreloadPolicy): void {
    this.syncCalls.push({ candidates: [...candidates], policy: effectivePolicy });
  }

  acquireCandidate(candidate: FeedPreloadCandidate): AcquiredFeedPreloadResource {
    const key = feedPreloadResourceKey(candidate.key);
    this.acquisitionCount.set(key, (this.acquisitionCount.get(key) ?? 0) + 1);
    const media = new FakeMedia();
    const record: FakeHandleRecord = {
      media,
      readiness: "loading",
      bufferedMs: 0,
      releaseCount: 0
    };
    const handle: AcquiredFeedPreloadResource = {
      key: candidate.key,
      media,
      get readiness() {
        return record.readiness;
      },
      get bufferedMs() {
        return record.bufferedMs;
      },
      release: () => {
        record.releaseCount += 1;
      }
    };
    this.records.push(record);
    return handle;
  }

  destroy(): void {
    this.destroyCount += 1;
  }
}

describe("FeedPlayerPool", () => {
  it("retains handles across forward and backward adjacent rotations", () => {
    const controller = new FakeController();
    const pool = new FeedPlayerPool(controller);
    const items = videos([11, 22, 33, 44, 55]);
    const first = deriveFeedPreloadCandidates(items, 1, generationOne, policy);
    pool.synchronize(first, policy);
    const video22 = pool.getResourceByVideoID(22);
    const video33 = pool.getResourceByVideoID(33);

    const forward = deriveFeedPreloadCandidates(items, 2, generationOne, policy);
    pool.synchronize(forward, policy);
    expect(pool.getResourceByVideoID(22)?.media).toBe(video22?.media);
    expect(pool.getResourceByVideoID(22)?.role).toBe("previous");
    expect(pool.getResourceByVideoID(33)?.media).toBe(video33?.media);
    expect(pool.getResourceByVideoID(33)?.role).toBe("current");
    expect(pool.getResourceByVideoID(11)).toBeUndefined();

    pool.synchronize(first, policy);
    expect(pool.getResourceByVideoID(22)?.media).toBe(video22?.media);
    expect(pool.getResourceByVideoID(33)?.media).toBe(video33?.media);
    expect(pool.getResourceByVideoID(22)?.role).toBe("current");
    expect(pool.getResourceByVideoID(33)?.role).toBe("next");
    expect(controller.acquisitionCount.get(video22?.resourceKey ?? "")).toBe(1);
    expect(controller.acquisitionCount.get(video33?.resourceKey ?? "")).toBe(1);
  });

  it("ignores retired and mixed stale generation candidates", () => {
    const controller = new FakeController();
    const pool = new FeedPlayerPool(controller);
    const items = videos([11, 22, 33, 44]);
    const stale = deriveFeedPreloadCandidates(items, 1, generationOne, policy);
    const current = deriveFeedPreloadCandidates(items, 1, generationTwo, policy);
    pool.synchronize(stale, policy);
    pool.synchronize(current, policy);
    const currentKeys = pool.listResources().map((resource) => resource.resourceKey);
    const syncCount = controller.syncCalls.length;

    pool.synchronize(stale, policy);
    expect(pool.generation).toBe(current[0]?.key.generation);
    expect(pool.listResources().map((resource) => resource.resourceKey)).toEqual(currentKeys);
    expect(controller.syncCalls).toHaveLength(syncCount);

    const mixed = [...current, stale.find((candidate) => candidate.role === "previous")!];
    pool.synchronize(mixed, policy);
    expect(pool.listResources().every((resource) => resource.candidate.key.generation === pool.generation)).toBe(
      true
    );
  });

  it("enforces a hard three-slot maximum and bounds the controller policy", () => {
    const controller = new FakeController();
    const pool = new FeedPlayerPool(controller);
    const candidates = deriveFeedPreloadCandidates(videos([1, 2, 3, 4, 5, 6, 7]), 2, generationOne, policy);

    const resources = pool.synchronize(candidates, policy);

    expect(resources).toHaveLength(MAX_FEED_PLAYER_POOL_SLOTS);
    expect(resources.map((resource) => resource.role)).toEqual(["previous", "current", "next"]);
    expect(resources.map((resource) => resource.videoID)).toEqual([2, 3, 4]);
    expect(controller.syncCalls[0]?.candidates).toHaveLength(3);
    expect(controller.syncCalls[0]?.policy.maxResources).toBe(3);
    expect(controller.records).toHaveLength(3);
  });

  it("prioritizes the current slot when effective policy permits one resource", () => {
    const controller = new FakeController();
    const pool = new FeedPlayerPool(controller);
    const singleResourcePolicy = { ...policy, maxResources: 1 };
    const candidates = deriveFeedPreloadCandidates(videos([1, 2, 3, 4]), 1, generationOne, singleResourcePolicy);

    pool.synchronize(candidates, singleResourcePolicy);

    expect(pool.size).toBe(1);
    expect(pool.getResourceByRole("current")?.videoID).toBe(2);
    expect(pool.getResourceByRole("previous")).toBeUndefined();
    expect(pool.getResourceByRole("next")).toBeUndefined();
  });

  it("uses the full resource key, exposes dynamic readiness, and replaces source revisions", () => {
    const controller = new FakeController();
    const pool = new FeedPlayerPool(controller);
    const first = deriveFeedPreloadCandidates(videos([11, 22, 33], "a"), 1, generationOne, policy);
    pool.synchronize(first, policy);
    const current = pool.getResourceByRole("current");
    if (!current) throw new Error("Expected current pool resource.");
    const record = controller.records.find((candidate) => candidate.media === current.media);
    if (!record) throw new Error("Expected acquired record.");
    record.readiness = "ready";
    record.bufferedMs = 1400;

    expect(pool.getResourceByKey(current.resourceKey)).toMatchObject({
      videoID: 22,
      role: "current",
      readiness: "ready",
      bufferedMs: 1400
    });

    const revised = deriveFeedPreloadCandidates(videos([11, 22, 33], "b"), 1, generationOne, policy);
    pool.synchronize(revised, policy);
    expect(pool.getResourceByKey(current.resourceKey)).toBeUndefined();
    expect(pool.getResourceByVideoID(22)?.resourceKey).not.toBe(current.resourceKey);
    expect(record.releaseCount).toBe(1);
  });

  it("releases resources leaving the window and destroys all resources on cleanup", () => {
    const controller = new FakeController();
    const pool = new FeedPlayerPool(controller);
    const candidates = deriveFeedPreloadCandidates(videos([11, 22, 33, 44]), 1, generationOne, policy);
    pool.synchronize(candidates, policy);
    const initialRecords = [...controller.records];

    expect(pool.synchronize([], policy)).toEqual([]);
    expect(initialRecords.every((record) => record.releaseCount === 1)).toBe(true);
    expect(pool.size).toBe(0);
    expect(controller.syncCalls.at(-1)?.candidates).toEqual([]);

    pool.synchronize(candidates, policy);
    const activeRecords = controller.records.slice(initialRecords.length);
    pool.destroy();
    pool.destroy();

    expect(activeRecords.every((record) => record.releaseCount === 1)).toBe(true);
    expect(controller.destroyCount).toBe(1);
    expect(pool.size).toBe(0);
    expect(pool.synchronize(candidates, policy)).toEqual([]);
  });
});

function videos(ids: readonly number[], revision = "a"): FeedVideo[] {
  return ids.map((id) => ({
    video_id: id,
    author_id: id + 100,
    title: `video-${id}`,
    media_url: `https://media.example/${id}.mp4?revision=${revision}`,
    cover_url: `https://media.example/${id}.jpg`,
    like_count: 0,
    comment_count: 0,
    favorite_count: 0,
    liked: false,
    favorited: false,
    author: `author-${id}`,
    avatar_url: "",
    description: "",
    feed_scene: generationOne.scene,
    request_id: generationOne.requestID
  }));
}
