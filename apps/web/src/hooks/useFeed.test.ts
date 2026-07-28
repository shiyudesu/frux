import { describe, expect, it } from "vitest";
import type { FeedVideo } from "../types";
import {
  filterSuppressedFeedItems,
  isFeedbackOriginCurrent,
  removeAcceptedFeedbackItems,
  resolveFeedRequestID,
  shouldApplyAcceptedRecommendationFeedback,
  shouldCancelSwipeForAcceptedFeedback,
  shouldAutoLoadSuppressedEmptyPage
} from "./useFeed";

const items: FeedVideo[] = [
  feedItem(1, 10),
  feedItem(2, 20),
  feedItem(3, 20),
  feedItem(4, 30)
];

describe("removeAcceptedFeedbackItems", () => {
  it("uses the server-resolved recommendation ID while retaining response compatibility", () => {
    expect(resolveFeedRequestID("recommend", "client-request", "srv-request")).toBe("srv-request");
    expect(resolveFeedRequestID("recommend", "client-request", "")).toBe("client-request");
    expect(resolveFeedRequestID("timeline", "client-request", "srv-request")).toBe("client-request");
  });

  it("removes an accepted video and keeps the active position on its successor", () => {
    const result = removeAcceptedFeedbackItems(items, 1, items[1], "not_interested");

    expect(result.items.map((item) => item.video_id)).toEqual([1, 3, 4]);
    expect(result.index).toBe(1);
    expect(result.removedActive).toBe(true);
  });

  describe("feedback suppression pagination guards", () => {
    it("filters delayed pagination results using persistent video and author suppressions", () => {
      const filtered = filterSuppressedFeedItems(
        items,
        new Set([1]),
        new Set([20])
      );

      expect(filtered.map((item) => item.video_id)).toEqual([4]);
    });

    it("does not apply recommendation suppressions to another feed scene", () => {
      const filtered = filterSuppressedFeedItems(
        items,
        new Set([1]),
        new Set([20]),
        "following"
      );

      expect(filtered.map((item) => item.video_id)).toEqual([1, 2, 3, 4]);
    });

    it("continues only bounded empty-page refill attempts", () => {
      expect(shouldAutoLoadSuppressedEmptyPage(0, true, "cursor", false, 0)).toBe(true);
      expect(shouldAutoLoadSuppressedEmptyPage(1, true, "cursor", false, 0)).toBe(false);
      expect(shouldAutoLoadSuppressedEmptyPage(0, true, "cursor", true, 0)).toBe(false);
      expect(shouldAutoLoadSuppressedEmptyPage(0, true, "cursor", false, 8)).toBe(false);
    });

    it("rejects late feedback from an obsolete request, generation, or scene", () => {
      const origin = { generation: 3, scene: "recommend", requestID: "request-3" };

      expect(isFeedbackOriginCurrent(origin, origin)).toBe(true);
      expect(isFeedbackOriginCurrent(origin, { ...origin, generation: 4 })).toBe(false);
      expect(isFeedbackOriginCurrent(origin, { ...origin, scene: "following" })).toBe(false);
      expect(isFeedbackOriginCurrent(origin, { ...origin, requestID: "request-4" })).toBe(false);
    });

    it("applies successful feedback only to the originating authenticated recommendation session", () => {
      const origin = { scene: "recommend", token: "token-a", userID: 7 };
      expect(shouldApplyAcceptedRecommendationFeedback(origin, origin)).toBe(true);
      expect(shouldApplyAcceptedRecommendationFeedback(origin, { ...origin, scene: "following" })).toBe(false);
      expect(shouldApplyAcceptedRecommendationFeedback(origin, { ...origin, token: "token-b" })).toBe(false);
      expect(shouldApplyAcceptedRecommendationFeedback(origin, { ...origin, token: "token-b", userID: 8 })).toBe(false);
      expect(shouldApplyAcceptedRecommendationFeedback(origin, { scene: "recommend", token: "", userID: 0 })).toBe(false);

      const refreshedItems = [feedItem(2, 20), feedItem(3, 20), feedItem(4, 30)];
      const filtered = filterSuppressedFeedItems(refreshedItems, new Set(), new Set([20]));
      expect(filtered.map((item) => item.video_id)).toEqual([4]);
    });
  });

  it("removes every loaded item by an accepted reduced author", () => {
    const result = removeAcceptedFeedbackItems(items, 2, items[2], "reduce_author");

    expect(result.items.map((item) => item.video_id)).toEqual([1, 4]);
    expect(result.index).toBe(1);
    expect(result.removedActive).toBe(true);
  });

  it("supports removing the final item into the empty state", () => {
    const result = removeAcceptedFeedbackItems([items[0]], 0, items[0], "already_seen");

    expect(result.items).toEqual([]);
    expect(result.index).toBe(0);
  });

  it("keeps an unrelated current swipe when delayed feedback removes an old item", () => {
    const removal = removeAcceptedFeedbackItems(items, 1, items[0], "not_interested");
    const transition = {
      from: { videoID: items[1].video_id, authorID: items[1].author_id },
      to: { videoID: items[2].video_id, authorID: items[2].author_id }
    };

    expect(removal.removedActive).toBe(false);
    expect(shouldCancelSwipeForAcceptedFeedback(removal, items[0], "not_interested", transition)).toBe(false);
  });

  it("cancels a swipe only when accepted feedback removes a transition target", () => {
    const removal = removeAcceptedFeedbackItems(items, 1, items[2], "not_interested");
    const transition = {
      from: { videoID: items[1].video_id, authorID: items[1].author_id },
      to: { videoID: items[2].video_id, authorID: items[2].author_id }
    };

    expect(shouldCancelSwipeForAcceptedFeedback(removal, items[2], "not_interested", transition)).toBe(true);
  });
});

function feedItem(videoID: number, authorID: number): FeedVideo {
  return {
    video_id: videoID,
    author_id: authorID,
    title: "",
    media_url: "",
    cover_url: "",
    like_count: 0,
    comment_count: 0,
    favorite_count: 0,
    liked: false,
    favorited: false,
    author: "",
    avatar_url: "",
    description: "",
    feed_scene: "recommend",
    request_id: "request"
  };
}
