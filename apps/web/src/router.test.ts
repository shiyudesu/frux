import { describe, expect, it } from "vitest";
import {
  normalizeRoute,
  videoDiscussionFromLocation,
  videoDiscussionPath
} from "./router";

describe("typed video discussion routing", () => {
  it("authors and reloads valid root and reply targets", () => {
    const path = videoDiscussionPath({ route: "/videos/42", comment: 7, highlight: 9 });
    expect(path).toBe("/videos/42?comment=7&highlight=9");
    const url = new URL(path, "https://gcfeed.test");
    const route = normalizeRoute(url.pathname);
    expect(videoDiscussionFromLocation(route, url.search)).toEqual({
      videoID: 42,
      commentID: 7,
      highlightID: 9,
      invalidFocus: false
    });
  });

  it("keeps the video route safe for malformed and inconsistent search parameters", () => {
    expect(videoDiscussionFromLocation("/videos/42", "?comment=nope&highlight=9")).toMatchObject({
      videoID: 42,
      invalidFocus: true
    });
    expect(videoDiscussionFromLocation("/videos/42", "?highlight=9")).toMatchObject({
      commentID: 0,
      highlightID: 9,
      invalidFocus: true
    });
    expect(videoDiscussionFromLocation("/videos/42", "?comment=-2")).toMatchObject({ invalidFocus: true });
  });
});
