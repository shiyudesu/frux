import { describe, expect, it } from "vitest";
import type { FeedSceneKey } from "./constants";
import type { FeedVideo, RecommendationContext } from "./types";
import {
  MAX_RETAINED_FEED_ITEMS_PER_SCENE,
  activateFeedSceneSnapshot,
  compactFeedSceneSnapshot,
  createFeedSceneSnapshot,
  feedAuthIdentity,
  patchFeedSceneSnapshots,
  removeFeedSceneSnapshot,
  replaceFeedSceneSnapshot,
  setFeedSceneSnapshotIndex,
  updateFeedSceneSnapshot
} from "./feedSceneState";

describe("feed scene snapshots", () => {
  it("restores the active item by video identity instead of trusting a stale index", () => {
    const snapshot = {
      ...sceneSnapshot("timeline", [video(1), video(2), video(3)], 1),
      index: 0
    };

    const restored = activateFeedSceneSnapshot(snapshot, "timeline", "anonymous");

    expect(restored?.index).toBe(1);
    expect(restored?.activeVideoID).toBe(2);
  });

  it("accepts a committed empty scene and rejects another authentication identity", () => {
    const empty = sceneSnapshot("timeline", [], 0);

    expect(activateFeedSceneSnapshot(empty, "timeline", "anonymous")?.items).toEqual([]);
    expect(activateFeedSceneSnapshot(empty, "timeline", "7:token")).toBeNull();
  });

  it("compacts a safe pagination suffix while preserving the active item", () => {
    const items = Array.from({ length: MAX_RETAINED_FEED_ITEMS_PER_SCENE + 5 }, (_, index) => video(index + 1));
    const snapshot = sceneSnapshot("timeline", items, items.length - 3);

    const compacted = compactFeedSceneSnapshot(snapshot);

    expect(compacted?.items).toHaveLength(MAX_RETAINED_FEED_ITEMS_PER_SCENE);
    expect(compacted?.activeVideoID).toBe(items.at(-3)?.video_id);
    expect(compacted?.items.at(-1)?.video_id).toBe(items.at(-1)?.video_id);
    expect(compacted?.nextCursor).toBe("next");
  });

  it("rejects compaction when the active item is outside the retained suffix", () => {
    const items = Array.from({ length: MAX_RETAINED_FEED_ITEMS_PER_SCENE + 5 }, (_, index) => video(index + 1));

    expect(compactFeedSceneSnapshot(sceneSnapshot("timeline", items, 0))).toBeNull();
  });

  it("updates the active identity when the scene index changes", () => {
    const updated = setFeedSceneSnapshotIndex(
      sceneSnapshot("timeline", [video(1), video(2)], 0),
      1
    );

    expect(updated.index).toBe(1);
    expect(updated.activeVideoID).toBe(2);
  });

  it("patches duplicate videos across scenes without touching unrelated cards", () => {
    const timeline = sceneSnapshot("timeline", [video(1), video(2)], 0);
    const hot = sceneSnapshot("hot", [video(2), video(3)], 0);
    const patched = patchFeedSceneSnapshots(
      replaceFeedSceneSnapshot(replaceFeedSceneSnapshot({}, timeline), hot),
      2,
      { item: { like_count: 9 }, liked: true }
    );

    expect(patched.timeline?.items[1].like_count).toBe(9);
    expect(patched.timeline?.liked[2]).toBe(true);
    expect(patched.hot?.items[0].like_count).toBe(9);
    expect(patched.hot?.liked[2]).toBe(true);
    expect(patched.hot?.items[1].like_count).toBe(0);
  });

  it("replaces or invalidates only the selected scene", () => {
    const snapshots = replaceFeedSceneSnapshot(
      replaceFeedSceneSnapshot({}, sceneSnapshot("timeline", [video(1)], 0)),
      sceneSnapshot("hot", [video(2)], 0)
    );
    const refreshed = updateFeedSceneSnapshot(snapshots, "timeline", () =>
      sceneSnapshot("timeline", [video(3)], 0)
    );
    const invalidated = removeFeedSceneSnapshot(refreshed, "timeline");

    expect(refreshed.timeline?.activeVideoID).toBe(3);
    expect(refreshed.hot?.activeVideoID).toBe(2);
    expect(invalidated.timeline).toBeUndefined();
    expect(invalidated.hot?.activeVideoID).toBe(2);
  });

  it("binds recommendation snapshots to their resolved request context", () => {
    const valid = sceneSnapshot("recommend", [video(1, "recommend")], 0);
    const invalid = {
      ...valid,
      recommendation: {
        ...valid.recommendation!,
        context: recommendationContext("another-request")
      }
    };

    expect(activateFeedSceneSnapshot(valid, "recommend", "7:token")?.requestID).toBe("request");
    expect(activateFeedSceneSnapshot(invalid, "recommend", "7:token")).toBeNull();
  });

  it("creates stable authentication keys for anonymous and authenticated sessions", () => {
    expect(feedAuthIdentity("", 0)).toBe("anonymous");
    expect(feedAuthIdentity("token", 7)).toBe("7:token");
  });
});

function sceneSnapshot(
  scene: FeedSceneKey,
  items: FeedVideo[],
  index: number
) {
  const authenticated = scene === "recommend" || scene === "following";
  return createFeedSceneSnapshot({
    scene,
    authIdentity: authenticated ? "7:token" : "anonymous",
    items,
    index,
    liked: Object.fromEntries(items.map((item) => [item.video_id, item.liked])),
    favorited: Object.fromEntries(items.map((item) => [item.video_id, item.favorited])),
    nextCursor: "next",
    hasMore: true,
    requestID: "request",
    recommendation: scene === "recommend"
      ? {
          sessionID: "session",
          nextRefreshIndex: 1,
          context: recommendationContext("request"),
          suppressedVideoIDs: [],
          suppressedAuthorIDs: []
        }
      : undefined
  });
}

function recommendationContext(requestID: string): RecommendationContext {
  return {
    request_id: requestID,
    session_id: "session",
    refresh_index: 0,
    recent_video_ids: [],
    current_video_id: 0,
    network_class: "unknown",
    save_data: false,
    viewport_class: "large",
    playback_capabilities: []
  };
}

function video(id: number, scene = "timeline"): FeedVideo {
  return {
    video_id: id,
    author_id: id + 100,
    title: `video ${id}`,
    media_url: `/video-${id}.mp4`,
    cover_url: `/cover-${id}.jpg`,
    like_count: 0,
    comment_count: 0,
    favorite_count: 0,
    liked: false,
    favorited: false,
    author: `author ${id}`,
    avatar_url: "",
    description: "",
    feed_scene: scene,
    request_id: "request"
  };
}
