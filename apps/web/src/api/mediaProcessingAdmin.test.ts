import { describe, expect, it } from "vitest";
import {
  isMediaProcessingHistoryPage,
  isMediaProcessingOverviewResponse,
  mediaProcessingHistoryPath,
  mediaProcessingOverviewPath,
  mediaProcessingRetryReasonLabel,
  mediaProcessingStageLabel,
  mediaProcessingStateLabel
} from "./mediaProcessingAdmin";
import type { MediaProcessingHistoryFilters } from "../types";

describe("media processing admin API helpers", () => {
  it("authors search paths and readable labels", () => {
    const filters: MediaProcessingHistoryFilters = {
      state: "failed",
      stage: "transcoding",
      error_code: "source_deleted",
      video_id: "42",
      completed_from: "2026-08-01T00:00",
      completed_to: "2026-08-06T00:00"
    };
    const url = new URL(mediaProcessingHistoryPath(filters, "cursor-1", 20), "https://frux.test");
    expect(mediaProcessingOverviewPath()).toBe("/api/admin/media-processing/overview");
    expect(url.searchParams.get("state")).toBe("failed");
    expect(url.searchParams.get("stage")).toBe("transcoding");
    expect(url.searchParams.get("error_code")).toBe("source_deleted");
    expect(url.searchParams.get("video_id")).toBe("42");
    expect(url.searchParams.get("cursor")).toBe("cursor-1");
    expect(url.searchParams.get("completed_from")).toBe(new Date(filters.completed_from).toISOString());
    expect(url.searchParams.get("completed_to")).toBe(new Date(filters.completed_to).toISOString());
    expect(mediaProcessingStateLabel("retryable")).toBe("可重新处理");
    expect(mediaProcessingStageLabel("uploading")).toBe("正在上传处理结果");
    expect(mediaProcessingRetryReasonLabel("operator_retry")).toBe("人工重试");
  });

  it("validates overview and history payloads", () => {
    expect(isMediaProcessingOverviewResponse({
      summary: { waiting: 1, processing: 2, failed: 3, completed: 4 },
      active_items: [],
      refreshed_at: "2026-08-06T00:00:00Z"
    })).toBe(true);
    expect(isMediaProcessingHistoryPage({
      items: [],
      next_cursor: "",
      has_more: false
    })).toBe(true);
  });
});
