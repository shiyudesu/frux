// useComments：评论面板状态与加载/提交逻辑。搬运自 LegacyApp.jsx FeedPage，逻辑不变。
import { useCallback, useEffect, useState } from "react";
import { apiErrorMessage, isUnauthorized } from "../api/client";
import { createComment, fetchComments } from "../api/social";
import { useNavigate } from "../router";
import { useSession } from "../session";
import type { Comment, FeedVideo } from "../types";

export type CommentsState = "idle" | "loading" | "ready" | "error";

export interface UseCommentsOptions {
  current: FeedVideo | undefined;
  updateCurrentItem: (videoID: number, patch: Partial<FeedVideo>) => void;
}

export function useComments({ current, updateCurrentItem }: UseCommentsOptions) {
  const session = useSession();
  const navigate = useNavigate();
  const [commentsOpen, setCommentsOpen] = useState(false);
  const [comments, setComments] = useState<Comment[]>([]);
  const [commentsState, setCommentsState] = useState<CommentsState>("idle");
  const [commentsError, setCommentsError] = useState("");
  const [commentText, setCommentText] = useState("");

  const requireLogin = useCallback(() => {
    if (session.token) return true;
    navigate("/auth");
    return false;
  }, [navigate, session.token]);

  const loadComments = useCallback(() => {
    if (!current) return undefined;
    let live = true;
    setCommentsState("loading");
    setCommentsError("");
    fetchComments(current.video_id)
      .then((data) => {
        if (!live) return;
        const nextComments = data.items || [];
        setComments(nextComments);
        if (!data.has_more && nextComments.length > Number(current.comment_count || 0)) {
          updateCurrentItem(current.video_id, { comment_count: nextComments.length });
        }
        setCommentsState("ready");
      })
      .catch((error: unknown) => {
        if (!live) return;
        setComments([]);
        setCommentsError(apiErrorMessage(error, "评论加载失败"));
        setCommentsState("error");
      });
    return () => {
      live = false;
    };
  }, [current, updateCurrentItem]);

  useEffect(() => {
    if (!commentsOpen) return undefined;
    return loadComments();
  }, [commentsOpen, loadComments]);

  async function submitComment() {
    if (!current || !requireLogin()) return;
    const content = commentText.trim();
    if (!content) return;
    try {
      const data = await createComment(session.token, current.video_id, content);
      setCommentText("");
      setComments((state) => [data, ...state.filter((item) => item.id !== data.id)]);
      updateCurrentItem(current.video_id, { comment_count: data.comment_count ?? current.comment_count + 1 });
      setCommentsState("ready");
    } catch (error) {
      if (isUnauthorized(error)) {
        session.clearAuth();
        navigate("/auth");
        return;
      }
      setCommentsError(apiErrorMessage(error, "评论发布失败"));
      setCommentsState("error");
    }
  }

  return {
    commentsOpen,
    setCommentsOpen,
    comments,
    commentsState,
    commentsError,
    commentText,
    setCommentText,
    loadComments,
    submitComment
  };
}
