import { useCallback, useEffect, useRef, useState } from "react";
import { ApiError, apiErrorMessage } from "../api/client";
import { searchUsers, searchVideos } from "../api/search";
import type { AsyncState, SearchUser, Video } from "../types";
import type { SearchTab } from "../router";

export interface SearchResultState<T> {
  items: T[];
  nextCursor: string;
  hasMore: boolean;
  state: AsyncState;
  error: string;
}

function createState<T>(): SearchResultState<T> {
  return { items: [], nextCursor: "", hasMore: false, state: "idle", error: "" };
}

function appendUnique<T>(
  current: T[],
  incoming: T[],
  id: (item: T) => number
): T[] {
  const seen = new Set(current.map(id));
  const appended = incoming.filter((item) => {
    const itemID = id(item);
    if (seen.has(itemID)) return false;
    seen.add(itemID);
    return true;
  });
  return appended.length > 0 ? [...current, ...appended] : current;
}

export function useSearch(query: string) {
  const normalizedQuery = query.trim();
  const queryRef = useRef(normalizedQuery);
  const requests = useRef<Record<SearchTab, number>>({ videos: 0, users: 0 });
  const loading = useRef(new Set<SearchTab>());
  const [videos, setVideos] = useState<SearchResultState<Video>>(createState);
  const [users, setUsers] = useState<SearchResultState<SearchUser>>(createState);
  const videosRef = useRef(videos);
  const usersRef = useRef(users);
  videosRef.current = videos;
  usersRef.current = users;

  useEffect(() => {
    if (queryRef.current === normalizedQuery) return;
    queryRef.current = normalizedQuery;
    requests.current.videos += 1;
    requests.current.users += 1;
    loading.current.clear();
    setVideos(createState());
    setUsers(createState());
  }, [normalizedQuery]);

  const load = useCallback(async (tab: SearchTab, reset = false) => {
    const activeQuery = queryRef.current;
    if (!activeQuery || loading.current.has(tab)) return;
    const current = tab === "videos" ? videosRef.current : usersRef.current;
    if (!reset && current.state !== "idle" && !current.hasMore) return;
    const shouldReset = reset || current.state === "idle";
    const requestID = requests.current[tab] + 1;
    requests.current[tab] = requestID;
    loading.current.add(tab);
    if (tab === "videos") {
      setVideos((state) => ({
        ...state,
        state: shouldReset || state.items.length === 0 ? "loading" : "loadingMore",
        error: ""
      }));
    } else {
      setUsers((state) => ({
        ...state,
        state: shouldReset || state.items.length === 0 ? "loading" : "loadingMore",
        error: ""
      }));
    }
    try {
      if (tab === "videos") {
        const data = await searchVideos(activeQuery, shouldReset ? "" : current.nextCursor);
        if (requests.current.videos !== requestID || queryRef.current !== activeQuery) return;
        setVideos((state) => ({
          items: shouldReset ? data.items || [] : appendUnique(state.items, data.items || [], (item) => item.id),
          nextCursor: data.next_cursor || "",
          hasMore: Boolean(data.has_more && data.next_cursor),
          state: "ready",
          error: ""
        }));
      } else {
        const data = await searchUsers(activeQuery, shouldReset ? "" : current.nextCursor);
        if (requests.current.users !== requestID || queryRef.current !== activeQuery) return;
        setUsers((state) => ({
          items: shouldReset ? data.items || [] : appendUnique(state.items, data.items || [], (item) => item.id),
          nextCursor: data.next_cursor || "",
          hasMore: Boolean(data.has_more && data.next_cursor),
          state: "ready",
          error: ""
        }));
      }
    } catch (error) {
      if (requests.current[tab] !== requestID || queryRef.current !== activeQuery) return;
      const message = searchErrorMessage(error);
      if (tab === "videos") {
        setVideos((state) => ({ ...state, state: "error", error: message }));
      } else {
        setUsers((state) => ({ ...state, state: "error", error: message }));
      }
    } finally {
      if (requests.current[tab] === requestID) loading.current.delete(tab);
    }
  }, []);

  const matchesRenderedQuery = queryRef.current === normalizedQuery;
  return {
    videos: matchesRenderedQuery ? videos : createState<Video>(),
    users: matchesRenderedQuery ? users : createState<SearchUser>(),
    load
  };
}

export function searchErrorMessage(error: unknown): string {
  if (error instanceof ApiError && error.status < 500) {
    return apiErrorMessage(error, "搜索参数有误，请修改后重试");
  }
  return apiErrorMessage(error, "搜索服务暂时不可用，请稍后重试");
}
