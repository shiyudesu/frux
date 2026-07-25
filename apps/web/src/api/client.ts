// 统一 HTTP 客户端：apiRequest<T> 泛型封装 + ApiError（含 status）+ uploadFile。
// 搬运自 LegacyApp.jsx:2637 的 apiRequest 与 :2669 的 uploadFile，行为不变。
import type { ApiErrorBody, UploadResponse } from "../types";

/** 带 HTTP status 的 API 错误；调用方以 error.status === 401 判断鉴权失效 */
export class ApiError extends Error {
  status: number;

  constructor(message: string, status: number) {
    super(message);
    this.name = "ApiError";
    this.status = status;
  }
}

export function isUnauthorized(error: unknown): boolean {
  return error instanceof ApiError && error.status === 401;
}

/** 等价于迁移前的 `error.message || fallback`（catch 变量类型为 unknown） */
export function apiErrorMessage(error: unknown, fallback: string): string {
  if (error instanceof Error && error.message) return error.message;
  return fallback;
}

export interface ApiRequestOptions {
  method?: string;
  token?: string;
  headers?: Record<string, string>;
  body?: unknown;
  keepalive?: boolean;
}

export async function apiRequest<T = unknown>(path: string, options: ApiRequestOptions = {}): Promise<T> {
  const headers: Record<string, string> = {
    Accept: "application/json",
    ...(options.headers || {})
  };
  if (options.body) headers["Content-Type"] = "application/json";
  if (options.token) headers.Authorization = `Bearer ${options.token}`;

  const response = await fetch(path, {
    method: options.method || "GET",
    headers,
    body: options.body ? JSON.stringify(options.body) : undefined,
    keepalive: options.keepalive
  });

  if (!response.ok) {
    let message = "请求失败";
    try {
      const data = (await response.json()) as ApiErrorBody;
      if (data.error) message = data.error;
      if (data.message) message = data.message;
    } catch {
      message = response.statusText || message;
    }
    throw new ApiError(message, response.status);
  }

  if (response.status === 204) return null as T;
  return (await response.json()) as T;
}

export async function uploadFile(file: File, kind: string, token: string): Promise<UploadResponse> {
  const data = new FormData();
  data.append("file", file);
  data.append("kind", kind);

  const response = await fetch("/api/uploads", {
    method: "POST",
    headers: {
      Accept: "application/json",
      Authorization: `Bearer ${token}`
    },
    body: data
  });

  if (!response.ok) {
    let message = "上传失败";
    try {
      const payload = (await response.json()) as ApiErrorBody;
      if (payload.error) message = payload.error;
      if (payload.message) message = payload.message;
    } catch {
      message = response.statusText || message;
    }
    throw new Error(message);
  }

  return (await response.json()) as UploadResponse;
}
