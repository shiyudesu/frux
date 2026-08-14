// @vitest-environment jsdom
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  fetchMyProfileWithAccessToken,
  login,
  logoutSession,
  registerUser
} from "../api/account";
import { ApiError, NetworkError } from "../api/client";
import { RouterProvider } from "../router";
import { SessionProvider } from "../session";
import { LoginPage } from "./LoginPage";

vi.mock("../api/account", () => ({
  fetchMyProfileWithAccessToken: vi.fn(),
  login: vi.fn(),
  logoutSession: vi.fn(),
  registerUser: vi.fn()
}));

describe("login and registration errors", () => {
  let container: HTMLDivElement;
  let root: Root;

  beforeEach(async () => {
    (globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
    localStorage.clear();
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(JSON.stringify({
      code: "AUTH_INVALID_REFRESH_SESSION",
      error: "invalid refresh session"
    }), {
      status: 401,
      headers: { "Content-Type": "application/json" }
    })));
    window.history.replaceState({}, "", "/auth");
    container = document.createElement("div");
    document.body.appendChild(container);
    root = createRoot(container);
    vi.mocked(fetchMyProfileWithAccessToken).mockReset();
    vi.mocked(logoutSession).mockReset();
    vi.mocked(login).mockReset();
    vi.mocked(registerUser).mockReset();
    await act(async () => {
      root.render(
        <RouterProvider>
          <SessionProvider>
            <LoginPage />
          </SessionProvider>
        </RouterProvider>
      );
      await flushPromises();
    });
  });

  afterEach(async () => {
    await act(async () => root.unmount());
    container.remove();
    localStorage.clear();
    vi.restoreAllMocks();
  });

  it.each(["unknown account", "wrong password"])(
    "shows the same safe credential message for %s",
    async () => {
      vi.mocked(login).mockRejectedValueOnce(
        new ApiError("invalid credentials", 401, "AUTH_INVALID_CREDENTIALS")
      );

      await submit();

      expect(message()).toBe("账号或密码错误，请重新输入");
    }
  );

  it("shows a distinct duplicate-account message", async () => {
    vi.mocked(registerUser).mockRejectedValueOnce(
      new ApiError("account already exists", 409, "ACCOUNT_ALREADY_EXISTS")
    );
    click(button("注册"));
    setInputValue(required<HTMLInputElement>('input[autocomplete="username"]'), "new-user");
    setInputValue(required<HTMLInputElement>('input[autocomplete="nickname"]'), "新用户");
    setInputValue(required<HTMLInputElement>('input[autocomplete="new-password"]'), "Password123!");

    await submit();

    expect(message()).toBe("该账号已注册，请直接登录或更换账号");
  });

  it("shows friendly validation, network, and internal messages", async () => {
    vi.mocked(login)
      .mockRejectedValueOnce(new ApiError("account is required", 400, "ACCOUNT_VALIDATION_FAILED"))
      .mockRejectedValueOnce(new NetworkError())
      .mockRejectedValueOnce(new ApiError("internal server error", 500, "INTERNAL_ERROR"))
      .mockRejectedValueOnce(new ApiError("legacy internal server error", 500));

    await submit();
    expect(message()).toBe("账号信息填写有误，请检查后重试");
    await submit();
    expect(message()).toBe("网络连接失败，请检查网络后重试");
    await submit();
    expect(message()).toBe("服务暂时不可用，请稍后重试");
    await submit();
    expect(message()).toBe("账号服务暂时不可用，请稍后重试");
  });

  it("uses a neutral service message for legacy registration failures", async () => {
    vi.mocked(registerUser).mockRejectedValueOnce(new ApiError("legacy internal server error", 500));
    click(button("注册"));
    setInputValue(required<HTMLInputElement>('input[autocomplete="username"]'), "new-user");
    setInputValue(required<HTMLInputElement>('input[autocomplete="nickname"]'), "新用户");
    setInputValue(required<HTMLInputElement>('input[autocomplete="new-password"]'), "Password123!");

    await submit();

    expect(message()).toBe("账号服务暂时不可用，请稍后重试");
  });

  it("does not display an uncoded raw backend message", async () => {
    vi.mocked(login).mockRejectedValueOnce(new ApiError("敏感后端详情", 400));

    await submit();

    expect(message()).toBe("登录失败，请检查账号与密码");
  });

  it("revokes a newly created session when profile hydration fails", async () => {
    vi.mocked(login).mockResolvedValueOnce({
      access_token: "new-token",
      token_type: "Bearer",
      expires_in_seconds: 300
    });
    vi.mocked(fetchMyProfileWithAccessToken).mockRejectedValueOnce(
      new NetworkError()
    );
    vi.mocked(logoutSession).mockResolvedValueOnce();
    setInputValue(required<HTMLInputElement>('input[autocomplete="username"]'), "owner");
    setInputValue(required<HTMLInputElement>('input[autocomplete="current-password"]'), "Password123!");

    await submit();

    expect(logoutSession).toHaveBeenCalledTimes(1);
    expect(message()).toBe("网络连接失败，请检查网络后重试");
  });

  it("uses the shared password rule and correct autocomplete for registration", async () => {
    click(button("注册"));
    const password = required<HTMLInputElement>('input[autocomplete="new-password"]');
    expect(password).toBeTruthy();
    expect(container.textContent).toContain("密码至少需要 8 个字符，且 UTF-8 编码不能超过 72 字节");

    setInputValue(required<HTMLInputElement>('input[autocomplete="username"]'), "new-user");
    setInputValue(required<HTMLInputElement>('input[autocomplete="nickname"]'), "新用户");
    setInputValue(password, "short");
    await submit();

    expect(registerUser).not.toHaveBeenCalled();
    expect(message()).toBe("密码至少需要 8 个字符，且 UTF-8 编码不能超过 72 字节");
  });

  async function submit() {
    await act(async () => {
      form().dispatchEvent(new Event("submit", { bubbles: true, cancelable: true }));
      await flushPromises();
    });
  }

  function message(): string {
    return container.querySelector(".form-message")?.textContent || "";
  }

  function form(): HTMLFormElement {
    const element = container.querySelector("form");
    if (!(element instanceof HTMLFormElement)) throw new Error("missing login form");
    return element;
  }

  function button(text: string): HTMLButtonElement {
    const element = [...container.querySelectorAll("button")]
      .find((candidate) => candidate.textContent?.trim() === text);
    if (!(element instanceof HTMLButtonElement)) throw new Error(`missing button: ${text}`);
    return element;
  }

  function click(element: HTMLElement) {
    act(() => element.dispatchEvent(new MouseEvent("click", { bubbles: true })));
  }

  function required<T extends Element>(selector: string): T {
    const element = container.querySelector<T>(selector);
    if (!element) throw new Error(`missing element: ${selector}`);
    return element;
  }
});

async function flushPromises() {
  await Promise.resolve();
  await Promise.resolve();
}

function setInputValue(input: HTMLInputElement, value: string) {
  act(() => {
    const setter = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, "value")?.set;
    setter?.call(input, value);
    input.dispatchEvent(new Event("input", { bubbles: true }));
  });
}
