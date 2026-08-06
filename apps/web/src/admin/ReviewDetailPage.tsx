import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  claimReviewCase,
  decideReviewCase,
  fetchReviewCase,
  fetchReviewPreview,
  releaseReviewLease,
  renewReviewLease,
  resumeReviewLease
} from "../api/review";
import { ApiError, apiErrorMessage } from "../api/client";
import { useNavigate } from "../router";
import { useSession } from "../session";
import type { ReviewCaseDetail, ReviewLeaseResponse, ReviewPreviewAccess } from "../types";
import {
  forgetReviewLease,
  getReviewLease,
  rememberReviewLease
} from "./reviewLeaseMemory";

type DetailState = "loading" | "ready" | "error" | "forbidden";
type PreviewState = "loading" | "ready" | "unavailable";

export function ReviewDetailPage({ reviewID }: { reviewID: number }) {
  const { token, adminPrincipal } = useSession();
  const navigate = useNavigate();
  const [detail, setDetail] = useState<ReviewCaseDetail | null>(null);
  const [detailRevision, setDetailRevision] = useState(0);
  const [state, setState] = useState<DetailState>("loading");
  const [message, setMessage] = useState("");
  const [busy, setBusy] = useState("");
  const [lease, setLease] = useState<ReviewLeaseResponse | null>(() => getReviewLease(reviewID));
  const [leaseClockOffset, setLeaseClockOffset] = useState(() => {
    const remembered = getReviewLease(reviewID);
    return serverClockOffset(remembered?.server_time);
  });
  const [leaseExpired, setLeaseExpired] = useState(false);
  const [versionConflict, setVersionConflict] = useState(false);
  const [clock, setClock] = useState(Date.now());
  const [preview, setPreview] = useState<ReviewPreviewAccess | null>(null);
  const [previewState, setPreviewState] = useState<PreviewState>("loading");
  const [previewMessage, setPreviewMessage] = useState("");
  const [previewClockOffset, setPreviewClockOffset] = useState(0);
  const [outcome, setOutcome] = useState<"approve" | "reject">("approve");
  const [reasonCode, setReasonCode] = useState("content_compliant");
  const [note, setNote] = useState("");
  const pendingDecision = useRef<{ signature: string; key: string } | null>(null);
  const resumeAttempt = useRef("");
  const leaseMutation = useRef(false);
  const previewGeneration = useRef(0);
  const canDecide = adminPrincipal?.permissions.includes("review.decide") || false;
  const currentReviewerID = adminPrincipal?.user_id || 0;

  const load = useCallback(async () => {
    previewGeneration.current++;
    setPreview(null);
    setPreviewState("loading");
    setState((current) => current === "ready" ? "ready" : "loading");
    setMessage("");
    try {
      const next = await fetchReviewCase(token, reviewID);
      setDetail(next);
      setDetailRevision((current) => current + 1);
      setState("ready");
      setVersionConflict(false);
      const remembered = getReviewLease(reviewID);
      if (remembered &&
        remembered.case.version === next.case.version &&
        next.case.assigned_reviewer_id === currentReviewerID) {
        setLease(remembered);
        setLeaseClockOffset(serverClockOffset(remembered.server_time));
      } else {
        if (remembered) forgetReviewLease(reviewID);
        setLease(null);
      }
      if (!next.case.assigned_reviewer_id) {
        resumeAttempt.current = "";
      }
    } catch (error: unknown) {
      if (error instanceof ApiError && error.status === 403) {
        setDetail(null);
        setState("forbidden");
      } else {
        setState("error");
      }
      setMessage(apiErrorMessage(error, "审核任务加载失败，请重试"));
    }
  }, [currentReviewerID, reviewID, token]);

  const loadPreview = useCallback(async () => {
    const requestGeneration = ++previewGeneration.current;
    setPreviewState("loading");
    setPreviewMessage("");
    try {
      const access = await fetchReviewPreview(token, reviewID);
      if (previewGeneration.current !== requestGeneration) return;
      setPreview(access);
      setPreviewClockOffset(serverClockOffset(access.server_time));
      setPreviewState("ready");
    } catch (error: unknown) {
      if (previewGeneration.current !== requestGeneration) return;
      setPreview(null);
      setPreviewState("unavailable");
      if (error instanceof ApiError && error.status === 403) {
        setState("forbidden");
        setPreviewMessage("服务端拒绝了视频预览访问");
      } else {
        setPreviewMessage(apiErrorMessage(error, "视频预览暂时不可用"));
      }
    }
  }, [reviewID, token]);

  useEffect(() => {
    void load();
    return () => {
      previewGeneration.current++;
    };
  }, [load]);

  useEffect(() => {
    if (state !== "ready" || !detail) return;
    void loadPreview();
  }, [detail?.case.id, detailRevision, loadPreview, state]);

  useEffect(() => {
    if (!preview?.expires_at || previewState !== "ready") return;
    const refreshAt = new Date(preview.expires_at).getTime() -
      serverNow(previewClockOffset) - 30_000;
    const timer = window.setTimeout(() => void loadPreview(), boundedTimerDelay(refreshAt));
    return () => window.clearTimeout(timer);
  }, [loadPreview, preview, previewClockOffset, previewState]);

  useEffect(() => {
    if (!lease?.case.lease_expires_at) {
      setLeaseExpired(false);
      return;
    }
    const update = () => {
      const now = serverNow(leaseClockOffset);
      setClock(now);
      setLeaseExpired(now >= new Date(lease.case.lease_expires_at || "").getTime());
    };
    update();
    const timer = window.setInterval(update, 1_000);
    return () => window.clearInterval(timer);
  }, [lease, leaseClockOffset]);

  const handleActionError = useCallback((error: unknown, fallback: string) => {
    if (error instanceof ApiError && error.status === 403) {
      previewGeneration.current++;
      setPreview(null);
      setPreviewState("unavailable");
      setState("forbidden");
      setMessage("服务端拒绝了当前操作");
      return;
    }
    if (error instanceof ApiError && error.code === "REVIEW_LEASE_EXPIRED") {
      forgetReviewLease(reviewID);
      setLease(null);
      setLeaseExpired(true);
      setMessage("审核占用时间已结束。已保留证据，请返回任务列表重新开始。");
      return;
    }
    if (error instanceof ApiError &&
      (error.code === "REVIEW_CASE_VERSION_CONFLICT" ||
        error.code === "REVIEW_SUBJECT_VERSION_CONFLICT" ||
        error.code === "REVIEW_CASE_CLAIMED" ||
        error.code === "REVIEW_LEASE_NOT_OWNED" ||
        error.code === "REVIEW_CONFLICT")) {
      forgetReviewLease(reviewID);
      setLease(null);
      previewGeneration.current++;
      setPreview(null);
      setPreviewState("unavailable");
      setPreviewMessage("任务状态已变化，请刷新后重新加载预览");
      setVersionConflict(true);
      setMessage("审核任务状态已变化，请刷新后继续。");
      return;
    }
    setMessage(apiErrorMessage(error, fallback));
  }, [reviewID]);

  const claim = useCallback(async () => {
    if (!detail || leaseMutation.current) return;
    leaseMutation.current = true;
    setBusy("claim");
    setMessage("");
    try {
      const result = await claimReviewCase(token, reviewID, detail.case.version);
      const offset = serverClockOffset(result.server_time);
      rememberReviewLease(reviewID, result);
      setLease(result);
      setLeaseClockOffset(offset);
      setDetail({ ...detail, case: result.case });
      const expired = Boolean(
        result.case.lease_expires_at &&
        serverNow(offset) >= new Date(result.case.lease_expires_at).getTime()
      );
      setLeaseExpired(expired);
      if (expired) {
        setMessage("审核占用时间已结束。已保留证据，请返回任务列表重新开始。");
      }
    } catch (error: unknown) {
      handleActionError(error, "开始审核失败，请重试");
    } finally {
      leaseMutation.current = false;
      setBusy("");
    }
  }, [detail, handleActionError, reviewID, token]);

  const resume = useCallback(async () => {
    if (!detail || leaseMutation.current) return;
    leaseMutation.current = true;
    setBusy("resume");
    setMessage("");
    try {
      const result = await resumeReviewLease(token, reviewID, detail.case.version);
      rememberReviewLease(reviewID, result);
      setLease(result);
      setLeaseClockOffset(serverClockOffset(result.server_time));
      setDetail({ ...detail, case: result.case });
      setLeaseExpired(false);
      setMessage("已恢复正在审核的任务。");
    } catch (error: unknown) {
      handleActionError(error, "恢复审核失败，请刷新任务状态");
    } finally {
      leaseMutation.current = false;
      setBusy("");
    }
  }, [detail, handleActionError, reviewID, token]);

  useEffect(() => {
    if (!detail || !canDecide || lease || versionConflict ||
      detail.case.status !== "pending_human" ||
      detail.case.assigned_reviewer_id !== currentReviewerID) {
      return;
    }
    const key = `${detail.case.id}:${detail.case.version}`;
    if (resumeAttempt.current === key) return;
    resumeAttempt.current = key;
    void resume();
  }, [canDecide, currentReviewerID, detail, lease, resume, versionConflict]);

  const extend = useCallback(async (manual: boolean) => {
    if (!detail || !lease || leaseMutation.current) return;
    leaseMutation.current = true;
    setBusy(manual ? "renew" : "auto-renew");
    try {
      const result = await renewReviewLease(
        token, reviewID, lease.lease_token, detail.case.version
      );
      rememberReviewLease(reviewID, result);
      setLease(result);
      setLeaseClockOffset(serverClockOffset(result.server_time));
      setDetail({ ...detail, case: result.case });
      setLeaseExpired(false);
      if (manual) setMessage("审核时间已延长。");
    } catch (error: unknown) {
      handleActionError(error, "延长审核时间失败，请重试");
    } finally {
      leaseMutation.current = false;
      setBusy("");
    }
  }, [detail, handleActionError, lease, reviewID, token]);

  useEffect(() => {
    if (!lease?.case.lease_expires_at || leaseExpired || Boolean(busy) || leaseMutation.current) {
      return;
    }
    const remaining = new Date(lease.case.lease_expires_at).getTime() -
      serverNow(leaseClockOffset);
    if (remaining <= 0) return;
    const timer = window.setTimeout(
      () => void extend(false),
      boundedTimerDelay(Math.floor(remaining / 2))
    );
    return () => window.clearTimeout(timer);
  }, [busy, extend, lease, leaseClockOffset, leaseExpired]);

  const release = async () => {
    if (!detail || !lease || leaseMutation.current) return;
    leaseMutation.current = true;
    setBusy("release");
    setMessage("");
    try {
      const released = await releaseReviewLease(
        token, reviewID, lease.lease_token, detail.case.version
      );
      forgetReviewLease(reviewID);
      setLease(null);
      setDetail({ ...detail, case: released });
      setMessage("任务已放回待处理列表。");
    } catch (error: unknown) {
      handleActionError(error, "放回任务失败，请重试");
    } finally {
      leaseMutation.current = false;
      setBusy("");
    }
  };

  const decide = async () => {
    if (!detail || !lease || leaseExpired || leaseMutation.current) return;
    leaseMutation.current = true;
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
      forgetReviewLease(reviewID);
      setDetail({
        ...detail,
        case: result.case,
        history: {
          ...detail.history,
          human_decisions: [...detail.history.human_decisions, result.decision]
        }
      });
      setLease(null);
      setMessage(result.duplicate ? "审核结果已存在。" : "审核结果已提交，可在“最近完成”中查看。");
    } catch (error: unknown) {
      handleActionError(error, "提交审核结果失败，请重试");
    } finally {
      leaseMutation.current = false;
      setBusy("");
    }
  };

  const signals = detail?.history.signals || [];
  const history = useMemo(() => {
    if (!detail) return [];
    return [
      ...detail.history.automated_decisions.map((item) => ({
        id: `auto-${item.id}`, time: item.created_at,
        label: `自动审核：${outcomeLabel(item.outcome)} · policy v${item.policy_version}`
      })),
      ...detail.history.assignments.map((item) => ({
        id: `assignment-${item.id}`, time: item.created_at,
        label: `${assignmentLabel(item.event)} · 审核员 #${item.reviewer_id}`
      })),
      ...detail.history.human_decisions.map((item) => ({
        id: `human-${item.id}`, time: item.created_at,
        label: `人工审核：${outcomeLabel(item.outcome)} · ${reasonLabel(item.reason_code)}`
      }))
    ].sort((a, b) => new Date(a.time).getTime() - new Date(b.time).getTime());
  }, [detail]);

  if (state === "loading") return <DetailState title="正在加载审核任务…" />;
  if (state === "error") return <DetailState title={message} action="重试" onAction={() => void load()} />;
  if (state === "forbidden") return <DetailState title="服务端拒绝了审核任务访问" />;
  if (!detail) return <DetailState title="审核任务不存在" />;

  const assignedToCurrentReviewer = detail.case.assigned_reviewer_id === currentReviewerID;
  const assignedToAnotherReviewer = Boolean(
    detail.case.assigned_reviewer_id && !assignedToCurrentReviewer
  );
  const remainingMS = lease?.case.lease_expires_at
    ? new Date(lease.case.lease_expires_at).getTime() - clock
    : 0;

  return (
    <section className="admin-page">
      <header className="admin-page-header">
        <div>
          <button className="admin-back" type="button" onClick={() => navigate("/admin/reviews")}>
            ← 返回任务列表
          </button>
          <span className="admin-eyebrow">审核任务 #{detail.case.id}</span>
          <h1>{detail.subject.title || `视频 #${detail.subject.video_id}`}</h1>
          <p>作者 #{detail.subject.author_id} · 内容版本 {detail.case.review_version}</p>
        </div>
        <span className="admin-status-pill">{reviewStatusLabel(detail.case.status)}</span>
      </header>
      {message && (
        <div className={leaseExpired || versionConflict ? "admin-alert warning" : "admin-alert"}>
          {message}
          {versionConflict && <button type="button" onClick={() => void load()}>刷新任务</button>}
          {leaseExpired && (
            <button type="button" onClick={() => navigate("/admin/reviews")}>返回任务列表</button>
          )}
        </div>
      )}
      <div className="admin-detail-grid">
        <article className="admin-panel admin-subject">
          <h2>视频内容</h2>
          {previewState === "ready" && preview?.media_url
            ? <video controls poster={preview.cover_url} src={preview.media_url} />
            : previewState === "loading"
              ? <div className="admin-media-empty">正在加载受保护预览…</div>
              : preview?.cover_url
                ? <img src={preview.cover_url} alt="" />
                : <div className="admin-media-empty">{previewMessage || "视频预览不可用"}</div>}
          <h3>{detail.subject.title}</h3>
          <p>{detail.subject.description || "无简介"}</p>
          {previewState === "unavailable" && (
            <button className="subtle" type="button" onClick={() => void loadPreview()}>重新加载预览</button>
          )}
        </article>
        <div className="admin-detail-stack">
          <article className="admin-panel">
            <h2>机器审核证据</h2>
            {signals.length === 0 && <p className="admin-muted">没有机器审核证据。</p>}
            <div className="admin-signal-list">
              {signals.map((signal) => (
                <div key={signal.id}>
                  <strong>{labelName(signal.label)} <small>({signal.label})</small></strong>
                  <span>{Math.round(signal.confidence * 100)}%</span>
                  <small>
                    {signal.source_kind === "test_seed" ? "测试证据" : "来源未验证"}
                    {" · "}{signal.provider} · {signal.model_version} · policy v{signal.policy_version}
                  </small>
                  {signal.evidence_refs.map((reference) => <code key={reference}>{reference}</code>)}
                </div>
              ))}
            </div>
          </article>
          <article className="admin-panel">
            <h2>审核记录</h2>
            {history.length === 0 && <p className="admin-muted">暂无审核记录。</p>}
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
            {!lease && !assignedToAnotherReviewer && (
              <button type="button" disabled={Boolean(busy)} onClick={() => void (
                assignedToCurrentReviewer ? resume() : claim()
              )}>
                {busy === "resume" ? "恢复中…" : busy === "claim" ? "开始中…" :
                  assignedToCurrentReviewer ? "继续审核" : "开始审核"}
              </button>
            )}
            {assignedToAnotherReviewer && <span>该任务正在由其他审核员处理</span>}
            {lease && (
              <>
                <span>
                  审核占用至 {new Date(lease.case.lease_expires_at || "").toLocaleTimeString()}
                  {" · "}{remainingLabel(remainingMS)}
                </span>
                <button type="button" disabled={Boolean(busy) || leaseExpired} onClick={() => void extend(true)}>
                  {busy === "renew" ? "延长中…" : "延长审核时间"}
                </button>
                <button className="subtle" type="button" disabled={Boolean(busy)} onClick={() => void release()}>
                  {busy === "release" ? "放回中…" : "放回待处理"}
                </button>
              </>
            )}
          </div>
          {lease && (
            <>
              <p className="admin-muted">离开页面后任务会暂时保留；不再处理时请主动放回待处理列表。</p>
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
            </>
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

function assignmentLabel(event: string): string {
  switch (event) {
    case "claimed": return "开始审核";
    case "resumed": return "恢复审核";
    case "renewed": return "延长审核时间";
    case "released": return "放回待处理";
    case "expired": return "审核占用已到期";
    case "decided": return "提交审核结果";
    case "cancelled": return "任务已取消";
    case "superseded": return "内容版本已更新";
    default: return "审核任务状态变化";
  }
}

function labelName(label: string): string {
  switch (label) {
    case "sexual_content": return "色情内容";
    case "graphic_violence": return "血腥暴力";
    case "hate": return "仇恨内容";
    case "harassment": return "骚扰内容";
    case "self_harm": return "自残内容";
    case "illegal_activity": return "违法活动";
    case "spam": return "垃圾内容";
    case "safe": return "未发现风险";
    default: return "未知标签";
  }
}

function reasonLabel(reason: string): string {
  const labels: Record<string, string> = {
    content_compliant: "内容合规",
    false_positive: "机器误判",
    sexual_content: "色情内容",
    graphic_violence: "血腥暴力",
    hate: "仇恨内容",
    harassment: "骚扰内容",
    self_harm: "自残内容",
    illegal_activity: "违法活动",
    spam: "垃圾内容",
    other_policy_violation: "其他策略违规"
  };
  return labels[reason] || reason;
}

function outcomeLabel(outcome: string): string {
  if (outcome === "approve") return "通过";
  if (outcome === "reject") return "驳回";
  if (outcome === "human") return "转人工审核";
  return outcome;
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

function remainingLabel(milliseconds: number): string {
  if (milliseconds <= 0) return "时间已结束";
  const seconds = Math.ceil(milliseconds / 1000);
  const minutes = Math.floor(seconds / 60);
  const remainder = seconds % 60;
  return `剩余 ${minutes}:${String(remainder).padStart(2, "0")}`;
}

function boundedTimerDelay(milliseconds: number): number {
  return Math.min(2_147_000_000, Math.max(1_000, milliseconds));
}

function serverClockOffset(serverTime?: string): number {
  if (!serverTime) return 0;
  const parsed = new Date(serverTime).getTime();
  return Number.isFinite(parsed) ? parsed - Date.now() : 0;
}

function serverNow(offset: number): number {
  return Date.now() + offset;
}
