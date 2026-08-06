// @vitest-environment jsdom
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { fetchAdminPrincipal } from "../api/admin";
import { ApiError } from "../api/client";
import { fetchUnreadStat } from "../api/messages";
import {
  claimReviewCase,
  decideReviewCase,
  fetchReviewCase,
  fetchReviewQueue,
  renewReviewLease
} from "../api/review";
import {
  restoreAdminVideo,
  searchAdminVideos,
  takeDownAdminVideo
} from "../api/videoAdmin";
import { emptyProfile, TOKEN_KEY, USER_KEY } from "../constants";
import { RouterProvider } from "../router";
import { SessionProvider } from "../session";
import type { ReviewCaseDetail } from "../types";
import AdminApp from "./AdminApp";
import { defaultAdminVideoFilters } from "./VideoOperationsPage";

vi.mock("../api/admin", () => ({ fetchAdminPrincipal: vi.fn() }));
vi.mock("../api/messages", () => ({ fetchUnreadStat: vi.fn() }));
vi.mock("../api/review", () => ({
  claimReviewCase: vi.fn(),
  decideReviewCase: vi.fn(),
  fetchReviewCase: vi.fn(),
  fetchReviewQueue: vi.fn(),
  renewReviewLease: vi.fn(),
  releaseReviewLease: vi.fn()
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
    localStorage.setItem(TOKEN_KEY, "admin-token");
    localStorage.setItem(USER_KEY, JSON.stringify({
      ...emptyProfile, id: 7, nickname: "Reviewer", role: "reviewer"
    }));
    vi.mocked(fetchUnreadStat).mockResolvedValue({ unread_count: 0 });
    vi.mocked(fetchReviewQueue).mockResolvedValue({ items: [], next_cursor: "", has_more: false });
  });

  afterEach(() => {
    act(() => root.unmount());
    container.remove();
    localStorage.clear();
    vi.clearAllMocks();
  });

  it("filters shell navigation to the server-confirmed permissions", async () => {
    vi.mocked(fetchAdminPrincipal).mockResolvedValue({
      user_id: 7, role: "reviewer", permissions: ["review.read", "review.decide"]
    });
    window.history.replaceState({}, "", "/admin/reviews");
    await renderAdmin();
    expect(container.textContent).toContain("审核队列");
    expect(container.textContent).not.toContain("视频运营");
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
    await clickButton("领取案件");
    expect(container.textContent).toContain("sexual_content");
    expect(container.textContent).toContain("租约已过期");
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
    await clickButton("领取案件");
    await clickButton("确认通过");
    expect(container.textContent).toContain("审核结果已提交");
    expect(container.textContent).toContain("approved");
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
    await clickButton("领取案件");
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
        has_more: true
      })
      .mockRejectedValueOnce(new ApiError("forbidden", 403, "ADMIN_PERMISSION_DENIED"));
    window.history.replaceState({}, "", "/admin/reviews");
    await renderAdmin();
    expect(container.querySelector("table")).not.toBeNull();
    expect(container.textContent).toContain("Cached subject");
    await clickButton("刷新");
    expect(container.textContent).toContain("服务端拒绝了审核队列访问");
    expect(container.querySelector("table")).toBeNull();
    expect(container.textContent).not.toContain("Cached subject");
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
        policy_version: 1, created_at: "2026-08-01T00:00:00Z"
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
void restoreAdminVideo;
