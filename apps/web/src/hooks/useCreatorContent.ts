import { useCallback, useRef, useState } from "react";
import {
  applyVideoBatchAction,
  createVideoCollection,
  deleteVideoCollection,
  fetchMyCollections,
  queryCreatorVideos,
  setCollectionVideo,
  updateVideoCollection
} from "../api/creator";
import { apiErrorMessage } from "../api/client";
import type {
  AsyncState,
  BatchVideoAction,
  CollectionVisibility,
  CreateVideoCollectionRequest,
  CreatorWorkTab,
  UpdateVideoCollectionRequest,
  Video,
  VideoCollection,
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

export interface CreatorCollectionState {
  items: VideoCollection[];
  nextCursor: string;
  hasMore: boolean;
  state: AsyncState;
  error: string;
}

export interface CollectionVideoOptionsState {
  items: Video[];
  pages: Record<VideoVisibility, { nextCursor: string; hasMore: boolean }>;
  query: string;
  state: AsyncState;
  error: string;
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

const initialCollections: CreatorCollectionState = {
  items: [],
  nextCursor: "",
  hasMore: false,
  state: "idle",
  error: ""
};

function createCollectionVideoOptionsState(): CollectionVideoOptionsState {
  return {
    items: [],
    pages: {
      public: { nextCursor: "", hasMore: true },
      private: { nextCursor: "", hasMore: true }
    },
    query: "",
    state: "idle",
    error: ""
  };
}

function visibilityForTab(tab: Exclude<CreatorWorkTab, "collections">): VideoVisibility {
  return tab === "published" ? "public" : "private";
}

function mutationKey(scope: string): string {
  return `web-profile-${scope}-${Date.now()}-${Math.random().toString(36).slice(2, 9)}`;
}

function collectionFingerprint(body: CreateVideoCollectionRequest): string {
  return JSON.stringify({
    title: body.title.trim(),
    description: body.description.trim(),
    visibility: body.visibility
  });
}

function mergeCollectionVideoOptions(current: Video[], incoming: Video[], reset: boolean): Video[] {
  const merged = new Map<number, Video>();
  if (!reset) {
    for (const video of current) merged.set(video.id, video);
  }
  for (const video of incoming) merged.set(video.id, video);
  return [...merged.values()].sort((left, right) => {
    const createdOrder = right.created_at.localeCompare(left.created_at);
    return createdOrder || right.id - left.id;
  });
}

export function useCreatorContent(token: string, onContentStatsChanged?: () => Promise<void> | void) {
  const [videos, setVideos] = useState<Record<Exclude<CreatorWorkTab, "collections">, CreatorVideoState>>({
    published: createVideoState(),
    private: createVideoState()
  });
  const [collections, setCollections] = useState<CreatorCollectionState>(initialCollections);
  const [collectionVideos, setCollectionVideos] = useState<CollectionVideoOptionsState>(
    createCollectionVideoOptionsState
  );
  const videoRequests = useRef({ published: 0, private: 0 });
  const collectionRequest = useRef(0);
  const collectionVideoRequest = useRef(0);
  const collectionCreateAttempt = useRef<{ fingerprint: string; key: string } | null>(null);

  const loadVideos = useCallback(
    async (
      tab: Exclude<CreatorWorkTab, "collections">,
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

  const loadCollections = useCallback(
    async (reset = false) => {
      if (!token) return;
      const shouldReset = reset || collections.state === "idle";
      const requestID = collectionRequest.current + 1;
      collectionRequest.current = requestID;
      setCollections((state) => ({
        ...state,
        state: shouldReset ? "loading" : "loadingMore",
        error: ""
      }));
      try {
        const data = await fetchMyCollections(token, shouldReset ? "" : collections.nextCursor);
        if (collectionRequest.current !== requestID) return;
        setCollections((state) => ({
          items: shouldReset ? data.items || [] : [...state.items, ...(data.items || [])],
          nextCursor: data.next_cursor || "",
          hasMore: Boolean(data.has_more && data.next_cursor),
          state: "ready",
          error: ""
        }));
      } catch (error) {
        if (collectionRequest.current !== requestID) return;
        setCollections((state) => ({
          ...state,
          state: "error",
          error: apiErrorMessage(error, "合集加载失败")
        }));
      }
    },
    [collections.nextCursor, collections.state, token]
  );

  const ensureTab = useCallback(
    (tab: CreatorWorkTab) => {
      if (tab === "collections") {
        if (collections.state === "idle") void loadCollections(true);
        return;
      }
      if (videos[tab].state === "idle") void loadVideos(tab, { reset: true });
    },
    [collections.state, loadCollections, loadVideos, videos]
  );

  const loadCollectionVideos = useCallback(
    async (query = collectionVideos.query, reset = false) => {
      if (!token) return;
      const normalizedQuery = query.trim();
      const shouldReset = reset || collectionVideos.state === "idle" || normalizedQuery !== collectionVideos.query;
      const visibilities = (["public", "private"] as const).filter(
        (visibility) => shouldReset || collectionVideos.pages[visibility].hasMore
      );
      if (visibilities.length === 0) return;
      const requestID = collectionVideoRequest.current + 1;
      collectionVideoRequest.current = requestID;
      setCollectionVideos((state) => ({
        ...state,
        query: normalizedQuery,
        state: shouldReset ? "loading" : "loadingMore",
        error: ""
      }));
      try {
        const pages = await Promise.all(
          visibilities.map(async (visibility) => ({
            visibility,
            data: await queryCreatorVideos(token, {
              visibility,
              query: normalizedQuery,
              created_from: "",
              created_to: "",
              cursor: shouldReset ? "" : collectionVideos.pages[visibility].nextCursor,
              limit: PAGE_LIMIT
            })
          }))
        );
        if (collectionVideoRequest.current !== requestID) return;
        const incoming = pages.flatMap(({ data }) => data.items || []);
        setCollectionVideos((state) => {
          const nextPages = shouldReset
            ? createCollectionVideoOptionsState().pages
            : { ...state.pages };
          for (const { visibility, data } of pages) {
            nextPages[visibility] = {
              nextCursor: data.next_cursor || "",
              hasMore: Boolean(data.has_more && data.next_cursor)
            };
          }
          return {
            items: mergeCollectionVideoOptions(state.items, incoming, shouldReset),
            pages: nextPages,
            query: normalizedQuery,
            state: "ready",
            error: ""
          };
        });
      } catch (error) {
        if (collectionVideoRequest.current !== requestID) return;
        setCollectionVideos((state) => ({
          ...state,
          state: "error",
          error: apiErrorMessage(error, "合集可选作品加载失败")
        }));
      }
    },
    [collectionVideos, token]
  );

  const runBatchAction = useCallback(
    async (tab: Exclude<CreatorWorkTab, "collections">, videoIDs: number[], action: BatchVideoAction) => {
      if (!token || videoIDs.length === 0) return;
      setVideos((state) => ({ ...state, [tab]: { ...state[tab], state: "mutating", error: "" } }));
      try {
        await applyVideoBatchAction(token, videoIDs, action, mutationKey(action));
        await Promise.all([
          loadVideos("published", { reset: true }),
          loadVideos("private", { reset: true }),
          collections.state === "idle" ? Promise.resolve() : loadCollections(true),
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
    [collections.state, loadCollections, loadVideos, onContentStatsChanged, token]
  );

  const createCollection = useCallback(
    async (body: CreateVideoCollectionRequest) => {
      if (!token) return null;
      const fingerprint = collectionFingerprint(body);
      if (!collectionCreateAttempt.current || collectionCreateAttempt.current.fingerprint !== fingerprint) {
        collectionCreateAttempt.current = { fingerprint, key: mutationKey("collection") };
      }
      const created = await createVideoCollection(token, body, collectionCreateAttempt.current.key);
      collectionCreateAttempt.current = null;
      await Promise.all([loadCollections(true), onContentStatsChanged?.()]);
      return created;
    },
    [loadCollections, onContentStatsChanged, token]
  );

  const editCollection = useCallback(
    async (collectionID: number, body: UpdateVideoCollectionRequest) => {
      if (!token) return null;
      const updated = await updateVideoCollection(token, collectionID, body);
      await loadCollections(true);
      return updated;
    },
    [loadCollections, token]
  );

  const removeCollection = useCallback(
    async (collectionID: number) => {
      if (!token) return;
      await deleteVideoCollection(token, collectionID);
      await Promise.all([loadCollections(true), onContentStatsChanged?.()]);
    },
    [loadCollections, onContentStatsChanged, token]
  );

  const setMembership = useCallback(
    async (collectionID: number, videoID: number, active: boolean) => {
      if (!token) return;
      await setCollectionVideo(token, collectionID, videoID, active);
      await loadCollections(true);
    },
    [loadCollections, token]
  );

  const setCollectionVisibility = useCallback(
    (collectionID: number, visibility: CollectionVisibility) =>
      editCollection(collectionID, { visibility }),
    [editCollection]
  );

  return {
    videos,
    collections,
    collectionVideos,
    ensureTab,
    loadVideos,
    loadCollections,
    loadCollectionVideos,
    runBatchAction,
    createCollection,
    editCollection,
    removeCollection,
    setMembership,
    setCollectionVisibility
  };
}
