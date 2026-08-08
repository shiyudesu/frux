import type { FeedSceneKey } from "./constants";
import type { FeedVideo, RecommendationContext } from "./types";

export const MAX_RETAINED_FEED_ITEMS_PER_SCENE = 120;

export interface RecommendationSceneSnapshot {
  sessionID: string;
  nextRefreshIndex: number;
  context?: RecommendationContext;
  suppressedVideoIDs: number[];
  suppressedAuthorIDs: number[];
}

export interface FeedSceneSnapshot {
  scene: FeedSceneKey;
  authIdentity: string;
  items: FeedVideo[];
  index: number;
  activeVideoID: number;
  liked: Record<number, boolean>;
  favorited: Record<number, boolean>;
  nextCursor: string;
  hasMore: boolean;
  requestID: string;
  recommendation?: RecommendationSceneSnapshot;
}

export type FeedSceneSnapshots = Partial<Record<FeedSceneKey, FeedSceneSnapshot>>;

export interface CreateFeedSceneSnapshotInput {
  scene: FeedSceneKey;
  authIdentity: string;
  items: FeedVideo[];
  index: number;
  liked: Record<number, boolean>;
  favorited: Record<number, boolean>;
  nextCursor: string;
  hasMore: boolean;
  requestID: string;
  recommendation?: RecommendationSceneSnapshot;
}

export interface FeedSnapshotVideoPatch {
  item?: Partial<FeedVideo>;
  liked?: boolean;
  favorited?: boolean;
}

export function createFeedSceneSnapshot(input: CreateFeedSceneSnapshotInput): FeedSceneSnapshot {
  const items = [...input.items];
  const index = clampFeedIndex(items, input.index);
  return {
    scene: input.scene,
    authIdentity: input.authIdentity,
    items,
    index,
    activeVideoID: items[index]?.video_id || 0,
    liked: retainViewerActions(input.liked, items),
    favorited: retainViewerActions(input.favorited, items),
    nextCursor: input.nextCursor,
    hasMore: Boolean(input.hasMore && input.nextCursor),
    requestID: input.requestID,
    recommendation: cloneRecommendationSnapshot(input.recommendation)
  };
}

export function activateFeedSceneSnapshot(
  snapshot: FeedSceneSnapshot | undefined,
  scene: FeedSceneKey,
  authIdentity: string
): FeedSceneSnapshot | null {
  if (!snapshot || snapshot.scene !== scene || snapshot.authIdentity !== authIdentity || !snapshot.requestID) {
    return null;
  }
  if ((scene === "recommend") !== Boolean(snapshot.recommendation)) {
    return null;
  }
  if (
    snapshot.recommendation &&
    (
      !snapshot.recommendation.sessionID ||
      snapshot.recommendation.nextRefreshIndex < 0 ||
      snapshot.recommendation.context?.request_id !== snapshot.requestID
    )
  ) {
    return null;
  }
  if (snapshot.items.length === 0) {
    return createFeedSceneSnapshot({ ...snapshot, index: 0 });
  }
  const activeIndex = snapshot.items.findIndex((item) => item.video_id === snapshot.activeVideoID);
  if (activeIndex < 0) return null;
  return createFeedSceneSnapshot({ ...snapshot, index: activeIndex });
}

export function compactFeedSceneSnapshot(
  snapshot: FeedSceneSnapshot,
  limit = MAX_RETAINED_FEED_ITEMS_PER_SCENE
): FeedSceneSnapshot | null {
  const normalizedLimit = Math.max(1, Math.trunc(limit));
  if (snapshot.items.length <= normalizedLimit) {
    return createFeedSceneSnapshot(snapshot);
  }
  const activeIndex = snapshot.items.findIndex((item) => item.video_id === snapshot.activeVideoID);
  const start = snapshot.items.length - normalizedLimit;
  if (activeIndex < start) return null;
  return createFeedSceneSnapshot({
    ...snapshot,
    items: snapshot.items.slice(start),
    index: activeIndex - start
  });
}

export function setFeedSceneSnapshotIndex(snapshot: FeedSceneSnapshot, index: number): FeedSceneSnapshot {
  return createFeedSceneSnapshot({ ...snapshot, index });
}

export function replaceFeedSceneSnapshot(
  snapshots: FeedSceneSnapshots,
  snapshot: FeedSceneSnapshot
): FeedSceneSnapshots {
  return { ...snapshots, [snapshot.scene]: snapshot };
}

export function removeFeedSceneSnapshot(
  snapshots: FeedSceneSnapshots,
  scene: FeedSceneKey
): FeedSceneSnapshots {
  if (!snapshots[scene]) return snapshots;
  const next = { ...snapshots };
  delete next[scene];
  return next;
}

export function updateFeedSceneSnapshot(
  snapshots: FeedSceneSnapshots,
  scene: FeedSceneKey,
  update: (snapshot: FeedSceneSnapshot) => FeedSceneSnapshot | null
): FeedSceneSnapshots {
  const current = snapshots[scene];
  if (!current) return snapshots;
  const updated = update(current);
  return updated
    ? replaceFeedSceneSnapshot(snapshots, updated)
    : removeFeedSceneSnapshot(snapshots, scene);
}

export function patchFeedSceneSnapshots(
  snapshots: FeedSceneSnapshots,
  videoID: number,
  patch: FeedSnapshotVideoPatch
): FeedSceneSnapshots {
  let changed = false;
  const next: FeedSceneSnapshots = { ...snapshots };
  for (const scene of feedSceneKeys()) {
    const snapshot = snapshots[scene];
    if (!snapshot || !snapshot.items.some((item) => item.video_id === videoID)) continue;
    changed = true;
    next[scene] = createFeedSceneSnapshot({
      ...snapshot,
      items: snapshot.items.map((item) => item.video_id === videoID
        ? {
            ...item,
            ...patch.item,
            ...(patch.liked === undefined ? {} : { liked: patch.liked }),
            ...(patch.favorited === undefined ? {} : { favorited: patch.favorited })
          }
        : item),
      liked: patch.liked === undefined
        ? snapshot.liked
        : { ...snapshot.liked, [videoID]: patch.liked },
      favorited: patch.favorited === undefined
        ? snapshot.favorited
        : { ...snapshot.favorited, [videoID]: patch.favorited }
    });
  }
  return changed ? next : snapshots;
}

export function feedAuthIdentity(token: string, userID: number): string {
  return token ? `${Math.max(0, Math.trunc(userID))}:${token}` : "anonymous";
}

function clampFeedIndex(items: FeedVideo[], index: number): number {
  if (items.length === 0) return 0;
  return Math.min(Math.max(0, Math.trunc(index)), items.length - 1);
}

function retainViewerActions(
  actions: Record<number, boolean>,
  items: FeedVideo[]
): Record<number, boolean> {
  const retainedIDs = new Set(items.map((item) => item.video_id));
  return Object.fromEntries(
    Object.entries(actions).filter(([videoID]) => retainedIDs.has(Number(videoID)))
  );
}

function cloneRecommendationSnapshot(
  snapshot: RecommendationSceneSnapshot | undefined
): RecommendationSceneSnapshot | undefined {
  if (!snapshot) return undefined;
  return {
    sessionID: snapshot.sessionID,
    nextRefreshIndex: snapshot.nextRefreshIndex,
    context: snapshot.context
      ? {
          ...snapshot.context,
          recent_video_ids: [...snapshot.context.recent_video_ids],
          playback_capabilities: [...snapshot.context.playback_capabilities]
        }
      : undefined,
    suppressedVideoIDs: [...snapshot.suppressedVideoIDs],
    suppressedAuthorIDs: [...snapshot.suppressedAuthorIDs]
  };
}

function feedSceneKeys(): FeedSceneKey[] {
  return ["timeline", "recommend", "following", "hot"];
}
