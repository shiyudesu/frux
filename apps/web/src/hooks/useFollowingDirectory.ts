import { useCallback, useEffect, useRef, useState } from "react";
import { apiErrorMessage } from "../api/client";
import { fetchRelationList } from "../api/social";
import type { AsyncState, RelationUser } from "../types";

const directoryPageLimit = 50;

export interface FollowingDirectoryState {
  items: RelationUser[];
  nextCursor: string;
  hasMore: boolean;
  state: AsyncState;
  error: string;
}

interface UseFollowingDirectoryOptions {
  token: string;
  enabled: boolean;
  debounceMs?: number;
}

function createState(): FollowingDirectoryState {
  return { items: [], nextCursor: "", hasMore: false, state: "idle", error: "" };
}

export function useFollowingDirectory({
  token,
  enabled,
  debounceMs = 250
}: UseFollowingDirectoryOptions) {
  const [query, setQuery] = useState("");
  const [normalizedQuery, setNormalizedQuery] = useState("");
  const [directory, setDirectory] = useState<FollowingDirectoryState>(createState);
  const directoryRef = useRef(directory);
  const tokenRef = useRef(token);
  const enabledRef = useRef(enabled);
  const queryRef = useRef(normalizedQuery);
  const generationRef = useRef(0);
  const loadingGenerationRef = useRef(0);
  const removedUserIDsRef = useRef(new Set<number>());
  directoryRef.current = directory;
  tokenRef.current = token;
  enabledRef.current = enabled;
  queryRef.current = normalizedQuery;

  useEffect(() => {
    const next = query.trim().toLocaleLowerCase();
    if (next === normalizedQuery) return undefined;
    const timer = window.setTimeout(() => setNormalizedQuery(next), debounceMs);
    return () => window.clearTimeout(timer);
  }, [debounceMs, normalizedQuery, query]);

  const replaceState = useCallback((next: FollowingDirectoryState) => {
    directoryRef.current = next;
    setDirectory(next);
  }, []);

  const load = useCallback(async (reset: boolean) => {
    const activeToken = tokenRef.current;
    if (!enabledRef.current || !activeToken) return;
    const current = directoryRef.current;
    if (!reset && (!current.hasMore || current.state === "loadingMore")) return;

    const generation = reset ? generationRef.current + 1 : generationRef.current;
    if (reset) {
      generationRef.current = generation;
    } else if (loadingGenerationRef.current === generation) {
      return;
    }

    loadingGenerationRef.current = generation;
    const cursor = reset ? "" : current.nextCursor;
    const activeQuery = queryRef.current;
    replaceState({
      ...current,
      ...(reset ? { items: [], nextCursor: "", hasMore: false } : {}),
      state: reset || current.items.length === 0 ? "loading" : "loadingMore",
      error: ""
    });

    try {
      const data = await fetchRelationList(
        "following",
        activeToken,
        cursor,
        directoryPageLimit,
        activeQuery
      );
      if (
        generationRef.current !== generation
        || tokenRef.current !== activeToken
        || queryRef.current !== activeQuery
      ) return;
      const incoming = (data.items || []).filter((item) => !removedUserIDsRef.current.has(item.user_id));
      const items = reset ? deduplicateUsers(incoming) : appendUniqueUsers(directoryRef.current.items, incoming);
      replaceState({
        items,
        nextCursor: data.next_cursor || "",
        hasMore: Boolean(data.has_more && data.next_cursor),
        state: "ready",
        error: ""
      });
    } catch (error) {
      if (generationRef.current !== generation || tokenRef.current !== activeToken) return;
      const latest = directoryRef.current;
      replaceState({
        ...latest,
        state: "error",
        error: apiErrorMessage(error, "关注列表加载失败，请稍后重试")
      });
    } finally {
      if (loadingGenerationRef.current === generation) loadingGenerationRef.current = 0;
    }
  }, [replaceState]);

  useEffect(() => {
    generationRef.current += 1;
    loadingGenerationRef.current = 0;
    replaceState(createState());
    if (enabled && token) {
      void load(true);
    }
  }, [enabled, load, normalizedQuery, replaceState, token]);

  const retry = useCallback(() => {
    void load(directoryRef.current.items.length === 0);
  }, [load]);

  const setUserActive = useCallback((user: RelationUser, active: boolean) => {
    if (!user.user_id) return;
    if (!active) {
      removedUserIDsRef.current.add(user.user_id);
      const current = directoryRef.current;
      replaceState({ ...current, items: current.items.filter((item) => item.user_id !== user.user_id) });
      return;
    }
    removedUserIDsRef.current.delete(user.user_id);
    if (!matchesQuery(user, queryRef.current)) return;
    const current = directoryRef.current;
    replaceState({
      ...current,
      items: [user, ...current.items.filter((item) => item.user_id !== user.user_id)]
    });
  }, [replaceState]);

  return {
    ...directory,
    query,
    normalizedQuery,
    setQuery,
    loadMore: () => void load(false),
    retry,
    refresh: () => void load(true),
    setUserActive
  };
}

export type ReturnTypeOfUseFollowingDirectory = ReturnType<typeof useFollowingDirectory>;

function matchesQuery(user: RelationUser, query: string): boolean {
  if (!query) return true;
  return user.nickname.toLocaleLowerCase().includes(query);
}

function deduplicateUsers(items: RelationUser[]): RelationUser[] {
  return appendUniqueUsers([], items);
}

function appendUniqueUsers(current: RelationUser[], incoming: RelationUser[]): RelationUser[] {
  const seen = new Set(current.map((item) => item.user_id));
  const next = [...current];
  for (const item of incoming) {
    if (!item.user_id || seen.has(item.user_id)) continue;
    seen.add(item.user_id);
    next.push(item);
  }
  return next;
}
