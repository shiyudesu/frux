import { useEffect, useMemo, useRef, useState } from "react";
import {
  restoreAdminVideo,
  searchAdminVideos,
  takeDownAdminVideo
} from "../api/videoAdmin";
import { ApiError, apiErrorMessage } from "../api/client";
import { useSession } from "../session";
import type {
  AdminEnforcementRequest,
  AdminVideo,
  AdminVideoSearchFilters
} from "../types";

export const defaultAdminVideoFilters = (now = new Date()): AdminVideoSearchFilters => {
  const from = new Date(now.getTime() - 30 * 24 * 60 * 60 * 1000);
  const to = new Date(now);
  to.setSeconds(59, 999);
  return {
    status: "",
    author_id: "",
    video_id: "",
    keyword: "",
    created_from: localDateTime(from),
    created_to: localDateTime(to, true)
  };
};

type VideoPageState = "loading" | "ready" | "empty" | "error" | "forbidden";

export function VideoOperationsPage() {
  const { token } = useSession();
  const [draftFilters, setDraftFilters] = useState<AdminVideoSearchFilters>(defaultAdminVideoFilters);
  const [appliedFilters, setAppliedFilters] = useState<AdminVideoSearchFilters>(defaultAdminVideoFilters);
  const [items, setItems] = useState<AdminVideo[]>([]);
  const [state, setState] = useState<VideoPageState>("loading");
  const [message, setMessage] = useState("");
  const [cursor, setCursor] = useState("");
  const [hasMore, setHasMore] = useState(false);
  const [loadingMore, setLoadingMore] = useState(false);
  const [dialog, setDialog] = useState<{ video: AdminVideo; action: "takedown" | "restore" } | null>(null);
  const [reasonCode, setReasonCode] = useState<AdminEnforcementRequest["reason_code"]>("policy_violation");
  const [note, setNote] = useState("");
  const [confirmed, setConfirmed] = useState(false);
  const [actionBusy, setActionBusy] = useState(false);
  const [actionError, setActionError] = useState("");
  const requestGeneration = useRef(0);

  const load = async (
    filters: AdminVideoSearchFilters,
    nextCursor = "",
    append = false
  ) => {
    const generation = ++requestGeneration.current;
    if (append) setLoadingMore(true);
    else setState("loading");
    setMessage("");
    try {
      const page = await searchAdminVideos(token, filters, nextCursor, 20);
      if (generation !== requestGeneration.current) return;
      setItems((current) => append ? [...current, ...page.items] : page.items);
      setCursor(page.next_cursor);
      setHasMore(page.has_more);
      setState((append ? items.length + page.items.length : page.items.length) > 0 ? "ready" : "empty");
    } catch (error: unknown) {
      if (generation !== requestGeneration.current) return;
      if (error instanceof ApiError && error.status === 403) setState("forbidden");
      else if (!append) setState("error");
      setMessage(apiErrorMessage(error, "视频查询失败，请重试"));
    } finally {
      if (generation === requestGeneration.current) setLoadingMore(false);
    }
  };

  useEffect(() => {
    void load(appliedFilters);
    return () => {
      requestGeneration.current++;
    };
  }, []);

  const openDialog = (video: AdminVideo, action: "takedown" | "restore") => {
    setDialog({ video, action });
    setReasonCode(action === "takedown" ? "policy_violation" : "compliance_restored");
    setNote("");
    setConfirmed(false);
    setActionError("");
  };

  const submitAction = async () => {
    if (!dialog || !confirmed) return;
    setActionBusy(true);
    setActionError("");
    const body: AdminEnforcementRequest = {
      reason_code: reasonCode,
      note,
      expected_version: dialog.video.version
    };
    try {
      const result = dialog.action === "takedown"
        ? await takeDownAdminVideo(token, dialog.video.id, body)
        : await restoreAdminVideo(token, dialog.video.id, body);
      if (!result.audit_committed) {
        setActionError("服务端未确认审计提交，未报告成功。");
        return;
      }
      setItems((current) => current.map((video) =>
        video.id === result.video.id ? result.video : video
      ));
      setDialog(null);
      setMessage(dialog.action === "takedown" ? "视频已下架并完成审计。" : "视频已恢复并完成审计。");
    } catch (error: unknown) {
      if (error instanceof ApiError && error.status === 403) {
        setState("forbidden");
        setDialog(null);
        return;
      }
      if (error instanceof ApiError &&
        (error.code === "ADMIN_VIDEO_VERSION_CONFLICT" ||
          error.code === "ADMIN_VIDEO_STATE_CONFLICT")) {
        setActionError("视频版本或状态已变化，请关闭弹窗并刷新查询。");
        return;
      }
      setActionError(apiErrorMessage(error, "操作失败，请重试"));
    } finally {
      setActionBusy(false);
    }
  };

  const filterFields = useMemo(
    () => Object.keys(draftFilters) as Array<keyof AdminVideoSearchFilters>,
    [draftFilters]
  );

  return (
    <section className="admin-page">
      <header className="admin-page-header">
        <div>
          <span className="admin-eyebrow">Content operations</span>
          <h1>视频运营</h1>
          <p>搜索生命周期状态，并使用版本检查执行下架或恢复。</p>
        </div>
      </header>
      <form className="admin-filter-grid" onSubmit={(event) => {
        event.preventDefault();
        const next = { ...draftFilters };
        setAppliedFilters(next);
        void load(next);
      }}>
        <label>
          状态
          <select value={draftFilters.status} onChange={(event) =>
            setDraftFilters({ ...draftFilters, status: event.target.value as AdminVideoSearchFilters["status"] })
          }>
            <option value="">全部</option>
            <option value="published">已发布</option>
            <option value="offline">已下架</option>
            <option value="pending_review">待审核</option>
            <option value="rejected">已拒绝</option>
            <option value="draft">草稿</option>
          </select>
        </label>
        <label>作者 ID<input value={draftFilters.author_id} onChange={(event) =>
          setDraftFilters({ ...draftFilters, author_id: event.target.value })
        } /></label>
        <label>视频 ID<input value={draftFilters.video_id} onChange={(event) =>
          setDraftFilters({ ...draftFilters, video_id: event.target.value })
        } /></label>
        <label>关键词<input maxLength={128} value={draftFilters.keyword} onChange={(event) =>
          setDraftFilters({ ...draftFilters, keyword: event.target.value })
        } /></label>
        <label>创建起点<input type="datetime-local" value={draftFilters.created_from} onChange={(event) =>
          setDraftFilters({ ...draftFilters, created_from: event.target.value })
        } /></label>
        <label>创建终点<input type="datetime-local" step="0.001" value={draftFilters.created_to} onChange={(event) =>
          setDraftFilters({ ...draftFilters, created_to: event.target.value })
        } /></label>
        <button type="submit">查询</button>
      </form>
      <output className="admin-filter-summary">
        已应用 {filterFields.filter((field) => Boolean(appliedFilters[field])).length} 个筛选条件
      </output>
      {message && <div className="admin-alert">{message}</div>}
      {state === "loading" && <VideoState title="正在查询视频…" />}
      {state === "error" && <VideoState title={message} action="重试" onAction={() => void load(appliedFilters)} />}
      {state === "forbidden" && <VideoState title="服务端拒绝了视频运营访问" />}
      {state === "empty" && <VideoState title="没有符合条件的视频" />}
      {state === "ready" && (
        <>
          <div className="admin-table-wrap">
            <table className="admin-table">
              <thead><tr><th>视频</th><th>作者</th><th>状态</th><th>版本</th><th>创建时间</th><th>操作</th></tr></thead>
              <tbody>
                {items.map((video) => (
                  <tr key={video.id}>
                    <td><strong>{video.title || `视频 #${video.id}`}</strong><small>#{video.id}</small></td>
                    <td>#{video.author_id}</td>
                    <td><span className={`admin-status-pill ${video.status_name}`}>{video.status_name}</span></td>
                    <td>v{video.version}</td>
                    <td>{new Date(video.created_at).toLocaleString()}</td>
                    <td>
                      {video.status_name === "published" && (
                        <button className="danger subtle" type="button" onClick={() => openDialog(video, "takedown")}>
                          下架
                        </button>
                      )}
                      {video.status_name === "offline" && (
                        <button type="button" onClick={() => openDialog(video, "restore")}>恢复</button>
                      )}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
          {hasMore && (
            <button
              className="admin-load-more"
              type="button"
              disabled={loadingMore}
              onClick={() => void load(appliedFilters, cursor, true)}
            >
              {loadingMore ? "加载中…" : "加载更多"}
            </button>
          )}
        </>
      )}
      {dialog && (
        <div className="admin-dialog-backdrop" role="presentation">
          <div className="admin-dialog" role="dialog" aria-modal="true" aria-labelledby="admin-action-title">
            <h2 id="admin-action-title">{dialog.action === "takedown" ? "确认下架视频" : "确认恢复视频"}</h2>
            <p>视频 #{dialog.video.id} · 当前版本 v{dialog.video.version}</p>
            <label>
              原因
              <select value={reasonCode} onChange={(event) =>
                setReasonCode(event.target.value as AdminEnforcementRequest["reason_code"])
              }>
                {dialog.action === "takedown"
                  ? <>
                      <option value="policy_violation">策略违规</option>
                      <option value="manual_enforcement">人工处置</option>
                    </>
                  : <option value="compliance_restored">已恢复合规</option>}
              </select>
            </label>
            <label>
              备注
              <textarea maxLength={1000} value={note} onChange={(event) => setNote(event.target.value)} />
            </label>
            <label className="admin-confirm-check">
              <input type="checkbox" checked={confirmed} onChange={(event) => setConfirmed(event.target.checked)} />
              我确认按当前版本执行并写入审计
            </label>
            {actionError && <p className="admin-inline-error">{actionError}</p>}
            <div className="admin-dialog-actions">
              <button type="button" disabled={actionBusy} onClick={() => setDialog(null)}>取消</button>
              <button
                className={dialog.action === "takedown" ? "danger" : ""}
                type="button"
                disabled={!confirmed || actionBusy}
                onClick={() => void submitAction()}
              >
                {actionBusy ? "提交中…" : dialog.action === "takedown" ? "确认下架" : "确认恢复"}
              </button>
            </div>
          </div>
        </div>
      )}
    </section>
  );
}

function VideoState({
  title,
  action,
  onAction
}: {
  title: string;
  action?: string;
  onAction?: () => void;
}) {
  return (
    <div className="admin-state">
      <strong>{title}</strong>
      {action && <button type="button" onClick={onAction}>{action}</button>}
    </div>
  );
}

function localDateTime(value: Date, includeSeconds = false): string {
  const offset = value.getTimezoneOffset() * 60_000;
  return new Date(value.getTime() - offset).toISOString().slice(0, includeSeconds ? 23 : 16);
}
