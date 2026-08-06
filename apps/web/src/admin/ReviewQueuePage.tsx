import { useCallback, useEffect, useRef, useState } from "react";
import { fetchReviewQueue } from "../api/review";
import { ApiError, apiErrorMessage } from "../api/client";
import { useNavigate } from "../router";
import { useSession } from "../session";
import type { ReviewQueueItem } from "../types";

type QueueState = "loading" | "ready" | "empty" | "error" | "forbidden";

export function ReviewQueuePage() {
  const { token } = useSession();
  const navigate = useNavigate();
  const [items, setItems] = useState<ReviewQueueItem[]>([]);
  const [state, setState] = useState<QueueState>("loading");
  const [message, setMessage] = useState("");
  const [cursor, setCursor] = useState("");
  const [hasMore, setHasMore] = useState(false);
  const [loadingMore, setLoadingMore] = useState(false);
  const [refreshing, setRefreshing] = useState(false);
  const [minPriority, setMinPriority] = useState(0);
  const [appliedMinPriority, setAppliedMinPriority] = useState(0);
  const generation = useRef(0);

  const load = useCallback(async (nextCursor = "", append = false) => {
    const requestGeneration = ++generation.current;
    if (append) setLoadingMore(true);
    else if (items.length > 0) setRefreshing(true);
    else setState("loading");
    setMessage("");
    try {
      const page = await fetchReviewQueue(token, {
        minPriority: appliedMinPriority,
        maxPriority: 100,
        cursor: nextCursor,
        limit: 20
      });
      if (generation.current !== requestGeneration) return;
      setItems((current) => append ? [...current, ...page.items] : page.items);
      setCursor(page.next_cursor);
      setHasMore(page.has_more);
      setState(append || page.items.length > 0 ? "ready" : "empty");
    } catch (error: unknown) {
      if (generation.current !== requestGeneration) return;
      if (error instanceof ApiError && error.status === 403) {
        setItems([]);
        setCursor("");
        setHasMore(false);
        setState("forbidden");
      } else if (!append) {
        setState("error");
      }
      setMessage(apiErrorMessage(error, "审核队列加载失败，请重试"));
    } finally {
      if (generation.current === requestGeneration) {
        setLoadingMore(false);
        setRefreshing(false);
      }
    }
  }, [appliedMinPriority, token]);

  useEffect(() => {
    void load();
    return () => {
      generation.current++;
    };
  }, [load]);

  return (
    <section className="admin-page">
      <header className="admin-page-header">
        <div>
          <span className="admin-eyebrow">Human review</span>
          <h1>审核队列</h1>
          <p>按风险优先级和案件年龄稳定排序。</p>
        </div>
        <button type="button" disabled={refreshing} onClick={() => void load()}>
          {refreshing ? "刷新中…" : "刷新"}
        </button>
      </header>
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
          if (minPriority === appliedMinPriority) void load();
          else setAppliedMinPriority(minPriority);
        }}>应用筛选</button>
      </div>
      {state === "loading" && <AdminState title="正在加载审核队列…" />}
      {state === "error" && <AdminState title={message} action="重试" onAction={() => void load()} />}
      {state === "forbidden" && <AdminState title="服务端拒绝了审核队列访问" />}
      {state === "empty" && <AdminState title="当前没有可领取的审核案件" action="刷新" onAction={() => void load()} />}
      {(state === "ready" || (state !== "forbidden" && items.length > 0)) && (
        <>
          <div className="admin-table-wrap">
            <table className="admin-table">
              <thead>
                <tr>
                  <th>案件</th>
                  <th>视频</th>
                  <th>优先级</th>
                  <th>创建时间</th>
                  <th>状态</th>
                </tr>
              </thead>
              <tbody>
                {items.map((item) => (
                  <tr key={item.case.id}>
                    <td>
                      <button
                        className="admin-link"
                        type="button"
                        onClick={() => navigate(`/admin/reviews/${item.case.id}`)}
                      >
                        #{item.case.id}
                      </button>
                    </td>
                    <td>
                      <strong>{item.title || `视频 #${item.case.video_id}`}</strong>
                      <small>作者 #{item.author_id}</small>
                    </td>
                    <td><span className="admin-priority">{item.case.priority}</span></td>
                    <td>{formatTime(item.case.created_at)}</td>
                    <td>{item.case.status}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
          {message && <p className="admin-inline-error">{message}</p>}
          {hasMore && (
            <button
              className="admin-load-more"
              type="button"
              disabled={loadingMore}
              onClick={() => void load(cursor, true)}
            >
              {loadingMore ? "加载中…" : "加载更多"}
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

function formatTime(value: string): string {
  return new Date(value).toLocaleString();
}
