// @vitest-environment jsdom
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { RelationListResponse, RelationUser } from "../types";
import { useFollowingDirectory } from "./useFollowingDirectory";

const socialAPI = vi.hoisted(() => ({
  fetchRelationList: vi.fn()
}));

vi.mock("../api/social", () => socialAPI);

describe("following directory state", () => {
  let container: HTMLDivElement;
  let root: Root;
  let current: ReturnType<typeof useFollowingDirectory>;

  beforeEach(() => {
    (globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
    container = document.createElement("div");
    document.body.appendChild(container);
    root = createRoot(container);
    socialAPI.fetchRelationList.mockReset();
    socialAPI.fetchRelationList.mockResolvedValue(page([], "", false));
  });

  afterEach(() => {
    act(() => root.unmount());
    container.remove();
    vi.restoreAllMocks();
  });

  it("loads the first page only when enabled", async () => {
    render(false);
    expect(socialAPI.fetchRelationList).not.toHaveBeenCalled();
    render(true);
    await flush();
    expect(socialAPI.fetchRelationList).toHaveBeenCalledWith("following", "token", "", 50, "");
  });

  it("resets for a new query and ignores the stale response", async () => {
    const initial = deferred<RelationListResponse>();
    const searched = deferred<RelationListResponse>();
    socialAPI.fetchRelationList
      .mockReturnValueOnce(initial.promise)
      .mockReturnValueOnce(searched.promise);
    render(true);
    act(() => current.setQuery("maker"));
    await flush();
    await flush();
    await act(async () => {
      searched.resolve(page([user(2, "maker")], "", false));
      await searched.promise;
    });
    await act(async () => {
      initial.resolve(page([user(1, "old")], "", false));
      await initial.promise;
    });
    expect(current.items.map((item) => item.user_id)).toEqual([2]);
  });

  it("deduplicates pagination", async () => {
    socialAPI.fetchRelationList
      .mockResolvedValueOnce(page([user(1, "one")], "next", true))
      .mockResolvedValueOnce(page([user(1, "one"), user(2, "two")], "", false));
    render(true);
    await flush();
    act(() => current.loadMore());
    await flush();
    expect(current.items.map((item) => item.user_id)).toEqual([1, 2]);
  });

  it("preserves existing rows after a pagination failure", async () => {
    socialAPI.fetchRelationList
      .mockResolvedValueOnce(page([user(1, "one")], "next", true))
      .mockRejectedValueOnce(new Error("offline"));
    render(true);
    await flush();
    act(() => current.loadMore());
    await flush();
    expect(current.state).toBe("error");
    expect(current.items.map((item) => item.user_id)).toEqual([1]);
  });

  it("removes an unfollowed user and blocks an older response from restoring it", async () => {
    const request = deferred<RelationListResponse>();
    socialAPI.fetchRelationList.mockReturnValueOnce(request.promise);
    render(true);
    act(() => current.setUserActive(user(3, "three"), false));
    await act(async () => {
      request.resolve(page([user(3, "three")], "", false));
      await request.promise;
    });
    expect(current.items).toEqual([]);
  });

  function render(enabled: boolean) {
    act(() => root.render(
      <Harness enabled={enabled} onValue={(value) => { current = value; }} />
    ));
  }
});

function Harness({
  enabled,
  onValue
}: {
  enabled: boolean;
  onValue: (value: ReturnType<typeof useFollowingDirectory>) => void;
}) {
  const value = useFollowingDirectory({ token: "token", enabled, debounceMs: 0 });
  onValue(value);
  return null;
}

function user(id: number, account: string): RelationUser {
  return {
    user_id: id,
    account,
    nickname: `用户 ${id}`,
    avatar_url: "",
    bio: "",
    followed_at: "2026-08-08T00:00:00Z"
  };
}

function page(items: RelationUser[], nextCursor: string, hasMore: boolean): RelationListResponse {
  return { items, next_cursor: nextCursor, has_more: hasMore };
}

async function flush() {
  await act(async () => {
    await new Promise((resolve) => window.setTimeout(resolve, 0));
  });
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
