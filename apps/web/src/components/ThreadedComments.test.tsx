// @vitest-environment jsdom
import { act, useState, type ReactNode } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { emptyProfile, image } from "../constants";
import type { CommentsController } from "../hooks/useComments";
import type { Comment, FeedVideo } from "../types";
import { FeedDetailsPanel } from "./FeedDetailsPanel";
import { VideoDetails } from "./VideoDetails";
import {
  mergeVisibleReplyIDs,
  requiresModeratorConfirmation,
  ThreadedComments
} from "./ThreadedComments";

describe("threaded comment components", () => {
  let container: HTMLDivElement;
  let root: Root;

  beforeEach(() => {
    (globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
    container = document.createElement("div");
    container.id = "root";
    document.body.appendChild(container);
    root = createRoot(container);
    stubAnimationFrame();
    stubMatchMedia(false);
  });

  afterEach(() => {
    act(() => root.unmount());
    container.remove();
    vi.restoreAllMocks();
    vi.unstubAllGlobals();
  });

  it("mounts distinct desktop panel and compact drawer markers", () => {
    render(
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
    const desktop = required<HTMLElement>('[data-ui="details-panel"]');
    expect(desktop.dataset.presentation).toBe("panel");
    expect(desktop.getAttribute("role")).toBe("complementary");

    act(() => root.unmount());
    root = createRoot(container);
    stubMatchMedia(true);
    render(
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
    const mobile = required<HTMLElement>('[data-ui="details-panel"]');
    expect(mobile.dataset.presentation).toBe("drawer");
    expect(mobile.getAttribute("role")).toBe("dialog");
    expect(mobile.getAttribute("aria-modal")).toBe("true");
  });

  it("fully hides the closed comments sheet on compact viewports", () => {
    stubMatchMedia(true);
    render(
      <FeedDetailsPanel
        item={video()}
        open={false}
        onClose={() => {}}
        user={emptyProfile}
        count={0}
        comments={controller()}
        authenticated
        onOpenUser={() => {}}
      />
    );
    const panel = required<HTMLElement>('[data-ui="details-panel"]');
    expect(panel.hidden).toBe(true);
    expect(panel.getAttribute("aria-hidden")).toBe("true");
  });

  it("renders a safe tombstone and unavailable discussion in a mounted tree", () => {
    const tombstone = comment(1, {
      user_id: 0,
      user_nickname: "",
      content: "",
      status: 2,
      deleted: true,
      reply_count: 1,
      reply_previews: [comment(2, { root_comment_id: 1, content: "仍可阅读的回复" })]
    });
    render(
      <ThreadedComments
        controller={controller({
          roots: [tombstone],
          entities: { 1: tombstone, 2: tombstone.reply_previews[0]! },
          focusUnavailable: true
        })}
        authenticated
        canModerateThreads={false}
        user={emptyProfile}
        onOpenUser={() => {}}
      />
    );
    expect(container.textContent).toContain("该评论已被作者删除");
    expect(container.textContent).toContain("仍可阅读的回复");
    expect(container.textContent).toContain("该讨论已不可用");
    expect(required<HTMLElement>('[data-comment-id="1"]').classList.contains("tombstone")).toBe(true);
    expect(container.textContent).not.toContain("评论内容");
  });

  it("confirms moderator cascades before deleting a root thread", () => {
    const removeComment = vi.fn(async () => {});
    const rootComment = comment(1, { user_id: 8, can_delete: true, reply_count: 2 });
    render(
      <ThreadedComments
        controller={controller({
          roots: [rootComment],
          entities: { 1: rootComment },
          removeComment
        })}
        authenticated
        canModerateThreads
        user={{ ...emptyProfile, id: 3 }}
        onOpenUser={() => {}}
      />
    );
    const confirm = vi.spyOn(window, "confirm").mockReturnValue(false);
    click(buttonByText("删除"));
    expect(confirm).toHaveBeenCalledWith("删除该根评论会隐藏整个讨论串，确认继续吗？");
    expect(removeComment).not.toHaveBeenCalled();

    confirm.mockReturnValue(true);
    click(buttonByText("删除"));
    expect(removeComment).toHaveBeenCalledWith(1);
    expect(requiresModeratorConfirmation(rootComment, true)).toBe(true);
  });

  it("targets and cancels replies while restoring composer and trigger focus", () => {
    const target = comment(1, { user_nickname: "被回复者" });
    render(<InteractiveThreadedComments entities={{ 1: target }} roots={[target]} />);

    const replyButton = required<HTMLButtonElement>('[data-reply-comment-id="1"]');
    click(replyButton);
    expect(container.textContent).toContain("回复 被回复者");
    const composer = required<HTMLTextAreaElement>('textarea[aria-label="回复评论"]');
    expect(document.activeElement).toBe(composer);

    click(buttonByText("取消"));
    expect(container.textContent).not.toContain("回复 被回复者");
    expect(document.activeElement).toBe(replyButton);
  });

  it("opens the directly replied user's profile from the @ target", () => {
    const onOpenUser = vi.fn();
    const reply = comment(2, {
      root_comment_id: 1,
      reply_to_comment_id: 1,
      reply_to_user_id: 7,
      reply_to_user_nickname: "目标用户",
      reply_to_user_avatar_url: "/target-avatar.png",
      content: "回复内容"
    });

    const rootComment = comment(1, {
      reply_count: 1,
      reply_previews: [reply]
    });
    render(
      <ThreadedComments
        controller={controller({
          roots: [rootComment],
          entities: { 1: rootComment, 2: reply }
        })}
        authenticated
        canModerateThreads={false}
        user={emptyProfile}
        onOpenUser={onOpenUser}
      />
    );

    click(buttonByText("@目标用户"));
    expect(onOpenUser).toHaveBeenCalledWith({
      id: 7,
      nickname: "目标用户",
      avatar_url: "/target-avatar.png",
      bio: ""
    });
  });

  it("renders video-author and author-liked markers with canonical identity", () => {
    const onOpenUser = vi.fn();
    const author = comment(1, {
      user_account: "creator_account",
      user_nickname: "视频作者",
      is_video_author: true,
      liked_by_video_author: true
    });
    render(
      <ThreadedComments
        controller={controller({ roots: [author], entities: { 1: author } })}
        authenticated
        canModerateThreads={false}
        user={emptyProfile}
        onOpenUser={onOpenUser}
      />
    );
    expect(container.textContent).toContain("作者");
    expect(container.textContent).toContain("作者赞过");
    click(buttonByText("视频作者"));
    expect(onOpenUser).toHaveBeenCalledWith(expect.objectContaining({
      id: 2,
      account: "creator_account",
      nickname: "视频作者"
    }));
  });

  it("uses one avatar fallback for the same video and comment author", () => {
    const authorComment = comment(1, {
      user_id: 2,
      user_account: "creator",
      user_avatar_url: "",
      is_video_author: true
    });
    render(
      <>
        <VideoDetails item={video()} onOpenUser={() => {}} />
        <ThreadedComments
          controller={controller({
            roots: [authorComment],
            entities: { 1: authorComment }
          })}
          authenticated
          canModerateThreads={false}
          user={emptyProfile}
          onOpenUser={() => {}}
        />
      </>
    );
    const authorImage = required<HTMLImageElement>(".details-author img");
    const commentImage = required<HTMLImageElement>(".comment-user-button img");
    const fallbackAvatar = new URL(image.currentUser, window.location.href).href;
    expect(authorImage.src).toBe(fallbackAvatar);
    expect(commentImage.src).toBe(fallbackAvatar);
  });

  it("counts Unicode code points and blocks over-limit submission behaviorally", () => {
    const target = comment(1);
    render(<InteractiveThreadedComments entities={{ 1: target }} roots={[target]} />);
    const composer = required<HTMLTextAreaElement>('textarea[aria-label="发表评论"]');

    input(composer, "界".repeat(999) + "🙂");
    expect(container.textContent).toContain("1000/1000");
    expect(required<HTMLButtonElement>('button[aria-label="发送评论"]').disabled).toBe(false);

    input(composer, "界".repeat(1000) + "🙂");
    expect(container.textContent).toContain("1001/1000");
    expect(required<HTMLButtonElement>('button[aria-label="发送评论"]').disabled).toBe(true);
  });

  it("restores panel focus and scrolls a newly focused discussion target", () => {
    stubMatchMedia(true);
    render(<PanelFocusHarness />);
    const opener = required<HTMLButtonElement>('[data-testid="open-comments"]');
    act(() => opener.focus());
    click(opener);
    expect(document.activeElement).toBe(required<HTMLButtonElement>('button[aria-label="关闭评论"]'));
    click(required<HTMLButtonElement>('button[aria-label="关闭评论"]'));
    expect(document.activeElement).toBe(opener);

    const scrollIntoView = vi.fn();
    Object.defineProperty(window.HTMLElement.prototype, "scrollIntoView", {
      configurable: true,
      value: scrollIntoView
    });
    const focused = comment(9);
    act(() => root.render(
      <ThreadedComments
        controller={controller({
          roots: [focused],
          entities: { 9: focused },
          focusedRootID: 9,
          focusedTargetID: 9,
          focusRevision: 1
        })}
        authenticated
        canModerateThreads={false}
        user={emptyProfile}
        onOpenUser={() => {}}
      />
    ));
    expect(required<HTMLElement>('[data-comment-id="9"]').classList.contains("focused")).toBe(true);
    expect(scrollIntoView).toHaveBeenCalledWith({ block: "center", behavior: "smooth" });
  });

  it("keeps separately loaded deep-link targets visible in chronological order", () => {
    const entities = {
      2: comment(2, { root_comment_id: 1, created_at: "2026-08-03T00:00:00Z" }),
      3: comment(3, { root_comment_id: 1, created_at: "2026-08-03T02:00:00Z" }),
      9: comment(9, { root_comment_id: 1, created_at: "2026-08-03T01:00:00Z" })
    };
    expect(mergeVisibleReplyIDs([2], [2, 3], [9], entities)).toEqual([2, 9, 3]);
  });

  it("commits comment like and unlike updates without rendering unrelated cards", () => {
    const reads: Record<number, number> = {};
    const first = trackedComment(1, reads);
    const second = trackedComment(2, reads);
    const base = controller();
    const renderState = (
      target: Comment,
      likeState: CommentsController["likeStates"][number] | undefined
    ) => {
      act(() => root.render(
        <ThreadedComments
          controller={{
            ...base,
            roots: [target, second],
            entities: { 1: target, 2: second },
            likeStates: likeState ? { 1: likeState } : {}
          }}
          authenticated
          canModerateThreads={false}
          user={emptyProfile}
          onOpenUser={() => {}}
        />
      ));
    };

    renderState(first, undefined);
    const commentList = required<HTMLElement>('[data-ui="comment-list"]');
    commentList.scrollTop = 72;
    const unrelatedReads = reads[2];

    const optimisticLike = { ...first, liked: true, like_count: 3 };
    renderState(optimisticLike, { busy: true, error: "" });
    expect(reads[2]).toBe(unrelatedReads);
    expect(commentList.scrollTop).toBe(72);

    const confirmedLike = { ...optimisticLike };
    renderState(confirmedLike, { busy: false, error: "" });
    expect(reads[2]).toBe(unrelatedReads);

    const optimisticUnlike = { ...confirmedLike, liked: false, like_count: 2 };
    renderState(optimisticUnlike, { busy: true, error: "" });
    expect(reads[2]).toBe(unrelatedReads);
    expect(required<HTMLButtonElement>('[data-comment-id="1"] button[aria-label="点赞评论"]').getAttribute("aria-pressed")).toBe("false");
  });

  it("rolls back one failed like without resetting discussion state", () => {
    const reads: Record<number, number> = {};
    const reply = comment(3, { root_comment_id: 1, content: "展开回复" });
    const first = trackedComment(1, reads, { reply_count: 2, reply_previews: [reply] });
    const second = trackedComment(2, reads);
    const base = controller({
      rootList: { ids: [1, 2], nextCursor: "next", hasMore: true, state: "ready", error: "" },
      replies: {
        1: { ids: [3], nextCursor: "reply-next", hasMore: true, state: "ready", error: "" }
      },
      expandedRootIDs: [1],
      draft: "保留草稿",
      draftLength: 4,
      focusedRootID: 2,
      focusedTargetID: 2,
      focusRevision: 1
    });
    const renderState = (
      target: Comment,
      likeState: CommentsController["likeStates"][number] | undefined
    ) => {
      act(() => root.render(
        <ThreadedComments
          controller={{
            ...base,
            roots: [target, second],
            entities: { 1: target, 2: second, 3: reply },
            likeStates: likeState ? { 1: likeState } : {}
          }}
          authenticated
          canModerateThreads={false}
          user={emptyProfile}
          onOpenUser={() => {}}
        />
      ));
    };

    renderState(first, undefined);
    const commentList = required<HTMLElement>('[data-ui="comment-list"]');
    commentList.scrollTop = 96;
    const unrelatedReads = reads[2];

    renderState({ ...first, liked: true, like_count: 3 }, { busy: true, error: "" });
    renderState(first, { busy: false, error: "点赞失败" });

    expect(reads[2]).toBe(unrelatedReads);
    expect(commentList.scrollTop).toBe(96);
    expect(required<HTMLTextAreaElement>("textarea").value).toBe("保留草稿");
    expect(container.textContent).toContain("展开回复");
    expect(container.textContent).toContain("收起回复");
    expect(container.textContent).toContain("点赞失败");
    expect(required<HTMLElement>('[data-comment-id="2"]').classList.contains("focused")).toBe(true);
    expect(required<HTMLButtonElement>('[data-comment-id="1"] button[aria-label="点赞评论"]').getAttribute("aria-pressed")).toBe("false");
  });

  function render(node: ReactNode) {
    act(() => root.render(node));
  }

  function required<T extends Element>(selector: string): T {
    const element = container.querySelector<T>(selector);
    if (!element) throw new Error(`missing element: ${selector}`);
    return element;
  }

  function buttonByText(text: string): HTMLButtonElement {
    const button = [...container.querySelectorAll("button")].find((item) => item.textContent?.trim() === text);
    if (!button) throw new Error(`missing button: ${text}`);
    return button;
  }
});

function InteractiveThreadedComments({
  entities,
  roots
}: {
  entities: Record<number, Comment>;
  roots: Comment[];
}) {
  const [draft, setDraft] = useState("");
  const [replyTarget, setReplyTarget] = useState<Comment | null>(null);
  return (
    <ThreadedComments
      controller={controller({
        roots,
        entities,
        draft,
        draftLength: Array.from(draft).length,
        replyTarget,
        setDraft,
        selectReplyTarget: (id) => setReplyTarget(entities[id] || null),
        cancelReply: () => setReplyTarget(null)
      })}
      authenticated
      canModerateThreads={false}
      user={emptyProfile}
      onOpenUser={() => {}}
    />
  );
}

function PanelFocusHarness() {
  const [open, setOpen] = useState(false);
  return (
    <>
      <button data-testid="open-comments" type="button" onClick={() => setOpen(true)}>打开评论</button>
      <FeedDetailsPanel
        item={video()}
        open={open}
        onClose={() => setOpen(false)}
        user={emptyProfile}
        count={0}
        comments={controller()}
        authenticated
        onOpenUser={() => {}}
      />
    </>
  );
}

function click(element: HTMLElement) {
  act(() => {
    element.dispatchEvent(new MouseEvent("click", { bubbles: true }));
  });
}

function input(element: HTMLTextAreaElement, value: string) {
  act(() => {
    const setter = Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, "value")?.set;
    setter?.call(element, value);
    element.dispatchEvent(new Event("input", { bubbles: true }));
  });
}

function stubAnimationFrame() {
  vi.spyOn(window, "requestAnimationFrame").mockImplementation((callback) => {
    callback(0);
    return 1;
  });
  vi.spyOn(window, "cancelAnimationFrame").mockImplementation(() => {});
}

function stubMatchMedia(matches: boolean) {
  Object.defineProperty(window, "matchMedia", {
    configurable: true,
    value: vi.fn((query: string) => ({
      matches,
      media: query,
      onchange: null,
      addListener: () => {},
      removeListener: () => {},
      addEventListener: () => {},
      removeEventListener: () => {},
      dispatchEvent: () => true
    }))
  });
}

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
    user_account: "user",
    user_nickname: "用户",
    user_avatar_url: "",
    root_comment_id: 0,
    reply_to_comment_id: 0,
    reply_to_user_id: 0,
    reply_to_user_account: "",
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
    is_video_author: false,
    liked_by_video_author: false,
    hot_score: 0,
    created_at: "2026-08-03T00:00:00Z",
    ...patch
  };
}

function trackedComment(
  id: number,
  reads: Record<number, number>,
  patch: Partial<Comment> = {}
): Comment {
  const value = comment(id, patch);
  const content = value.content;
  Object.defineProperty(value, "content", {
    configurable: true,
    enumerable: true,
    get() {
      reads[id] = (reads[id] || 0) + 1;
      return content;
    }
  });
  return value;
}

function video(): FeedVideo {
  return {
    video_id: 3,
    author_id: 2,
    title: "视频",
    media_url: "/cover.jpg",
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
