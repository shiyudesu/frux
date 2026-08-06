import { useEffect, useState } from "react";
import type { FormEvent } from "react";
import { ApiError, apiErrorMessage } from "../api/client";
import { useAdminLoginRoute, useNavigate } from "../router";
import { useAdminSession } from "./adminSession";

export function AdminLoginPage() {
  const navigate = useNavigate();
  const loginRoute = useAdminLoginRoute();
  const { login, state } = useAdminSession();
  const [account, setAccount] = useState("");
  const [password, setPassword] = useState("");
  const [message, setMessage] = useState("");

  useEffect(() => {
    if (state === "ready") {
      navigate(loginRoute?.returnTo || "/admin/reviews");
    }
  }, [loginRoute, navigate, state]);

  const submit = async (event: FormEvent) => {
    event.preventDefault();
    setMessage("");
    try {
      await login(account, password);
      navigate(loginRoute?.returnTo || "/admin/reviews");
    } catch (error: unknown) {
      const fallback = error instanceof ApiError && error.status === 401
        ? "管理员账号或密码错误"
        : "后台登录暂时不可用，请稍后重试";
      setMessage(apiErrorMessage(error, fallback));
    }
  };

  return (
    <main className="admin-login-page">
      <section className="admin-login-card">
        <div>
          <span className="admin-eyebrow">Frux Operations</span>
          <h1>登录运营后台</h1>
          <p>仅限已配置后台权限的账号，不提供自助注册。</p>
        </div>
        <form onSubmit={(event) => void submit(event)}>
          <label>
            管理员账号
            <input
              autoComplete="username"
              value={account}
              onChange={(event) => setAccount(event.target.value)}
              placeholder="请输入管理员账号"
            />
          </label>
          <label>
            密码
            <input
              type="password"
              autoComplete="current-password"
              value={password}
              onChange={(event) => setPassword(event.target.value)}
              placeholder="请输入密码"
            />
          </label>
          {message && <p className="admin-inline-error">{message}</p>}
          <button type="submit" disabled={state === "loading"}>
            {state === "loading" ? "登录中…" : "登录后台"}
          </button>
        </form>
      </section>
    </main>
  );
}
