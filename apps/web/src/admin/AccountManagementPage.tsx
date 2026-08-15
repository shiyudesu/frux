import { useCallback, useEffect, useRef, useState } from "react";
import {
  fetchManagedAccount,
  freezeManagedAccount,
  revokeManagedAccountSessions,
  searchManagedAccounts,
  unfreezeManagedAccount
} from "../api/accountAdmin";
import { ApiError, apiErrorMessage } from "../api/client";
import { useNavigate } from "../router";
import type {
  ManageAccountRequest,
  ManagedAccount,
  ManagedAccountAction,
  ManagedAccountReason,
  ManagedAccountSearchFilters
} from "../types";
import { useAdminSession } from "./adminSession";

const emptyFilters: ManagedAccountSearchFilters = {
  query: "",
  user_id: "",
  status: ""
};

type AccountPageState = "loading" | "ready" | "empty" | "error" | "forbidden";

interface AccountActionDialog {
  account: ManagedAccount;
  action: ManagedAccountAction;
  idempotencyKey: string;
}

export function AccountManagementPage() {
  const { token } = useAdminSession();
  const navigate = useNavigate();
  const [draftFilters, setDraftFilters] = useState<ManagedAccountSearchFilters>(emptyFilters);
  const [appliedFilters, setAppliedFilters] = useState<ManagedAccountSearchFilters>(emptyFilters);
  const [items, setItems] = useState<ManagedAccount[]>([]);
  const [state, setState] = useState<AccountPageState>("loading");
  const [message, setMessage] = useState("");
  const [cursor, setCursor] = useState("");
  const [hasMore, setHasMore] = useState(false);
  const [loadingMore, setLoadingMore] = useState(false);
  const [selected, setSelected] = useState<ManagedAccount | null>(null);
  const [detailBusy, setDetailBusy] = useState(false);
  const [detailError, setDetailError] = useState("");
  const [dialog, setDialog] = useState<AccountActionDialog | null>(null);
  const [reasonCode, setReasonCode] = useState<ManagedAccountReason>("abuse");
  const [confirmed, setConfirmed] = useState(false);
  const [actionBusy, setActionBusy] = useState(false);
  const [actionError, setActionError] = useState("");
  const requestGeneration = useRef(0);

  const load = useCallback(async (
    filters: ManagedAccountSearchFilters,
    nextCursor = "",
    append = false
  ) => {
    const generation = ++requestGeneration.current;
    if (append) setLoadingMore(true);
    else setState("loading");
    setMessage("");
    try {
      const page = await searchManagedAccounts(token, filters, nextCursor, 20);
      if (generation !== requestGeneration.current) return;
      setItems((current) => append ? [...current, ...page.items] : page.items);
      setCursor(page.next_cursor);
      setHasMore(page.has_more);
      setState(append || page.items.length > 0 ? "ready" : "empty");
    } catch (error: unknown) {
      if (generation !== requestGeneration.current) return;
      if (error instanceof ApiError && error.status === 403) {
        setItems([]);
        setSelected(null);
        setState("forbidden");
      } else if (!append) {
        setState("error");
      }
      setMessage(apiErrorMessage(error, "账号查询失败，请重试"));
    } finally {
      if (generation === requestGeneration.current) setLoadingMore(false);
    }
  }, [token]);

  useEffect(() => {
    const generation = requestGeneration;
    void load(emptyFilters);
    return () => {
      generation.current++;
    };
  }, [load]);

  const openDetail = async (account: ManagedAccount) => {
    setSelected(account);
    setDetailBusy(true);
    setDetailError("");
    try {
      const detail = await fetchManagedAccount(token, account.id);
      setSelected(detail);
    } catch (error: unknown) {
      if (error instanceof ApiError && error.status === 403) {
        setItems([]);
        setSelected(null);
        setState("forbidden");
        return;
      }
      setDetailError(apiErrorMessage(error, "账号详情加载失败，请重试"));
    } finally {
      setDetailBusy(false);
    }
  };

  const openAction = (account: ManagedAccount, action: ManagedAccountAction) => {
    setDialog({
      account,
      action,
      idempotencyKey: createAccountOperationKey()
    });
    setReasonCode(defaultReason(action));
    setConfirmed(false);
    setActionError("");
  };

  const submitAction = async () => {
    if (!dialog || !confirmed) return;
    setActionBusy(true);
    setActionError("");
    const body: ManageAccountRequest = {
      expected_version: dialog.account.version,
      reason_code: reasonCode
    };
    try {
      const result = dialog.action === "freeze"
        ? await freezeManagedAccount(token, dialog.account.id, body, dialog.idempotencyKey)
        : dialog.action === "unfreeze"
          ? await unfreezeManagedAccount(token, dialog.account.id, body, dialog.idempotencyKey)
          : await revokeManagedAccountSessions(token, dialog.account.id, body, dialog.idempotencyKey);
      if (!result.audit_committed) {
        setActionError("服务端未确认审计提交，未报告成功。");
        return;
      }
      const updated: ManagedAccount = {
        ...dialog.account,
        status: result.status,
        status_name: result.status_name,
        version: result.version,
        active_session_count: dialog.action === "unfreeze"
          ? dialog.account.active_session_count
          : Math.max(0, dialog.account.active_session_count - result.revoked_session_count),
        updated_at: result.occurred_at
      };
      setItems((current) => current.map((account) => account.id === updated.id ? updated : account));
      setSelected((current) => current?.id === updated.id ? updated : current);
      setDialog(null);
      setMessage(actionSuccessMessage(dialog.action, result.replayed));
    } catch (error: unknown) {
      if (error instanceof ApiError && error.status === 403) {
        setItems([]);
        setSelected(null);
        setDialog(null);
        setState("forbidden");
        return;
      }
      if (error instanceof ApiError &&
        (error.code === "ADMIN_USER_ACCOUNT_VERSION_CONFLICT" ||
          error.code === "ADMIN_USER_ACCOUNT_STATE_CONFLICT")) {
        setActionError("账号版本或状态已变化，已重新加载最新信息。");
        await Promise.all([
          openDetail(dialog.account),
          load(appliedFilters)
        ]);
        return;
      }
      setActionError(apiErrorMessage(error, "账号操作失败，请重试"));
    } finally {
      setActionBusy(false);
    }
  };

  return (
    <section className="admin-page">
      <header className="admin-page-header">
        <div>
          <span className="admin-eyebrow">Consumer accounts</span>
          <h1>账号管理</h1>
          <p>仅管理普通用户账号；后台管理员、角色和权限不在此页面展示或修改。</p>
        </div>
      </header>
      <form className="admin-filter-grid admin-account-filters" onSubmit={(event) => {
        event.preventDefault();
        const next = { ...draftFilters };
        setAppliedFilters(next);
        setSelected(null);
        void load(next);
      }}>
        <label>
          账号或昵称
          <input
            maxLength={128}
            value={draftFilters.query}
            onChange={(event) => setDraftFilters({ ...draftFilters, query: event.target.value })}
          />
        </label>
        <label>
          用户 ID
          <input
            inputMode="numeric"
            value={draftFilters.user_id}
            onChange={(event) => setDraftFilters({ ...draftFilters, user_id: event.target.value })}
          />
        </label>
        <label>
          状态
          <select
            value={draftFilters.status}
            onChange={(event) => setDraftFilters({
              ...draftFilters,
              status: event.target.value as ManagedAccountSearchFilters["status"]
            })}
          >
            <option value="">全部</option>
            <option value="normal">正常</option>
            <option value="frozen">已冻结</option>
            <option value="cancelled">已注销</option>
          </select>
        </label>
        <button type="submit">查询</button>
      </form>
      {message && <div className="admin-alert">{message}</div>}
      {state === "loading" && <AccountState title="正在查询普通用户账号…" />}
      {state === "error" && (
        <AccountState title={message} action="重试" onAction={() => void load(appliedFilters)} />
      )}
      {state === "forbidden" && <AccountState title="服务端拒绝了账号管理访问" />}
      {state === "empty" && <AccountState title="没有符合条件的普通用户账号" />}
      {state === "ready" && (
        <>
          <div className="admin-table-wrap">
            <table className="admin-table">
              <thead>
                <tr>
                  <th>用户</th><th>状态</th><th>作品</th><th>登录会话</th>
                  <th>版本</th><th>注册时间</th><th>操作</th>
                </tr>
              </thead>
              <tbody>
                {items.map((account) => (
                  <tr key={account.id} className={selected?.id === account.id ? "selected" : ""}>
                    <td>
                      <strong>{account.nickname || `用户 #${account.id}`}</strong>
                      <small>{account.account} · #{account.id}</small>
                    </td>
                    <td>
                      <span className={`admin-status-pill ${account.status_name}`}>
                        {managedAccountStatusLabel(account.status_name)}
                      </span>
                    </td>
                    <td>{account.public_work_count} 公开 / {account.private_work_count} 私密</td>
                    <td>{account.active_session_count}</td>
                    <td>v{account.version}</td>
                    <td>{new Date(account.created_at).toLocaleString()}</td>
                    <td>
                      <button type="button" className="subtle" onClick={() => void openDetail(account)}>
                        查看
                      </button>
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
      {selected && (
        <section className="admin-panel admin-account-detail" aria-label="账号详情">
          <header>
            <div>
              <span className="admin-eyebrow">Account #{selected.id}</span>
              <h2>{selected.nickname || selected.account}</h2>
              <p>{selected.account} · 更新于 {new Date(selected.updated_at).toLocaleString()}</p>
            </div>
            <span className={`admin-status-pill ${selected.status_name}`}>
              {managedAccountStatusLabel(selected.status_name)}
            </span>
          </header>
          {detailBusy && <p className="admin-muted">正在刷新账号详情…</p>}
          {detailError && <p className="admin-inline-error">{detailError}</p>}
          <div className="admin-account-stat-grid">
            <AccountFact label="关注" value={selected.following_count} />
            <AccountFact label="粉丝" value={selected.follower_count} />
            <AccountFact label="公开作品" value={selected.public_work_count} />
            <AccountFact label="私密作品" value={selected.private_work_count} />
            <AccountFact label="获赞" value={selected.received_like_count} />
            <AccountFact label="活跃会话" value={selected.active_session_count} />
          </div>
          {selected.bio && <p className="admin-account-bio">{selected.bio}</p>}
          <div className="admin-account-actions">
            <button type="button" onClick={() => navigate("/admin/videos")}>查看视频运营</button>
            {selected.status_name === "normal" && (
              <button className="danger" type="button" onClick={() => openAction(selected, "freeze")}>
                冻结账号
              </button>
            )}
            {selected.status_name === "frozen" && (
              <button type="button" onClick={() => openAction(selected, "unfreeze")}>
                解冻账号
              </button>
            )}
            {selected.status_name !== "cancelled" && (
              <button className="subtle" type="button" onClick={() => openAction(selected, "revoke_sessions")}>
                强制退出全部设备
              </button>
            )}
          </div>
        </section>
      )}
      {dialog && (
        <div className="admin-dialog-backdrop" role="presentation">
          <div className="admin-dialog" role="dialog" aria-modal="true" aria-labelledby="account-action-title">
            <h2 id="account-action-title">{accountActionTitle(dialog.action)}</h2>
            <p>普通用户 #{dialog.account.id} · 当前版本 v{dialog.account.version}</p>
            <p className="admin-muted">{accountActionDescription(dialog.action)}</p>
            <label>
              原因
              <select
                value={reasonCode}
                onChange={(event) => setReasonCode(event.target.value as ManagedAccountReason)}
              >
                {reasonOptions(dialog.action).map((option) => (
                  <option key={option.value} value={option.value}>{option.label}</option>
                ))}
              </select>
            </label>
            <label className="admin-confirm-check">
              <input
                type="checkbox"
                checked={confirmed}
                onChange={(event) => setConfirmed(event.target.checked)}
              />
              我确认按当前版本执行并写入审计
            </label>
            {actionError && <p className="admin-inline-error">{actionError}</p>}
            <div className="admin-dialog-actions">
              <button type="button" disabled={actionBusy} onClick={() => setDialog(null)}>取消</button>
              <button
                className={dialog.action === "freeze" ? "danger" : ""}
                type="button"
                disabled={!confirmed || actionBusy}
                onClick={() => void submitAction()}
              >
                {actionBusy ? "提交中…" : accountActionSubmitLabel(dialog.action)}
              </button>
            </div>
          </div>
        </div>
      )}
    </section>
  );
}

function AccountState({
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

function AccountFact({ label, value }: { label: string; value: number }) {
  return <div><span>{label}</span><strong>{value}</strong></div>;
}

function managedAccountStatusLabel(status: ManagedAccount["status_name"]): string {
  if (status === "normal") return "正常";
  if (status === "frozen") return "已冻结";
  return "已注销";
}

function defaultReason(action: ManagedAccountAction): ManagedAccountReason {
  if (action === "freeze") return "abuse";
  if (action === "unfreeze") return "appeal_approved";
  return "security_response";
}

function reasonOptions(action: ManagedAccountAction): Array<{
  value: ManagedAccountReason;
  label: string;
}> {
  if (action === "freeze") {
    return [
      { value: "abuse", label: "滥用行为" },
      { value: "policy_violation", label: "违反平台规则" },
      { value: "security_risk", label: "账号安全风险" }
    ];
  }
  if (action === "unfreeze") {
    return [
      { value: "appeal_approved", label: "申诉通过" },
      { value: "issue_resolved", label: "问题已解决" },
      { value: "manual_correction", label: "人工纠正" }
    ];
  }
  return [
    { value: "security_response", label: "安全处置" },
    { value: "user_request", label: "用户请求" },
    { value: "operator_request", label: "运营处置" }
  ];
}

function accountActionTitle(action: ManagedAccountAction): string {
  if (action === "freeze") return "确认冻结账号";
  if (action === "unfreeze") return "确认解冻账号";
  return "确认强制退出";
}

function accountActionSubmitLabel(action: ManagedAccountAction): string {
  if (action === "freeze") return "确认冻结";
  if (action === "unfreeze") return "确认解冻";
  return "确认退出全部设备";
}

function accountActionDescription(action: ManagedAccountAction): string {
  if (action === "freeze") {
    return "冻结会立即阻止登录和刷新并撤销耐久会话；已签发的短期访问令牌最长仍可能有效约 5 分钟。现有作品不会自动下架。";
  }
  if (action === "unfreeze") {
    return "解冻不会恢复旧会话，用户需要重新使用密码登录。现有作品状态保持不变。";
  }
  return "所有耐久登录会话会被撤销；已签发的短期访问令牌最长仍可能有效约 5 分钟。";
}

function actionSuccessMessage(action: ManagedAccountAction, replayed: boolean): string {
  const suffix = replayed ? "（安全重放）" : "";
  if (action === "freeze") return `账号已冻结并完成审计${suffix}。`;
  if (action === "unfreeze") return `账号已解冻并完成审计${suffix}。`;
  return `账号登录会话已撤销并完成审计${suffix}。`;
}

function createAccountOperationKey(): string {
  if (typeof globalThis.crypto?.randomUUID === "function") {
    return globalThis.crypto.randomUUID();
  }
  return `account-${Date.now()}-${Math.random().toString(16).slice(2)}`;
}
