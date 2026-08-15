// @vitest-environment jsdom
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  bulkRetryMediaProcessingJobs,
  fetchMediaProcessingHistory,
  fetchMediaProcessingOverview,
  retryMediaProcessingJob
} from "../api/mediaProcessingAdmin";
import { searchAdminVideos } from "../api/videoAdmin";
import type { MediaProcessingOverviewResponse } from "../types";
import VideoOperationsPage from "./VideoOperationsPage";

vi.mock("../api/videoAdmin", async (importOriginal) => ({
  ...(await importOriginal<typeof import("../api/videoAdmin")>()),
  restoreAdminVideo: vi.fn(),
  searchAdminVideos: vi.fn(),
  takeDownAdminVideo: vi.fn()
}));

vi.mock("../api/mediaProcessingAdmin", async (importOriginal) => ({
  ...(await importOriginal<typeof import("../api/mediaProcessingAdmin")>()),
  bulkRetryMediaProcessingJobs: vi.fn(),
  fetchMediaProcessingHistory: vi.fn(),
  fetchMediaProcessingOverview: vi.fn(),
  retryMediaProcessingJob: vi.fn()
}));

vi.mock("./adminSession", () => ({
  useAdminSession: () => ({
    token: "admin-token",
    principal: { user_id: 7, role: "operator", permissions: ["content.enforce"] },
    state: "ready",
    login: vi.fn(),
    logout: vi.fn(),
    refresh: vi.fn()
  })
}));

describe("video operations processing view", () => {
  let container: HTMLDivElement;
  let root: Root;

  beforeEach(() => {
    (globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
    container = document.createElement("div");
    document.body.appendChild(container);
    root = createRoot(container);
    vi.mocked(searchAdminVideos).mockResolvedValue({
      items: [],
      next_cursor: "",
      has_more: false
    });
  });

  afterEach(() => {
    act(() => root.unmount());
    container.remove();
    vi.clearAllMocks();
    vi.useRealTimers();
  });

  it("renders plain-language processing labels, diagnostics, and single retry", async () => {
    vi.mocked(fetchMediaProcessingOverview).mockResolvedValue({
      summary: {
        waiting: 2,
        processing: 1,
        failed: 3,
        completed: 4,
        oldest_waiting_at: "2026-08-01T00:00:00Z"
      },
      active_items: [activeItem()],
      refreshed_at: "2026-08-06T00:00:00Z"
    });
    vi.mocked(fetchMediaProcessingHistory).mockResolvedValue({
      items: [failedItem(2), completedItem(3)],
      next_cursor: "",
      has_more: false
    });
    vi.mocked(retryMediaProcessingJob).mockResolvedValue({
      item: failedItem(2),
      audit_committed: true,
      replayed: false
    });

    await renderPage();
    clickButton("处理进度");
    await flush();

    const processing = activeProcessingSection();
    expect(processing.textContent).toContain("等待中");
    expect(processing.textContent).toContain("正在下载视频");
    expect(processing.textContent).toContain("诊断信息");

    const activeDiagnostics = processing.querySelectorAll<HTMLDetailsElement>("details")[0];
    expect(activeDiagnostics).toBeDefined();
    const summary = activeDiagnostics.querySelector<HTMLElement>("summary");
    expect(summary?.textContent).toContain("诊断信息");
    act(() => summary?.click());
    expect(activeDiagnostics.open).toBe(true);
    expect(processing.textContent).toContain("任务 ID");

    clickButton("重新处理");
    await flush();
    expect(activeProcessingSection().textContent).toContain("确认重新处理");
    checkConfirm();
    clickDialogButton("确认重新处理");
    await flush();

    expect(vi.mocked(retryMediaProcessingJob)).toHaveBeenCalledWith(
      "admin-token",
      2,
      { reason_code: "temporary_failure", note: "" },
      expect.any(String)
    );
    expect(activeProcessingSection().textContent).toContain("已返回处理队列");
  });

  it("supports bulk retry partial outcomes", async () => {
    vi.mocked(fetchMediaProcessingOverview).mockResolvedValue({
      summary: {
        waiting: 0,
        processing: 1,
        failed: 4,
        completed: 1,
        oldest_waiting_at: "2026-08-01T00:00:00Z"
      },
      active_items: [activeItem()],
      refreshed_at: "2026-08-06T00:00:00Z"
    });
    vi.mocked(fetchMediaProcessingHistory).mockResolvedValue({
      items: [failedItem(2), failedItem(4), failedItem(5)],
      next_cursor: "",
      has_more: false
    });
    vi.mocked(bulkRetryMediaProcessingJobs).mockResolvedValue({
      items: [
        { job_id: 2, status: "retried", item: failedItem(2) },
        { job_id: 4, status: "conflict", error_code: "STATE_CHANGED" },
        { job_id: 5, status: "rejected", error_code: "SOURCE_DELETED" }
      ]
    });

    await renderPage();
    clickButton("处理进度");
    await flush();

    const processing = activeProcessingSection();
    const checkboxes = [...processing.querySelectorAll<HTMLInputElement>('input[type="checkbox"]')];
    expect(checkboxes.length).toBe(3);
    act(() => {
      checkboxes.forEach((item) => {
        item.click();
      });
    });
    await flush();
    clickButton("重新处理所选");
    await flush();
    checkConfirm();
    clickDialogButton("确认批量重新处理");
    await flush();

    expect(vi.mocked(bulkRetryMediaProcessingJobs)).toHaveBeenCalledWith(
      "admin-token",
      {
        job_ids: [2, 4, 5],
        reason_code: "temporary_failure",
        note: ""
      },
      expect.any(String)
    );
    expect(activeProcessingSection().textContent).toContain("已重新处理");
    expect(activeProcessingSection().textContent).toContain("状态已变化");
    expect(activeProcessingSection().textContent).toContain("已拒绝");
  });

  it("refreshes overview adaptively and pauses hidden polling", async () => {
    vi.useFakeTimers();
    vi.mocked(fetchMediaProcessingOverview).mockResolvedValue({
      summary: {
        waiting: 0,
        processing: 1,
        failed: 0,
        completed: 0,
        oldest_waiting_at: "2026-08-01T00:00:00Z"
      },
      active_items: [activeItem()],
      refreshed_at: "2026-08-06T00:00:00Z"
    });
    vi.mocked(fetchMediaProcessingHistory).mockResolvedValue({
      items: [],
      next_cursor: "",
      has_more: false
    });
    await renderPage();
    clickButton("处理进度");
    await flush();

    expect(vi.mocked(fetchMediaProcessingOverview)).toHaveBeenCalledTimes(1);
    await act(async () => {
      vi.advanceTimersByTime(4_999);
    });
    expect(vi.mocked(fetchMediaProcessingOverview)).toHaveBeenCalledTimes(1);
    await act(async () => {
      vi.advanceTimersByTime(1);
    });
    expect(vi.mocked(fetchMediaProcessingOverview)).toHaveBeenCalledTimes(2);

    Object.defineProperty(document, "visibilityState", { configurable: true, value: "hidden" });
    act(() => {
      document.dispatchEvent(new Event("visibilitychange"));
    });
    await act(async () => {
      vi.advanceTimersByTime(30_000);
    });
    expect(vi.mocked(fetchMediaProcessingOverview)).toHaveBeenCalledTimes(2);

    Object.defineProperty(document, "visibilityState", { configurable: true, value: "visible" });
    act(() => {
      document.dispatchEvent(new Event("visibilitychange"));
    });
    await flush();
    expect(vi.mocked(fetchMediaProcessingOverview)).toHaveBeenCalledTimes(3);
  });

  it("ignores stale overview responses after a newer refresh", async () => {
    const firstOverview = defer<MediaProcessingOverviewResponse>();
    const secondOverview = defer<MediaProcessingOverviewResponse>();
    vi.mocked(fetchMediaProcessingOverview)
      .mockResolvedValueOnce({
        summary: {
          waiting: 0,
          processing: 1,
          failed: 0,
          completed: 0,
          oldest_waiting_at: "2026-08-01T00:00:00Z"
        },
        active_items: [activeItem()],
        refreshed_at: "2026-08-06T00:00:00Z"
      })
      .mockImplementationOnce(() => firstOverview.promise)
      .mockImplementationOnce(() => secondOverview.promise);
    vi.mocked(fetchMediaProcessingHistory).mockResolvedValue({
      items: [],
      next_cursor: "",
      has_more: false
    });
    await renderPage();
    clickButton("处理进度");
    await flush();
    Object.defineProperty(document, "visibilityState", { configurable: true, value: "hidden" });
    act(() => {
      document.dispatchEvent(new Event("visibilitychange"));
    });
    Object.defineProperty(document, "visibilityState", { configurable: true, value: "visible" });
    act(() => {
      document.dispatchEvent(new Event("visibilitychange"));
    });
    await flush();
    Object.defineProperty(document, "visibilityState", { configurable: true, value: "hidden" });
    act(() => {
      document.dispatchEvent(new Event("visibilitychange"));
    });
    Object.defineProperty(document, "visibilityState", { configurable: true, value: "visible" });
    act(() => {
      document.dispatchEvent(new Event("visibilitychange"));
    });
    await flush();
    secondOverview.resolve({
      summary: {
        waiting: 0,
        processing: 0,
        failed: 8,
        completed: 4,
        oldest_waiting_at: "2026-08-01T00:00:00Z"
      },
      active_items: [],
      refreshed_at: "2026-08-06T00:02:00Z"
    });
    await flush();
    firstOverview.resolve({
      summary: {
        waiting: 0,
        processing: 0,
        failed: 99,
        completed: 99,
        oldest_waiting_at: "2026-08-01T00:00:00Z"
      },
      active_items: [],
      refreshed_at: "2026-08-06T00:03:00Z"
    });
    await flush();
    const summaryCards = activeProcessingSection().querySelectorAll<HTMLElement>(".admin-summary-card strong");
    expect(summaryCards[2]?.textContent).toBe("8");
  });

  async function renderPage() {
    await act(async () => {
      root.render(<VideoOperationsPage />);
    });
    await flush();
  }

  function clickButton(label: string) {
    const button = [...container.querySelectorAll<HTMLButtonElement>("button")]
      .find((item) => item.textContent?.trim() === label);
    if (!button) throw new Error(`missing button: ${label}`);
    act(() => button.click());
  }

  function clickDialogButton(label: string) {
    const dialog = container.querySelector<HTMLElement>('[role="dialog"]');
    if (!dialog) throw new Error("missing retry dialog");
    const button = [...dialog.querySelectorAll<HTMLButtonElement>("button")]
      .find((item) => item.textContent?.trim() === label);
    if (!button) throw new Error(`missing dialog button: ${label}`);
    act(() => button.click());
  }

  function checkConfirm() {
    const dialog = container.querySelector<HTMLElement>('[role="dialog"]');
    if (!dialog) throw new Error("missing retry dialog");
    const checkbox = dialog.querySelector<HTMLInputElement>('input[type="checkbox"]');
    if (!checkbox) throw new Error("missing confirm checkbox");
    act(() => checkbox.click());
  }

  function activeProcessingSection(): HTMLElement {
    const section = container.querySelector<HTMLElement>('section[aria-label="处理进度"]:not([hidden])');
    if (!section) throw new Error("missing active processing section");
    return section;
  }
});

function activeItem() {
  return {
    job_id: 1,
    video_id: 10,
    author_id: 11,
    title: "进行中视频",
    profile_version: "v2",
    state: "processing" as const,
    stage: "downloading" as const,
    stage_progress_bps: 2_500,
    attempts: 1,
    max_attempts: 5,
    created_at: "2026-08-06T00:00:00Z",
    updated_at: "2026-08-06T00:01:00Z",
    progress_updated_at: "2026-08-06T00:01:00Z",
    next_attempt_at: null,
    completed_at: null
  };
}

function failedItem(jobId: number) {
  return {
    job_id: jobId,
    video_id: 20 + jobId,
    author_id: 30 + jobId,
    title: `失败视频 ${jobId}`,
    profile_version: "v3",
    state: "failed" as const,
    stage: "failed" as const,
    stage_progress_bps: null,
    attempts: 2,
    max_attempts: 5,
    error_code: "source_deleted",
    error_message: "源文件已删除",
    created_at: "2026-08-05T00:00:00Z",
    updated_at: "2026-08-06T00:01:00Z",
    progress_updated_at: "2026-08-06T00:01:00Z",
    next_attempt_at: "2026-08-07T00:00:00Z",
    completed_at: "2026-08-06T00:01:00Z"
  };
}

function completedItem(jobId: number) {
  return {
    job_id: jobId,
    video_id: 20 + jobId,
    author_id: 30 + jobId,
    title: `已完成视频 ${jobId}`,
    profile_version: "v3",
    state: "completed" as const,
    stage: "completed" as const,
    stage_progress_bps: null,
    attempts: 1,
    max_attempts: 5,
    created_at: "2026-08-05T00:00:00Z",
    updated_at: "2026-08-06T00:02:00Z",
    progress_updated_at: "2026-08-06T00:02:00Z",
    next_attempt_at: null,
    completed_at: "2026-08-06T00:02:00Z"
  };
}

function defer<T>() {
  let resolve!: (value: T) => void;
  let reject!: (reason?: unknown) => void;
  const promise = new Promise<T>((res, rej) => {
    resolve = res;
    reject = rej;
  });
  return { promise, resolve, reject };
}

async function flush() {
  await act(async () => {
    await Promise.resolve();
    await Promise.resolve();
  });
}
