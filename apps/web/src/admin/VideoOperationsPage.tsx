import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { ApiError, apiErrorMessage } from "../api/client";
import {
  bulkRetryMediaProcessingJobs,
  fetchMediaProcessingHistory,
  fetchMediaProcessingOverview,
  mediaProcessingRetryReasonLabel,
  mediaProcessingStageLabel,
  mediaProcessingStateLabel,
  retryMediaProcessingJob
} from "../api/mediaProcessingAdmin";
import {
  restoreAdminVideo,
  searchAdminVideos,
  takeDownAdminVideo
} from "../api/videoAdmin";
import { useAdminSession } from "./adminSession";
import type {
  AdminEnforcementRequest,
  AdminVideo,
  AdminVideoSearchFilters,
  MediaProcessingAdminItem,
  MediaProcessingBulkRetryItemResult,
  MediaProcessingBulkRetryResponse,
  MediaProcessingHistoryFilters,
  MediaProcessingOverviewResponse,
  MediaProcessingRetryReasonCode,
  MediaProcessingRetryResponse,
  MediaProcessingStage,
  MediaProcessingState,
  MediaProcessingSummary
} from "../types";

type VideoOperationsView = "videos" | "processing";
type FetchTone = "info" | "warning" | "error";

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

export const defaultMediaProcessingHistoryFilters = (now = new Date()): MediaProcessingHistoryFilters => {
  const from = new Date(now.getTime() - 30 * 24 * 60 * 60 * 1000);
  const to = new Date(now);
  to.setSeconds(59, 999);
  return {
    state: "",
    stage: "",
    error_code: "",
    video_id: "",
    completed_from: localDateTime(from),
    completed_to: localDateTime(to, true)
  };
};

export function VideoOperationsPage() {
  const [view, setView] = useState<VideoOperationsView>("videos");

  return (
    <section className="admin-page">
      <header className="admin-page-header">
        <div>
          <span className="admin-eyebrow">Video operations</span>
          <h1>视频运营</h1>
          <p>在视频列表和处理进度之间切换，统一完成内容运营操作。</p>
        </div>
      </header>
      <div className="admin-review-tabs" role="tablist" aria-label="视频运营视图">
        <button
          type="button"
          role="tab"
          aria-selected={view === "videos"}
          className={view === "videos" ? "active" : ""}
          onClick={() => setView("videos")}
        >
          视频列表
        </button>
        <button
          type="button"
          role="tab"
          aria-selected={view === "processing"}
          className={view === "processing" ? "active" : ""}
          onClick={() => setView("processing")}
        >
          处理进度
        </button>
      </div>
      <VideoListView active={view === "videos"} />
      <MediaProcessingView active={view === "processing"} />
    </section>
  );
}

export default VideoOperationsPage;

function VideoListView({ active }: { active: boolean }) {
  const { token } = useAdminSession();
  const [draftFilters, setDraftFilters] = useState<AdminVideoSearchFilters>(defaultAdminVideoFilters);
  const [appliedFilters, setAppliedFilters] = useState<AdminVideoSearchFilters>(defaultAdminVideoFilters);
  const [items, setItems] = useState<AdminVideo[]>([]);
  const [state, setState] = useState<"loading" | "ready" | "empty" | "error" | "forbidden">("loading");
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
  const hasLoadedRef = useRef(false);

  const load = useCallback(async (
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
      setItems((current) => {
        const next = append ? [...current, ...page.items] : page.items;
        setState(next.length > 0 ? "ready" : "empty");
        return next;
      });
      setCursor(page.next_cursor);
      setHasMore(page.has_more);
    } catch (error: unknown) {
      if (generation !== requestGeneration.current) return;
      if (error instanceof ApiError && error.status === 403) {
        setState("forbidden");
        setItems([]);
      } else if (!append) {
        setState("error");
      }
      setMessage(apiErrorMessage(error, "视频查询失败，请重试"));
    } finally {
      if (generation === requestGeneration.current) setLoadingMore(false);
    }
  }, [token]);

  useEffect(() => {
    if (!active || hasLoadedRef.current) return;
    hasLoadedRef.current = true;
    void load(appliedFilters);
    return () => {
      requestGeneration.current++;
    };
  }, [active, appliedFilters, load]);

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
        setItems([]);
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
    <section className="admin-section" hidden={!active} aria-label="视频列表">
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
      {state === "loading" && <ProcessingState title="正在查询视频…" />}
      {state === "error" && <ProcessingState title={message} action="重试" onAction={() => void load(appliedFilters)} />}
      {state === "forbidden" && <ProcessingState title="服务端拒绝了视频运营访问" />}
      {state === "empty" && <ProcessingState title="没有符合条件的视频" />}
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

interface OverviewState {
  data: MediaProcessingOverviewResponse | null;
  loading: boolean;
  refreshing: boolean;
  message: string;
  tone: FetchTone;
}

interface HistoryState {
  items: MediaProcessingAdminItem[];
  cursor: string;
  hasMore: boolean;
  loading: boolean;
  refreshing: boolean;
  loadingMore: boolean;
  message: string;
  tone: FetchTone;
}

interface RetryDialogState {
  mode: "single" | "bulk";
  items: MediaProcessingAdminItem[];
  idempotencyKey: string;
  reasonCode: MediaProcessingRetryReasonCode;
  note: string;
  confirmed: boolean;
  busy: boolean;
  outcome: MediaProcessingRetryResponse | MediaProcessingBulkRetryResponse | null;
  error: string;
}

function MediaProcessingView({ active }: { active: boolean }) {
  const { token } = useAdminSession();
  const [overview, setOverview] = useState<OverviewState>({
    data: null,
    loading: true,
    refreshing: false,
    message: "",
    tone: "info"
  });
  const [historyDraft, setHistoryDraft] = useState<MediaProcessingHistoryFilters>(defaultMediaProcessingHistoryFilters);
  const [appliedHistory, setAppliedHistory] = useState<MediaProcessingHistoryFilters>(defaultMediaProcessingHistoryFilters);
  const appliedHistoryRef = useRef(appliedHistory);
  const [history, setHistory] = useState<HistoryState>({
    items: [],
    cursor: "",
    hasMore: false,
    loading: true,
    refreshing: false,
    loadingMore: false,
    message: "",
    tone: "info"
  });
  const [selectedJobIds, setSelectedJobIds] = useState<number[]>([]);
  const [retryDialog, setRetryDialog] = useState<RetryDialogState | null>(null);
  const overviewGeneration = useRef(0);
  const historyGeneration = useRef(0);
  const overviewAbortRef = useRef<AbortController | null>(null);
  const historyAbortRef = useRef<AbortController | null>(null);
  const timerRef = useRef<number | null>(null);
  const [documentVisible, setDocumentVisible] = useState(() =>
    typeof document === "undefined" ? true : document.visibilityState === "visible"
  );

  useEffect(() => {
    appliedHistoryRef.current = appliedHistory;
  }, [appliedHistory]);

  const clearTimer = useCallback(() => {
    if (timerRef.current !== null) {
      window.clearTimeout(timerRef.current);
      timerRef.current = null;
    }
  }, []);

  const clearProcessingData = useCallback((message: string, tone: FetchTone = "error") => {
    overviewAbortRef.current?.abort();
    historyAbortRef.current?.abort();
    clearTimer();
    setOverview({
      data: null,
      loading: false,
      refreshing: false,
      message,
      tone
    });
    setHistory({
      items: [],
      cursor: "",
      hasMore: false,
      loading: false,
      refreshing: false,
      loadingMore: false,
      message,
      tone
    });
    setSelectedJobIds([]);
    setRetryDialog(null);
  }, [clearTimer]);

  const loadOverview = useCallback(async () => {
    const generation = ++overviewGeneration.current;
    overviewAbortRef.current?.abort();
    const controller = new AbortController();
    overviewAbortRef.current = controller;
    setOverview((current) => ({
      ...current,
      loading: current.data === null,
      refreshing: current.data !== null,
      message: "",
      tone: "info"
    }));
    try {
      const response = await fetchMediaProcessingOverview(token, controller.signal);
      if (generation !== overviewGeneration.current) return;
      setOverview({
        data: response,
        loading: false,
        refreshing: false,
        message: "",
        tone: "info"
      });
    } catch (error: unknown) {
      if (controller.signal.aborted || generation !== overviewGeneration.current) return;
      if (error instanceof ApiError && error.status === 401) {
        clearProcessingData("后台会话已失效，请重新登录");
        return;
      }
      if (error instanceof ApiError && error.status === 403) {
        clearProcessingData("服务端拒绝了处理进度访问");
        return;
      }
      setOverview((current) => ({
        ...current,
        loading: false,
        refreshing: false,
        message: apiErrorMessage(error, "处理进度加载失败，请重试"),
        tone: "error"
      }));
    }
  }, [clearProcessingData, token]);

  const loadHistory = useCallback(async (
    filters: MediaProcessingHistoryFilters,
    nextCursor = "",
    append = false,
    reset = false
  ) => {
    const generation = ++historyGeneration.current;
    historyAbortRef.current?.abort();
    const controller = new AbortController();
    historyAbortRef.current = controller;
    if (reset) setSelectedJobIds([]);
    setHistory((current) => {
      const keepExisting = append && current.items.length > 0;
      return {
        ...current,
        items: reset ? [] : current.items,
        cursor: reset ? "" : current.cursor,
        hasMore: reset ? false : current.hasMore,
        loading: !keepExisting && !append,
        refreshing: keepExisting && !append,
        loadingMore: append,
        message: "",
        tone: "info"
      };
    });
    try {
      const page = await fetchMediaProcessingHistory(token, filters, nextCursor, 20, controller.signal);
      if (generation !== historyGeneration.current) return;
      setHistory((current) => {
        const items = append ? [...current.items, ...page.items] : page.items;
        return {
          ...current,
          items,
          cursor: page.next_cursor,
          hasMore: page.has_more,
          loading: false,
          refreshing: false,
          loadingMore: false,
          message: "",
          tone: "info"
        };
      });
    } catch (error: unknown) {
      if (controller.signal.aborted || generation !== historyGeneration.current) return;
      if (error instanceof ApiError && error.status === 401) {
        clearProcessingData("后台会话已失效，请重新登录");
        return;
      }
      if (error instanceof ApiError && error.status === 403) {
        clearProcessingData("服务端拒绝了处理进度访问");
        return;
      }
      const cursorStale = error instanceof ApiError && (
        error.code === "ADMIN_MEDIA_PROCESSING_CURSOR_INVALID" ||
        error.code === "MEDIA_PROCESSING_CURSOR_INVALID"
      );
      setHistory((current) => ({
        ...current,
        loading: false,
        refreshing: false,
        loadingMore: false,
        message: cursorStale
          ? "筛选条件已变化，请重新查询"
          : apiErrorMessage(error, "处理历史加载失败，请重试"),
        tone: cursorStale ? "warning" : "error"
      }));
    }
  }, [clearProcessingData, token]);

  const refreshProcessingData = useCallback(() => {
    void Promise.all([loadOverview(), loadHistory(appliedHistoryRef.current, "", false, true)]);
  }, [loadHistory, loadOverview]);

  useEffect(() => {
    const handleVisibilityChange = () => {
      setDocumentVisible(document.visibilityState === "visible");
    };
    document.addEventListener("visibilitychange", handleVisibilityChange);
    return () => document.removeEventListener("visibilitychange", handleVisibilityChange);
  }, []);

  useEffect(() => {
    if (!active || !documentVisible) {
      overviewAbortRef.current?.abort();
      historyAbortRef.current?.abort();
      clearTimer();
      return;
    }
    void refreshProcessingData();
  }, [active, clearTimer, documentVisible, refreshProcessingData]);

  useEffect(() => {
    clearTimer();
    if (!active || !documentVisible || !overview.data || overview.loading || overview.refreshing) return;
    timerRef.current = window.setTimeout(() => {
      void loadOverview();
    }, computePollingDelay(overview.data.summary));
    return clearTimer;
  }, [
    active,
    clearTimer,
    documentVisible,
    loadOverview,
    overview.data,
    overview.loading,
    overview.refreshing
  ]);

  useEffect(() => () => {
    overviewAbortRef.current?.abort();
    historyAbortRef.current?.abort();
    clearTimer();
  }, [clearTimer]);

  const hasSelectedRetryTargets = selectedJobIds.length > 0 && selectedJobIds.length <= 50;
  const selectedRetryItems = useMemo(() =>
    history.items.filter((item) => selectedJobIds.includes(item.job_id)),
    [history.items, selectedJobIds]
  );
  const selectedCountLabel = selectedJobIds.length === 0
    ? "未选择"
    : selectedJobIds.length > 50
      ? `已选 ${selectedJobIds.length} 项（最多 50 项）`
      : `已选 ${selectedJobIds.length} 项`;

  const activeItems = overview.data?.active_items || [];
  const summary = overview.data?.summary || emptySummary();
  const lastRefreshedAt = overview.data?.refreshed_at || "";

  const submitFilters = () => {
    const next = { ...historyDraft };
    setAppliedHistory(next);
    appliedHistoryRef.current = next;
    void loadHistory(next, "", false, true);
  };

  const openSingleRetry = (item: MediaProcessingAdminItem) => {
    setRetryDialog({
      mode: "single",
      items: [item],
      idempotencyKey: createIdempotencyKey(),
      reasonCode: "temporary_failure",
      note: "",
      confirmed: false,
      busy: false,
      outcome: null,
      error: ""
    });
  };

  const openBulkRetry = () => {
    if (selectedRetryItems.length === 0) return;
    if (selectedRetryItems.length > 50) {
      setHistory((current) => ({
        ...current,
        message: "一次最多只能重新处理 50 项任务，请缩小选择范围。",
        tone: "warning"
      }));
      return;
    }
    setRetryDialog({
      mode: "bulk",
      items: selectedRetryItems,
      idempotencyKey: createIdempotencyKey(),
      reasonCode: "temporary_failure",
      note: "",
      confirmed: false,
      busy: false,
      outcome: null,
      error: ""
    });
  };

  const submitRetry = async () => {
    if (!retryDialog || !retryDialog.confirmed) return;
    setRetryDialog((current) => current ? { ...current, busy: true, error: "" } : current);
    try {
      if (retryDialog.mode === "single") {
        const result = await retryMediaProcessingJob(
          token,
          retryDialog.items[0].job_id,
          {
            reason_code: retryDialog.reasonCode,
            note: retryDialog.note
          },
          retryDialog.idempotencyKey
        );
        if (!result.audit_committed) {
          setRetryDialog((current) => current ? {
            ...current,
            busy: false,
            error: "服务端未确认审计提交，未报告成功。"
          } : current);
          return;
        }
        setRetryDialog((current) => current ? {
          ...current,
          busy: false,
          outcome: result,
          error: ""
        } : current);
        refreshProcessingData();
      } else {
        const result = await bulkRetryMediaProcessingJobs(
          token,
          {
            job_ids: retryDialog.items.map((item) => item.job_id),
            reason_code: retryDialog.reasonCode,
            note: retryDialog.note
          },
          retryDialog.idempotencyKey
        );
        setRetryDialog((current) => current ? {
          ...current,
          busy: false,
          outcome: result,
          error: ""
        } : current);
        refreshProcessingData();
      }
    } catch (error: unknown) {
      if (error instanceof ApiError && (error.status === 401 || error.status === 403)) {
        clearProcessingData(error.status === 401 ? "后台会话已失效，请重新登录" : "服务端拒绝了处理进度访问");
        return;
      }
      if (isRetryConflictError(error)) {
        setRetryDialog((current) => current ? {
          ...current,
          busy: false,
          error: "任务状态已变化，请刷新后重试。"
        } : current);
        refreshProcessingData();
        return;
      }
      setRetryDialog((current) => current ? {
        ...current,
        busy: false,
        error: apiErrorMessage(error, "重新处理失败，请重试")
      } : current);
    }
  };

  return (
    <section className="admin-section" hidden={!active} aria-label="处理进度">
      <header className="admin-processing-header">
        <div>
          <h2>处理进度</h2>
          <p>查看当前等待、处理和历史任务；技术详情仅在诊断信息中展开。</p>
        </div>
        <div className="admin-processing-meta">
          <button
            type="button"
            disabled={overview.loading || overview.refreshing || history.loading}
            onClick={() => refreshProcessingData()}
          >
            {overview.loading || overview.refreshing || history.loading ? "刷新中…" : "刷新"}
          </button>
          <span className="admin-muted">
            最近刷新：{lastRefreshedAt ? formatDateTime(lastRefreshedAt) : "—"}
          </span>
        </div>
      </header>

      {overview.message && (
        <div className={`admin-alert ${overview.tone === "warning" ? "warning" : ""}`}>
          {overview.message}
        </div>
      )}
      {overview.loading && !overview.data && <ProcessingState title="正在加载处理进度…" />}
      {!overview.loading && !overview.data && overview.message && (
        <ProcessingState title={overview.message} />
      )}

      <section className="admin-summary-grid" aria-label="处理摘要">
        <SummaryCard title="等待中" value={summary.waiting} />
        <SummaryCard title="处理中" value={summary.processing} />
        <SummaryCard title="失败" value={summary.failed} />
        <SummaryCard title="已完成" value={summary.completed} />
        <SummaryCard title="最早等待" value={summary.oldest_waiting_at ? formatDateTime(summary.oldest_waiting_at) : "—"} />
      </section>

      <section className="admin-panel">
        <div className="admin-panel-header">
          <div>
            <h3>当前任务</h3>
            <p>仅展示当前等待和处理中的任务。</p>
          </div>
          <span className="admin-muted">
            {activeItems.length > 0 ? `共 ${activeItems.length} 项` : "暂无当前任务"}
          </span>
        </div>
        {activeItems.length > 0 ? (
          <div className="admin-table-wrap">
            <table className="admin-table">
              <thead>
                <tr>
                  <th>视频</th>
                  <th>当前状态</th>
                  <th>当前步骤</th>
                  <th>步骤进度</th>
                  <th>已等待/处理</th>
                  <th>已尝试</th>
                  <th>最后更新</th>
                  <th>操作</th>
                </tr>
              </thead>
              <tbody>
                {activeItems.map((item) => (
                  <tr key={item.job_id}>
                    <td>
                      <strong>{item.title || "未命名视频"}</strong>
                      <small>{item.state === "processing" ? "正在更新中" : mediaProcessingStateLabel(item.state)}</small>
                    </td>
                    <td>
                      <span className={`admin-status-pill media-processing-${item.state}`}>
                        {mediaProcessingStateLabel(item.state)}
                      </span>
                      {item.state === "failed" && item.error_message && <small>{item.error_message}</small>}
                    </td>
                    <td>
                      <strong>{mediaProcessingStageLabel(item.stage)}</strong>
                      <small>{stageDetail(item)}</small>
                    </td>
                    <td>{formatStageProgress(item.stage_progress_bps)}</td>
                    <td>{formatElapsed(item.created_at, item.state === "completed" || item.state === "failed" ? item.completed_at || item.updated_at : undefined)}</td>
                    <td>{item.attempts}/{item.max_attempts}</td>
                    <td>{formatDateTime(item.progress_updated_at || item.updated_at)}</td>
                    <td>
                      <details className="admin-row-details">
                        <summary>诊断信息</summary>
                        <dl className="admin-diagnostics-grid">
                          <div><dt>任务 ID</dt><dd>{item.job_id}</dd></div>
                          <div><dt>视频 ID</dt><dd>{formatOptionalNumber(item.video_id)}</dd></div>
                          <div><dt>作者 ID</dt><dd>{formatOptionalNumber(item.author_id)}</dd></div>
                          <div><dt>配置版本</dt><dd>{item.profile_version}</dd></div>
                          <div><dt>错误码</dt><dd>{item.error_code || "—"}</dd></div>
                          <div><dt>下一次尝试</dt><dd>{formatDateTime(item.next_attempt_at)}</dd></div>
                        </dl>
                      </details>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        ) : (
          <ProcessingState title="当前没有正在等待或处理中的任务" />
        )}
      </section>

      <section className="admin-panel">
        <div className="admin-panel-header">
          <div>
            <h3>历史记录</h3>
            <p>按完成时间筛选最近的处理结果，并对失败任务执行重新处理。</p>
          </div>
          <div className="admin-history-toolbar">
            <button type="button" className="subtle" disabled={!hasSelectedRetryTargets} onClick={openBulkRetry}>
              重新处理所选
            </button>
            <span className="admin-muted">{selectedCountLabel}</span>
          </div>
        </div>

        <form className="admin-filter-grid admin-processing-filters" onSubmit={(event) => {
          event.preventDefault();
          submitFilters();
        }}>
          <label>
            结果
            <select
              value={historyDraft.state}
              onChange={(event) => setHistoryDraft({
                ...historyDraft,
                state: event.target.value as "" | MediaProcessingState
              })}
            >
              <option value="">全部</option>
              <option value="failed">已失败</option>
              <option value="completed">已完成</option>
            </select>
          </label>
          <label>
            步骤
            <select
              value={historyDraft.stage}
              onChange={(event) => setHistoryDraft({
                ...historyDraft,
                stage: event.target.value as MediaProcessingHistoryFilters["stage"]
              })}
            >
              <option value="">全部</option>
              {mediaProcessingStageOptions().map((option) => (
                <option key={option.value} value={option.value}>{option.label}</option>
              ))}
            </select>
          </label>
          <label>
            错误码
            <input
              maxLength={64}
              value={historyDraft.error_code}
              onChange={(event) => setHistoryDraft({ ...historyDraft, error_code: event.target.value })}
              placeholder="例如 source_deleted"
            />
          </label>
          <label>
            视频 ID
            <input
              inputMode="numeric"
              value={historyDraft.video_id}
              onChange={(event) => setHistoryDraft({ ...historyDraft, video_id: event.target.value })}
            />
          </label>
          <label>
            完成起点
            <input
              type="datetime-local"
              value={historyDraft.completed_from}
              onChange={(event) => setHistoryDraft({ ...historyDraft, completed_from: event.target.value })}
            />
          </label>
          <label>
            完成终点
            <input
              type="datetime-local"
              step="0.001"
              value={historyDraft.completed_to}
              onChange={(event) => setHistoryDraft({ ...historyDraft, completed_to: event.target.value })}
            />
          </label>
          <button type="submit">查询历史</button>
        </form>

        {history.message && (
          <div className={`admin-alert ${history.tone === "warning" ? "warning" : ""}`}>
            {history.message}
          </div>
        )}
        {history.loading && !history.items.length && <ProcessingState title="正在加载历史记录…" />}
        {!history.loading && !history.items.length && history.message && (
          <ProcessingState title={history.message} />
        )}
        {!history.loading && !history.items.length && !history.message && (
          <ProcessingState title="当前没有符合条件的历史记录" />
        )}

        {history.items.length > 0 && (
          <>
            <div className="admin-table-wrap">
              <table className="admin-table">
                <thead>
                  <tr>
                    <th>选择</th>
                    <th>视频</th>
                    <th>结果</th>
                    <th>当前步骤</th>
                    <th>完成时间</th>
                    <th>操作</th>
                  </tr>
                </thead>
                <tbody>
                  {history.items.map((item) => {
                    const canRetry = item.state === "failed";
                    const selected = selectedJobIds.includes(item.job_id);
                    return (
                      <tr key={item.job_id}>
                        <td>
                          {canRetry ? (
                            <label className="admin-row-check">
                              <input
                                type="checkbox"
                                checked={selected}
                                onChange={(event) => {
                                  setSelectedJobIds((current) => event.target.checked
                                    ? [...current, item.job_id]
                                    : current.filter((jobID) => jobID !== item.job_id));
                                }}
                              />
                              <span>选择</span>
                            </label>
                          ) : (
                            <span className="admin-muted">—</span>
                          )}
                        </td>
                        <td>
                          <strong>{item.title || "未命名视频"}</strong>
                          <small>{mediaProcessingStateLabel(item.state)}</small>
                        </td>
                        <td>
                          <span className={`admin-status-pill media-processing-${item.state}`}>
                            {mediaProcessingStateLabel(item.state)}
                          </span>
                          {item.state === "failed" && (
                            <small>{item.error_message || "处理失败，稍后可重新处理"}</small>
                          )}
                        </td>
                        <td>
                          <strong>{mediaProcessingStageLabel(item.stage)}</strong>
                          <small>{stageDetail(item)}</small>
                        </td>
                        <td>{formatDateTime(item.completed_at || item.updated_at)}</td>
                        <td className="admin-history-actions">
                          {canRetry && (
                            <button type="button" className="subtle" onClick={() => openSingleRetry(item)}>
                              重新处理
                            </button>
                          )}
                          <details className="admin-row-details">
                            <summary>诊断信息</summary>
                            <dl className="admin-diagnostics-grid">
                              <div><dt>任务 ID</dt><dd>{item.job_id}</dd></div>
                              <div><dt>视频 ID</dt><dd>{formatOptionalNumber(item.video_id)}</dd></div>
                              <div><dt>作者 ID</dt><dd>{formatOptionalNumber(item.author_id)}</dd></div>
                              <div><dt>配置版本</dt><dd>{item.profile_version}</dd></div>
                              <div><dt>错误码</dt><dd>{item.error_code || "—"}</dd></div>
                              <div><dt>下次尝试</dt><dd>{formatDateTime(item.next_attempt_at)}</dd></div>
                              <div><dt>进度更新时间</dt><dd>{formatDateTime(item.progress_updated_at)}</dd></div>
                            </dl>
                          </details>
                        </td>
                      </tr>
                    );
                  })}
                </tbody>
              </table>
            </div>
            {history.hasMore && (
              <button
                className="admin-load-more"
                type="button"
                disabled={history.loadingMore}
                onClick={() => void loadHistory(appliedHistoryRef.current, history.cursor, true, false)}
              >
                {history.loadingMore ? "加载中…" : "加载更多"}
              </button>
            )}
          </>
        )}
      </section>

      {retryDialog && (
        <div className="admin-dialog-backdrop" role="presentation">
          <div className="admin-dialog admin-processing-dialog" role="dialog" aria-modal="true" aria-labelledby="processing-retry-title">
            <h2 id="processing-retry-title">
              {retryDialog.mode === "single" ? "确认重新处理" : "确认批量重新处理"}
            </h2>
            <p>
              {retryDialog.mode === "single"
                ? `任务 #${retryDialog.items[0].job_id} · ${retryDialog.items[0].title || "未命名视频"}`
                : `已选择 ${retryDialog.items.length} 项任务`}
            </p>
            <label>
              原因
              <select
                value={retryDialog.reasonCode}
                onChange={(event) => setRetryDialog((current) => current ? {
                  ...current,
                  reasonCode: event.target.value as MediaProcessingRetryReasonCode
                } : current)}
              >
                {retryReasonOptions().map((option) => (
                  <option key={option.value} value={option.value}>{option.label}</option>
                ))}
              </select>
            </label>
            <label>
              备注
              <textarea
                maxLength={1000}
                value={retryDialog.note}
                onChange={(event) => setRetryDialog((current) => current ? {
                  ...current,
                  note: event.target.value
                } : current)}
              />
            </label>
            <label className="admin-confirm-check">
              <input
                type="checkbox"
                checked={retryDialog.confirmed}
                onChange={(event) => setRetryDialog((current) => current ? {
                  ...current,
                  confirmed: event.target.checked
                } : current)}
              />
              我确认按当前条件重新处理并写入审计
            </label>
            {retryDialog.error && <p className="admin-inline-error">{retryDialog.error}</p>}
            {retryDialog.outcome && (
              <div className="admin-retry-result">
                {retryDialog.mode === "single"
                  ? renderSingleRetryResult(retryDialog.outcome as MediaProcessingRetryResponse)
                  : renderBulkRetryResult(retryDialog.outcome as MediaProcessingBulkRetryResponse, retryDialog.items)}
              </div>
            )}
            <div className="admin-dialog-actions">
              <button
                type="button"
                disabled={retryDialog.busy}
                onClick={() => setRetryDialog(null)}
              >
                {retryDialog.outcome ? "关闭" : "取消"}
              </button>
              <button
                type="button"
                className="danger"
                disabled={!retryDialog.confirmed || retryDialog.busy}
                onClick={() => void submitRetry()}
              >
                {retryDialog.busy ? "提交中…" : retryDialog.mode === "single" ? "确认重新处理" : "确认批量重新处理"}
              </button>
            </div>
          </div>
        </div>
      )}
    </section>
  );
}

function ProcessingState({
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

function SummaryCard({ title, value }: { title: string; value: string | number }) {
  return (
    <article className="admin-summary-card">
      <span>{title}</span>
      <strong>{value}</strong>
    </article>
  );
}

function renderSingleRetryResult(result: MediaProcessingRetryResponse): JSX.Element {
  return (
    <div className="admin-retry-outcome">
      <strong>{result.replayed ? "已使用已提交结果重放" : "已返回处理队列"}</strong>
      <small>{result.item.title || "未命名视频"} · {mediaProcessingStateLabel(result.item.state)}</small>
    </div>
  );
}

function renderBulkRetryResult(
  result: MediaProcessingBulkRetryResponse,
  sourceItems: MediaProcessingAdminItem[]
): JSX.Element {
  const sourceByJobId = new Map(sourceItems.map((item) => [item.job_id, item]));
  return (
    <div className="admin-retry-outcome">
      <strong>部分结果如下</strong>
      <ul className="admin-bulk-results">
        {result.items.map((item) => {
          const source = sourceByJobId.get(item.job_id);
          return (
            <li key={item.job_id}>
              <span>{source?.title || `任务 #${item.job_id}`}</span>
              <strong>{retryOutcomeLabel(item)}</strong>
            </li>
          );
        })}
      </ul>
    </div>
  );
}

function retryOutcomeLabel(item: MediaProcessingBulkRetryItemResult): string {
  switch (item.status) {
    case "retried":
      return item.item ? "已重新处理" : "已重新处理";
    case "conflict":
      return "状态已变化";
    case "rejected":
      return "已拒绝";
    default:
      return item.status;
  }
}

function emptySummary(): MediaProcessingSummary {
  return {
    waiting: 0,
    processing: 0,
    failed: 0,
    completed: 0
  };
}

function computePollingDelay(summary: MediaProcessingSummary): number {
  if (summary.processing > 0) return 5_000;
  if (summary.waiting > 0) return 10_000;
  return 30_000;
}

function stageDetail(item: MediaProcessingAdminItem): string {
  if (item.stage_progress_bps === undefined || item.stage_progress_bps === null) return "无可量化进度";
  return formatStageProgress(item.stage_progress_bps);
}

function formatStageProgress(value?: number | null): string {
  if (value === undefined || value === null) return "—";
  if (!Number.isFinite(value) || value < 0 || value > 10_000) return "—";
  const percent = value / 100;
  return `${percent % 1 === 0 ? percent.toFixed(0) : percent.toFixed(2)}%`;
}

function formatElapsed(startAt: string, endAt?: string | null): string {
  const start = Date.parse(startAt);
  const end = endAt ? Date.parse(endAt) : Date.now();
  if (!Number.isFinite(start) || !Number.isFinite(end) || end < start) return "—";
  return formatDuration(end - start);
}

function formatDuration(milliseconds: number): string {
  const seconds = Math.max(0, Math.floor(milliseconds / 1000));
  const days = Math.floor(seconds / 86_400);
  const hours = Math.floor((seconds % 86_400) / 3_600);
  const minutes = Math.floor((seconds % 3_600) / 60);
  const remainder = seconds % 60;
  if (days > 0) return `${days}天${hours}小时`;
  if (hours > 0) return `${hours}小时${minutes}分`;
  if (minutes > 0) return `${minutes}分${remainder}秒`;
  return `${remainder}秒`;
}

function formatDateTime(value?: string | null): string {
  if (!value) return "—";
  const time = Date.parse(value);
  if (!Number.isFinite(time)) return "—";
  return new Date(time).toLocaleString();
}

function formatOptionalNumber(value?: number): string {
  return value === undefined ? "—" : String(value);
}

function mediaProcessingStageOptions(): Array<{ value: MediaProcessingStage; label: string }> {
  return [
    { value: "waiting", label: mediaProcessingStageLabel("waiting") },
    { value: "downloading", label: mediaProcessingStageLabel("downloading") },
    { value: "inspecting", label: mediaProcessingStageLabel("inspecting") },
    { value: "remuxing", label: mediaProcessingStageLabel("remuxing") },
    { value: "transcoding", label: mediaProcessingStageLabel("transcoding") },
    { value: "uploading", label: mediaProcessingStageLabel("uploading") },
    { value: "finalizing", label: mediaProcessingStageLabel("finalizing") },
    { value: "completed", label: mediaProcessingStageLabel("completed") },
    { value: "failed", label: mediaProcessingStageLabel("failed") }
  ];
}

function retryReasonOptions(): Array<{ value: MediaProcessingRetryReasonCode; label: string }> {
  return [
    { value: "configuration_changed", label: mediaProcessingRetryReasonLabel("configuration_changed") },
    { value: "temporary_failure", label: mediaProcessingRetryReasonLabel("temporary_failure") },
    { value: "operator_retry", label: mediaProcessingRetryReasonLabel("operator_retry") }
  ];
}

function createIdempotencyKey(): string {
  if (typeof crypto !== "undefined" && typeof crypto.randomUUID === "function") {
    return `media-processing-${crypto.randomUUID()}`;
  }
  return `media-processing-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 10)}`;
}

function isRetryConflictError(error: unknown): boolean {
  return error instanceof ApiError &&
    (error.status === 409 ||
      error.code === "IDEMPOTENCY_CONFLICT" ||
      error.code === "ADMIN_MEDIA_PROCESSING_IDEMPOTENCY_CONFLICT" ||
      error.code === "ADMIN_MEDIA_PROCESSING_STATE_CONFLICT" ||
      error.code === "MEDIA_PROCESSING_STATE_CONFLICT");
}

function localDateTime(value: Date, includeSeconds = false): string {
  const offset = value.getTimezoneOffset() * 60_000;
  return new Date(value.getTime() - offset).toISOString().slice(0, includeSeconds ? 23 : 16);
}
