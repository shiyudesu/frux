import { useCallback, useRef, useState } from "react";
import { fetchFeedPage } from "../api/feed";
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

function createState(): ProfileLibraryState {
  return { items: [], nextCursor: "", hasMore: false, state: "idle", error: "" };
}

function compareLibraryItems(left: LibraryVideoItem, right: LibraryVideoItem): number {
  const updatedOrder = right.updated_at.localeCompare(left.updated_at);
  return updatedOrder || right.video.id - left.video.id;
}

function feedVideo(item: {
  video_id: number;
  author_id: number;
  title: string;
  description: string;
  media_url: string;
  cover_url: string;
  like_count: number;
  comment_count: number;
  favorite_count: number;
  published_at: string;
}): Video {
  return {
    id: item.video_id,
    author_id: item.author_id,
    title: item.title,
    description: item.description,
    media_url: item.media_url,
    cover_url: item.cover_url,
    status: 2,
    visibility: "public",
    like_count: item.like_count,
    comment_count: item.comment_count,
    favorite_count: item.favorite_count,
    published_at: item.published_at,
    created_at: item.published_at,
    updated_at: item.published_at
  };
}

export function useProfileLibrary(token: string) {
  const [tabs, setTabs] = useState<Record<ProfileLibraryTab, ProfileLibraryState>>({
    recommend: createState(),
    likes: createState(),
    favorites: createState(),
    history: createState(),
    watchLater: createState()
  });
  const requests = useRef<Record<ProfileLibraryTab, number>>({
    recommend: 0,
    likes: 0,
    favorites: 0,
    history: 0,
    watchLater: 0
  });
  const historyClearing = useRef(false);

  const requestPage = useCallback(
    async (tab: ProfileLibraryTab, cursor: string): Promise<LibraryVideoPage> => {
      if (tab === "recommend") {
        const data = await fetchFeedPage("recommend", token, cursor, `profile-${Date.now()}`);
        return {
          items: (data.items || []).map((item) => ({
            video: feedVideo(item),
            updated_at: item.published_at
          })),
          next_cursor: data.next_cursor,
          has_more: data.has_more
        };
      }
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
      const current = tabs[tab];
      const shouldReset = reset || current.state === "idle";
      const requestID = requests.current[tab] + 1;
      requests.current[tab] = requestID;
      setTabs((state) => ({
        ...state,
        [tab]: { ...state[tab], state: shouldReset ? "loading" : "loadingMore", error: "" }
      }));
      try {
        const data = await requestPage(tab, shouldReset ? "" : current.nextCursor);
        if (requests.current[tab] !== requestID) return;
        setTabs((state) => ({
          ...state,
          [tab]: {
            items: shouldReset ? data.items || [] : [...state[tab].items, ...(data.items || [])],
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
      }
    },
    [requestPage, tabs, token]
  );

  const ensureTab = useCallback(
    (tab: ProfileLibraryTab) => {
      if (tabs[tab].state === "idle") void loadTab(tab, true);
    },
    [loadTab, tabs]
  );

  const removeWatchLater = useCallback(
    async (videoID: number) => {
      if (!token) return;
      const previous = tabs.watchLater.items;
      const removedItem = previous.find((item) => item.video.id === videoID) || null;
      if (!removedItem) return;
      setTabs((state) => ({
        ...state,
        watchLater: {
          ...state.watchLater,
          items: state.watchLater.items.filter((item) => item.video.id !== videoID)
        }
      }));
      try {
        await setWatchLater(token, videoID, false);
      } catch (error) {
        setTabs((state) => ({
          ...state,
          watchLater: {
            ...state.watchLater,
            items: state.watchLater.items.some((item) => item.video.id === videoID)
              ? state.watchLater.items
              : [...state.watchLater.items, removedItem].sort(compareLibraryItems),
            error: apiErrorMessage(error, "移除稍后再看失败")
          }
        }));
      }
    },
    [tabs.watchLater.items, token]
  );

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

  return { tabs, ensureTab, loadTab, removeWatchLater, removeHistory, clearHistory };
}
