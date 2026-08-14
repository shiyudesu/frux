// @vitest-environment jsdom
import { act, useEffect } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { changeMyPassword } from "../api/account";
import { loadFollowingMap } from "../api/social";
import { ApiError } from "../api/client";
import { emptyProfile, TOKEN_KEY } from "../constants";
import { RouterProvider } from "../router";
import { SessionProvider, useSession } from "../session";
import { PasswordChangeDialog } from "../components/PasswordChangeDialog";
import { ProfilePage } from "./ProfilePage";

vi.mock("../api/account", async (importOriginal) => ({
  ...(await importOriginal<typeof import("../api/account")>()),
  changeMyPassword: vi.fn(),
  fetchMyProfile: vi.fn().mockResolvedValue({
    id: 7,
    account: "owner",
    nickname: "Owner",
    avatar_url: "",
    bio: "",
    role: "",
    status: 0,
    following_count: 0,
    follower_count: 0,
    work_count: 0,
    gender: 0,
    public_work_count: 0,
    private_work_count: 0,
    received_like_count: 0
  }),
  updateMyProfile: vi.fn()
}));

vi.mock("../api/social", () => ({
  fetchRelationList: vi.fn(),
  followUser: vi.fn(),
  loadFollowingMap: vi.fn().mockResolvedValue({})
}));

vi.mock("../hooks/useCreatorContent", () => ({
  useCreatorContent: () => ({
    videos: {
      published: { items: [], state: "ready", error: "", hasMore: false },
      private: { items: [], state: "ready", error: "", hasMore: false }
    },
    ensureTab: () => {},
    loadVideos: async () => {},
    runBatchAction: async () => {}
  })
}));

vi.mock("../hooks/useProfileLibrary", () => ({
  useProfileLibrary: () => ({
    tabs: {
      likes: { items: [], nextCursor: "", hasMore: false, state: "ready", error: "" },
      favorites: { items: [], nextCursor: "", hasMore: false, state: "ready", error: "" },
      history: { items: [], nextCursor: "", hasMore: false, state: "ready", error: "" },
      watchLater: { items: [], nextCursor: "", hasMore: false, state: "ready", error: "" }
    },
    ensureTab: () => {},
    loadTab: async () => {},
    removeHistory: async () => true,
    removeWatchLater: async () => true,
    clearHistory: async () => {},
    patchVideo: () => {},
    applyVideoAction: () => {},
    addWatchLater: async () => true
  })
}));

describe("profile account security", () => {
  let container: HTMLDivElement;
  let root: Root;

  beforeEach(() => {
    (globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
    localStorage.clear();
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(JSON.stringify({
      code: "AUTH_INVALID_REFRESH_SESSION",
      error: "invalid refresh session"
    }), {
      status: 401,
      headers: { "Content-Type": "application/json" }
    })));
    window.history.replaceState({}, "", "/profile");
    container = document.createElement("div");
    document.body.appendChild(container);
    root = createRoot(container);
    vi.mocked(changeMyPassword).mockReset();
    vi.mocked(loadFollowingMap).mockResolvedValue({});
  });

  afterEach(async () => {
    await act(async () => root.unmount());
    container.remove();
    vi.restoreAllMocks();
  });

  it("opens a dedicated accessible password change dialog from the own-profile entry", async () => {
    await renderProfile();

    await clickAsync(buttonByText("账号安全"));

    expect(required('[role="dialog"]')).toBeTruthy();
    expect(required<HTMLInputElement>('input[autocomplete="current-password"]')).toBeTruthy();
    expect(container.querySelectorAll('input[autocomplete="new-password"]')).toHaveLength(2);
    expect(container.textContent).toContain("密码至少需要 8 个字符，且 UTF-8 编码不能超过 72 字节");
  });

  it("keeps the current session on a current-password error", async () => {
    vi.mocked(changeMyPassword).mockRejectedValueOnce(
      new ApiError("current password invalid", 400, "AUTH_CURRENT_PASSWORD_INVALID")
    );
    await renderDialog();
    fillPasswordForm("WrongPass123!", "NextPassword123!", "NextPassword123!");

    await clickAsync(buttonByText("确认修改"));

    expect(required('[data-testid="session-token"]').textContent).toBe("profile-token");
    expect(container.textContent).toContain("当前密码不正确，请重新输入");
  });

  it("adopts the replacement credential in memory after a successful password change", async () => {
    vi.mocked(changeMyPassword).mockResolvedValueOnce({
      access_token: "rotated-token",
      token_type: "Bearer",
      expires_in_seconds: 300
    });
    await renderDialog();
    fillPasswordForm("OldPass123!", "NextPassword123!", "NextPassword123!");

    await clickAsync(buttonByText("确认修改"));
    await waitForToken(container, "rotated-token");

    expect(changeMyPassword).toHaveBeenCalledWith({
      current_password: "OldPass123!",
      new_password: "NextPassword123!"
    }, "profile-token");
    expect(required('[data-testid="session-token"]').textContent).toBe("rotated-token");
    expect(localStorage.getItem(TOKEN_KEY)).toBeNull();
    window.history.replaceState({}, "", "/uploads");
    expect(document.cookie).toContain("frux_asset_active=1");
    expect(container.textContent).toContain("密码已更新，当前设备将继续保持登录");
  });

  async function renderProfile() {
    await act(async () => {
      root.render(
        <RouterProvider>
          <SessionProvider>
            <AuthenticatedSessionGate>
              <SessionTokenProbe />
              <ProfilePage />
            </AuthenticatedSessionGate>
          </SessionProvider>
        </RouterProvider>
      );
      for (let index = 0; index < 5; index += 1) {
        await Promise.resolve();
      }
    });
  }

  async function renderDialog() {
    await act(async () => {
      root.render(
        <RouterProvider>
          <SessionProvider>
            <AuthenticatedSessionGate>
              <SessionTokenProbe />
              <PasswordChangeDialog onClose={() => {}} />
            </AuthenticatedSessionGate>
          </SessionProvider>
        </RouterProvider>
      );
      for (let index = 0; index < 5; index += 1) {
        await Promise.resolve();
      }
    });
  }

  function required<T extends Element>(selector: string): T {
    const element = container.querySelector<T>(selector);
    if (!element) throw new Error(`missing element: ${selector}`);
    return element;
  }

  function buttonByText(text: string): HTMLButtonElement {
    const element = [...container.querySelectorAll("button")].find((button) => button.textContent?.trim() === text);
    if (!(element instanceof HTMLButtonElement)) throw new Error(`missing button: ${text}`);
    return element;
  }

  function fillPasswordForm(current: string, next: string, confirm: string) {
    const inputs = [
      required<HTMLInputElement>('input[autocomplete="current-password"]'),
      ...container.querySelectorAll<HTMLInputElement>('input[autocomplete="new-password"]')
    ];
    setInputValue(inputs[0]!, current);
    setInputValue(inputs[1]!, next);
    setInputValue(inputs[2]!, confirm);
  }

});

function AuthenticatedSessionGate({ children }: { children: React.ReactNode }) {
  const session = useSession();
  useEffect(() => {
    if (!(session.token && session.user)) {
      session.setAuth("profile-token", { ...emptyProfile, id: 7, account: "owner", nickname: "Owner" }, 300);
    }
  }, [session]);
  if (!(session.token && session.user)) return null;
  return <>{children}</>;
}

function SessionTokenProbe() {
  const session = useSession();
  return <output data-testid="session-token">{session.token}</output>;
}

async function clickAsync(element: HTMLElement) {
  await act(async () => {
    element.dispatchEvent(new MouseEvent("click", { bubbles: true }));
    await Promise.resolve();
  });
}

async function waitForToken(container: HTMLDivElement, token: string) {
  for (let index = 0; index < 20; index += 1) {
    if (container.querySelector('[data-testid="session-token"]')?.textContent === token) return;
    await act(async () => {
      await Promise.resolve();
    });
  }
}

function setInputValue(input: HTMLInputElement, value: string) {
  act(() => {
    const setter = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, "value")?.set;
    setter?.call(input, value);
    input.dispatchEvent(new Event("input", { bubbles: true }));
  });
}
