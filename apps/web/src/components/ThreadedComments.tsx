import { memo, useCallback, useEffect, useRef } from "react";
import { COMMENT_CONTENT_LIMIT, type CommentsController } from "../hooks/useComments";
import type { Comment, SessionUser } from "../types";
import type { PublicProfileInput } from "../utils";
import {
  formatMetric,
  formatRelativeTime,
  profileFromComment,
  profileFromReplyTarget,
  publicUserAvatar
} from "../utils";
import { Icon } from "./Icon";
import { CommentMessage } from "./StatusMessages";

interface ThreadedCommentsProps {
  controller: CommentsController;
  authenticated: boolean;
  user: SessionUser;
  canModerateThreads: boolean;
  onOpenUser: (profile: PublicProfileInput) => void;
}

export function ThreadedComments({
  controller,
  authenticated,
  user,
  canModerateThreads,
  onOpenUser
}: ThreadedCommentsProps) {
  const composerRef = useRef<HTMLTextAreaElement | null>(null);
  const focusTimerRef = useRef<number>();
  const onOpenUserRef = useRef(onOpenUser);
  onOpenUserRef.current = onOpenUser;

  const openUser = useCallback((profile: PublicProfileInput) => {
    onOpenUserRef.current(profile);
  }, []);
  const selectReply = useCallback((commentID: number) => {
    controller.selectReplyTarget(commentID);
    window.requestAnimationFrame(() => composerRef.current?.focus());
  }, [controller.selectReplyTarget]);

  useEffect(() => {
    const targetID = controller.focusedTargetID;
    if (!targetID) return undefined;
    const timer = window.requestAnimationFrame(() => {
      document.querySelector<HTMLElement>(`[data-comment-id="${targetID}"]`)?.scrollIntoView({
        block: "center",
        behavior: "smooth"
      });
    });
    focusTimerRef.current = window.setTimeout(() => {
      controller.clearFocus();
    }, 3200);
    return () => {
      window.cancelAnimationFrame(timer);
      if (focusTimerRef.current) window.clearTimeout(focusTimerRef.current);
    };
  }, [controller.clearFocus, controller.focusedTargetID, controller.focusRevision]);

  const cancelReply = () => {
    const targetID = controller.replyTarget?.id || 0;
    controller.cancelReply();
    window.requestAnimationFrame(() => {
      document.querySelector<HTMLButtonElement>(`[data-reply-comment-id="${targetID}"]`)?.focus();
    });
  };

  return (
    <div className="threaded-comments" data-ui="threaded-comments">
      <div className="comment-toolbar">
        <div className="comment-sort" role="tablist" aria-label="评论排序">
          <button
            aria-selected={controller.sort === "hot"}
            className={controller.sort === "hot" ? "active" : ""}
            role="tab"
            type="button"
            onClick={() => controller.setSort("hot")}
          >
            热门
          </button>
          <button
            aria-selected={controller.sort === "latest"}
            className={controller.sort === "latest" ? "active" : ""}
            role="tab"
            type="button"
            onClick={() => controller.setSort("latest")}
          >
            最新
          </button>
        </div>
      </div>

      <div className="comment-list" data-ui="comment-list">
        {controller.focusUnavailable && (
          <CommentMessage icon="alert" title="该讨论已不可用，可能已删除或视频状态已变更" />
        )}
        {controller.rootList.state === "loading" && controller.roots.length === 0 && (
          <CommentMessage icon="hourglass" title="正在加载评论" />
        )}
        {controller.rootList.state === "error" && controller.roots.length === 0 && (
          <CommentMessage
            icon="alert"
            title={controller.rootList.error || "评论加载失败"}
            action="重试"
            onAction={() => controller.loadRoots(true)}
          />
        )}
        {controller.rootList.state === "ready" && controller.roots.length === 0 && !controller.focusUnavailable && (
          <CommentMessage icon="comment" title="暂无评论" />
        )}
        {controller.roots.map((root) => {
          const expanded = controller.expandedRootIDs.includes(root.id);
          const replyList = controller.replies[root.id];
          const previewIDs = (root.reply_previews || []).map((reply) => reply.id);
          const replyIDs = expanded
            ? mergeVisibleReplyIDs(
                previewIDs,
                replyList?.ids || [],
                [
                  ...(controller.contextReplyIDs[root.id] || []),
                  ...(controller.pendingReplyIDs[root.id] || [])
                ],
                controller.entities
              )
            : previewIDs;
          return (
            <CommentThread
              key={root.id}
              root={commentCardState(root, controller)}
              replies={replyIDs
                .map((id) => controller.entities[id])
                .filter(Boolean)
                .map((reply) => commentCardState(reply, controller))}
              replyList={replyList}
              expanded={expanded}
              authenticated={authenticated}
              canModerateThreads={canModerateThreads}
              onOpenUser={openUser}
              onReply={selectReply}
              onRequireLogin={controller.requireLogin}
              onToggleLike={controller.toggleCommentLike}
              onRemove={controller.removeComment}
              onToggleReplies={controller.toggleReplies}
              onLoadReplies={controller.loadReplies}
            />
          );
        })}
        {controller.rootList.error && controller.roots.length > 0 && (
          <p className="comment-operation-error" role="alert">{controller.rootList.error}</p>
        )}
        {controller.rootList.hasMore && (
          <button
            className="comment-load-more"
            type="button"
            disabled={controller.rootList.state === "loadingMore"}
            onClick={() => controller.loadRoots(false)}
          >
            {controller.rootList.state === "loadingMore" ? "加载中" : "加载更多评论"}
          </button>
        )}
      </div>

      <form
        className="comment-form threaded"
        onSubmit={(event) => {
          event.preventDefault();
          void controller.submitComment();
        }}
      >
        <img src={publicUserAvatar(user.avatar_url)} alt="" />
        <div className="comment-composer">
          {controller.replyTarget && (
            <div className="comment-reply-context">
              <span>回复 {controller.replyTarget.user_nickname || "该用户"}</span>
              <button type="button" onClick={cancelReply}>取消</button>
            </div>
          )}
          {authenticated ? (
            <>
              <textarea
                ref={composerRef}
                aria-label={controller.replyTarget ? "回复评论" : "发表评论"}
                maxLength={COMMENT_CONTENT_LIMIT * 2}
                placeholder={controller.replyTarget ? "写下回复" : "留下你的评论"}
                rows={2}
                value={controller.draft}
                onChange={(event) => controller.setDraft(event.target.value)}
              />
              <div className="comment-composer-footer">
                <span className={controller.draftLength > COMMENT_CONTENT_LIMIT ? "over-limit" : ""}>
                  {controller.draftLength}/{COMMENT_CONTENT_LIMIT}
                </span>
                <button
                  aria-label="发送评论"
                  disabled={
                    controller.createState.busy ||
                    !controller.draft.trim() ||
                    controller.draftLength > COMMENT_CONTENT_LIMIT
                  }
                >
                  <Icon name="send" size={18} />
                  <span>{controller.createState.busy ? "发送中" : "发送"}</span>
                </button>
              </div>
            </>
          ) : (
            <button className="comment-login-action" type="button" onClick={controller.requireLogin}>
              登录后参与讨论
            </button>
          )}
          {controller.createState.error && (
            <p className="comment-operation-error" role="alert">{controller.createState.error}</p>
          )}
        </div>
      </form>
    </div>
  );
}

interface CommentThreadProps {
  root: CommentCardState;
  replies: CommentCardState[];
  replyList?: CommentsController["replies"][number];
  expanded: boolean;
  authenticated: boolean;
  canModerateThreads: boolean;
  onOpenUser: (profile: PublicProfileInput) => void;
  onReply: (commentID: number) => void;
  onRequireLogin: () => boolean;
  onToggleLike: (commentID: number) => Promise<void>;
  onRemove: (commentID: number) => Promise<void>;
  onToggleReplies: (rootCommentID: number) => void;
  onLoadReplies: (rootCommentID: number) => Promise<void>;
}

const CommentThread = memo(function CommentThread({
  root,
  replies,
  replyList,
  expanded,
  authenticated,
  canModerateThreads,
  onOpenUser,
  onReply,
  onRequireLogin,
  onToggleLike,
  onRemove,
  onToggleReplies,
  onLoadReplies
}: CommentThreadProps) {
  const previewCount = root.comment.reply_previews?.length || 0;
  const canExpand = root.comment.reply_count > previewCount || expanded;

  return (
    <article className="comment-thread">
      <CommentCard
        {...root}
        authenticated={authenticated}
        canModerateThreads={canModerateThreads}
        onOpenUser={onOpenUser}
        onReply={onReply}
        onRequireLogin={onRequireLogin}
        onToggleLike={onToggleLike}
        onRemove={onRemove}
      />
      {replies.length > 0 && (
        <div className="comment-replies" aria-label={`评论 ${root.comment.id} 的回复`}>
          {replies.map((reply) => (
            <CommentCard
              key={reply.comment.id}
              {...reply}
              authenticated={authenticated}
              canModerateThreads={canModerateThreads}
              onOpenUser={onOpenUser}
              onReply={onReply}
              onRequireLogin={onRequireLogin}
              onToggleLike={onToggleLike}
              onRemove={onRemove}
              reply
            />
          ))}
        </div>
      )}
      {canExpand && (
        <div className="comment-thread-controls">
          <button type="button" onClick={() => onToggleReplies(root.comment.id)}>
            <Icon name={expanded ? "chevron-up" : "chevron-down"} size={15} />
            {expanded ? "收起回复" : `展开 ${formatMetric(root.comment.reply_count)} 条回复`}
          </button>
          {expanded && replyList?.hasMore && (
            <button
              type="button"
              disabled={replyList.state === "loadingMore"}
              onClick={() => onLoadReplies(root.comment.id)}
            >
              {replyList.state === "loadingMore" ? "加载中" : "加载更多回复"}
            </button>
          )}
        </div>
      )}
      {expanded && replyList?.error && (
        <p className="comment-operation-error" role="alert">{replyList.error}</p>
      )}
    </article>
  );
}, sameCommentThreadProps);

interface CommentCardProps {
  comment: Comment;
  likeState?: CommentsController["likeStates"][number];
  deleteState?: CommentsController["deleteStates"][number];
  focused: boolean;
  authenticated: boolean;
  canModerateThreads: boolean;
  onOpenUser: (profile: PublicProfileInput) => void;
  onReply: (commentID: number) => void;
  onRequireLogin: () => boolean;
  onToggleLike: (commentID: number) => Promise<void>;
  onRemove: (commentID: number) => Promise<void>;
  reply?: boolean;
}

const CommentCard = memo(function CommentCard({
  comment,
  likeState,
  deleteState,
  focused,
  authenticated,
  canModerateThreads,
  onOpenUser,
  onReply,
  onRequireLogin,
  onToggleLike,
  onRemove,
  reply = false
}: CommentCardProps) {
  const tombstone = comment.deleted && comment.root_comment_id === 0;

  const confirmDelete = () => {
    const moderatingRoot = requiresModeratorConfirmation(comment, canModerateThreads);
    if (
      moderatingRoot &&
      !window.confirm("删除该根评论会隐藏整个讨论串，确认继续吗？")
    ) {
      return;
    }
    void onRemove(comment.id);
  };

  return (
    <article
      className={`comment-item ${reply ? "reply" : "root"} ${tombstone ? "tombstone" : ""} ${focused ? "focused" : ""}`}
      data-comment-id={comment.id}
    >
      {tombstone ? (
        <span className="comment-tombstone-icon"><Icon name="comment" size={18} /></span>
      ) : (
        <button className="comment-user-button" type="button" onClick={() => onOpenUser(profileFromComment(comment))}>
          <img src={publicUserAvatar(comment.user_avatar_url)} alt="" />
        </button>
      )}
      <div className="comment-body">
        <div className="comment-meta">
          {tombstone ? (
            <strong>该评论已被作者删除</strong>
          ) : (
            <button type="button" onClick={() => onOpenUser(profileFromComment(comment))}>
              {comment.user_nickname || `用户_${comment.user_id}`}
            </button>
          )}
          {!tombstone && comment.is_video_author && (
            <span className="comment-author-marker">作者</span>
          )}
          <span>{formatRelativeTime(comment.created_at)}</span>
        </div>
        {!tombstone && (
          <p>
            {comment.reply_to_user_id > 0 && (
              <>
                <span>回复 </span>
                <button
                  className="comment-reply-label"
                  type="button"
                  onClick={() => onOpenUser(profileFromReplyTarget(comment))}
                >
                  @{comment.reply_to_user_nickname || `用户_${comment.reply_to_user_id}`}
                </button>
                <span>：</span>
              </>
            )}
            {comment.content}
          </p>
        )}
        <div className="comment-actions">
          {!tombstone && comment.liked_by_video_author && (
            <span className="comment-author-liked">作者赞过</span>
          )}
          <button
            aria-label={comment.liked ? "取消点赞评论" : "点赞评论"}
            aria-pressed={comment.liked}
            className={comment.liked ? "active" : ""}
            type="button"
            disabled={tombstone || likeState?.busy}
            onClick={() => onToggleLike(comment.id)}
          >
            <Icon name="heart" size={15} filled={comment.liked} />
            {formatMetric(comment.like_count)}
          </button>
          {!tombstone && (
            <button
              data-reply-comment-id={comment.id}
              type="button"
              onClick={() => authenticated ? onReply(comment.id) : onRequireLogin()}
            >
              回复
            </button>
          )}
          {comment.can_delete && (
            <details className="comment-delete-menu">
              <summary aria-label="评论操作"><Icon name="more" size={16} /></summary>
              <button
                className="danger"
                type="button"
                disabled={deleteState?.busy}
                onClick={confirmDelete}
              >
                {deleteState?.busy ? "删除中" : "删除"}
              </button>
            </details>
          )}
        </div>
        {(likeState?.error || deleteState?.error) && (
          <p className="comment-operation-error" role="alert">{likeState?.error || deleteState?.error}</p>
        )}
      </div>
    </article>
  );
});

interface CommentCardState {
  comment: Comment;
  likeState?: CommentsController["likeStates"][number];
  deleteState?: CommentsController["deleteStates"][number];
  focused: boolean;
}

function commentCardState(comment: Comment, controller: CommentsController): CommentCardState {
  return {
    comment,
    likeState: controller.likeStates[comment.id],
    deleteState: controller.deleteStates[comment.id],
    focused: controller.focusedTargetID === comment.id
  };
}

function sameCommentCardState(left: CommentCardState, right: CommentCardState): boolean {
  return left.comment === right.comment
    && left.likeState === right.likeState
    && left.deleteState === right.deleteState
    && left.focused === right.focused;
}

function sameCommentThreadProps(left: CommentThreadProps, right: CommentThreadProps): boolean {
  return sameCommentCardState(left.root, right.root)
    && left.replyList === right.replyList
    && left.expanded === right.expanded
    && left.authenticated === right.authenticated
    && left.canModerateThreads === right.canModerateThreads
    && left.onOpenUser === right.onOpenUser
    && left.onReply === right.onReply
    && left.onRequireLogin === right.onRequireLogin
    && left.onToggleLike === right.onToggleLike
    && left.onRemove === right.onRemove
    && left.onToggleReplies === right.onToggleReplies
    && left.onLoadReplies === right.onLoadReplies
    && left.replies.length === right.replies.length
    && left.replies.every((reply, index) => sameCommentCardState(reply, right.replies[index]));
}

export function mergeVisibleReplyIDs(
  previews: number[],
  loaded: number[],
  supplemental: number[],
  entities: Record<number, Comment>
): number[] {
  const seen = new Set<number>();
  return [...previews, ...loaded, ...supplemental]
    .filter((id) => {
      if (seen.has(id) || !entities[id]) return false;
      seen.add(id);
      return true;
    })
    .sort((leftID, rightID) => {
      const left = entities[leftID]!;
      const right = entities[rightID]!;
      const createdOrder = left.created_at.localeCompare(right.created_at);
      return createdOrder || left.id - right.id;
    });
}

export function requiresModeratorConfirmation(
  comment: Comment,
  canModerateThreads: boolean
): boolean {
  return canModerateThreads && comment.root_comment_id === 0 && comment.reply_count > 0;
}
