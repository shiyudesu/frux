// @vitest-environment jsdom
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { fetchMyProfile, login, registerUser } from "../api/account";
import { ApiError, NetworkError } from "../api/client";
import { RouterProvider } from "../router";
import { SessionProvider } from "../session";
import { LoginPage } from "./LoginPage";

vi.mock("../api/account", () => ({
  fetchMyProfile: vi.fn(),
  login: vi.fn(),
  registerUser: vi.fn()
}));

describe("login and registration errors", () => {
  let container: HTMLDivElement;
  let root: Root;

  beforeEach(() => {
    (globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
    localStorage.clear();
    window.history.replaceState({}, "", "/auth");
    container = document.createElement("div");
    document.body.appendChild(container);
    root = createRoot(container);
    vi.mocked(fetchMyProfile).mockReset();
    vi.mocked(login).mockReset();
    vi.mocked(registerUser).mockReset();
    act(() => {
      root.render(
        <RouterProvider>
          <SessionProvider>
            <LoginPage />
          </SessionProvider>
        </RouterProvider>
      );
    });
  });

  afterEach(() => {
    act(() => root.unmount());
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

    await submit();

    expect(message()).toBe("账号服务暂时不可用，请稍后重试");
  });

  it("does not display an uncoded raw backend message", async () => {
    vi.mocked(login).mockRejectedValueOnce(new ApiError("敏感后端详情", 400));

    await submit();

    expect(message()).toBe("登录失败，请检查账号与密码");
  });

  async function submit() {
    await act(async () => {
      form().dispatchEvent(new Event("submit", { bubbles: true, cancelable: true }));
      await Promise.resolve();
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
});
