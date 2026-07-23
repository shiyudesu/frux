// 登录/注册页。迁移后通过 useSession/useNavigate 自取会话与导航。
import { useState } from "react";
import type { FormEvent } from "react";
import { fetchMyProfile, login, registerUser } from "../api/account";
import { apiErrorMessage } from "../api/client";
import { image } from "../constants";
import { useNavigate } from "../router";
import { useSession } from "../session";

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
        await registerUser({
          account: form.account.trim(),
          password: form.password,
          nickname: form.nickname.trim()
        });
      }
      const tokenResponse = await login({
        account: form.account.trim(),
        password: form.password
      });
      const accessToken = tokenResponse.access_token;
      const profile = await fetchMyProfile(accessToken);
      session.setAuth(accessToken, profile);
      navigate("/recommend");
    } catch (error) {
      setMessage(apiErrorMessage(error, "登录失败，请检查账号与密码"));
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <main className="auth-page">
      <section className="auth-visual" aria-label="GCFeed">
        <div className="auth-preview">
          <img src={image.stage} alt="" />
          <div className="auth-preview-card">
            <span className="material-symbols-outlined">play_arrow</span>
            <div>
              <strong>GCFeed</strong>
              <span>16:9 桌面 Feed</span>
            </div>
          </div>
        </div>
      </section>
      <section className="auth-panel">
        <div className="auth-card">
          <div className="brand-block">
            <span className="brand-mark">GC</span>
            <div>
              <h1>登录 GCFeed</h1>
              <p>连接后端账号、Feed 和个人资料。</p>
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
                autoComplete="current-password"
              />
            </label>
            {message && <p className="form-message">{message}</p>}
            <button className="primary-button" disabled={submitting}>
              <span className="material-symbols-outlined">login</span>
              {submitting ? "提交中" : mode === "register" ? "注册并登录" : "登录"}
            </button>
          </form>
        </div>
      </section>
    </main>
  );
}
