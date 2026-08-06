import { useCallback, useEffect, useRef, useState } from "react";
import { claimReviewCase, fetchReviewQueue } from "../api/review";
import { ApiError, apiErrorMessage } from "../api/client";
import { useNavigate } from "../router";
import type { ReviewQueueItem, ReviewQueueScope } from "../types";
import { useAdminSession } from "./adminSession";
import { rememberReviewLease } from "./reviewLeaseMemory";

type QueueState = "loading" | "ready" | "empty" | "error" | "forbidden";

interface QueueSnapshot {
  items: ReviewQueueItem[];
  state: QueueState;
  message: string;
  cursor: string;
  hasMore: boolean;
  loadingMore: boolean;
  refreshing: boolean;
}

const scopes: Array<{ value: ReviewQueueScope; label: string }> = [
  { value: "available", label: "待我处理" },
  { value: "mine", label: "我正在审核" },
  { value: "recent", label: "最近完成" }
];

function emptySnapshot(): QueueSnapshot {
  return {
    items: [], state: "loading", message: "", cursor: "", hasMore: false,
    loadingMore: false, refreshing: false
  };
}

function initialSnapshots(): Record<ReviewQueueScope, QueueSnapshot> {
  return {
    available: emptySnapshot(),
    mine: emptySnapshot(),
    recent: emptySnapshot()
  };
}

export function ReviewQueuePage() {
  const { token, principal } = useAdminSession();
  const navigate = useNavigate();
  const [scope, setScope] = useState<ReviewQueueScope>("available");
  const [snapshots, setSnapshots] = useState(initialSnapshots);
  const [minPriority, setMinPriority] = useState(0);
  const [appliedMinPriority, setAppliedMinPriority] = useState(0);
  const [startingID, setStartingID] = useState(0);
  const generations = useRef<Record<ReviewQueueScope, number>>({
    available: 0, mine: 0, recent: 0
  });
  const canDecide = principal?.permissions.includes("review.decide") || false;
  const active = snapshots[scope];

  const updateSnapshot = useCallback((
    target: ReviewQueueScope,
    update: (current: QueueSnapshot) => QueueSnapshot
  ) => {
    setSnapshots((current) => ({ ...current, [target]: update(current[target]) }));
  }, []);

  const load = useCallback(async (
    target: ReviewQueueScope,
    nextCursor = "",
    append = false
  ) => {
    const requestGeneration = ++generations.current[target];
    updateSnapshot(target, (current) => ({
      ...current,
      state: append || current.items.length > 0 ? current.state : "loading",
      message: "",
      loadingMore: append,
      refreshing: !append && current.items.length > 0
    }));
    try {
      const page = await fetchReviewQueue(token, {
        scope: target,
        minPriority: appliedMinPriority,
        maxPriority: 100,
        cursor: nextCursor,
        limit: 20
      });
      if (generations.current[target] !== requestGeneration) return;
      updateSnapshot(target, (current) => {
        const items = append ? [...current.items, ...page.items] : page.items;
        return {
          items,
          cursor: page.next_cursor,
          hasMore: page.has_more,
          state: items.length > 0 ? "ready" : "empty",
          message: "",
          loadingMore: false,
          refreshing: false
        };
      });
    } catch (error: unknown) {
      if (generations.current[target] !== requestGeneration) return;
      if (error instanceof ApiError && error.status === 403) {
        const forbidden = {
          ...emptySnapshot(),
          state: "forbidden" as const,
          message: apiErrorMessage(error, "服务端拒绝了审核任务访问")
        };
        setSnapshots({
          available: { ...forbidden },
          mine: { ...forbidden },
          recent: { ...forbidden }
        });
        return;
      }
      updateSnapshot(target, (current) => {
        return {
          ...current,
          state: append && current.items.length > 0 ? "ready" : "error",
          message: apiErrorMessage(error, "审核任务加载失败，请重试"),
          loadingMore: false,
          refreshing: false
        };
      });
    }
  }, [appliedMinPriority, token, updateSnapshot]);

  useEffect(() => {
    void load(scope);
    return () => {
      generations.current[scope]++;
    };
  }, [load, scope]);

  const startReview = async (item: ReviewQueueItem) => {
    if (!canDecide) {
      navigate(`/admin/reviews/${item.case.id}`);
      return;
    }
    setStartingID(item.case.id);
    updateSnapshot("available", (current) => ({ ...current, message: "" }));
    try {
      const lease = await claimReviewCase(token, item.case.id, item.case.version);
      rememberReviewLease(item.case.id, lease);
      updateSnapshot("available", (current) => {
        const items = current.items.filter((candidate) => candidate.case.id !== item.case.id);
        return { ...current, items, state: items.length > 0 ? "ready" : "empty" };
      });
      updateSnapshot("mine", (current) => ({
        ...current,
        items: [{ ...item, case: lease.case }, ...current.items.filter(
          (candidate) => candidate.case.id !== item.case.id
        )],
        state: "ready"
      }));
      navigate(`/admin/reviews/${item.case.id}`);
    } catch (error: unknown) {
      updateSnapshot("available", (current) => ({
        ...current,
        message: apiErrorMessage(error, "开始审核失败，任务可能已被其他审核员处理")
      }));
    } finally {
      setStartingID(0);
    }
  };

  const emptyMessage = scope === "available"
    ? "当前没有待处理的审核任务"
    : scope === "mine"
      ? "当前没有正在审核的任务"
      : "最近没有已完成的审核任务";

  return (
    <section className="admin-page">
      <header className="admin-page-header">
        <div>
          <span className="admin-eyebrow">Human review</span>
          <h1>审核任务</h1>
          <p>按处理状态查看待处理、进行中和最近完成的内容。</p>
        </div>
        <button type="button" disabled={active.refreshing} onClick={() => void load(scope)}>
          {active.refreshing ? "刷新中…" : "刷新"}
        </button>
      </header>
      <div className="admin-review-tabs" role="tablist" aria-label="审核任务范围">
        {scopes.map((item) => (
          <button
            key={item.value}
            type="button"
            role="tab"
            aria-selected={scope === item.value}
            className={scope === item.value ? "active" : ""}
            onClick={() => setScope(item.value)}
          >
            {item.label}
          </button>
        ))}
      </div>
      <div className="admin-toolbar">
        <label>
          最低优先级
          <input
            type="number"
            min="0"
            max="100"
            value={minPriority}
            onChange={(event) => setMinPriority(Number(event.target.value))}
          />
        </label>
        <button type="button" onClick={() => {
          if (minPriority === appliedMinPriority) void load(scope);
          else setAppliedMinPriority(minPriority);
        }}>应用筛选</button>
      </div>
      {active.state === "loading" && <AdminState title="正在加载审核任务…" />}
      {active.state === "error" && (
        <AdminState title={active.message} action="重试" onAction={() => void load(scope)} />
      )}
      {active.state === "forbidden" && <AdminState title="服务端拒绝了审核任务访问" />}
      {active.state === "empty" && (
        <AdminState title={emptyMessage} action="刷新" onAction={() => void load(scope)} />
      )}
      {(active.state === "ready" ||
        (active.state !== "forbidden" && active.items.length > 0)) && (
        <>
          <div className="admin-table-wrap">
            <table className="admin-table">
              <thead>
                <tr>
                  <th>审核任务</th>
                  <th>视频</th>
                  <th>优先级</th>
                  <th>{scope === "mine" ? "审核占用至" : scope === "recent" ? "完成时间" : "创建时间"}</th>
                  <th>状态</th>
                  <th>操作</th>
                </tr>
              </thead>
              <tbody>
                {active.items.map((item) => (
                  <tr key={item.case.id}>
                    <td>#{item.case.id}</td>
                    <td>
                      <strong>{item.title || `视频 #${item.case.video_id}`}</strong>
                      <small>作者 #{item.author_id}</small>
                    </td>
                    <td><span className="admin-priority">{item.case.priority}</span></td>
                    <td>{formatScopeTime(scope, item)}</td>
                    <td>{reviewStatusLabel(item.case.status)}</td>
                    <td>
                      {scope === "available" ? (
                        <button
                          className="subtle"
                          type="button"
                          disabled={startingID === item.case.id}
                          onClick={() => void startReview(item)}
                        >
                          {startingID === item.case.id ? "开始中…" : canDecide ? "开始审核" : "查看内容"}
                        </button>
                      ) : (
                        <button
                          className="subtle"
                          type="button"
                          onClick={() => navigate(`/admin/reviews/${item.case.id}`)}
                        >
                          {scope === "mine" ? "继续审核" : "查看记录"}
                        </button>
                      )}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
          {active.message && <p className="admin-inline-error">{active.message}</p>}
          {active.hasMore && (
            <button
              className="admin-load-more"
              type="button"
              disabled={active.loadingMore}
              onClick={() => void load(scope, active.cursor, true)}
            >
              {active.loadingMore ? "加载中…" : "加载更多"}
            </button>
          )}
        </>
      )}
    </section>
  );
}

function AdminState({
  title,
  action,
  onAction
}: {
  title: string;
  action?: string;
  onAction?: () => void;
}) {
  return (
    <div className="admin-state" role="status">
      <strong>{title}</strong>
      {action && <button type="button" onClick={onAction}>{action}</button>}
    </div>
  );
}

function formatScopeTime(scope: ReviewQueueScope, item: ReviewQueueItem): string {
  const value = scope === "mine"
    ? item.case.lease_expires_at
    : scope === "recent"
      ? item.case.closed_at
      : item.case.created_at;
  return value ? new Date(value).toLocaleString() : "—";
}

function reviewStatusLabel(status: string): string {
  switch (status) {
    case "pending_human": return "待人工审核";
    case "approved": return "已通过";
    case "rejected": return "未通过";
    case "cancelled": return "已取消";
    case "superseded": return "已更新";
    default: return status;
  }
}
