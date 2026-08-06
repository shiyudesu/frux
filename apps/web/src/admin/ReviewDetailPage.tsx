import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  claimReviewCase,
  decideReviewCase,
  fetchReviewCase,
  renewReviewLease
} from "../api/review";
import { ApiError, apiErrorMessage } from "../api/client";
import { useNavigate } from "../router";
import { useSession } from "../session";
import type { ReviewCaseDetail, ReviewLeaseResponse } from "../types";

type DetailState = "loading" | "ready" | "error" | "forbidden";

export function ReviewDetailPage({ reviewID }: { reviewID: number }) {
  const { token, adminPrincipal } = useSession();
  const navigate = useNavigate();
  const [detail, setDetail] = useState<ReviewCaseDetail | null>(null);
  const [state, setState] = useState<DetailState>("loading");
  const [message, setMessage] = useState("");
  const [busy, setBusy] = useState("");
  const [lease, setLease] = useState<ReviewLeaseResponse | null>(null);
  const [leaseExpired, setLeaseExpired] = useState(false);
  const [versionConflict, setVersionConflict] = useState(false);
  const [outcome, setOutcome] = useState<"approve" | "reject">("approve");
  const [reasonCode, setReasonCode] = useState("content_compliant");
  const [note, setNote] = useState("");
  const pendingDecision = useRef<{ signature: string; key: string } | null>(null);
  const canDecide = adminPrincipal?.permissions.includes("review.decide") || false;

  const load = useCallback(async () => {
    setState(detail ? "ready" : "loading");
    setMessage("");
    try {
      const next = await fetchReviewCase(token, reviewID);
      setDetail(next);
      setState("ready");
      setVersionConflict(false);
      if (!next.case.assigned_reviewer_id) setLease(null);
    } catch (error: unknown) {
      if (error instanceof ApiError && error.status === 403) setState("forbidden");
      else if (!detail) setState("error");
      setMessage(apiErrorMessage(error, "审核详情加载失败，请重试"));
    }
  }, [detail, reviewID, token]);

  useEffect(() => {
    void load();
  }, [reviewID, token]);

  useEffect(() => {
    if (!lease?.case.lease_expires_at) return;
    const update = () => setLeaseExpired(
      Date.now() >= new Date(lease.case.lease_expires_at || "").getTime()
    );
    update();
    const timer = window.setInterval(update, 1000);
    return () => window.clearInterval(timer);
  }, [lease]);

  const signals = detail?.history.signals || [];
  const history = useMemo(() => {
    if (!detail) return [];
    return [
      ...detail.history.automated_decisions.map((item) => ({
        id: `auto-${item.id}`, time: item.created_at,
        label: `自动判定：${item.outcome} · policy v${item.policy_version}`
      })),
      ...detail.history.assignments.map((item) => ({
        id: `assignment-${item.id}`, time: item.created_at,
        label: `租约事件：${item.event} · reviewer #${item.reviewer_id}`
      })),
      ...detail.history.human_decisions.map((item) => ({
        id: `human-${item.id}`, time: item.created_at,
        label: `人工判定：${item.outcome} · ${item.reason_code}`
      }))
    ].sort((a, b) => new Date(a.time).getTime() - new Date(b.time).getTime());
  }, [detail]);

  const handleActionError = (error: unknown, fallback: string) => {
    if (error instanceof ApiError && error.status === 403) {
      setState("forbidden");
      setMessage("服务端拒绝了当前操作");
      return;
    }
    if (error instanceof ApiError && error.code === "REVIEW_LEASE_EXPIRED") {
      setLeaseExpired(true);
      setMessage("租约已过期。已保留证据，请返回队列重新领取。");
      return;
    }
    if (error instanceof ApiError &&
      (error.code === "REVIEW_CASE_VERSION_CONFLICT" ||
        error.code === "REVIEW_SUBJECT_VERSION_CONFLICT" ||
        error.code === "REVIEW_CASE_CLAIMED")) {
      setVersionConflict(true);
      setMessage("案件状态已变化，请刷新后继续。");
      return;
    }
    setMessage(apiErrorMessage(error, fallback));
  };

  const claim = async () => {
    if (!detail) return;
    setBusy("claim");
    setMessage("");
    try {
      const result = await claimReviewCase(token, reviewID, detail.case.version);
      setLease(result);
      setDetail({ ...detail, case: result.case });
      const expired = Boolean(
        result.case.lease_expires_at &&
        Date.now() >= new Date(result.case.lease_expires_at).getTime()
      );
      setLeaseExpired(expired);
      if (expired) {
        setMessage("租约已过期。已保留证据，请返回队列重新领取。");
      }
    } catch (error: unknown) {
      handleActionError(error, "领取失败，请重试");
    } finally {
      setBusy("");
    }
  };

  const renew = async () => {
    if (!detail || !lease) return;
    setBusy("renew");
    setMessage("");
    try {
      const result = await renewReviewLease(token, reviewID, lease.lease_token, detail.case.version);
      setLease(result);
      setDetail({ ...detail, case: result.case });
      setLeaseExpired(false);
    } catch (error: unknown) {
      handleActionError(error, "续租失败，请重试");
    } finally {
      setBusy("");
    }
  };

  const decide = async () => {
    if (!detail || !lease || leaseExpired) return;
    setBusy("decision");
    setMessage("");
    const signature = JSON.stringify({
      reviewID,
      expectedCaseVersion: detail.case.version,
      reviewVersion: detail.case.review_version,
      outcome,
      reasonCode,
      note
    });
    if (pendingDecision.current?.signature !== signature) {
      pendingDecision.current = { signature, key: crypto.randomUUID() };
    }
    try {
      const result = await decideReviewCase(token, reviewID, {
        leaseToken: lease.lease_token,
        expectedCaseVersion: detail.case.version,
        reviewVersion: detail.case.review_version,
        outcome,
        reasonCode,
        note,
        idempotencyKey: pendingDecision.current.key
      });
      pendingDecision.current = null;
      setDetail({
        ...detail,
        case: result.case,
        history: {
          ...detail.history,
          human_decisions: [...detail.history.human_decisions, result.decision]
        }
      });
      setLease(null);
      setMessage(result.duplicate ? "审核结果已存在。" : "审核结果已提交。");
    } catch (error: unknown) {
      handleActionError(error, "提交审核结果失败，请重试");
    } finally {
      setBusy("");
    }
  };

  if (state === "loading") return <DetailState title="正在加载审核详情…" />;
  if (state === "error") return <DetailState title={message} action="重试" onAction={() => void load()} />;
  if (state === "forbidden") return <DetailState title="服务端拒绝了审核详情访问" />;
  if (!detail) return <DetailState title="审核案件不存在" />;

  return (
    <section className="admin-page">
      <header className="admin-page-header">
        <div>
          <button className="admin-back" type="button" onClick={() => navigate("/admin/reviews")}>← 返回队列</button>
          <span className="admin-eyebrow">Case #{detail.case.id}</span>
          <h1>{detail.subject.title || `视频 #${detail.subject.video_id}`}</h1>
          <p>作者 #{detail.subject.author_id} · review v{detail.case.review_version} · case v{detail.case.version}</p>
        </div>
        <span className="admin-status-pill">{detail.case.status}</span>
      </header>
      {message && (
        <div className={leaseExpired || versionConflict ? "admin-alert warning" : "admin-alert"}>
          {message}
          {versionConflict && <button type="button" onClick={() => void load()}>刷新案件</button>}
          {leaseExpired && <button type="button" onClick={() => navigate("/admin/reviews")}>返回队列</button>}
        </div>
      )}
      <div className="admin-detail-grid">
        <article className="admin-panel admin-subject">
          <h2>审核主体</h2>
          {detail.subject.media_url
            ? <video controls poster={detail.subject.cover_url} src={detail.subject.media_url} />
            : detail.subject.cover_url
              ? <img src={detail.subject.cover_url} alt="" />
              : <div className="admin-media-empty">视频预览不可用</div>}
          <h3>{detail.subject.title}</h3>
          <p>{detail.subject.description || "无简介"}</p>
        </article>
        <div className="admin-detail-stack">
          <article className="admin-panel">
            <h2>机器证据</h2>
            {signals.length === 0 && <p className="admin-muted">没有机器证据。</p>}
            <div className="admin-signal-list">
              {signals.map((signal) => (
                <div key={signal.id}>
                  <strong>{signal.label}</strong>
                  <span>{Math.round(signal.confidence * 100)}%</span>
                  <small>{signal.provider} · {signal.model_version} · policy v{signal.policy_version}</small>
                  {signal.evidence_refs.map((reference) => <code key={reference}>{reference}</code>)}
                </div>
              ))}
            </div>
          </article>
          <article className="admin-panel">
            <h2>不可变历史</h2>
            {history.length === 0 && <p className="admin-muted">暂无历史。</p>}
            <ol className="admin-history">
              {history.map((item) => (
                <li key={item.id}><span>{new Date(item.time).toLocaleString()}</span>{item.label}</li>
              ))}
            </ol>
          </article>
        </div>
      </div>
      {canDecide && detail.case.status === "pending_human" && (
        <article className="admin-panel admin-decision-panel">
          <div className="admin-lease-actions">
            {!lease && (
              <button type="button" disabled={Boolean(busy)} onClick={() => void claim()}>
                {busy === "claim" ? "领取中…" : "领取案件"}
              </button>
            )}
            {lease && (
              <>
                <span>租约至 {new Date(lease.case.lease_expires_at || "").toLocaleTimeString()}</span>
                <button type="button" disabled={Boolean(busy) || leaseExpired} onClick={() => void renew()}>
                  {busy === "renew" ? "续租中…" : "续租"}
                </button>
              </>
            )}
          </div>
          {lease && (
            <div className="admin-decision-form">
              <label>
                判定
                <select value={outcome} onChange={(event) => {
                  const next = event.target.value as "approve" | "reject";
                  setOutcome(next);
                  setReasonCode(next === "approve" ? "content_compliant" : "other_policy_violation");
                }}>
                  <option value="approve">通过</option>
                  <option value="reject">驳回</option>
                </select>
              </label>
              <label>
                原因
                <select value={reasonCode} onChange={(event) => setReasonCode(event.target.value)}>
                  {outcome === "approve"
                    ? <>
                        <option value="content_compliant">内容合规</option>
                        <option value="false_positive">机器误判</option>
                      </>
                    : <>
                        <option value="sexual_content">色情内容</option>
                        <option value="graphic_violence">血腥暴力</option>
                        <option value="hate">仇恨</option>
                        <option value="harassment">骚扰</option>
                        <option value="self_harm">自残</option>
                        <option value="illegal_activity">违法活动</option>
                        <option value="spam">垃圾内容</option>
                        <option value="other_policy_violation">其他策略违规</option>
                      </>}
                </select>
              </label>
              <label className="admin-field-wide">
                备注
                <textarea maxLength={1000} value={note} onChange={(event) => setNote(event.target.value)} />
              </label>
              <button
                className={outcome === "reject" ? "danger" : ""}
                type="button"
                disabled={Boolean(busy) || leaseExpired ||
                  (reasonCode === "other_policy_violation" && !note.trim())}
                onClick={() => void decide()}
              >
                {busy === "decision" ? "提交中…" : outcome === "approve" ? "确认通过" : "确认驳回"}
              </button>
            </div>
          )}
        </article>
      )}
    </section>
  );
}

function DetailState({
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
