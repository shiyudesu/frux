import { describe, expect, it } from "vitest";
import { creatorVideoStatusLabel } from "./utils";
import type { Video, VideoStatus } from "./types";

function video(status: VideoStatus, mediaStatus: Video["media_status"] = "ready"): Video {
  return {
    id: 1,
    author_id: 2,
    title: "video",
    description: "",
    media_url: "",
    cover_url: "",
    status,
    visibility: "public",
    like_count: 0,
    comment_count: 0,
    favorite_count: 0,
    created_at: "2026-08-05T00:00:00Z",
    updated_at: "2026-08-05T00:00:00Z",
    media_status: mediaStatus
  };
}

describe("creatorVideoStatusLabel", () => {
  it("shows review-aware lifecycle labels", () => {
    expect(creatorVideoStatusLabel(video(5))).toBe("审核中");
    expect(creatorVideoStatusLabel(video(5, "processing"))).toBe("处理中，等待审核");
    expect(creatorVideoStatusLabel(video(6))).toBe("未通过");
    expect(creatorVideoStatusLabel(video(2))).toBe("已发布");
  });
});
