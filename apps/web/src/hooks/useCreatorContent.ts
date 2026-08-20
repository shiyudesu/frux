import { useCallback, useEffect, useRef, useState } from "react";
import {
  applyVideoBatchAction,
  fetchCreatorArchiveMonths,
  queryCreatorVideos
} from "../api/creator";
import { apiErrorMessage } from "../api/client";
import { creatorArchiveMonthRange } from "../creatorArchive";
import type {
  AsyncState,
  BatchVideoAction,
  CreatorWorkTab,
  Video,
  VideoVisibility
} from "../types";

const PAGE_LIMIT = 24;

export interface CreatorFilters {
  query: string;
  createdMonth: string;
}

export interface CreatorVideoState {
  items: Video[];
  nextCursor: string;
  hasMore: boolean;
  state: AsyncState;
  error: string;
  filters: CreatorFilters;
}

export interface CreatorArchiveState {
  months: string[];
  state: "idle" | "loading" | "ready" | "error";
  error: string;
}

export type CreatorArchiveRefreshResult = Record<CreatorWorkTab, string[]>;

const emptyFilters: CreatorFilters = { query: "", createdMonth: "" };

function createVideoState(): CreatorVideoState {
  return {
    items: [],
    nextCursor: "",
    hasMore: false,
    state: "idle",
    error: "",
    filters: { ...emptyFilters }
  };
}

function createArchiveState(): CreatorArchiveState {
  return {
    months: [],
    state: "idle",
    error: ""
  };
}

function visibilityForTab(tab: CreatorWorkTab): VideoVisibility {
  return tab === "published" ? "public" : "private";
}

function mutationKey(scope: string): string {
  return `web-profile-${scope}-${Date.now()}-${Math.random().toString(36).slice(2, 9)}`;
}

export function useCreatorContent(token: string, onContentStatsChanged?: () => Promise<void> | void) {
  const [videos, setVideos] = useState<Record<CreatorWorkTab, CreatorVideoState>>({
    published: createVideoState(),
    private: createVideoState()
  });
  const [archives, setArchives] = useState<Record<CreatorWorkTab, CreatorArchiveState>>({
    published: createArchiveState(),
    private: createArchiveState()
  });
  const videoRequests = useRef({ published: 0, private: 0 });
  const archiveRequests = useRef({ published: 0, private: 0 });
  const tokenRef = useRef(token);
  tokenRef.current = token;

  useEffect(() => {
    videoRequests.current.published += 1;
    videoRequests.current.private += 1;
    archiveRequests.current.published += 1;
    archiveRequests.current.private += 1;
    setVideos({
      published: createVideoState(),
      private: createVideoState()
    });
    setArchives({
      published: createArchiveState(),
      private: createArchiveState()
    });
  }, [token]);

  const loadVideos = useCallback(
    async (
      tab: CreatorWorkTab,
      options: { reset?: boolean; filters?: CreatorFilters } = {}
    ) => {
      if (!token) return;
      const current = videos[tab];
      const reset = options.reset ?? current.state === "idle";
      const filters = options.filters || current.filters;
      const cursor = reset ? "" : current.nextCursor;
      const range = creatorArchiveMonthRange(filters.createdMonth);
      const requestID = videoRequests.current[tab] + 1;
      videoRequests.current[tab] = requestID;
      const requestToken = token;
      setVideos((state) => ({
        ...state,
        [tab]: {
          ...state[tab],
          filters,
          state: reset ? "loading" : "loadingMore",
          error: ""
        }
      }));
      try {
        const data = await queryCreatorVideos(token, {
          visibility: visibilityForTab(tab),
          query: filters.query.trim(),
          created_from: range.createdFrom,
          created_to: range.createdTo,
          cursor,
          limit: PAGE_LIMIT
        });
        if (videoRequests.current[tab] !== requestID || tokenRef.current !== requestToken) return;
        setVideos((state) => ({
          ...state,
          [tab]: {
            ...state[tab],
            items: reset ? data.items || [] : [...state[tab].items, ...(data.items || [])],
            nextCursor: data.next_cursor || "",
            hasMore: Boolean(data.has_more && data.next_cursor),
            state: "ready",
            error: ""
          }
        }));
      } catch (error) {
        if (videoRequests.current[tab] !== requestID || tokenRef.current !== requestToken) return;
        setVideos((state) => ({
          ...state,
          [tab]: {
            ...state[tab],
            state: "error",
            error: apiErrorMessage(error, "作品加载失败")
          }
        }));
      }
    },
    [token, videos]
  );

  const loadArchiveMonths = useCallback(
    async (tab: CreatorWorkTab): Promise<string[] | null> => {
      if (!token) return null;
      const requestID = archiveRequests.current[tab] + 1;
      archiveRequests.current[tab] = requestID;
      const requestToken = token;
      setArchives((state) => ({
        ...state,
        [tab]: {
          ...state[tab],
          state: "loading",
          error: ""
        }
      }));
      try {
        const data = await fetchCreatorArchiveMonths(token, visibilityForTab(tab));
        if (archiveRequests.current[tab] !== requestID || tokenRef.current !== requestToken) {
          return null;
        }
        setArchives((state) => ({
          ...state,
          [tab]: {
            months: data.months,
            state: "ready",
            error: ""
          }
        }));
        return data.months;
      } catch (error) {
        if (archiveRequests.current[tab] !== requestID || tokenRef.current !== requestToken) {
          return null;
        }
        setArchives((state) => ({
          ...state,
          [tab]: {
            ...state[tab],
            state: "error",
            error: apiErrorMessage(error, "作品日期加载失败")
          }
        }));
        return null;
      }
    },
    [token]
  );

  const ensureTab = useCallback(
    (tab: CreatorWorkTab) => {
      if (videos[tab].state === "idle") void loadVideos(tab, { reset: true });
      if (archives[tab].state === "idle") void loadArchiveMonths(tab);
    },
    [archives, loadArchiveMonths, loadVideos, videos]
  );

  const runBatchAction = useCallback(
    async (
      tab: CreatorWorkTab,
      videoIDs: number[],
      action: BatchVideoAction
    ): Promise<CreatorArchiveRefreshResult | null> => {
      if (!token || videoIDs.length === 0) return null;
      setVideos((state) => ({ ...state, [tab]: { ...state[tab], state: "mutating", error: "" } }));
      try {
        await applyVideoBatchAction(token, videoIDs, action, mutationKey(action));
        const [publishedMonths, privateMonths] = await Promise.all([
          loadArchiveMonths("published"),
          loadArchiveMonths("private")
        ]);
        const refreshedArchives: CreatorArchiveRefreshResult = {
          published: publishedMonths ?? archives.published.months,
          private: privateMonths ?? archives.private.months
        };
        const refreshedFilters = {
          published: reconcileArchiveFilter(videos.published.filters, refreshedArchives.published),
          private: reconcileArchiveFilter(videos.private.filters, refreshedArchives.private)
        };
        await Promise.all([
          loadVideos("published", { reset: true, filters: refreshedFilters.published }),
          loadVideos("private", { reset: true, filters: refreshedFilters.private }),
          onContentStatsChanged?.()
        ]);
        return refreshedArchives;
      } catch (error) {
        setVideos((state) => ({
          ...state,
          [tab]: {
            ...state[tab],
            state: "error",
            error: apiErrorMessage(error, "批量操作失败")
          }
        }));
        throw error;
      }
    },
    [archives, loadArchiveMonths, loadVideos, onContentStatsChanged, token, videos]
  );

  return {
    videos,
    archives,
    ensureTab,
    loadVideos,
    loadArchiveMonths,
    runBatchAction
  };
}

export function reconcileArchiveFilter(
  filters: CreatorFilters,
  months: readonly string[]
): CreatorFilters {
  if (!filters.createdMonth || months.includes(filters.createdMonth)) return filters;
  return { ...filters, createdMonth: "" };
}
