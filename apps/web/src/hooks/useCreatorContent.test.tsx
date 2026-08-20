// @vitest-environment jsdom
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { NetworkError } from "../api/client";
import type {
  BatchVideoActionResponse,
  CreatorArchiveMonthResponse,
  CreatorVideoPage,
  CreatorVideoQueryRequest
} from "../types";
import {
  reconcileArchiveFilter,
  useCreatorContent
} from "./useCreatorContent";

const creatorAPI = vi.hoisted(() => ({
  applyVideoBatchAction: vi.fn(),
  fetchCreatorArchiveMonths: vi.fn(),
  queryCreatorVideos: vi.fn()
}));

vi.mock("../api/creator", () => creatorAPI);

describe("creator content archive state", () => {
  let container: HTMLDivElement;
  let root: Root;
  let current: ReturnType<typeof useCreatorContent>;

  beforeEach(() => {
    (globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
    container = document.createElement("div");
    document.body.appendChild(container);
    root = createRoot(container);
    Object.values(creatorAPI).forEach((mock) => mock.mockReset());
    creatorAPI.queryCreatorVideos.mockResolvedValue(emptyPage());
    creatorAPI.fetchCreatorArchiveMonths.mockResolvedValue({ months: [] });
    creatorAPI.applyVideoBatchAction.mockResolvedValue({
      action: "delete",
      video_ids: [1],
      replayed: false
    } satisfies BatchVideoActionResponse);
    act(() => root.render(<Harness onValue={(value) => { current = value; }} />));
  });

  afterEach(() => {
    act(() => root.unmount());
    container.remove();
    vi.restoreAllMocks();
  });

  it("maps a selected month through the existing inclusive range query", async () => {
    await act(async () => {
      await current.loadVideos("published", {
        reset: true,
        filters: { query: " draft ", createdMonth: "2024-02" }
      });
    });

    expect(creatorAPI.queryCreatorVideos).toHaveBeenCalledWith("token", expect.objectContaining({
      visibility: "public",
      query: "draft",
      created_from: "2024-02-01",
      created_to: "2024-02-29",
      cursor: ""
    } satisfies Partial<CreatorVideoQueryRequest>));
    expect(current.videos.published.filters).toEqual({
      query: " draft ",
      createdMonth: "2024-02"
    });
  });

  it("keeps public and private archives isolated and ignores stale responses", async () => {
    const stale = deferred<CreatorArchiveMonthResponse>();
    const latest = deferred<CreatorArchiveMonthResponse>();
    const privateArchive = deferred<CreatorArchiveMonthResponse>();
    creatorAPI.fetchCreatorArchiveMonths
      .mockReturnValueOnce(stale.promise)
      .mockReturnValueOnce(latest.promise)
      .mockReturnValueOnce(privateArchive.promise);

    let staleLoad: Promise<string[] | null>;
    let latestLoad: Promise<string[] | null>;
    let privateLoad: Promise<string[] | null>;
    act(() => {
      staleLoad = current.loadArchiveMonths("published");
      latestLoad = current.loadArchiveMonths("published");
      privateLoad = current.loadArchiveMonths("private");
    });
    await act(async () => {
      latest.resolve({ months: ["2026-08"] });
      privateArchive.resolve({ months: ["2025-12"] });
      await Promise.all([latestLoad!, privateLoad!]);
    });
    await act(async () => {
      stale.resolve({ months: ["2020-01"] });
      await staleLoad!;
    });

    expect(current.archives.published.months).toEqual(["2026-08"]);
    expect(current.archives.private.months).toEqual(["2025-12"]);
  });

  it("reports archive failure without replacing a ready video page", async () => {
    creatorAPI.queryCreatorVideos.mockResolvedValue(page(1));
    await act(async () => {
      await current.loadVideos("published", { reset: true });
    });
    creatorAPI.fetchCreatorArchiveMonths.mockRejectedValueOnce(new NetworkError());
    await act(async () => {
      await current.loadArchiveMonths("published");
    });

    expect(current.videos.published).toMatchObject({
      state: "ready",
      items: [expect.objectContaining({ id: 1 })]
    });
    expect(current.archives.published).toMatchObject({
      state: "error",
      error: "网络连接失败，请检查网络后重试"
    });
  });

  it("refreshes both archives after a batch and clears invalid applied months", async () => {
    await act(async () => {
      await current.loadVideos("published", {
        reset: true,
        filters: { query: "keep", createdMonth: "2026-08" }
      });
      await current.loadVideos("private", {
        reset: true,
        filters: { query: "", createdMonth: "2025-12" }
      });
    });
    creatorAPI.queryCreatorVideos.mockClear();
    creatorAPI.fetchCreatorArchiveMonths
      .mockResolvedValueOnce({ months: ["2026-07"] })
      .mockResolvedValueOnce({ months: ["2025-12"] });

    let refreshed: Awaited<ReturnType<typeof current.runBatchAction>>;
    await act(async () => {
      refreshed = await current.runBatchAction("published", [1], "delete");
    });

    expect(refreshed!).toEqual({
      published: ["2026-07"],
      private: ["2025-12"]
    });
    expect(creatorAPI.queryCreatorVideos).toHaveBeenCalledWith("token", expect.objectContaining({
      visibility: "public",
      query: "keep",
      created_from: "",
      created_to: ""
    }));
    expect(creatorAPI.queryCreatorVideos).toHaveBeenCalledWith("token", expect.objectContaining({
      visibility: "private",
      created_from: "2025-12-01",
      created_to: "2025-12-31"
    }));
    expect(current.videos.published.filters.createdMonth).toBe("");
    expect(current.videos.private.filters.createdMonth).toBe("2025-12");
  });

  it("reconciles only selections absent from the refreshed archive", () => {
    const filters = { query: "keep", createdMonth: "2026-08" };
    expect(reconcileArchiveFilter(filters, ["2026-08"])).toBe(filters);
    expect(reconcileArchiveFilter(filters, ["2026-07"])).toEqual({
      query: "keep",
      createdMonth: ""
    });
  });
});

function Harness({ onValue }: {
  onValue: (value: ReturnType<typeof useCreatorContent>) => void;
}) {
  const value = useCreatorContent("token");
  onValue(value);
  return null;
}

function emptyPage(): CreatorVideoPage {
  return { items: [], next_cursor: "", has_more: false };
}

function page(id: number): CreatorVideoPage {
  return {
    items: [{
      id,
      author_id: 7,
      title: `作品 ${id}`,
      description: "",
      media_url: "",
      cover_url: "",
      status: 2,
      visibility: "public",
      like_count: 0,
      comment_count: 0,
      favorite_count: 0,
      created_at: "2026-08-01T00:00:00Z",
      updated_at: "2026-08-01T00:00:00Z"
    }],
    next_cursor: "",
    has_more: false
  };
}

function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (reason?: unknown) => void;
  const promise = new Promise<T>((resolvePromise, rejectPromise) => {
    resolve = resolvePromise;
    reject = rejectPromise;
  });
  return { promise, resolve, reject };
}
