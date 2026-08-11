import { useCallback, useRef, useState } from "react";
import {
  applyVideoBatchAction,
  queryCreatorVideos
} from "../api/creator";
import { apiErrorMessage } from "../api/client";
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
  createdFrom: string;
  createdTo: string;
}

export interface CreatorVideoState {
  items: Video[];
  nextCursor: string;
  hasMore: boolean;
  state: AsyncState;
  error: string;
  filters: CreatorFilters;
}

const emptyFilters: CreatorFilters = { query: "", createdFrom: "", createdTo: "" };

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
  const videoRequests = useRef({ published: 0, private: 0 });

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
      const requestID = videoRequests.current[tab] + 1;
      videoRequests.current[tab] = requestID;
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
          created_from: filters.createdFrom,
          created_to: filters.createdTo,
          cursor,
          limit: PAGE_LIMIT
        });
        if (videoRequests.current[tab] !== requestID) return;
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
        if (videoRequests.current[tab] !== requestID) return;
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

  const ensureTab = useCallback(
    (tab: CreatorWorkTab) => {
      if (videos[tab].state === "idle") void loadVideos(tab, { reset: true });
    },
    [loadVideos, videos]
  );

  const runBatchAction = useCallback(
    async (tab: CreatorWorkTab, videoIDs: number[], action: BatchVideoAction) => {
      if (!token || videoIDs.length === 0) return;
      setVideos((state) => ({ ...state, [tab]: { ...state[tab], state: "mutating", error: "" } }));
      try {
        await applyVideoBatchAction(token, videoIDs, action, mutationKey(action));
        await Promise.all([
          loadVideos("published", { reset: true }),
          loadVideos("private", { reset: true }),
          onContentStatsChanged?.()
        ]);
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
    [loadVideos, onContentStatsChanged, token]
  );

  return {
    videos,
    ensureTab,
    loadVideos,
    runBatchAction
  };
}
