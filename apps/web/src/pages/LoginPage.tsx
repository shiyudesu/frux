import { useState } from "react";
import type { FormEvent } from "react";
import {
  fetchMyProfileWithAccessToken,
  login,
  logoutSession,
  registerUser
} from "../api/account";
import { ApiError, UserFacingError, apiErrorMessage } from "../api/client";
import { image } from "../constants";
import { validateSelectedPassword, PASSWORD_RULE_MESSAGE } from "../passwordPolicy";
import { useNavigate } from "../router";
import { useSession } from "../session";
import { BrandMark } from "../components/BrandMark";
import { Icon } from "../components/Icon";

type AuthMode = "login" | "register";

interface AuthForm {
  account: string;
  password: string;
  nickname: string;
}

export function LoginPage() {
  const session = useSession();
  const navigate = useNavigate();
  const [mode, setMode] = useState<AuthMode>("login");
  const [form, setForm] = useState<AuthForm>({ account: "", password: "", nickname: "" });
  const [message, setMessage] = useState("");
  const [submitting, setSubmitting] = useState(false);

  async function handleSubmit(event: FormEvent) {
    event.preventDefault();
    setSubmitting(true);
    setMessage("");
    try {
      if (mode === "register") {
        const passwordMessage = validateSelectedPassword(form.password);
        if (passwordMessage) throw new UserFacingError(passwordMessage);
        await registerUser({
          account: form.account.trim(),
          password: form.password,
          nickname: form.nickname.trim()
        });
      }
      await session.runCredentialMutation(async () => {
        const tokenResponse = await login({
          account: form.account.trim(),
          password: form.password
        });
        try {
          const accessToken = tokenResponse.access_token;
          const profile = await fetchMyProfileWithAccessToken(accessToken);
          session.setAuth(accessToken, profile, tokenResponse.expires_in_seconds);
        } catch (error) {
          session.beginLogout();
          try {
            await logoutSession();
            session.clearAuth();
            session.completeLogout();
          } catch {
            session.beginLogout();
          }
          throw error;
        }
      });
      navigate("/recommend");
    } catch (error) {
      const fallback = error instanceof ApiError && error.status >= 500
        ? "账号服务暂时不可用，请稍后重试"
        : mode === "register"
          ? "注册失败，请检查填写内容"
          : "登录失败，请检查账号与密码";
      setMessage(apiErrorMessage(error, fallback));
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <main className="auth-page" data-ui="auth-page">
      <section className="auth-visual" aria-label="Frux">
        <div className="auth-preview">
          <img src={image.stage} alt="" />
          <div className="auth-preview-card">
            <span className="auth-preview-icon"><Icon name="play" /></span>
            <div>
              <strong>FRUX</strong>
              <span>沉浸式桌面短视频</span>
            </div>
          </div>
        </div>
      </section>
      <section className="auth-panel">
        <div className="auth-card" data-ui="auth-dialog">
          <div className="brand-block">
            <BrandMark />
            <div>
              <h1>登录 Frux</h1>
              <p>登录后继续刷视频、互动和发布作品。</p>
            </div>
          </div>

          <form className="auth-form" onSubmit={handleSubmit}>
            <div className="auth-mode-tabs">
              <button className={mode === "login" ? "active" : ""} type="button" onClick={() => setMode("login")}>
                登录
              </button>
              <button className={mode === "register" ? "active" : ""} type="button" onClick={() => setMode("register")}>
                注册
              </button>
            </div>
            <label>
              <span>账号</span>
              <input
                value={form.account}
                onChange={(event) => setForm({ ...form, account: event.target.value })}
                placeholder="请输入账号"
                autoComplete="username"
              />
            </label>
            {mode === "register" && (
              <label>
                <span>昵称</span>
                <input
                  value={form.nickname}
                  onChange={(event) => setForm({ ...form, nickname: event.target.value })}
                  placeholder="输入昵称"
                  autoComplete="nickname"
                />
              </label>
            )}
            <label>
              <span>密码</span>
              <input
                value={form.password}
                onChange={(event) => setForm({ ...form, password: event.target.value })}
                placeholder="输入密码"
                type="password"
                autoComplete={mode === "register" ? "new-password" : "current-password"}
              />
            </label>
            {mode === "register" && <p className="auth-input-hint">{PASSWORD_RULE_MESSAGE}</p>}
            {message && <p className="form-message">{message}</p>}
            <button className="primary-button" disabled={submitting}>
              <Icon name="login" size={18} />
              {submitting ? "提交中" : mode === "register" ? "注册并登录" : "登录"}
            </button>
          </form>
        </div>
      </section>
    </main>
  );
}
