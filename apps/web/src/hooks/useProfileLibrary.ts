import { useCallback, useRef, useState } from "react";
import {
  clearWatchHistory,
  deleteWatchHistoryItem,
  fetchFavoriteVideos,
  fetchLikedVideos,
  fetchWatchHistory,
  fetchWatchLater,
  setWatchLater
} from "../api/library";
import { apiErrorMessage } from "../api/client";
import type { AsyncState, LibraryVideoItem, LibraryVideoPage, ProfilePrimaryTab, Video } from "../types";

export type ProfileLibraryTab = Exclude<ProfilePrimaryTab, "works">;

export interface ProfileLibraryState {
  items: LibraryVideoItem[];
  nextCursor: string;
  hasMore: boolean;
  state: AsyncState;
  error: string;
}

export type LibraryActionType = "like" | "favorite";
export type LibraryActionCounts = Partial<Pick<Video, "like_count" | "favorite_count">>;

function createState(): ProfileLibraryState {
  return { items: [], nextCursor: "", hasMore: false, state: "idle", error: "" };
}

function compareLibraryItems(left: LibraryVideoItem, right: LibraryVideoItem): number {
  const updatedOrder = right.updated_at.localeCompare(left.updated_at);
  return updatedOrder || right.video.id - left.video.id;
}

export function restoreLibraryItem(
  items: LibraryVideoItem[],
  item: LibraryVideoItem
): LibraryVideoItem[] {
  if (items.some((candidate) => candidate.video.id === item.video.id)) return items;
  return [...items, item].sort(compareLibraryItems);
}

export function upsertLibraryItem(
  items: LibraryVideoItem[],
  item: LibraryVideoItem
): LibraryVideoItem[] {
  return [
    item,
    ...items.filter((candidate) => candidate.video.id !== item.video.id)
  ].sort(compareLibraryItems);
}

export function appendLibraryItems(
  current: LibraryVideoItem[],
  incoming: LibraryVideoItem[]
): LibraryVideoItem[] {
  const seen = new Set(current.map((item) => item.video.id));
  const appended = incoming.filter((item) => {
    if (seen.has(item.video.id)) return false;
    seen.add(item.video.id);
    return true;
  });
  return appended.length > 0 ? [...current, ...appended] : current;
}

export function useProfileLibrary(token: string) {
  const [tabs, setTabs] = useState<Record<ProfileLibraryTab, ProfileLibraryState>>({
    likes: createState(),
    favorites: createState(),
    history: createState(),
    watchLater: createState()
  });
  const tabsRef = useRef(tabs);
  tabsRef.current = tabs;
  const requests = useRef<Record<ProfileLibraryTab, number>>({
    likes: 0,
    favorites: 0,
    history: 0,
    watchLater: 0
  });
  const loadingTabs = useRef(new Set<ProfileLibraryTab>());
  const historyClearing = useRef(false);

  const requestPage = useCallback(
    async (tab: ProfileLibraryTab, cursor: string): Promise<LibraryVideoPage> => {
      if (tab === "likes") return fetchLikedVideos(token, cursor);
      if (tab === "favorites") return fetchFavoriteVideos(token, cursor);
      if (tab === "history") return fetchWatchHistory(token, cursor);
      return fetchWatchLater(token, cursor);
    },
    [token]
  );

  const loadTab = useCallback(
    async (tab: ProfileLibraryTab, reset = false) => {
      if (!token || (tab === "history" && historyClearing.current)) return;
      if (loadingTabs.current.has(tab)) return;
      const current = tabsRef.current[tab];
      if (!reset && current.state !== "idle" && !current.hasMore) return;
      const shouldReset = reset || current.state === "idle";
      const showInitialLoading = shouldReset || current.items.length === 0;
      const requestID = requests.current[tab] + 1;
      requests.current[tab] = requestID;
      loadingTabs.current.add(tab);
      setTabs((state) => ({
        ...state,
        [tab]: { ...state[tab], state: showInitialLoading ? "loading" : "loadingMore", error: "" }
      }));
      try {
        const data = await requestPage(tab, shouldReset ? "" : current.nextCursor);
        if (requests.current[tab] !== requestID) return;
        setTabs((state) => ({
          ...state,
          [tab]: {
            items: shouldReset ? data.items || [] : appendLibraryItems(state[tab].items, data.items || []),
            nextCursor: data.next_cursor || "",
            hasMore: Boolean(data.has_more && data.next_cursor),
            state: "ready",
            error: ""
          }
        }));
      } catch (error) {
        if (requests.current[tab] !== requestID) return;
        setTabs((state) => ({
          ...state,
          [tab]: { ...state[tab], state: "error", error: apiErrorMessage(error, "内容加载失败") }
        }));
      } finally {
        if (requests.current[tab] === requestID) loadingTabs.current.delete(tab);
      }
    },
    [requestPage, token]
  );

  const ensureTab = useCallback(
    (tab: ProfileLibraryTab) => {
      if (tabsRef.current[tab].state === "idle") void loadTab(tab, true);
    },
    [loadTab]
  );

  const removeWatchLater = useCallback(
    async (videoID: number): Promise<boolean> => {
      if (!token) return false;
      const previous = tabsRef.current.watchLater.items;
      const removedItem = previous.find((item) => item.video.id === videoID) || null;
      if (!removedItem) return true;
      const shouldContinue = previous.length === 1 && tabsRef.current.watchLater.hasMore;
      setTabs((state) => ({
        ...state,
        watchLater: {
          ...state.watchLater,
          items: state.watchLater.items.filter((item) => item.video.id !== videoID)
        }
      }));
      try {
        await setWatchLater(token, videoID, false);
        if (shouldContinue) {
          await loadTab("watchLater");
        }
        return true;
      } catch (error) {
        setTabs((state) => ({
          ...state,
          watchLater: {
            ...state.watchLater,
            items: restoreLibraryItem(state.watchLater.items, removedItem),
            error: apiErrorMessage(error, "移除稍后再看失败")
          }
        }));
        return false;
      }
    },
    [loadTab, token]
  );

  const addWatchLater = useCallback((item: LibraryVideoItem, updatedAt: string) => {
    const current = tabsRef.current.watchLater;
    if (current.state !== "ready") {
      requests.current.watchLater += 1;
      loadingTabs.current.delete("watchLater");
      setTabs((state) => ({ ...state, watchLater: createState() }));
      return;
    }
    const existing = current.items.some((candidate) => candidate.video.id === item.video.id);
    if (current.hasMore && !existing) {
      requests.current.watchLater += 1;
      loadingTabs.current.delete("watchLater");
      setTabs((state) => ({ ...state, watchLater: createState() }));
      return;
    }
    const nextItem = { ...item, updated_at: updatedAt };
    setTabs((state) => ({
      ...state,
      watchLater: {
        ...state.watchLater,
        items: upsertLibraryItem(state.watchLater.items, nextItem),
        error: ""
      }
    }));
  }, []);

  const applyVideoAction = useCallback((
    source: ProfileLibraryTab,
    videoID: number,
    action: LibraryActionType,
    active: boolean,
    counts: LibraryActionCounts
  ) => {
    const membershipTab: ProfileLibraryTab = action === "like" ? "likes" : "favorites";
    const membership = tabsRef.current[membershipTab];
    const membershipContains = membership.items.some((item) => item.video.id === videoID);
    const invalidateMembership =
      membership.state !== "idle"
      && membershipTab !== source
      && (
        membership.state !== "ready"
        || (active && !membershipContains)
      );
    if (invalidateMembership) {
      requests.current[membershipTab] += 1;
      loadingTabs.current.delete(membershipTab);
    }
    const continueMembershipPage =
      !active
      && membershipTab === source
      && membership.items.length === 1
      && membership.hasMore;
    setTabs((state) => {
      const next = { ...state };
      for (const tab of Object.keys(state) as ProfileLibraryTab[]) {
        if (invalidateMembership && tab === membershipTab) {
          next[tab] = createState();
          continue;
        }
        const removeMembership = !active && tab === membershipTab;
        next[tab] = {
          ...state[tab],
          items: state[tab].items
            .filter((item) => !removeMembership || item.video.id !== videoID)
            .map((item) => item.video.id === videoID
              ? {
                  ...item,
                  video: {
                    ...item.video,
                    liked: action === "like" ? active : item.video.liked,
                    favorited: action === "favorite" ? active : item.video.favorited,
                    like_count: counts.like_count ?? item.video.like_count,
                    favorite_count: counts.favorite_count ?? item.video.favorite_count
                  }
                }
              : item)
        };
      }
      return next;
    });
    if (continueMembershipPage) void loadTab(membershipTab);
  }, [loadTab]);

  const removeHistory = useCallback(
    async (videoID: number) => {
      if (!token) return;
      await deleteWatchHistoryItem(token, videoID);
      setTabs((state) => ({
        ...state,
        history: {
          ...state.history,
          items: state.history.items.filter((item) => item.video.id !== videoID)
        }
      }));
    },
    [token]
  );

  const clearHistory = useCallback(async () => {
    if (!token) return;
    const requestID = requests.current.history + 1;
    requests.current.history = requestID;
    loadingTabs.current.delete("history");
    historyClearing.current = true;
    setTabs((state) => ({
      ...state,
      history: { ...state.history, state: "mutating", error: "" }
    }));
    try {
      await clearWatchHistory(token);
      if (requests.current.history !== requestID) return;
      setTabs((state) => ({
        ...state,
        history: { ...state.history, items: [], nextCursor: "", hasMore: false, state: "ready", error: "" }
      }));
    } catch (error) {
      if (requests.current.history !== requestID) return;
      setTabs((state) => ({
        ...state,
        history: { ...state.history, state: "error", error: apiErrorMessage(error, "清空观看历史失败") }
      }));
    } finally {
      if (requests.current.history === requestID) historyClearing.current = false;
    }
  }, [token]);

  const patchVideo = useCallback((
    tab: ProfileLibraryTab,
    videoID: number,
    patch: Partial<Video>
  ) => {
    setTabs((state) => ({
      ...state,
      [tab]: {
        ...state[tab],
        items: state[tab].items.map((item) => item.video.id === videoID
          ? { ...item, video: { ...item.video, ...patch } }
          : item)
      }
    }));
  }, []);

  return {
    tabs,
    ensureTab,
    loadTab,
    patchVideo,
    applyVideoAction,
    addWatchLater,
    removeWatchLater,
    removeHistory,
    clearHistory
  };
}
