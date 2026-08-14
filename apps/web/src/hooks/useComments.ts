import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { apiErrorMessage, isUnauthorized } from "../api/client";
import {
  createComment,
  createCommentOperationKey,
  createCommentReply,
  deleteComment,
  fetchCommentReplies,
  fetchComments,
  fetchCommentThread,
  setCommentLike
} from "../api/social";
import { useNavigate } from "../router";
import { useSession } from "../session";
import type {
  Comment,
  CommentLikeResponse,
  CommentSort,
  CommentThreadContextResponse,
  DeleteCommentResponse
} from "../types";

export const COMMENT_CONTENT_LIMIT = 1000;
export type CommentLoadState = "idle" | "loading" | "loadingMore" | "ready" | "error";

export interface CommentListState {
  ids: number[];
  nextCursor: string;
  hasMore: boolean;
  state: CommentLoadState;
  error: string;
}

export interface CommentOperationState {
  busy: boolean;
  error: string;
}

export interface VideoCommentsState {
  sort: CommentSort;
  roots: Record<CommentSort, CommentListState>;
  contextRootIDs: number[];
  replies: Record<number, CommentListState>;
  contextReplyIDs: Record<number, number[]>;
  pendingReplyIDs: Record<number, number[]>;
  expandedRootIDs: number[];
  draft: string;
  replyTargetID: number;
  focusedRootID: number;
  focusedTargetID: number;
  focusRevision: number;
  focusUnavailable: boolean;
  create: CommentOperationState;
  likes: Record<number, CommentOperationState>;
  deletes: Record<number, CommentOperationState>;
}

export interface CommentsStore {
  entities: Record<number, Comment>;
  videos: Record<number, VideoCommentsState>;
}

export interface CommentRequestIdentity {
  key: string;
  generation: number;
  viewerGeneration: number;
}

export interface UseCommentsOptions {
  videoID: number;
  enabled: boolean;
  focusedCommentID?: number;
  focusedTargetID?: number;
  onCommentCountChange?: (count: number) => void;
}

const emptyOperation = (): CommentOperationState => ({ busy: false, error: "" });
const emptyList = (): CommentListState => ({
  ids: [],
  nextCursor: "",
  hasMore: false,
  state: "idle",
  error: ""
});

export function createVideoCommentsState(): VideoCommentsState {
  return {
    sort: "hot",
    roots: { hot: emptyList(), latest: emptyList() },
    contextRootIDs: [],
    replies: {},
    contextReplyIDs: {},
    pendingReplyIDs: {},
    expandedRootIDs: [],
    draft: "",
    replyTargetID: 0,
    focusedRootID: 0,
    focusedTargetID: 0,
    focusRevision: 0,
    focusUnavailable: false,
    create: emptyOperation(),
    likes: {},
    deletes: {}
  };
}

export function createCommentsStore(): CommentsStore {
  return { entities: {}, videos: {} };
}

export function mergeCommentIDs(current: number[], incoming: number[], prepend = false): number[] {
  const seen = new Set<number>();
  const merged: number[] = [];
  for (const id of prepend ? [...incoming, ...current] : [...current, ...incoming]) {
    if (id <= 0 || seen.has(id)) continue;
    seen.add(id);
    merged.push(id);
  }
  return merged;
}

export function unicodeLength(value: string): number {
  return Array.from(value).length;
}

export function useComments({
  videoID,
  enabled,
  focusedCommentID = 0,
  focusedTargetID = 0,
  onCommentCountChange
}: UseCommentsOptions) {
  const session = useSession();
  const navigate = useNavigate();
  const [store, setStore] = useState<CommentsStore>(createCommentsStore);
  const storeRef = useRef(store);
  const requestGenerationRef = useRef<Record<string, number>>({});
  const viewerGenerationRef = useRef(0);
  const createRetryRef = useRef<Record<number, { fingerprint: string; key: string }>>({});
  const viewerIdentity = session.token && session.user
    ? `${session.user.id}:${session.token}`
    : "anonymous";
  const renderedViewerIdentityRef = useRef(viewerIdentity);
  const appliedViewerIdentityRef = useRef(viewerIdentity);
  if (renderedViewerIdentityRef.current !== viewerIdentity) {
    renderedViewerIdentityRef.current = viewerIdentity;
    viewerGenerationRef.current += 1;
    requestGenerationRef.current = {};
    createRetryRef.current = {};
  }
  storeRef.current = store;

  const updateStore = useCallback((update: (current: CommentsStore) => CommentsStore) => {
    setStore((current) => {
      const next = update(current);
      storeRef.current = next;
      return next;
    });
  }, []);

  useEffect(() => {
    if (videoID <= 0 || storeRef.current.videos[videoID]) return;
    updateStore((current) => ({
      ...current,
      videos: { ...current.videos, [videoID]: createVideoCommentsState() }
    }));
  }, [updateStore, videoID]);

  const requireLogin = useCallback(() => {
    if (session.token && session.user) return true;
    navigate("/auth");
    return false;
  }, [navigate, session.token, session.user]);

  const handleUnauthorized = useCallback(() => {
    session.clearAuth();
    navigate("/auth");
  }, [navigate, session]);

  const mergeEntities = useCallback((comments: Comment[]) => {
    updateStore((current) => {
      const entities = { ...current.entities };
      for (const comment of comments) {
        entities[comment.id] = normalizeComment(comment);
        for (const preview of comment.reply_previews || []) {
          entities[preview.id] = normalizeComment(preview);
        }
      }
      return { ...current, entities };
    });
  }, [updateStore]);

  const loadRoots = useCallback(async (reset = false, requestedSort?: CommentSort) => {
    if (videoID <= 0) return;
    const video = storeRef.current.videos[videoID] || createVideoCommentsState();
    const sort = requestedSort || video.sort;
    const list = video.roots[sort];
    if (!reset && (list.state === "loading" || list.state === "loadingMore")) return;
    if (!reset && list.state === "ready" && !list.hasMore && list.ids.length > 0) return;
    const cursor = reset ? "" : list.nextCursor;
    const key = `roots:${videoID}:${sort}`;
    const request = beginCommentRequest(
      requestGenerationRef.current,
      key,
      viewerGenerationRef.current
    );
    updateStore((current) => updateVideo(current, videoID, (state) => ({
      ...state,
      roots: {
        ...state.roots,
        [sort]: {
          ...(reset ? emptyList() : state.roots[sort]),
          state: cursor ? "loadingMore" : "loading",
          error: ""
        }
      }
    })));
    try {
      const data = await fetchComments(videoID, sort, cursor, 20, session.token);
      if (!isCurrentCommentRequest(
        requestGenerationRef.current,
        request,
        viewerGenerationRef.current
      )) return;
      const items = data.items || [];
      if (
        data.sort !== sort ||
        items.some((item) => item.video_id !== videoID || Number(item.root_comment_id || 0) !== 0)
      ) {
        throw new Error("invalid root page context");
      }
      mergeEntities(items);
      updateStore((current) => updateVideo(current, videoID, (state) => ({
        ...state,
        roots: {
          ...state.roots,
          [sort]: {
            ids: mergeCommentIDs(reset ? [] : state.roots[sort].ids, items.map((item) => item.id)),
            nextCursor: data.next_cursor || "",
            hasMore: Boolean(data.has_more && data.next_cursor),
            state: "ready",
            error: ""
          }
        }
      })));
      onCommentCountChange?.(data.comment_count);
    } catch (error) {
      if (!isCurrentCommentRequest(
        requestGenerationRef.current,
        request,
        viewerGenerationRef.current
      )) return;
      updateStore((current) => updateVideo(current, videoID, (state) => ({
        ...state,
        roots: {
          ...state.roots,
          [sort]: {
            ...state.roots[sort],
            state: "error",
            error: apiErrorMessage(error, "评论加载失败")
          }
        }
      })));
    }
  }, [mergeEntities, onCommentCountChange, session.token, updateStore, videoID]);

  const loadReplies = useCallback(async (rootCommentID: number, reset = false) => {
    if (rootCommentID <= 0) return;
    const video = storeRef.current.videos[videoID] || createVideoCommentsState();
    const list = video.replies[rootCommentID] || emptyList();
    if (!reset && (list.state === "loading" || list.state === "loadingMore")) return;
    if (!reset && list.state === "ready" && !list.hasMore && list.ids.length > 0) return;
    const cursor = reset ? "" : list.nextCursor;
    const key = `replies:${videoID}:${rootCommentID}`;
    const request = beginCommentRequest(
      requestGenerationRef.current,
      key,
      viewerGenerationRef.current
    );
    updateStore((current) => updateVideo(current, videoID, (state) => ({
      ...state,
      replies: {
        ...state.replies,
        [rootCommentID]: {
          ...(reset ? emptyList() : state.replies[rootCommentID] || emptyList()),
          state: cursor ? "loadingMore" : "loading",
          error: ""
        }
      }
    })));
    try {
      const data = await fetchCommentReplies(rootCommentID, cursor, 20, session.token);
      if (!isCurrentCommentRequest(
        requestGenerationRef.current,
        request,
        viewerGenerationRef.current
      )) return;
      if (
        data.root_comment_id !== rootCommentID ||
        (data.items || []).some((item) =>
          item.video_id !== videoID || item.root_comment_id !== rootCommentID
        )
      ) {
        throw new Error("invalid reply page context");
      }
      mergeEntities(data.items || []);
      updateStore((current) => updateVideo(current, videoID, (state) => ({
        ...state,
        replies: {
          ...state.replies,
          [rootCommentID]: {
            ids: mergeCommentIDs(
              reset ? [] : state.replies[rootCommentID]?.ids || [],
              (data.items || []).map((item) => item.id)
            ),
            nextCursor: data.next_cursor || "",
            hasMore: Boolean(data.has_more && data.next_cursor),
            state: "ready",
            error: ""
          }
        }
      })));
      onCommentCountChange?.(data.comment_count);
    } catch (error) {
      if (!isCurrentCommentRequest(
        requestGenerationRef.current,
        request,
        viewerGenerationRef.current
      )) return;
      updateStore((current) => updateVideo(current, videoID, (state) => ({
        ...state,
        replies: {
          ...state.replies,
          [rootCommentID]: {
            ...(state.replies[rootCommentID] || emptyList()),
            state: "error",
            error: apiErrorMessage(error, "回复加载失败")
          }
        }
      })));
    }
  }, [mergeEntities, onCommentCountChange, session.token, updateStore, videoID]);

  const loadThreadContext = useCallback(async (targetCommentID: number) => {
    if (videoID <= 0 || targetCommentID <= 0) return;
    const key = `thread:${videoID}`;
    const request = beginCommentRequest(
      requestGenerationRef.current,
      key,
      viewerGenerationRef.current
    );
    updateStore((current) => updateVideo(current, videoID, (state) => ({
      ...state,
      contextRootIDs: [],
      contextReplyIDs: {},
      focusedRootID: focusedCommentID,
      focusedTargetID: targetCommentID,
      focusUnavailable: false
    })));
    try {
      const data = await fetchCommentThread(targetCommentID, 20, session.token);
      if (!isCurrentCommentRequest(
        requestGenerationRef.current,
        request,
        viewerGenerationRef.current
      )) return;
      if (!isValidCommentThreadContext(data, videoID, targetCommentID, focusedCommentID)) {
        updateStore((current) => updateVideo(current, videoID, (state) => ({
          ...state,
          focusedRootID: focusedCommentID,
          focusedTargetID: targetCommentID,
          focusUnavailable: true
        })));
        return;
      }
      mergeEntities([data.root, ...(data.replies || []), data.target]);
      const rootID = data.root.id;
      const pageReplyIDs = (data.replies || []).map((item) => item.id);
      const supplementalTargetIDs = data.target.id !== rootID && !pageReplyIDs.includes(data.target.id)
        ? [data.target.id]
        : [];
      updateStore((current) => updateVideo(current, videoID, (state) => ({
        ...state,
        expandedRootIDs: mergeCommentIDs(state.expandedRootIDs, [rootID]),
        contextRootIDs: mergeCommentIDs(state.contextRootIDs, [rootID], true),
        focusedRootID: rootID,
        focusedTargetID: data.target.id,
        focusRevision: state.focusRevision + 1,
        focusUnavailable: false,
        replies: {
          ...state.replies,
          [rootID]: {
            ids: mergeCommentIDs([], pageReplyIDs),
            nextCursor: data.next_cursor || "",
            hasMore: Boolean(data.has_more && data.next_cursor),
            state: "ready",
            error: ""
          }
        },
        contextReplyIDs: {
          ...state.contextReplyIDs,
          [rootID]: mergeCommentIDs(
            state.contextReplyIDs[rootID] || [],
            supplementalTargetIDs
          )
        }
      })));
      onCommentCountChange?.(data.comment_count);
    } catch {
      if (!isCurrentCommentRequest(
        requestGenerationRef.current,
        request,
        viewerGenerationRef.current
      )) return;
      updateStore((current) => updateVideo(current, videoID, (state) => ({
        ...state,
        focusedRootID: focusedCommentID,
        focusedTargetID: targetCommentID,
        focusUnavailable: true
      })));
    }
  }, [focusedCommentID, mergeEntities, onCommentCountChange, session.token, updateStore, videoID]);

  useEffect(() => {
    if (!enabled || videoID <= 0) return;
    const video = storeRef.current.videos[videoID] || createVideoCommentsState();
    if (video.roots[video.sort].state === "idle") void loadRoots(false, video.sort);
    for (const rootID of video.expandedRootIDs) {
      if (focusedCommentID > 0 && rootID === focusedCommentID) continue;
      const replies = video.replies[rootID];
      if (!replies || replies.state === "idle") void loadReplies(rootID, true);
    }
  }, [enabled, focusedCommentID, loadReplies, loadRoots, videoID]);

  useEffect(() => {
    if (!enabled || videoID <= 0) return;
    const targetID = focusedTargetID || focusedCommentID;
    if (targetID > 0) void loadThreadContext(targetID);
  }, [enabled, focusedCommentID, focusedTargetID, loadThreadContext, videoID]);

  useEffect(() => {
    if (appliedViewerIdentityRef.current === viewerIdentity) return;
    appliedViewerIdentityRef.current = viewerIdentity;
    const expandedRootIDs = storeRef.current.videos[videoID]?.expandedRootIDs || [];
    const targetID = focusedTargetID || focusedCommentID;
    updateStore((current) => applyCommentViewerTransition(current));
    if (enabled && videoID > 0) {
      const sort = storeRef.current.videos[videoID]?.sort || "hot";
      void loadRoots(true, sort);
      if (targetID > 0) void loadThreadContext(targetID);
      for (const rootID of expandedRootIDs) {
        if (rootID === focusedCommentID && targetID > 0) continue;
        void loadReplies(rootID, true);
      }
    }
  }, [
    enabled,
    focusedCommentID,
    focusedTargetID,
    loadRoots,
    loadReplies,
    loadThreadContext,
    updateStore,
    viewerIdentity,
    videoID
  ]);

  const setSort = useCallback((sort: CommentSort) => {
    updateStore((current) => updateVideo(current, videoID, (state) => ({ ...state, sort })));
    const list = storeRef.current.videos[videoID]?.roots[sort];
    if (!list || list.state === "idle") void loadRoots(false, sort);
  }, [loadRoots, updateStore, videoID]);

  const toggleReplies = useCallback((rootCommentID: number) => {
    const expanded = storeRef.current.videos[videoID]?.expandedRootIDs.includes(rootCommentID);
    updateStore((current) => updateVideo(current, videoID, (state) => ({
      ...state,
      expandedRootIDs: expanded
        ? state.expandedRootIDs.filter((id) => id !== rootCommentID)
        : mergeCommentIDs(state.expandedRootIDs, [rootCommentID])
    })));
    if (!expanded) {
      const list = storeRef.current.videos[videoID]?.replies[rootCommentID];
      if (!list || list.state === "idle") void loadReplies(rootCommentID, true);
    }
  }, [loadReplies, updateStore, videoID]);

  const setDraft = useCallback((draft: string) => {
    updateStore((current) => updateVideo(current, videoID, (state) => ({ ...state, draft })));
  }, [updateStore, videoID]);

  const selectReplyTarget = useCallback((commentID: number) => {
    if (!requireLogin()) return;
    updateStore((current) => updateVideo(current, videoID, (state) => ({
      ...state,
      replyTargetID: commentID,
      create: emptyOperation()
    })));
  }, [requireLogin, updateStore, videoID]);

  const cancelReply = useCallback(() => {
    updateStore((current) => updateVideo(current, videoID, (state) => ({
      ...state,
      replyTargetID: 0,
      create: emptyOperation()
    })));
  }, [updateStore, videoID]);

  const clearFocus = useCallback(() => {
    updateStore((current) => updateVideo(current, videoID, (state) => ({
      ...state,
      focusedTargetID: 0
    })));
  }, [updateStore, videoID]);

  const submitComment = useCallback(async () => {
    const video = storeRef.current.videos[videoID] || createVideoCommentsState();
    if (!requireLogin() || video.create.busy) return;
    const content = video.draft.trim();
    if (!content || unicodeLength(content) > COMMENT_CONTENT_LIMIT) return;
    const targetID = video.replyTargetID;
    const fingerprint = `${videoID}:${targetID}:${content}`;
    const retry = createRetryRef.current[videoID];
    const key = retry?.fingerprint === fingerprint
      ? retry.key
      : createCommentOperationKey(targetID ? "reply" : "root", targetID || videoID);
    const operationViewerGeneration = viewerGenerationRef.current;
    createRetryRef.current[videoID] = { fingerprint, key };
    updateStore((current) => updateVideo(current, videoID, (state) => ({
      ...state,
      create: { busy: true, error: "" }
    })));
    try {
      const created = targetID
        ? await createCommentReply(session.token, videoID, targetID, content, key)
        : await createComment(session.token, videoID, content, key);
      if (viewerGenerationRef.current !== operationViewerGeneration) return;
      createRetryRef.current[videoID] = { fingerprint: "", key: "" };
      const normalized = normalizeComment(created);
      const replyList = normalized.root_comment_id > 0
        ? storeRef.current.videos[videoID]?.replies[normalized.root_comment_id]
        : undefined;
      updateStore((current) => applyCreatedComment(current, videoID, normalized));
      onCommentCountChange?.(created.comment_count || 0);
      if (normalized.root_comment_id > 0 && (!replyList || replyList.state === "idle")) {
        void loadReplies(normalized.root_comment_id, true);
      } else if (normalized.root_comment_id <= 0) {
        void loadRoots(true, "hot");
      }
    } catch (error) {
      if (viewerGenerationRef.current !== operationViewerGeneration) return;
      if (isUnauthorized(error)) {
        updateStore((current) => updateVideo(current, videoID, (state) => ({
          ...state,
          create: emptyOperation()
        })));
        handleUnauthorized();
        return;
      }
      updateStore((current) => updateVideo(current, videoID, (state) => ({
        ...state,
        create: { busy: false, error: apiErrorMessage(error, "评论发布失败") }
      })));
    }
  }, [
    handleUnauthorized,
    loadReplies,
    loadRoots,
    onCommentCountChange,
    requireLogin,
    session.token,
    updateStore,
    videoID
  ]);

  const toggleCommentLike = useCallback(async (commentID: number) => {
    if (!requireLogin()) return;
    const comment = storeRef.current.entities[commentID];
    const operation = storeRef.current.videos[videoID]?.likes[commentID];
    if (!comment || comment.deleted || operation?.busy) return;
    const previous = { liked: comment.liked, likeCount: comment.like_count };
    const liked = !comment.liked;
    const operationViewerGeneration = viewerGenerationRef.current;
    updateStore((current) => applyOptimisticCommentLike(current, videoID, commentID, liked));
    try {
      const result = await setCommentLike(session.token, commentID, liked);
      if (viewerGenerationRef.current !== operationViewerGeneration) return;
      updateStore((current) => applyConfirmedCommentLike(
        current, videoID, commentID, result
      ));
    } catch (error) {
      if (viewerGenerationRef.current !== operationViewerGeneration) return;
      if (isUnauthorized(error)) {
        updateStore((current) => applyCommentLikeRollback(
          current,
          videoID,
          commentID,
          previous.liked,
          previous.likeCount,
          ""
        ));
        handleUnauthorized();
        return;
      }
      updateStore((current) => applyCommentLikeRollback(
        current,
        videoID,
        commentID,
        previous.liked,
        previous.likeCount,
        apiErrorMessage(error, "点赞失败")
      ));
    }
  }, [handleUnauthorized, requireLogin, session.token, updateStore, videoID]);

  const removeComment = useCallback(async (commentID: number) => {
    if (!requireLogin()) return;
    const comment = storeRef.current.entities[commentID];
    const operation = storeRef.current.videos[videoID]?.deletes[commentID];
    if (!comment?.can_delete || operation?.busy) return;
    const operationViewerGeneration = viewerGenerationRef.current;
    updateStore((current) => updateVideo(current, videoID, (state) => ({
      ...state,
      deletes: { ...state.deletes, [commentID]: { busy: true, error: "" } }
    })));
    try {
      const result = await deleteComment(session.token, commentID);
      if (viewerGenerationRef.current !== operationViewerGeneration) return;
      updateStore((current) => applyDeletedComment(current, videoID, comment, result));
      onCommentCountChange?.(result.comment_count);
    } catch (error) {
      if (viewerGenerationRef.current !== operationViewerGeneration) return;
      if (isUnauthorized(error)) {
        updateStore((current) => updateVideo(current, videoID, (state) => ({
          ...state,
          deletes: { ...state.deletes, [commentID]: emptyOperation() }
        })));
        handleUnauthorized();
        return;
      }
      updateStore((current) => updateVideo(current, videoID, (state) => ({
        ...state,
        deletes: {
          ...state.deletes,
          [commentID]: { busy: false, error: apiErrorMessage(error, "删除失败") }
        }
      })));
    }
  }, [handleUnauthorized, onCommentCountChange, requireLogin, session.token, updateStore, videoID]);

  const video = store.videos[videoID] || createVideoCommentsState();
  const rootList = video.roots[video.sort];
  const roots = mergeCommentIDs(video.contextRootIDs, rootList.ids)
    .map((id) => store.entities[id])
    .filter(Boolean);
  const replyTarget: Comment | null = video.replyTargetID > 0 ? store.entities[video.replyTargetID] : null;

  return useMemo(() => ({
    videoID,
    sort: video.sort,
    roots,
    rootList,
    entities: store.entities,
    replies: video.replies,
    contextReplyIDs: video.contextReplyIDs,
    pendingReplyIDs: video.pendingReplyIDs,
    expandedRootIDs: video.expandedRootIDs,
    draft: video.draft,
    draftLength: unicodeLength(video.draft),
    replyTarget,
    focusedRootID: video.focusedRootID,
    focusedTargetID: video.focusedTargetID,
    focusRevision: video.focusRevision,
    focusUnavailable: video.focusUnavailable,
    createState: video.create,
    likeStates: video.likes,
    deleteStates: video.deletes,
    setSort,
    loadRoots,
    loadReplies,
    loadThreadContext,
    toggleReplies,
    setDraft,
    selectReplyTarget,
    cancelReply,
    clearFocus,
    submitComment,
    toggleCommentLike,
    removeComment,
    requireLogin
  }), [
    loadReplies,
    loadRoots,
    loadThreadContext,
    removeComment,
    requireLogin,
    roots,
    replyTarget,
    rootList,
    selectReplyTarget,
    setDraft,
    setSort,
    store.entities,
    submitComment,
    toggleCommentLike,
    toggleReplies,
    video,
    videoID,
    cancelReply,
    clearFocus
  ]);
}

export type CommentsController = ReturnType<typeof useComments>;

function normalizeComment(comment: Comment): Comment {
  return {
    ...comment,
    root_comment_id: Number(comment.root_comment_id || 0),
    reply_to_comment_id: Number(comment.reply_to_comment_id || 0),
    reply_to_user_id: Number(comment.reply_to_user_id || 0),
    reply_to_user_nickname: comment.reply_to_user_nickname || "",
    reply_to_user_avatar_url: comment.reply_to_user_avatar_url || "",
    reply_count: Number(comment.reply_count || 0),
    reply_previews: (comment.reply_previews || []).map(normalizeComment),
    like_count: Number(comment.like_count || 0),
    liked: Boolean(comment.liked),
    can_delete: Boolean(comment.can_delete),
    is_video_author: Boolean(comment.is_video_author),
    liked_by_video_author: Boolean(comment.liked_by_video_author),
    hot_score: Number(comment.hot_score || 0)
  };
}

function updateVideo(
  store: CommentsStore,
  videoID: number,
  update: (state: VideoCommentsState) => VideoCommentsState
): CommentsStore {
  const current = store.videos[videoID] || createVideoCommentsState();
  return {
    ...store,
    videos: { ...store.videos, [videoID]: update(current) }
  };
}

export function applyCreatedComment(store: CommentsStore, videoID: number, comment: Comment): CommentsStore {
  const alreadyExists = Boolean(store.entities[comment.id]);
  const base = {
    ...store,
    entities: { ...store.entities, [comment.id]: comment }
  };
  const next = updateVideo(base, videoID, (state) => {
    if (comment.root_comment_id > 0) {
      const rootID = comment.root_comment_id;
      const replyList = state.replies[rootID] || emptyList();
      const canAppendToPage = replyList.state === "ready" && !replyList.hasMore;
      return {
        ...state,
        draft: "",
        replyTargetID: 0,
        create: emptyOperation(),
        expandedRootIDs: mergeCommentIDs(state.expandedRootIDs, [rootID]),
        replies: {
          ...state.replies,
          [rootID]: {
            ...replyList,
            ids: canAppendToPage
              ? mergeCommentIDs(replyList.ids, [comment.id])
              : replyList.ids,
            state: canAppendToPage ? "ready" : replyList.state
          }
        },
        pendingReplyIDs: canAppendToPage
          ? state.pendingReplyIDs
          : {
              ...state.pendingReplyIDs,
              [rootID]: mergeCommentIDs(state.pendingReplyIDs[rootID] || [], [comment.id])
            }
      };
    }
    return {
      ...state,
      draft: "",
      replyTargetID: 0,
      create: emptyOperation(),
      roots: {
        hot: emptyList(),
        latest: {
          ...state.roots.latest,
          ids: mergeCommentIDs(state.roots.latest.ids, [comment.id], true),
          state: "ready"
        }
      }
    };
  });
  if (comment.root_comment_id <= 0) return next;
  if (alreadyExists) return next;
  const rootID = comment.root_comment_id;
  const root = next.entities[rootID];
  if (!root) return next;
  return {
    ...next,
    entities: {
      ...next.entities,
      [rootID]: {
        ...root,
        reply_count: root.reply_count + 1,
        reply_previews: root.reply_previews.length < 3
          ? [...root.reply_previews.filter((item) => item.id !== comment.id), comment]
          : root.reply_previews
      }
    }
  };
}

export function applyDeletedComment(
  store: CommentsStore,
  videoID: number,
  comment: Comment,
  result: DeleteCommentResponse
): CommentsStore {
  const rootID = comment.root_comment_id || comment.id;
  const removeIDs = result.thread_hidden
    ? Object.values(store.entities)
        .filter((item) => item.id === rootID || item.root_comment_id === rootID)
        .map((item) => item.id)
    : [comment.id];
  const removed = new Set(removeIDs);
  const entities = { ...store.entities };
  const scrubReplyTarget = (item: Comment, targetID: number): Comment => item.reply_to_comment_id === targetID
    ? {
        ...item,
        reply_to_user_id: 0,
        reply_to_user_nickname: "",
        reply_to_user_avatar_url: ""
      }
    : item;
  if (result.tombstone && comment.id === rootID) {
    const currentRoot = entities[rootID] || comment;
    entities[rootID] = {
      ...currentRoot,
      user_id: 0,
      user_nickname: "",
      user_avatar_url: "",
      reply_to_user_id: 0,
      reply_to_user_nickname: "",
      reply_to_user_avatar_url: "",
      content: "",
      status: result.status,
      deleted: true,
      can_delete: false,
      liked: false,
      is_video_author: false,
      liked_by_video_author: false,
      like_count: 0,
      reply_previews: currentRoot.reply_previews.map((item) => scrubReplyTarget(item, rootID)),
      reply_count: result.root_reply_count
    };
    removed.delete(rootID);
  }
  for (const [id, item] of Object.entries(entities)) {
    const numericID = Number(id);
    const scrubbed = scrubReplyTarget(item, comment.id);
    entities[numericID] = scrubbed.reply_previews.length > 0
      ? {
          ...scrubbed,
          reply_previews: scrubbed.reply_previews.map(
            (preview) => scrubReplyTarget(preview, comment.id)
          )
        }
      : scrubbed;
  }
  for (const id of removed) delete entities[id];
  const next = updateVideo({ ...store, entities }, videoID, (state) => ({
    ...state,
    replyTargetID: removed.has(state.replyTargetID) ||
      (result.tombstone && state.replyTargetID === rootID)
      ? 0
      : state.replyTargetID,
    focusUnavailable: removed.has(state.focusedTargetID) || result.thread_hidden ? true : state.focusUnavailable,
    expandedRootIDs: result.thread_hidden
      ? state.expandedRootIDs.filter((id) => id !== rootID)
      : state.expandedRootIDs,
    contextRootIDs: state.contextRootIDs.filter((id) => !removed.has(id)),
    roots: {
      hot: { ...state.roots.hot, ids: state.roots.hot.ids.filter((id) => !removed.has(id)) },
      latest: { ...state.roots.latest, ids: state.roots.latest.ids.filter((id) => !removed.has(id)) }
    },
    replies: Object.fromEntries(
      Object.entries(state.replies)
        .filter(([id]) => !result.thread_hidden || Number(id) !== rootID)
        .map(([id, list]) => [
          Number(id),
          { ...list, ids: list.ids.filter((commentID) => !removed.has(commentID)) }
        ])
    ),
    contextReplyIDs: Object.fromEntries(
      Object.entries(state.contextReplyIDs)
        .filter(([id]) => !result.thread_hidden || Number(id) !== rootID)
        .map(([id, ids]) => [
          Number(id),
          ids.filter((commentID) => !removed.has(commentID))
        ])
    ),
    pendingReplyIDs: Object.fromEntries(
      Object.entries(state.pendingReplyIDs)
        .filter(([id]) => !result.thread_hidden || Number(id) !== rootID)
        .map(([id, ids]) => [
          Number(id),
          ids.filter((commentID) => !removed.has(commentID))
        ])
    ),
    deletes: { ...state.deletes, [comment.id]: emptyOperation() }
  }));
  if (comment.root_comment_id > 0 && next.entities[rootID]) {
    next.entities[rootID] = {
      ...next.entities[rootID],
      reply_count: result.root_reply_count,
      reply_previews: next.entities[rootID].reply_previews.filter((item) => !removed.has(item.id))
    };
  }
  return next;
}

export function setCommentSortState(store: CommentsStore, videoID: number, sort: CommentSort): CommentsStore {
  return updateVideo(store, videoID, (state) => ({ ...state, sort }));
}

export function setCommentDraftState(store: CommentsStore, videoID: number, draft: string): CommentsStore {
  return updateVideo(store, videoID, (state) => ({ ...state, draft }));
}

export function setCommentReplyTargetState(store: CommentsStore, videoID: number, commentID: number): CommentsStore {
  return updateVideo(store, videoID, (state) => ({ ...state, replyTargetID: commentID }));
}

export function setCommentExpandedState(
  store: CommentsStore,
  videoID: number,
  rootCommentID: number,
  expanded: boolean
): CommentsStore {
  return updateVideo(store, videoID, (state) => ({
    ...state,
    expandedRootIDs: expanded
      ? mergeCommentIDs(state.expandedRootIDs, [rootCommentID])
      : state.expandedRootIDs.filter((id) => id !== rootCommentID)
  }));
}

export function applyOptimisticCommentLike(
  store: CommentsStore,
  videoID: number,
  commentID: number,
  liked: boolean
): CommentsStore {
  const comment = store.entities[commentID];
  if (!comment) return store;
  return {
    ...updateVideo(store, videoID, (state) => ({
      ...state,
      likes: { ...state.likes, [commentID]: { busy: true, error: "" } }
    })),
    entities: {
      ...store.entities,
      [commentID]: {
        ...comment,
        liked,
        like_count: Math.max(0, comment.like_count + (liked === comment.liked ? 0 : liked ? 1 : -1))
      }
    }
  };
}

export function applyConfirmedCommentLike(
  store: CommentsStore,
  videoID: number,
  commentID: number,
  result: CommentLikeResponse
): CommentsStore {
  const comment = store.entities[commentID];
  if (!comment) return store;
  return {
    ...updateVideo(store, videoID, (state) => ({
      ...state,
      likes: { ...state.likes, [commentID]: emptyOperation() }
    })),
    entities: {
      ...store.entities,
      [commentID]: {
        ...comment,
        liked: result.liked,
        like_count: result.like_count,
        liked_by_video_author: result.liked_by_video_author
      }
    }
  };
}

export function applyCommentLikeRollback(
  store: CommentsStore,
  videoID: number,
  commentID: number,
  liked: boolean,
  likeCount: number,
  error: string
): CommentsStore {
  const comment = store.entities[commentID];
  if (!comment) return store;
  return {
    ...updateVideo(store, videoID, (state) => ({
      ...state,
      likes: { ...state.likes, [commentID]: { busy: false, error } }
    })),
    entities: {
      ...store.entities,
      [commentID]: { ...comment, liked, like_count: likeCount }
    }
  };
}

export function applyCommentViewerTransition(store: CommentsStore): CommentsStore {
  return {
    ...store,
    entities: Object.fromEntries(
      Object.entries(store.entities).map(([id, comment]) => [
        id,
        { ...comment, liked: false, can_delete: false }
      ])
    ),
    videos: Object.fromEntries(
      Object.entries(store.videos).map(([videoID, video]) => [
        Number(videoID),
        {
          ...video,
          roots: {
            hot: emptyList(),
            latest: emptyList()
          },
          contextRootIDs: [],
          replies: {},
          contextReplyIDs: {},
          pendingReplyIDs: {},
          draft: "",
          replyTargetID: 0,
          create: emptyOperation(),
          likes: {},
          deletes: {}
        }
      ])
    )
  };
}

export function beginCommentRequest(
  generations: Record<string, number>,
  key: string,
  viewerGeneration: number
): CommentRequestIdentity {
  const generation = (generations[key] || 0) + 1;
  generations[key] = generation;
  return { key, generation, viewerGeneration };
}

export function isCurrentCommentRequest(
  generations: Record<string, number>,
  request: CommentRequestIdentity,
  viewerGeneration: number
): boolean {
  return request.viewerGeneration === viewerGeneration &&
    generations[request.key] === request.generation;
}

export function isValidCommentThreadContext(
  data: CommentThreadContextResponse,
  videoID: number,
  targetCommentID: number,
  requestedRootID: number
): boolean {
  const root = data.root;
  const target = data.target;
  if (
    root.id <= 0 ||
    root.video_id !== videoID ||
    Number(root.root_comment_id || 0) !== 0 ||
    target.id !== targetCommentID ||
    target.video_id !== videoID ||
    (requestedRootID > 0 && root.id !== requestedRootID)
  ) {
    return false;
  }
  const targetBelongsToRoot = target.id === root.id
    ? Number(target.root_comment_id || 0) === 0
    : Number(target.root_comment_id || 0) === root.id;
  if (!targetBelongsToRoot) return false;
  return (data.replies || []).every((reply) =>
    reply.video_id === videoID && reply.root_comment_id === root.id
  );
}
