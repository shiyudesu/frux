import { describe, expect, it, vi } from "vitest";
import type { Message } from "../types";
import { messageDiscussionTarget, messageIcon, messageTypeLabel } from "../utils";
import { videoDiscussionPath } from "../router";
import { activateMessageNavigation } from "./MessagesPage";

describe("comment message navigation", () => {
  it("renders comment message types distinctly and navigates only after read succeeds", async () => {
    expect(messageTypeLabel("COMMENT")).toBe("新评论");
    expect(messageTypeLabel("COMMENT_REPLY")).toBe("新回复");
    expect(messageTypeLabel("COMMENT_LIKE")).toBe("评论获赞");
    expect(messageIcon("COMMENT_REPLY")).toBe("reply");
    expect(messageIcon("COMMENT_LIKE")).toBe("heart");

    const message = targetedMessage();
    const order: string[] = [];
    const markRead = vi.fn(async () => {
      order.push("read");
      return true;
    });
    const navigate = vi.fn((target) => {
      order.push(videoDiscussionPath(typeof target === "string" ? { route: "/videos/1" } : target));
    });

    await activateMessageNavigation(message, markRead, navigate);
    expect(order).toEqual(["read", "/videos/3?comment=7&highlight=9"]);
  });

  it("keeps legacy and removed targets readable without invalid navigation", async () => {
    const legacy = { ...targetedMessage(), video_id: undefined, comment_id: undefined, root_comment_id: undefined };
    expect(messageDiscussionTarget(legacy)).toBeNull();
    const navigate = vi.fn();
    expect(await activateMessageNavigation(legacy, async () => true, navigate)).toBe(true);
    expect(navigate).not.toHaveBeenCalled();

    const removed = { ...targetedMessage(), comment_id: 0, root_comment_id: 0 };
    expect(messageDiscussionTarget(removed)).toBeNull();
    expect(await activateMessageNavigation(targetedMessage(), async () => false, navigate)).toBe(false);
    expect(navigate).not.toHaveBeenCalled();
  });
});

function targetedMessage(): Message {
  return {
    id: 1,
    user_id: 2,
    type: "COMMENT_REPLY",
    title: "有人回复了你",
    content: "回复内容",
    video_id: 3,
    root_comment_id: 7,
    comment_id: 9,
    is_read: false,
    created_at: "2026-08-03T00:00:00Z"
  };
}
