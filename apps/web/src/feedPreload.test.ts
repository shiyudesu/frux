import { describe, expect, it } from "vitest";
import {
  deriveEffectiveFeedPreloadPolicy,
  deriveFeedPreloadCandidates,
  playbackNetworkType,
  shouldLoadMoreForPreload,
  type FeedPreloadEnvironment,
  type FeedPreloadGeneration
} from "./feedPreload";
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
  requestGeneration: 2,
  authGeneration: 1
};

describe("deriveEffectiveFeedPreloadPolicy", () => {
  it.each([
    {
      name: "WiFi",
      environment: environment({ connectionType: "wifi" }),
      expected: { networkClass: "fast", forwardCount: 3, immediateMode: "buffer", remainingMode: "buffer" }
    },
    {
      name: "5G",
      environment: environment({ connectionType: "5g" }),
      expected: { networkClass: "fast", forwardCount: 3, immediateMode: "buffer", remainingMode: "buffer" }
    },
    {
      name: "4G",
      environment: environment({ connectionType: "cellular", effectiveType: "4g" }),
      expected: { networkClass: "default", forwardCount: 2, immediateMode: "buffer", remainingMode: "metadata" }
    },
    {
      name: "default",
      environment: environment({}),
      expected: { networkClass: "default", forwardCount: 2, immediateMode: "buffer", remainingMode: "metadata" }
    },
    {
      name: "slow network",
      environment: environment({ effectiveType: "3g" }),
      expected: { networkClass: "slow", forwardCount: 1, immediateMode: "metadata", remainingMode: "cover" }
    },
    {
      name: "offline",
      environment: environment({ online: false }),
      expected: { networkClass: "offline", forwardCount: 3, immediateMode: "cover", remainingMode: "cover" }
    },
    {
      name: "save data",
      environment: environment({ saveData: true }),
      expected: { networkClass: "save-data", forwardCount: 3, immediateMode: "cover", remainingMode: "cover" }
    }
  ])("normalizes the $name policy", ({ environment: input, expected }) => {
    expect(deriveEffectiveFeedPreloadPolicy(config, input)).toMatchObject(expected);
  });

  it("bounds prepared resources on low-memory devices", () => {
    const policy = deriveEffectiveFeedPreloadPolicy(config, environment({ connectionType: "wifi", deviceMemoryGB: 2 }));
    expect(policy.forwardCount).toBe(1);
    expect(policy.maxResources).toBe(3);
  });

  it("maps browser signals to the playback-config network contract", () => {
    expect(playbackNetworkType(environment({ connectionType: "wifi" }))).toBe("WiFi");
    expect(playbackNetworkType(environment({ connectionType: "5g" }))).toBe("5G");
    expect(playbackNetworkType(environment({ effectiveType: "4g" }))).toBe("4G");
    expect(playbackNetworkType(environment({ effectiveType: "3g" }))).toBe("3G");
    expect(playbackNetworkType(environment({}))).toBe("DEFAULT");
  });
});

describe("deriveFeedPreloadCandidates", () => {
  it.each(["recommend", "hot", "following", "timeline"])(
    "keeps %s candidates in the exact Feed order",
    (scene) => {
      const items = [feedVideo(11, scene), feedVideo(22, scene), feedVideo(33, scene), feedVideo(44, scene)];
      const policy = deriveEffectiveFeedPreloadPolicy(config, environment({ connectionType: "wifi" }));

      const candidates = deriveFeedPreloadCandidates(items, 1, { ...generation, scene }, policy);

      expect(candidates.map((candidate) => candidate.item.video_id)).toEqual([11, 22, 33, 44]);
      expect(candidates.map((candidate) => candidate.role)).toEqual(["previous", "active", "forward", "forward"]);
    }
  );

  it("uses source revisions to distinguish replaced signed media URLs", () => {
    const policy = deriveEffectiveFeedPreloadPolicy(config, environment({ effectiveType: "4g" }));
    const first = deriveFeedPreloadCandidates([feedVideo(11, "timeline", "a")], 0, generation, policy);
    const second = deriveFeedPreloadCandidates([feedVideo(11, "timeline", "b")], 0, generation, policy);

    expect(first[0].key.sourceRevision).not.toBe(second[0].key.sourceRevision);
  });

  it("invalidates pooled resources when adaptive source metadata changes", () => {
    const policy = deriveEffectiveFeedPreloadPolicy(config, environment({ effectiveType: "4g" }));
    const firstItem = {
      ...feedVideo(11, "timeline"),
      playback_sources: [
        { type: "dash" as const, url: "https://media.example/11.mpd?v=1", quality: "auto" }
      ]
    };
    const secondItem = {
      ...firstItem,
      playback_sources: [
        { type: "dash" as const, url: "https://media.example/11.mpd?v=2", quality: "auto" }
      ]
    };

    const first = deriveFeedPreloadCandidates([firstItem], 0, generation, policy);
    const second = deriveFeedPreloadCandidates([secondItem], 0, generation, policy);

    expect(first[0].key.sourceRevision).not.toBe(second[0].key.sourceRevision);
  });
});

describe("shouldLoadMoreForPreload", () => {
  it("loads the next page before the forward window crosses the loaded boundary", () => {
    expect(
      shouldLoadMoreForPreload({
        ready: true,
        hasMore: true,
        loadingMore: false,
        itemCount: 10,
        activeIndex: 6,
        forwardCount: 3
      })
    ).toBe(true);
  });

  it("does not duplicate pagination while loading or before the boundary", () => {
    expect(
      shouldLoadMoreForPreload({
        ready: true,
        hasMore: true,
        loadingMore: true,
        itemCount: 10,
        activeIndex: 6,
        forwardCount: 3
      })
    ).toBe(false);
    expect(
      shouldLoadMoreForPreload({
        ready: true,
        hasMore: true,
        loadingMore: false,
        itemCount: 10,
        activeIndex: 5,
        forwardCount: 3
      })
    ).toBe(false);
  });
});

function environment(overrides: Partial<FeedPreloadEnvironment>): FeedPreloadEnvironment {
  return {
    online: true,
    saveData: false,
    effectiveType: "",
    connectionType: "",
    ...overrides
  };
}

function feedVideo(videoID: number, scene: string, revision = "a"): FeedVideo {
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
    feed_scene: scene,
    request_id: generation.requestID
  };
}
