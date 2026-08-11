// @vitest-environment jsdom
import { act, useState } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { TOKEN_KEY, USER_KEY, emptyProfile } from "../constants";
import type { ProfileLibraryState } from "../hooks/useProfileLibrary";
import { RouterProvider } from "../router";
import { SessionProvider } from "../session";
import type { LibraryVideoItem } from "../types";
import { PROFILE_PRIMARY_TABS } from "../pages/ProfilePage";
import {
  CollectionQueueViewer,
  mapLibraryQueueItem,
  nextCollectionQueueIndex,
  nextVideoAfterRemoval,
  resolveCollectionQueueIndex
} from "./CollectionQueueViewer";

describe("collection queue viewer", () => {
  let container: HTMLDivElement;
  let root: Root;

  beforeEach(() => {
    (globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
    localStorage.clear();
    localStorage.setItem(TOKEN_KEY, "token");
    localStorage.setItem(USER_KEY, JSON.stringify({ ...emptyProfile, id: 2, account: "owner", nickname: "Owner" }));
    container = document.createElement("div");
    document.body.appendChild(container);
    root = createRoot(container);
    vi.spyOn(window, "requestAnimationFrame").mockImplementation((callback) => {
      callback(0);
      return 1;
    });
    vi.spyOn(window, "cancelAnimationFrame").mockImplementation(() => {});
    Object.defineProperty(window, "matchMedia", {
      configurable: true,
      value: vi.fn((query: string) => ({
        matches: false,
        media: query,
        onchange: null,
        addListener: () => {},
        removeListener: () => {},
        addEventListener: () => {},
        removeEventListener: () => {},
        dispatchEvent: () => true
      }))
    });
  });

  afterEach(() => {
    act(() => root.unmount());
    container.remove();
    localStorage.clear();
    vi.useRealTimers();
    vi.restoreAllMocks();
  });

  it("maps library cards and resolves queue positions deterministically", () => {
    const items = [libraryItem(1), libraryItem(2), libraryItem(3)];
    const mapped = items.map((item) => mapLibraryQueueItem(item, "likes"));
    expect(mapped[0]).toMatchObject({
      video_id: 1,
      liked: true,
      author: "作者 1",
      feed_scene: "library_likes"
    });
    expect(resolveCollectionQueueIndex(mapped, 2)).toBe(1);
    expect(nextCollectionQueueIndex(1, mapped.length)).toBe(2);
    expect(nextCollectionQueueIndex(2, mapped.length)).toBeNull();
    expect(nextVideoAfterRemoval(mapped, 1)).toBe(3);
    expect(nextVideoAfterRemoval(mapped, 2)).toBe(2);
    expect(mapLibraryQueueItem({
      ...items[0],
      video: { ...items[0].video, liked: false }
    }, "likes").liked).toBe(false);
    expect(mapLibraryQueueItem({
      ...items[0],
      video: { ...items[0].video, liked: undefined }
    }, "publicWorks")).toMatchObject({
      video_id: 1,
      liked: false,
      feed_scene: "profile_works"
    });
    expect(PROFILE_PRIMARY_TABS.map((tab) => tab.id)).toEqual([
      "works",
      "likes",
      "favorites",
      "history",
      "watchLater"
    ]);
  });

  it("opens on the selected item, navigates adjacent items, and restores opener focus", () => {
    vi.useFakeTimers();
    render(<QueueHarness selectedVideoID={2} />);
    const opener = required<HTMLButtonElement>('[data-testid="open-queue"]');
    act(() => opener.focus());
    click(opener);

    expect(required<HTMLElement>(".collection-queue-dialog").dataset.activeVideoId).toBe("2");
    expect(document.activeElement).toBe(required<HTMLButtonElement>('button[aria-label="关闭连续播放"]'));

    click(required<HTMLButtonElement>('button[aria-label="下一个视频"]'));
    act(() => vi.advanceTimersByTime(400));
    expect(required<HTMLElement>(".collection-queue-dialog").dataset.activeVideoId).toBe("3");

    click(required<HTMLButtonElement>('button[aria-label="关闭连续播放"]'));
    expect(document.activeElement).toBe(opener);
    expect(container.querySelector(".collection-queue-dialog")).toBeNull();
  });

  it("requests more items when the selected queue position reaches the preload boundary", () => {
    const onLoadMore = vi.fn();
    render(<QueueHarness selectedVideoID={3} hasMore onLoadMore={onLoadMore} />);
    click(required<HTMLButtonElement>('[data-testid="open-queue"]'));
    expect(onLoadMore).toHaveBeenCalledTimes(1);
  });

  it("does not automatically retry pagination while the source is in an error state", () => {
    const onLoadMore = vi.fn();
    render(<QueueHarness selectedVideoID={3} hasMore state="error" onLoadMore={onLoadMore} />);
    click(required<HTMLButtonElement>('[data-testid="open-queue"]'));
    expect(onLoadMore).not.toHaveBeenCalled();
    click(buttonByText("加载失败，重试"));
    expect(onLoadMore).toHaveBeenCalledTimes(1);
  });

  it("keeps the queue open and restores the final Watch Later item when removal fails", async () => {
    render(<QueueHarness selectedVideoID={1} source="watchLater" itemCount={1} removeFails />);
    click(required<HTMLButtonElement>('[data-testid="open-queue"]'));
    click(required<HTMLButtonElement>('button[aria-label="更多操作"]'));
    await act(async () => {
      required<HTMLButtonElement>('button[aria-label="从稍后再看移除"]')
        .dispatchEvent(new MouseEvent("click", { bubbles: true }));
      await Promise.resolve();
      await Promise.resolve();
    });
    expect(required<HTMLElement>(".collection-queue-dialog").dataset.activeVideoId).toBe("1");
    expect(container.textContent).toContain("移除稍后再看失败");
  });

  function render(node: React.ReactNode) {
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

function QueueHarness({
  selectedVideoID,
  hasMore = false,
  state = "ready",
  source = "likes",
  itemCount = 3,
  removeFails = false,
  onLoadMore = () => {}
}: {
  selectedVideoID: number;
  hasMore?: boolean;
  state?: ProfileLibraryState["state"];
  source?: "likes" | "watchLater";
  itemCount?: number;
  removeFails?: boolean;
  onLoadMore?: () => void;
}) {
  const [open, setOpen] = useState(false);
  const [items, setItems] = useState(
    Array.from({ length: itemCount }, (_, index) => libraryItem(index + 1))
  );
  const sourceState: ProfileLibraryState = {
    items,
    nextCursor: hasMore ? "next" : "",
    hasMore,
    state,
    error: state === "error" ? "加载失败" : ""
  };
  return (
    <RouterProvider>
      <SessionProvider>
        <button data-testid="open-queue" type="button" onClick={() => setOpen(true)}>打开</button>
        {open && (
          <CollectionQueueViewer
            source={source}
            sourceState={sourceState}
            selectedVideoID={selectedVideoID}
            onClose={() => setOpen(false)}
            onLoadMore={onLoadMore}
            onPatchVideo={(videoID, patch) => {
              setItems((state) => state.map((item) => item.video.id === videoID
                ? { ...item, video: { ...item.video, ...patch } }
                : item));
            }}
            onApplyVideoAction={() => {}}
            onAddWatchLater={() => {}}
            onRemoveWatchLater={async (videoID) => {
              const removed = items.find((item) => item.video.id === videoID);
              setItems((stateItems) => stateItems.filter((item) => item.video.id !== videoID));
              await Promise.resolve();
              if (removeFails && removed) {
                setItems((stateItems) => [removed, ...stateItems]);
                return false;
              }
              return true;
            }}
          />
        )}
      </SessionProvider>
    </RouterProvider>
  );
}

function click(element: HTMLElement) {
  act(() => {
    element.dispatchEvent(new MouseEvent("click", { bubbles: true }));
  });
}

function libraryItem(id: number): LibraryVideoItem {
  return {
    video: {
      id,
      author_id: 2,
      author_nickname: `作者 ${id}`,
      author_avatar_url: "",
      title: `视频 ${id}`,
      description: "",
      media_url: `/image-${id}.jpg`,
      cover_url: `/image-${id}.jpg`,
      status: 2,
      visibility: "public",
      like_count: id,
      comment_count: 0,
      favorite_count: 0,
      published_at: "2026-08-04T00:00:00Z",
      created_at: "2026-08-04T00:00:00Z",
      updated_at: "2026-08-04T00:00:00Z",
      media_status: "legacy_ready",
      liked: true,
      favorited: false
    },
    updated_at: `2026-08-04T00:00:0${id}Z`
  };
}
