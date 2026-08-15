import {
  ADMIN_AUTH_INVALID_EVENT,
  ApiError,
  UserFacingError
} from "./client";
import type {
  MediaProcessingAdminItem,
  MediaProcessingBulkRetryItemResult,
  MediaProcessingBulkRetryRequest,
  MediaProcessingBulkRetryResponse,
  MediaProcessingHistoryFilters,
  MediaProcessingHistoryPage,
  MediaProcessingOverviewResponse,
  MediaProcessingRetryRequest,
  MediaProcessingRetryResponse,
  MediaProcessingRetryReasonCode,
  MediaProcessingStage,
  MediaProcessingState,
  MediaProcessingSummary
} from "../types";

interface MediaProcessingRequestOptions {
  method?: string;
  token: string;
  headers?: Record<string, string>;
  body?: unknown;
  signal?: AbortSignal;
}

const STATE_LABELS: Record<MediaProcessingState, string> = {
  pending: "等待处理",
  processing: "处理中",
  retryable: "可重新处理",
  completed: "已完成",
  failed: "已失败"
};

const STAGE_LABELS: Record<MediaProcessingStage, string> = {
  waiting: "等待中",
  downloading: "正在下载视频",
  inspecting: "正在检查视频",
  remuxing: "正在整理视频格式",
  transcoding: "正在转换视频格式",
  uploading: "正在上传处理结果",
  finalizing: "正在完成处理",
  completed: "处理完成",
  failed: "处理失败"
};

const RETRY_REASON_LABELS: Record<MediaProcessingRetryReasonCode, string> = {
  configuration_changed: "配置已变更",
  temporary_failure: "临时故障",
  operator_retry: "人工重试"
};

export function mediaProcessingStateLabel(state: MediaProcessingState): string {
  return STATE_LABELS[state];
}

export function mediaProcessingStageLabel(stage: MediaProcessingStage): string {
  return STAGE_LABELS[stage];
}

export function mediaProcessingRetryReasonLabel(reasonCode: MediaProcessingRetryReasonCode): string {
  return RETRY_REASON_LABELS[reasonCode];
}

export function mediaProcessingOverviewPath(): string {
  return "/api/admin/media-processing/overview";
}

export function mediaProcessingHistoryPath(
  filters: MediaProcessingHistoryFilters,
  cursor = "",
  limit = 20
): string {
  const params = new URLSearchParams();
  if (filters.state) params.set("state", filters.state);
  if (filters.stage) params.set("stage", filters.stage);
  if (filters.error_code.trim()) params.set("error_code", filters.error_code.trim());
  if (filters.video_id.trim()) params.set("video_id", filters.video_id.trim());
  if (filters.completed_from) params.set("completed_from", new Date(filters.completed_from).toISOString());
  if (filters.completed_to) params.set("completed_to", new Date(filters.completed_to).toISOString());
  if (cursor) params.set("cursor", cursor);
  params.set("limit", String(limit));
  return `/api/admin/media-processing/history?${params.toString()}`;
}

export async function fetchMediaProcessingOverview(
  token: string,
  signal?: AbortSignal
): Promise<MediaProcessingOverviewResponse> {
  return mediaProcessingRequest<MediaProcessingOverviewResponse>(
    mediaProcessingOverviewPath(),
    { token, signal }
  );
}

export async function fetchMediaProcessingHistory(
  token: string,
  filters: MediaProcessingHistoryFilters,
  cursor = "",
  limit = 20,
  signal?: AbortSignal
): Promise<MediaProcessingHistoryPage> {
  return mediaProcessingRequest<MediaProcessingHistoryPage>(
    mediaProcessingHistoryPath(filters, cursor, limit),
    { token, signal }
  );
}

export async function retryMediaProcessingJob(
  token: string,
  jobID: number,
  body: MediaProcessingRetryRequest,
  idempotencyKey: string,
  signal?: AbortSignal
): Promise<MediaProcessingRetryResponse> {
  return mediaProcessingRequest<MediaProcessingRetryResponse>(
    `/api/admin/media-processing/jobs/${jobID}/retry`,
    {
      method: "POST",
      token,
      headers: { "Idempotency-Key": idempotencyKey },
      body,
      signal
    }
  );
}

export async function bulkRetryMediaProcessingJobs(
  token: string,
  body: MediaProcessingBulkRetryRequest,
  idempotencyKey: string,
  signal?: AbortSignal
): Promise<MediaProcessingBulkRetryResponse> {
  return mediaProcessingRequest<MediaProcessingBulkRetryResponse>(
    "/api/admin/media-processing/jobs/bulk-retry",
    {
      method: "POST",
      token,
      headers: { "Idempotency-Key": idempotencyKey },
      body,
      signal
    }
  );
}

async function mediaProcessingRequest<T>(
  path: string,
  options: MediaProcessingRequestOptions
): Promise<T> {
  const headers: Record<string, string> = {
    Accept: "application/json",
    ...(options.headers || {})
  };
  if (options.body !== undefined) headers["Content-Type"] = "application/json";
  if (options.token) headers.Authorization = `Bearer ${options.token}`;

  let response: Response;
  try {
    response = await fetch(path, {
      method: options.method || "GET",
      headers,
      body: options.body !== undefined ? JSON.stringify(options.body) : undefined,
      signal: options.signal
    });
  } catch (error) {
    if (error instanceof DOMException && error.name === "AbortError") {
      throw error;
    }
    throw new UserFacingError("处理进度请求失败，请重试");
  }

  if (!response.ok) {
    const error = await createApiError(response);
    if (
      response.status === 401 &&
      path.startsWith("/api/admin/") &&
      path !== "/api/admin/auth/login" &&
      typeof window !== "undefined"
    ) {
      window.dispatchEvent(new CustomEvent(ADMIN_AUTH_INVALID_EVENT, {
        detail: { token: options.token }
      }));
    }
    throw error;
  }

  if (response.status === 204) return null as T;
  const data = await response.json() as T;
  return validateMediaProcessingPayload(path, data);
}

async function createApiError(response: Response): Promise<ApiError> {
  let message = response.statusText || "请求失败";
  let code = "";
  try {
    const data = await response.json() as { code?: string; error?: string; message?: string };
    if (data.error) message = data.error;
    if (data.message) message = data.message;
    if (data.code) code = data.code;
  } catch {
    if (!message) message = "请求失败";
  }
  return new ApiError(message, response.status, code);
}

function validateMediaProcessingPayload<T>(path: string, data: T): T {
  if (path.endsWith("/overview")) {
    if (isMediaProcessingOverviewResponse(data)) return data;
  } else if (path.includes("/history?")) {
    if (isMediaProcessingHistoryPage(data)) return data;
  } else if (path.includes("/retry") || path.endsWith("/bulk-retry")) {
    if (isMediaProcessingRetryResponse(data) || isMediaProcessingBulkRetryResponse(data)) return data;
  }
  throw new UserFacingError("处理进度数据格式异常，请刷新后重试");
}

export function isMediaProcessingOverviewResponse(
  value: unknown
): value is MediaProcessingOverviewResponse {
  if (!isRecord(value)) return false;
  return isMediaProcessingSummary(value.summary) &&
    Array.isArray(value.active_items) &&
    value.active_items.every(isMediaProcessingAdminItem) &&
    typeof value.refreshed_at === "string";
}

export function isMediaProcessingHistoryPage(value: unknown): value is MediaProcessingHistoryPage {
  if (!isRecord(value)) return false;
  return Array.isArray(value.items) &&
    value.items.every(isMediaProcessingAdminItem) &&
    typeof value.next_cursor === "string" &&
    typeof value.has_more === "boolean";
}

export function isMediaProcessingRetryResponse(
  value: unknown
): value is MediaProcessingRetryResponse {
  if (!isRecord(value)) return false;
  return isMediaProcessingAdminItem(value.item) &&
    typeof value.audit_committed === "boolean" &&
    typeof value.replayed === "boolean";
}

export function isMediaProcessingBulkRetryResponse(
  value: unknown
): value is MediaProcessingBulkRetryResponse {
  if (!isRecord(value)) return false;
  return Array.isArray(value.items) &&
    value.items.every(isMediaProcessingBulkRetryItemResult);
}

export function isMediaProcessingAdminItem(value: unknown): value is MediaProcessingAdminItem {
  if (!isRecord(value)) return false;
  if (!isPositiveInteger(value.job_id) ||
    !isOptionalPositiveInteger(value.video_id) ||
    !isOptionalPositiveInteger(value.author_id) ||
    typeof value.title !== "string" ||
    !isBoundedNonEmptyString(value.profile_version, 64) ||
    !isMediaProcessingState(value.state) ||
    !isMediaProcessingStage(value.stage) ||
    !isOptionalNonNegativeInteger(value.stage_progress_bps) ||
    !isNonNegativeInteger(value.attempts) ||
    !isPositiveInteger(value.max_attempts) ||
    !isOptionalString(value.error_code) ||
    !isOptionalNullableString(value.error_message) ||
    typeof value.created_at !== "string" ||
    typeof value.updated_at !== "string" ||
    !isOptionalNullableString(value.progress_updated_at) ||
    !isOptionalNullableString(value.next_attempt_at) ||
    !isOptionalNullableString(value.completed_at)) {
    return false;
  }
  return true;
}

function isMediaProcessingSummary(value: unknown): value is MediaProcessingSummary {
  if (!isRecord(value)) return false;
  return isNonNegativeInteger(value.waiting) &&
    isNonNegativeInteger(value.processing) &&
    isNonNegativeInteger(value.failed) &&
    isNonNegativeInteger(value.completed) &&
    (!("oldest_waiting_at" in value) || isOptionalNullableString(value.oldest_waiting_at));
}

function isMediaProcessingBulkRetryItemResult(
  value: unknown
): value is MediaProcessingBulkRetryItemResult {
  if (!isRecord(value)) return false;
  return isPositiveInteger(value.job_id) &&
    (value.status === "retried" || value.status === "conflict" || value.status === "rejected") &&
    (!("item" in value) || isMediaProcessingAdminItem(value.item)) &&
    (!("error_code" in value) || isOptionalString(value.error_code));
}

function isMediaProcessingState(value: unknown): value is MediaProcessingState {
  return value === "pending" ||
    value === "processing" ||
    value === "retryable" ||
    value === "completed" ||
    value === "failed";
}

function isMediaProcessingStage(value: unknown): value is MediaProcessingStage {
  return value === "waiting" ||
    value === "downloading" ||
    value === "inspecting" ||
    value === "remuxing" ||
    value === "transcoding" ||
    value === "uploading" ||
    value === "finalizing" ||
    value === "completed" ||
    value === "failed";
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return Boolean(value) && typeof value === "object";
}

function isNonNegativeInteger(value: unknown): boolean {
  return Number.isInteger(value) && Number(value) >= 0;
}

function isPositiveInteger(value: unknown): boolean {
  return Number.isInteger(value) && Number(value) > 0;
}

function isOptionalPositiveInteger(value: unknown): boolean {
  return value === undefined || value === null || isPositiveInteger(value);
}

function isOptionalNonNegativeInteger(value: unknown): boolean {
  return value === undefined || value === null || isNonNegativeInteger(value);
}

function isOptionalString(value: unknown): boolean {
  return value === undefined || typeof value === "string";
}

function isOptionalNullableString(value: unknown): boolean {
  return value === undefined || value === null || typeof value === "string";
}

function isBoundedNonEmptyString(value: unknown, maxLength: number): boolean {
  return typeof value === "string" && value.trim().length > 0 && value.length <= maxLength;
}
