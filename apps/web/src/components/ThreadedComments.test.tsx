import { renderToStaticMarkup } from "react-dom/server";
import { afterEach, describe, expect, it, vi } from "vitest";
import { emptyProfile } from "../constants";
import type { CommentsController } from "../hooks/useComments";
import type { Comment, FeedVideo } from "../types";
import { FeedDetailsPanel } from "./FeedDetailsPanel";
import {
  mergeVisibleReplyIDs,
  requiresModeratorConfirmation,
  ThreadedComments
} from "./ThreadedComments";

describe("threaded comment components", () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("renders the shared desktop panel shell used by responsive drawer and sheet layouts", () => {
    const html = renderToStaticMarkup(
      <FeedDetailsPanel
        item={video()}
        open
        onClose={() => {}}
        user={emptyProfile}
        count={2}
        comments={controller({ roots: [comment(1)] })}
        authenticated
        onOpenUser={() => {}}
      />
    );
    expect(html).toContain('data-ui="details-panel"');
    expect(html).toContain('class="details-panel open"');
    expect(html).toContain('aria-label="关闭评论"');
    expect(html).toContain("热门");
    expect(html).toContain("最新");
  });

  it("switches the shared panel to an accessible dialog for compact and mobile sheet viewports", () => {
    vi.stubGlobal("window", {
      matchMedia: () => ({
        matches: true,
        addEventListener: () => {},
        removeEventListener: () => {}
      })
    });
    const html = renderToStaticMarkup(
      <FeedDetailsPanel
        item={video()}
        open
        onClose={() => {}}
        user={emptyProfile}
        count={0}
        comments={controller()}
        authenticated
        onOpenUser={() => {}}
      />
    );
    expect(html).toContain('role="dialog"');
    expect(html).toContain('aria-modal="true"');
  });

  it("renders tombstones, reply targets, character limits, and unavailable discussions safely", () => {
    const replyTarget = comment(4, { user_nickname: "被回复者" });
    const tombstone = comment(1, {
      user_id: 0,
      user_nickname: "",
      content: "",
      status: 2,
      deleted: true,
      reply_count: 1,
      reply_previews: [comment(2, { root_comment_id: 1, content: "仍可阅读的回复" })]
    });
    const html = renderToStaticMarkup(
      <ThreadedComments
        controller={controller({
          roots: [tombstone],
          entities: {
            1: tombstone,
            2: tombstone.reply_previews[0]!,
            4: replyTarget
          },
          replyTarget,
          draft: "🙂评论",
          draftLength: 3,
          focusUnavailable: true
        })}
        authenticated
        canModerateThreads={false}
        user={emptyProfile}
        onOpenUser={() => {}}
      />
    );
    expect(html).toContain("该评论已被作者删除");
    expect(html).toContain("仍可阅读的回复");
    expect(html).toContain("回复 被回复者");
    expect(html).toContain("3/1000");
    expect(html).toContain("该讨论已不可用");
    expect(html).toContain('data-reply-comment-id="2"');
  });

  it("exposes login action, busy/error states, and moderator cascade confirmation rules", () => {
    const root = comment(1, { user_id: 8, can_delete: true });
    const html = renderToStaticMarkup(
      <ThreadedComments
        controller={controller({
          roots: [root],
          entities: { 1: root },
          createState: { busy: false, error: "发送失败" }
        })}
        authenticated={false}
        canModerateThreads
        user={{ ...emptyProfile, id: 3 }}
        onOpenUser={() => {}}
      />
    );
    expect(html).toContain("登录后参与讨论");
    expect(html).toContain("发送失败");
    expect(requiresModeratorConfirmation({ ...root, reply_count: 1 }, true)).toBe(true);
    expect(requiresModeratorConfirmation({ ...root, reply_count: 1 }, false)).toBe(false);
    expect(requiresModeratorConfirmation(root, true)).toBe(false);
  });

  it("marks focused targets and disables submission beyond the Unicode character limit", () => {
    const root = comment(1);
    const html = renderToStaticMarkup(
      <ThreadedComments
        controller={controller({
          roots: [root],
          entities: { 1: root },
          focusedTargetID: 1,
          draft: "界".repeat(1001),
          draftLength: 1001
        })}
        authenticated
        canModerateThreads={false}
        user={emptyProfile}
        onOpenUser={() => {}}
      />
    );
    expect(html).toContain("comment-item root  focused");
    expect(html).toContain("1001/1000");
    expect(html).toContain('disabled=""');
  });

  it("keeps separately loaded deep-link targets visible in chronological order", () => {
    const entities = {
      2: comment(2, { root_comment_id: 1, created_at: "2026-08-03T00:00:00Z" }),
      3: comment(3, { root_comment_id: 1, created_at: "2026-08-03T02:00:00Z" }),
      9: comment(9, { root_comment_id: 1, created_at: "2026-08-03T01:00:00Z" })
    };
    expect(mergeVisibleReplyIDs([2], [2, 3], [9], entities)).toEqual([2, 9, 3]);
  });
});

function controller(patch: Partial<CommentsController> = {}): CommentsController {
  return {
    videoID: 3,
    sort: "hot",
    roots: [],
    rootList: { ids: [], nextCursor: "", hasMore: false, state: "ready", error: "" },
    entities: {},
    replies: {},
    contextReplyIDs: {},
    pendingReplyIDs: {},
    expandedRootIDs: [],
    draft: "",
    draftLength: 0,
    replyTarget: null,
    focusedRootID: 0,
    focusedTargetID: 0,
    focusRevision: 0,
    focusUnavailable: false,
    createState: { busy: false, error: "" },
    likeStates: {},
    deleteStates: {},
    setSort: vi.fn(),
    loadRoots: vi.fn(async () => {}),
    loadReplies: vi.fn(async () => {}),
    loadThreadContext: vi.fn(async () => {}),
    toggleReplies: vi.fn(),
    setDraft: vi.fn(),
    selectReplyTarget: vi.fn(),
    cancelReply: vi.fn(),
    clearFocus: vi.fn(),
    submitComment: vi.fn(async () => {}),
    toggleCommentLike: vi.fn(async () => {}),
    removeComment: vi.fn(async () => {}),
    requireLogin: vi.fn(() => false),
    ...patch
  };
}

function comment(id: number, patch: Partial<Comment> = {}): Comment {
  return {
    id,
    video_id: 3,
    user_id: 2,
    user_nickname: "用户",
    user_avatar_url: "",
    root_comment_id: 0,
    reply_to_comment_id: 0,
    reply_to_user_id: 0,
    reply_to_user_nickname: "",
    reply_to_user_avatar_url: "",
    content: "评论内容",
    status: 1,
    deleted: false,
    reply_count: 0,
    reply_previews: [],
    like_count: 2,
    liked: false,
    can_delete: false,
    hot_score: 0,
    created_at: "2026-08-03T00:00:00Z",
    ...patch
  };
}

function video(): FeedVideo {
  return {
    video_id: 3,
    author_id: 2,
    title: "视频",
    media_url: "/video.mp4",
    cover_url: "/cover.jpg",
    like_count: 1,
    comment_count: 2,
    favorite_count: 0,
    liked: false,
    favorited: false,
    author: "作者",
    avatar_url: "",
    description: "简介",
    feed_scene: "timeline",
    request_id: "request"
  };
}
