// @vitest-environment jsdom
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { fetchAdminPrincipal, loginAdmin } from "../api/admin";
import {
  fetchManagedAccount,
  freezeManagedAccount,
  revokeManagedAccountSessions,
  searchManagedAccounts,
  unfreezeManagedAccount
} from "../api/accountAdmin";
import { ADMIN_AUTH_INVALID_EVENT, ApiError } from "../api/client";
import { fetchUnreadStat } from "../api/messages";
import {
  claimReviewCase,
  decideReviewCase,
  fetchReviewCase,
  fetchReviewPreview,
  fetchReviewQueue,
  releaseReviewLease,
  renewReviewLease,
  resumeReviewLease
} from "../api/review";
import {
  restoreAdminVideo,
  searchAdminVideos,
  takeDownAdminVideo
} from "../api/videoAdmin";
import { emptyProfile, USER_KEY } from "../constants";
import { RouterProvider } from "../router";
import { SessionProvider } from "../session";
import type { ReviewCaseDetail } from "../types";
import AdminApp from "./AdminApp";
import { ADMIN_SESSION_KEY } from "./adminSession";
import { forgetReviewLease } from "./reviewLeaseMemory";
import { defaultAdminVideoFilters } from "./VideoOperationsPage";

vi.mock("../api/admin", () => ({ fetchAdminPrincipal: vi.fn(), loginAdmin: vi.fn() }));
vi.mock("../api/accountAdmin", async (importOriginal) => ({
  ...(await importOriginal<typeof import("../api/accountAdmin")>()),
  fetchManagedAccount: vi.fn(),
  freezeManagedAccount: vi.fn(),
  revokeManagedAccountSessions: vi.fn(),
  searchManagedAccounts: vi.fn(),
  unfreezeManagedAccount: vi.fn()
}));
vi.mock("../api/messages", () => ({ fetchUnreadStat: vi.fn() }));
vi.mock("../api/review", () => ({
  claimReviewCase: vi.fn(),
  decideReviewCase: vi.fn(),
  fetchReviewCase: vi.fn(),
  fetchReviewPreview: vi.fn(),
  fetchReviewQueue: vi.fn(),
  renewReviewLease: vi.fn(),
  releaseReviewLease: vi.fn(),
  resumeReviewLease: vi.fn()
}));
vi.mock("../api/videoAdmin", async (importOriginal) => ({
  ...(await importOriginal<typeof import("../api/videoAdmin")>()),
  restoreAdminVideo: vi.fn(),
  searchAdminVideos: vi.fn(),
  takeDownAdminVideo: vi.fn()
}));

describe("admin content operations workspace", () => {
  let container: HTMLDivElement;
  let root: Root;

  beforeEach(() => {
    (globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
    container = document.createElement("div");
    document.body.appendChild(container);
    root = createRoot(container);
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(JSON.stringify({
      code: "AUTH_INVALID_REFRESH_SESSION",
      error: "invalid refresh session"
    }), {
      status: 401,
      headers: { "Content-Type": "application/json" }
    })));
    localStorage.setItem("consumer-marker", "1");
    localStorage.setItem(USER_KEY, JSON.stringify({
      ...emptyProfile, id: 7, nickname: "Reviewer", role: "reviewer"
    }));
    sessionStorage.setItem(ADMIN_SESSION_KEY, JSON.stringify({
      version: 1,
      token: "admin-token",
      principal: {
        user_id: 7,
        role: "reviewer",
        permissions: ["review.read", "review.decide"]
      },
      expires_at: Date.now() + 30 * 60 * 1000
    }));
    vi.mocked(fetchUnreadStat).mockResolvedValue({ unread_count: 0 });
    vi.mocked(fetchReviewQueue).mockResolvedValue({
      items: [], next_cursor: "", has_more: false, scope: "available"
    });
    vi.mocked(fetchReviewPreview).mockResolvedValue({
      media_url: "https://preview.example.test/video.mp4",
      cover_url: "https://preview.example.test/cover.jpg",
      expires_at: "2099-01-01T00:00:00Z"
    });
    vi.mocked(searchManagedAccounts).mockResolvedValue({
      items: [], next_cursor: "", has_more: false
    });
  });

  afterEach(() => {
    act(() => root.unmount());
    container.remove();
    localStorage.clear();
    sessionStorage.clear();
    forgetReviewLease(1);
    forgetReviewLease(2);
    forgetReviewLease(3);
    vi.clearAllMocks();
  });

  it("filters shell navigation to the server-confirmed permissions", async () => {
    vi.mocked(fetchAdminPrincipal).mockResolvedValue({
      user_id: 7, role: "reviewer", permissions: ["review.read", "review.decide"]
    });
    window.history.replaceState({}, "", "/admin/reviews");
    await renderAdmin();
    expect(container.textContent).toContain("审核任务");
    expect(container.textContent).not.toContain("视频运营");
  });

  it("routes direct admin navigation to the dedicated login without reusing consumer auth", async () => {
      sessionStorage.clear();
      window.history.replaceState({}, "", "/admin/reviews");
      await renderAdmin();
      expect(window.location.pathname).toBe("/admin/login");
      expect(new URLSearchParams(window.location.search).get("return")).toBe("/admin/reviews");
      expect(container.textContent).toContain("登录运营后台");
      expect([...container.querySelectorAll("button")]
        .some((item) => item.textContent?.trim() === "注册")).toBe(false);
      expect(loginAdmin).not.toHaveBeenCalled();
  });

  it("discards malformed persisted admin state", async () => {
      sessionStorage.setItem(ADMIN_SESSION_KEY, '{"version":1,"token":42}');
      window.history.replaceState({}, "", "/admin/videos");
      await renderAdmin();
      expect(window.location.pathname).toBe("/admin/login");
      expect(sessionStorage.getItem(ADMIN_SESSION_KEY)).toBeNull();
  });

  it("keeps consumer and admin sessions isolated through login and logout", async () => {
      sessionStorage.clear();
      vi.mocked(loginAdmin).mockResolvedValue({
        access_token: "dedicated-admin-token",
        token_type: "Bearer",
        expires_in_seconds: 1800,
        principal: {
          user_id: 7,
          role: "reviewer",
          permissions: ["review.read", "review.decide"]
        }
      });
      window.history.replaceState({}, "", "/admin/login?return=%2Fadmin%2Freviews");
      await renderAdmin();
      const account = required<HTMLInputElement>('input[autocomplete="username"]');
        const password = required<HTMLInputElement>('input[autocomplete="current-password"]');
        await act(async () => {
        setInputValue(account, "reviewer");
        setInputValue(password, "Password123!");
        await flush();
      });
      await clickButton("登录后台");
      expect(loginAdmin).toHaveBeenCalledWith("reviewer", "Password123!");
      expect(window.location.pathname).toBe("/admin/reviews");
      expect(localStorage.getItem("consumer-marker")).toBe("1");
      expect(sessionStorage.getItem(ADMIN_SESSION_KEY)).toContain("dedicated-admin-token");
      await clickButton("退出后台");
      expect(window.location.pathname).toBe("/admin/login");
      expect(sessionStorage.getItem(ADMIN_SESSION_KEY)).toBeNull();
      expect(localStorage.getItem("consumer-marker")).toBe("1");
  });

  it("clears only the admin session after an authoritative 401 event", async () => {
      vi.mocked(fetchAdminPrincipal).mockResolvedValue({
        user_id: 7, role: "reviewer", permissions: ["review.read", "review.decide"]
      });
      window.history.replaceState({}, "", "/admin/reviews");
      await renderAdmin();
      await act(async () => {
        window.dispatchEvent(new CustomEvent(ADMIN_AUTH_INVALID_EVENT, {
          detail: { token: "old-admin-token" }
        }));
        await flush();
      });
      expect(window.location.pathname).toBe("/admin/reviews");
      expect(sessionStorage.getItem(ADMIN_SESSION_KEY)).toContain("admin-token");
      await act(async () => {
        window.dispatchEvent(new CustomEvent(ADMIN_AUTH_INVALID_EVENT, {
          detail: { token: "admin-token" }
        }));
        await flush();
      });
      expect(window.location.pathname).toBe("/admin/login");
      expect(sessionStorage.getItem(ADMIN_SESSION_KEY)).toBeNull();
      expect(localStorage.getItem("consumer-marker")).toBe("1");
  });

  it("renders authorization service failure without exposing admin data", async () => {
      vi.mocked(fetchAdminPrincipal).mockRejectedValue(
        new ApiError("unavailable", 503, "ADMIN_AUTHORIZATION_UNAVAILABLE")
      );
      window.history.replaceState({}, "", "/admin/reviews");
      await renderAdmin();
      expect(container.textContent).toContain("后台会话暂时无法验证");
      expect(container.querySelector("table")).toBeNull();
  });

  it("preserves evidence and disables decisions after lease expiry", async () => {
    vi.mocked(fetchAdminPrincipal).mockResolvedValue({
      user_id: 7, role: "reviewer", permissions: ["review.read", "review.decide"]
    });
    vi.mocked(fetchReviewCase).mockResolvedValue(reviewDetail());
    vi.mocked(claimReviewCase).mockResolvedValue({
      case: { ...reviewDetail().case, version: 2, assigned_reviewer_id: 7, lease_expires_at: "2020-01-01T00:00:00Z" },
      lease_token: "lease"
    });
    window.history.replaceState({}, "", "/admin/reviews/1");
    await renderAdmin();
    await clickButton("开始审核");
    expect(container.textContent).toContain("sexual_content");
    expect(container.textContent).toContain("审核占用时间已结束");
    expect(button("确认通过").disabled).toBe(true);
  });

  it("renders the committed final state after a successful decision", async () => {
    vi.mocked(fetchAdminPrincipal).mockResolvedValue({
      user_id: 7, role: "reviewer", permissions: ["review.read", "review.decide"]
    });
    vi.mocked(fetchReviewCase).mockResolvedValue(reviewDetail());
    vi.mocked(claimReviewCase).mockResolvedValue({
      case: { ...reviewDetail().case, version: 2, assigned_reviewer_id: 7, lease_expires_at: "2099-01-01T00:00:00Z" },
      lease_token: "lease"
    });
    vi.mocked(decideReviewCase).mockResolvedValue({
      case: { ...reviewDetail().case, version: 3, status: "approved", closed_at: "2026-08-06T00:00:00Z" },
      decision: {
        id: 9, reviewer_id: 7, outcome: "approve", reason_code: "content_compliant",
        note: "", review_version: 1, case_version: 2, created_at: "2026-08-06T00:00:00Z"
      },
      duplicate: false
    });
    window.history.replaceState({}, "", "/admin/reviews/1");
    await renderAdmin();
    await clickButton("开始审核");
    await clickButton("确认通过");
    expect(container.textContent).toContain("审核结果已提交");
    expect(container.textContent).toContain("已通过");
  });

  it("reuses the decision idempotency key after response loss", async () => {
    vi.mocked(fetchAdminPrincipal).mockResolvedValue({
      user_id: 7, role: "reviewer", permissions: ["review.read", "review.decide"]
    });
    vi.mocked(fetchReviewCase).mockResolvedValue(reviewDetail());
    vi.mocked(claimReviewCase).mockResolvedValue({
      case: { ...reviewDetail().case, version: 2, assigned_reviewer_id: 7, lease_expires_at: "2099-01-01T00:00:00Z" },
      lease_token: "lease"
    });
    vi.mocked(decideReviewCase)
      .mockRejectedValueOnce(new TypeError("response lost"))
      .mockResolvedValueOnce({
        case: { ...reviewDetail().case, version: 3, status: "approved", closed_at: "2026-08-06T00:00:00Z" },
        decision: {
          id: 9, reviewer_id: 7, outcome: "approve", reason_code: "content_compliant",
          note: "", review_version: 1, case_version: 2, created_at: "2026-08-06T00:00:00Z"
        },
        duplicate: true
      });
    window.history.replaceState({}, "", "/admin/reviews/1");
    await renderAdmin();
    await clickButton("开始审核");
    await clickButton("确认通过");
    await clickButton("确认通过");
    const first = vi.mocked(decideReviewCase).mock.calls[0][2].idempotencyKey;
    const second = vi.mocked(decideReviewCase).mock.calls[1][2].idempotencyKey;
    expect(second).toBe(first);
    expect(container.textContent).toContain("审核结果已存在");
  });

  it("clears cached review rows when a refresh is forbidden", async () => {
    vi.mocked(fetchAdminPrincipal).mockResolvedValue({
      user_id: 7, role: "reviewer", permissions: ["review.read"]
    });
    vi.mocked(fetchReviewQueue)
      .mockResolvedValueOnce({
        items: [reviewQueueItem()],
        next_cursor: "next",
        has_more: true,
        scope: "available"
      })
      .mockRejectedValueOnce(new ApiError("forbidden", 403, "ADMIN_PERMISSION_DENIED"));
    window.history.replaceState({}, "", "/admin/reviews");
    await renderAdmin();
    expect(container.querySelector("table")).not.toBeNull();
    expect(container.textContent).toContain("Cached subject");
    await clickButton("刷新");
    expect(container.textContent).toContain("服务端拒绝了审核任务访问");
    expect(container.querySelector("table")).toBeNull();
    expect(container.textContent).not.toContain("Cached subject");
    await clickButton("我正在审核");
    expect(container.querySelector("table")).toBeNull();
  });

  it("keeps queue scope states isolated", async () => {
    vi.mocked(fetchAdminPrincipal).mockResolvedValue({
      user_id: 7, role: "reviewer", permissions: ["review.read", "review.decide"]
    });
    vi.mocked(fetchReviewQueue).mockImplementation((_token, query) => {
      const scope = query?.scope || "available";
      const item = scope === "mine"
        ? { ...reviewQueueItem(), case: { ...reviewQueueItem().case, id: 3 }, title: "Owned subject" }
        : reviewQueueItem();
      return Promise.resolve({ items: [item], next_cursor: "", has_more: false, scope });
    });
    window.history.replaceState({}, "", "/admin/reviews");
    await renderAdmin();
    expect(container.textContent).toContain("Cached subject");
    await clickButton("我正在审核");
    expect(container.textContent).toContain("Owned subject");
    expect(container.textContent).not.toContain("Cached subject");
  });

  it("starts available work and carries the in-memory credential to detail", async () => {
    vi.mocked(fetchAdminPrincipal).mockResolvedValue({
      user_id: 7, role: "reviewer", permissions: ["review.read", "review.decide"]
    });
    vi.mocked(fetchReviewQueue).mockResolvedValue({
      items: [reviewQueueItem()], next_cursor: "", has_more: false, scope: "available"
    });
    const claimedCase = {
      ...reviewQueueItem().case,
      version: 2,
      assigned_reviewer_id: 7,
      lease_expires_at: "2099-01-01T00:00:00Z"
    };
    vi.mocked(claimReviewCase).mockResolvedValue({ case: claimedCase, lease_token: "queue-lease" });
    vi.mocked(fetchReviewCase).mockResolvedValue({
      ...reviewDetail(),
      case: claimedCase
    });
    window.history.replaceState({}, "", "/admin/reviews");
    await renderAdmin();
    await clickButton("开始审核");
    expect(claimReviewCase).toHaveBeenCalledWith("admin-token", 2, 1);
    expect(container.textContent).toContain("审核任务 #2");
    expect(container.textContent).toContain("审核占用至");
  });

  it("resumes owned work after reload and can return it to the queue", async () => {
    vi.mocked(fetchAdminPrincipal).mockResolvedValue({
      user_id: 7, role: "reviewer", permissions: ["review.read", "review.decide"]
    });
    const owned = {
      ...reviewDetail().case,
      version: 2,
      assigned_reviewer_id: 7,
      lease_expires_at: "2099-01-01T00:00:00Z"
    };
    vi.mocked(fetchReviewCase).mockResolvedValue({ ...reviewDetail(), case: owned });
    vi.mocked(resumeReviewLease).mockResolvedValue({
      case: { ...owned, version: 3 },
      lease_token: "resumed-lease"
    });
    vi.mocked(releaseReviewLease).mockResolvedValue({
      ...owned,
      version: 4,
      assigned_reviewer_id: undefined,
      lease_expires_at: undefined
    });
    window.history.replaceState({}, "", "/admin/reviews/1");
    await renderAdmin();
    expect(resumeReviewLease).toHaveBeenCalledWith("admin-token", 1, 2);
    expect(container.textContent).toContain("已恢复正在审核的任务");
    await clickButton("放回待处理");
    expect(releaseReviewLease).toHaveBeenCalledWith("admin-token", 1, "resumed-lease", 3);
    expect(container.textContent).toContain("任务已放回待处理列表");
  });

  it("automatically extends actively viewed work", async () => {
    vi.mocked(fetchAdminPrincipal).mockResolvedValue({
      user_id: 7, role: "reviewer", permissions: ["review.read", "review.decide"]
    });
    const owned = {
      ...reviewDetail().case,
      version: 2,
      assigned_reviewer_id: 7,
      lease_expires_at: new Date(Date.now() + 2_100).toISOString()
    };
    vi.mocked(fetchReviewCase).mockResolvedValue({ ...reviewDetail(), case: owned });
    vi.mocked(resumeReviewLease).mockResolvedValue({
      case: { ...owned, version: 3, lease_expires_at: new Date(Date.now() + 2_100).toISOString() },
      lease_token: "resumed-lease"
    });
    vi.mocked(renewReviewLease).mockResolvedValue({
      case: {
        ...owned,
        version: 4,
        lease_expires_at: new Date(Date.now() + 60_000).toISOString()
      },
      lease_token: "resumed-lease"
    });
    window.history.replaceState({}, "", "/admin/reviews/1");
    await renderAdmin();
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 1_200));
    });
    expect(renewReviewLease).toHaveBeenCalledWith("admin-token", 1, "resumed-lease", 3);
  });

  it("uses server time instead of the browser clock for occupancy", async () => {
    vi.mocked(fetchAdminPrincipal).mockResolvedValue({
      user_id: 7, role: "reviewer", permissions: ["review.read", "review.decide"]
    });
    vi.mocked(fetchReviewCase).mockResolvedValue(reviewDetail());
    const serverTime = Date.now() - 60 * 60 * 1000;
    vi.mocked(claimReviewCase).mockResolvedValue({
      case: {
        ...reviewDetail().case,
        version: 2,
        assigned_reviewer_id: 7,
        lease_expires_at: new Date(serverTime + 5 * 60 * 1000).toISOString()
      },
      lease_token: "clock-safe-lease",
      server_time: new Date(serverTime).toISOString()
    });
    window.history.replaceState({}, "", "/admin/reviews/1");
    await renderAdmin();
    await clickButton("开始审核");
    expect(button("确认通过").disabled).toBe(false);
    expect(container.textContent).not.toContain("时间已结束");
  });

  it("shows protected-preview denial and seeded evidence truthfully", async () => {
    vi.mocked(fetchAdminPrincipal).mockResolvedValue({
      user_id: 7, role: "reviewer", permissions: ["review.read"]
    });
    const seeded = reviewDetail();
    seeded.history.signals[0] = {
      ...seeded.history.signals[0],
      provider: "manual-seed",
      source_kind: "test_seed",
      generated_at: "2026-08-01T00:00:00Z"
    };
    vi.mocked(fetchReviewCase).mockResolvedValue(seeded);
    vi.mocked(fetchReviewPreview).mockRejectedValue(
      new ApiError("preview unavailable", 409, "REVIEW_PREVIEW_UNAVAILABLE")
    );
    window.history.replaceState({}, "", "/admin/reviews/1");
    await renderAdmin();
    expect(container.textContent).toContain("视频预览暂时不可用");
    expect(container.textContent).toContain("测试证据");
    expect(container.textContent).toContain("manual-seed");
  });

  it("shows the protected cover separately from the review video", async () => {
    vi.mocked(fetchAdminPrincipal).mockResolvedValue({
      user_id: 7, role: "reviewer", permissions: ["review.read"]
    });
    vi.mocked(fetchReviewCase).mockResolvedValue(reviewDetail());
    window.history.replaceState({}, "", "/admin/reviews/1");
    await renderAdmin();
    const cover = container.querySelector<HTMLImageElement>(
      'img[alt="当前审核视频封面"]'
    );
    expect(cover?.src).toContain("https://preview.example.test/cover.jpg");
    expect(container.textContent).toContain("视频封面");
  });

  it("distinguishes production, recovery, and legacy evidence provenance", async () => {
    vi.mocked(fetchAdminPrincipal).mockResolvedValue({
      user_id: 7, role: "reviewer", permissions: ["review.read"]
    });
    const detail = reviewDetail();
    detail.history.signals = [
      {
        ...detail.history.signals[0],
        id: 1,
        provider: "production-gateway",
        source_kind: "production_provider"
      },
      {
        ...detail.history.signals[0],
        id: 2,
        result_id: "recovery",
        label: "moderation_unavailable",
        provider: "frux-moderation-recovery",
        source_kind: "recovery"
      },
      {
        ...detail.history.signals[0],
        id: 3,
        result_id: "legacy",
        provider: "old-provider",
        source_kind: "legacy_unknown"
      }
    ];
    detail.history.automated_decisions = [{
      id: 1,
      result_id: "r1",
      outcome: "human",
      policy_version: 1,
      rollout_mode: "observe",
      created_at: "2026-08-01T00:00:01Z"
    }];
    vi.mocked(fetchReviewCase).mockResolvedValue(detail);
    window.history.replaceState({}, "", "/admin/reviews/1");
    await renderAdmin();
    expect(container.textContent).toContain("生产模型证据");
    expect(container.textContent).toContain("系统恢复记录（非模型判断）");
    expect(container.textContent).toContain("未获得模型判断");
    expect(container.textContent).toContain("历史来源未验证");
    expect(container.textContent).toContain("观察模式");
    expect(container.textContent).toContain("证据生成于");
  });

  it("does not restore an in-flight preview after a version conflict", async () => {
    vi.mocked(fetchAdminPrincipal).mockResolvedValue({
      user_id: 7, role: "reviewer", permissions: ["review.read", "review.decide"]
    });
    vi.mocked(fetchReviewCase).mockResolvedValue(reviewDetail());
    vi.mocked(claimReviewCase).mockResolvedValue({
      case: {
        ...reviewDetail().case,
        version: 2,
        assigned_reviewer_id: 7,
        lease_expires_at: "2099-01-01T00:00:00Z"
      },
      lease_token: "lease"
    });
    let resolvePreview: ((access: {
      media_url: string;
      cover_url: string;
      expires_at: string;
    }) => void) | undefined;
    vi.mocked(fetchReviewPreview)
      .mockReturnValueOnce(new Promise((resolve) => {
        resolvePreview = resolve;
      }))
      .mockResolvedValue({
        media_url: "https://preview.example.test/fresh.mp4",
        cover_url: "",
        expires_at: "2099-01-01T00:00:00Z"
      });
    vi.mocked(renewReviewLease).mockRejectedValue(
      new ApiError("conflict", 409, "REVIEW_CONFLICT")
    );
    window.history.replaceState({}, "", "/admin/reviews/1");
    await renderAdmin();
    await clickButton("开始审核");
    await clickButton("延长审核时间");
    await act(async () => {
      resolvePreview?.({
        media_url: "https://preview.example.test/stale.mp4",
        cover_url: "",
        expires_at: "2099-01-01T00:00:00Z"
      });
      await flush();
    });
    expect(container.querySelector("video")).toBeNull();
    expect(container.textContent).toContain("审核任务状态已变化");
    await clickButton("刷新任务");
    expect(container.querySelector<HTMLVideoElement>("video")?.src)
      .toContain("https://preview.example.test/fresh.mp4");
  });

  it("includes the current minute in the default video creation upper bound", () => {
    const now = new Date("2026-08-06T13:48:28.098+08:00");
    const filters = defaultAdminVideoFilters(now);
    const createdTo = new Date(filters.created_to);
    expect(createdTo.getSeconds()).toBe(59);
    expect(createdTo.getMilliseconds()).toBe(999);
    expect(createdTo.getTime()).toBeGreaterThanOrEqual(now.getTime());
  });

  it("shows forbidden and stale-version outcomes without reporting success", async () => {
    vi.mocked(fetchAdminPrincipal).mockResolvedValue({
      user_id: 7, role: "operator", permissions: ["review.read", "content.enforce"]
    });
    vi.mocked(searchAdminVideos).mockResolvedValue({
      items: [{
        id: 3, author_id: 4, title: "Video", description: "", media_url: "", cover_url: "",
        status: 2, status_name: "published", visibility: "public", media_status: "ready",
        review_version: 1, version: 5, created_at: "2026-08-01T00:00:00Z",
        updated_at: "2026-08-01T00:00:00Z"
      }],
      next_cursor: "",
      has_more: false
    });
    vi.mocked(takeDownAdminVideo).mockRejectedValue(
      new ApiError("conflict", 409, "ADMIN_VIDEO_VERSION_CONFLICT")
    );
    window.history.replaceState({}, "", "/admin/videos");
    await renderAdmin();
    await clickButton("下架");
    const checkbox = required<HTMLInputElement>('input[type="checkbox"]');
    await act(async () => {
      checkbox.click();
    });
    await clickButton("确认下架");
    expect(container.textContent).toContain("版本或状态已变化");
    expect(container.textContent).not.toContain("视频已下架并完成审计");

    vi.mocked(searchAdminVideos).mockRejectedValueOnce(
      new ApiError("forbidden", 403, "ADMIN_PERMISSION_DENIED")
    );
    await clickButton("取消");
    const form = required<HTMLFormElement>("form");
    await act(async () => {
      form.dispatchEvent(new Event("submit", { bubbles: true, cancelable: true }));
      await flush();
    });
    expect(container.textContent).toContain("服务端拒绝了视频运营访问");
  });

  it("shows ordinary account management only with account.manage", async () => {
    sessionStorage.setItem(ADMIN_SESSION_KEY, JSON.stringify({
      version: 1,
      token: "admin-token",
      principal: {
        user_id: 9,
        role: "admin",
        permissions: ["review.read", "account.manage"]
      },
      expires_at: Date.now() + 30 * 60 * 1000
    }));
    vi.mocked(fetchAdminPrincipal).mockResolvedValue({
      user_id: 9, role: "admin", permissions: ["review.read", "account.manage"]
    });
    vi.mocked(searchManagedAccounts).mockResolvedValue({
      items: [{
        id: 42, account: "alice-login", nickname: "Alice", avatar_url: "", bio: "",
        gender: 0, status: 1, status_name: "normal", version: 3,
        following_count: 2, follower_count: 4, public_work_count: 5,
        private_work_count: 1, received_like_count: 8, active_session_count: 2,
        created_at: "2026-08-01T00:00:00Z", updated_at: "2026-08-02T00:00:00Z"
      }],
      next_cursor: "",
      has_more: false
    });
    window.history.replaceState({}, "", "/admin/accounts");
    await renderAdmin();
    expect(container.textContent).toContain("账号管理");
    expect(container.textContent).toContain("alice-login");
    expect(container.textContent).not.toContain("视频运营");
  });

  it("freezes an ordinary account with versioned audited confirmation", async () => {
    const account = {
      id: 42, account: "alice-login", nickname: "Alice", avatar_url: "", bio: "",
      gender: 0, status: 1, status_name: "normal" as const, version: 3,
      following_count: 2, follower_count: 4, public_work_count: 5,
      private_work_count: 1, received_like_count: 8, active_session_count: 2,
      created_at: "2026-08-01T00:00:00Z", updated_at: "2026-08-02T00:00:00Z"
    };
    sessionStorage.setItem(ADMIN_SESSION_KEY, JSON.stringify({
      version: 1,
      token: "admin-token",
      principal: {
        user_id: 9,
        role: "admin",
        permissions: ["review.read", "account.manage"]
      },
      expires_at: Date.now() + 30 * 60 * 1000
    }));
    vi.mocked(fetchAdminPrincipal).mockResolvedValue({
      user_id: 9, role: "admin", permissions: ["review.read", "account.manage"]
    });
    vi.mocked(searchManagedAccounts).mockResolvedValue({
      items: [account], next_cursor: "", has_more: false
    });
    vi.mocked(fetchManagedAccount).mockResolvedValue(account);
    vi.mocked(freezeManagedAccount).mockResolvedValue({
      user_id: 42, operation: "freeze", status: 2, status_name: "frozen",
      version: 4, revoked_session_count: 2,
      occurred_at: "2026-08-03T00:00:00Z",
      replayed: false, audit_committed: true
    });
    window.history.replaceState({}, "", "/admin/accounts");
    await renderAdmin();
    await clickButton("查看");
    await clickButton("冻结账号");
    expect(container.textContent).toContain("现有作品不会自动下架");
    const checkbox = required<HTMLInputElement>('input[type="checkbox"]');
    await act(async () => {
      checkbox.click();
    });
    await clickButton("确认冻结");
    expect(freezeManagedAccount).toHaveBeenCalledWith(
      "admin-token",
      42,
      { expected_version: 3, reason_code: "abuse" },
      expect.any(String)
    );
    expect(container.textContent).toContain("账号已冻结并完成审计");
    expect(container.textContent).toContain("已冻结");
  });

  async function renderAdmin() {
    await act(async () => {
      root.render(
        <RouterProvider>
          <SessionProvider>
            <AdminApp />
          </SessionProvider>
        </RouterProvider>
      );
      await flush();
      await flush();
    });
  }

  async function clickButton(label: string) {
    await act(async () => {
      button(label).click();
      await flush();
      await flush();
    });
  }

  function button(label: string): HTMLButtonElement {
    const match = [...container.querySelectorAll<HTMLButtonElement>("button")]
      .find((item) => item.textContent?.trim() === label);
    if (!match) throw new Error(`missing button: ${label}`);
    return match;
  }

  function required<T extends Element>(selector: string): T {
    const element = container.querySelector<T>(selector);
    if (!element) throw new Error(`missing element: ${selector}`);
    return element;
  }

  function setInputValue(input: HTMLInputElement, value: string) {
    const setter = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, "value")?.set;
    setter?.call(input, value);
    input.dispatchEvent(new Event("input", { bubbles: true }));
  }
});

function reviewDetail(): ReviewCaseDetail {
  return {
    case: {
      id: 1, video_id: 10, review_version: 1, status: "pending_human",
      policy_version: 1, priority: 90, version: 1,
      created_at: "2026-08-01T00:00:00Z", updated_at: "2026-08-01T00:00:00Z"
    },
    subject: {
      video_id: 10, author_id: 11, title: "Subject", description: "Evidence remains",
      media_url: "", cover_url: "", review_version: 1
    },
    history: {
      signals: [{
        id: 1, result_id: "r1", label: "sexual_content", confidence: 0.91,
        evidence_refs: ["frame:10"], provider: "model", model_version: "v1",
        policy_version: 1, source_kind: "legacy_unknown",
        generated_at: "2026-08-01T00:00:00Z", created_at: "2026-08-01T00:00:00Z"
      }],
      automated_decisions: [],
      assignments: [],
      human_decisions: []
    }
  };
}

function reviewQueueItem() {
  return {
    case: {
      ...reviewDetail().case,
      id: 2
    },
    author_id: 11,
    title: "Cached subject",
    cover_url: "",
    media_url: ""
  };
}

async function flush() {
  await Promise.resolve();
  await new Promise((resolve) => setTimeout(resolve, 0));
}

void renewReviewLease;
void releaseReviewLease;
void resumeReviewLease;
void restoreAdminVideo;
void revokeManagedAccountSessions;
void unfreezeManagedAccount;
