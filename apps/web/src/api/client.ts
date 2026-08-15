import type { ApiErrorBody, UploadResponse } from "../types";

export class ApiError extends Error {
  readonly status: number;
  readonly code: string;
  readonly diagnosticMessage: string;

  constructor(message: string, status: number, code = "") {
    const diagnosticMessage = message.trim() || "API request failed";
    super(diagnosticMessage);
    this.name = "ApiError";
    this.status = status;
    this.code = code.trim().toUpperCase();
    this.diagnosticMessage = diagnosticMessage;
  }
}

export const ADMIN_AUTH_INVALID_EVENT = "frux:admin-auth-invalid";

export class UserFacingError extends Error {
  constructor(message: string) {
    super(message);
    this.name = "UserFacingError";
  }
}

export class NetworkError extends Error {
  constructor(message = "network request failed") {
    super(message);
    this.name = "NetworkError";
  }
}

const INVALID_REFRESH_SESSION_CODES = new Set([
  "AUTH_REFRESH_INVALID",
  "AUTH_INVALID_REFRESH_SESSION",
  "AUTH_REFRESH_SESSION_INVALID"
]);
const REFRESH_SESSION_REPLAYED_CODES = new Set([
  "AUTH_REFRESH_REPLAYED",
  "AUTH_REFRESH_SESSION_REPLAYED",
  "AUTH_REFRESH_SESSION_REPLAY"
]);
const REFRESH_SESSION_SUPERSEDED_CODES = new Set([
  "AUTH_REFRESH_SESSION_SUPERSEDED",
  "AUTH_REFRESH_SUPERSEDED"
]);

export function isInvalidAccessTokenError(error: unknown): boolean {
  return error instanceof ApiError && error.code === "AUTH_INVALID_ACCESS_TOKEN";
}

export function isInvalidRefreshSessionError(error: unknown): boolean {
  return error instanceof ApiError && INVALID_REFRESH_SESSION_CODES.has(error.code);
}

export function isRefreshSessionReplayedError(error: unknown): boolean {
  return error instanceof ApiError && REFRESH_SESSION_REPLAYED_CODES.has(error.code);
}

export function isSupersededRefreshSessionError(error: unknown): boolean {
  return error instanceof ApiError && REFRESH_SESSION_SUPERSEDED_CODES.has(error.code);
}

export function isUnauthorized(error: unknown): boolean {
  return isInvalidAccessTokenError(error) ||
    isInvalidRefreshSessionError(error) ||
    isRefreshSessionReplayedError(error);
}

const API_ERROR_MESSAGES: Readonly<Record<string, string>> = {
  AUTH_INVALID_CREDENTIALS: "账号或密码错误，请重新输入",
  AUTH_ACCOUNT_FROZEN: "该账号已被冻结，请查看账号消息或联系管理员",
  AUTH_INVALID_ACCESS_TOKEN: "登录状态已失效，请重新登录",
  AUTH_REFRESH_INVALID: "登录状态已失效，请重新登录",
  AUTH_INVALID_REFRESH_SESSION: "登录状态已失效，请重新登录",
  AUTH_REFRESH_SESSION_INVALID: "登录状态已失效，请重新登录",
  AUTH_REFRESH_SESSION_REPLAYED: "登录状态存在异常，请重新登录",
  AUTH_REFRESH_REPLAYED: "登录状态存在异常，请重新登录",
  AUTH_REFRESH_SESSION_REPLAY: "登录状态存在异常，请重新登录",
  AUTH_REFRESH_SESSION_SUPERSEDED: "登录状态已更新，请重试刚才的操作",
  AUTH_REFRESH_SUPERSEDED: "登录状态已更新，请重试刚才的操作",
  AUTHENTICATION_REQUIRED: "请先登录后再继续操作",
  AUTHENTICATION_UNAVAILABLE: "登录服务暂时不可用，请稍后重试",
  AUTH_SESSION_CHANGED: "登录账号已变化，请重新执行刚才的操作",
  ACCOUNT_ALREADY_EXISTS: "该账号已注册，请直接登录或更换账号",
  ACCOUNT_VALIDATION_FAILED: "账号信息填写有误，请检查后重试",
  ACCOUNT_PASSWORD_VALIDATION_FAILED: "密码至少需要 8 个字符，且 UTF-8 编码不能超过 72 字节",
  ACCOUNT_PASSWORD_INVALID: "密码至少需要 8 个字符，且 UTF-8 编码不能超过 72 字节",
  AUTH_CURRENT_PASSWORD_INVALID: "当前密码不正确，请重新输入",
  ACCOUNT_CURRENT_PASSWORD_INCORRECT: "当前密码不正确，请重新输入",
  AUTH_PASSWORD_UNCHANGED: "新密码不能与当前密码相同",
  ACCOUNT_PASSWORD_UNCHANGED: "新密码不能与当前密码相同",
  AUTH_CREDENTIALS_CHANGED: "账号凭证已更新，请重新登录后再试",
  ACCOUNT_CREDENTIAL_CHANGED: "账号凭证已更新，请重新登录后再试",
  ACCOUNT_NOT_FOUND: "用户不存在或已不可用",
  ACCOUNT_REQUIRED: "请输入账号",
  PASSWORD_REQUIRED: "请输入密码",
  NICKNAME_REQUIRED: "请输入昵称",
  REQUEST_INVALID: "请求内容有误，请检查后重试",
  INVALID_REQUEST: "请求内容有误，请检查后重试",
  FORBIDDEN: "你没有权限执行此操作",
  NOT_FOUND: "内容不存在或已不可用",
  CONFLICT: "操作状态已变化，请刷新后重试",
  SEARCH_QUERY_REQUIRED: "请输入搜索关键词",
  SEARCH_QUERY_INVALID: "搜索关键词格式无效",
  SEARCH_QUERY_TOO_LONG: "搜索关键词不能超过 64 个字符",
  SEARCH_PARAMETERS_INVALID: "搜索参数已失效，请重新搜索",
  SEARCH_SERVICE_UNAVAILABLE: "搜索服务暂时不可用，请稍后重试",
  SEARCH_CURSOR_INVALID: "搜索参数已失效，请重新搜索",
  SEARCH_UNAVAILABLE: "搜索服务暂时不可用，请稍后重试",
  USER_NOT_FOUND: "用户不存在或已不可用",
  TARGET_USER_NOT_FOUND: "目标用户不存在或已不可用",
  RELATION_TARGET_USER_NOT_FOUND: "目标用户不存在或已不可用",
  VIDEO_NOT_FOUND: "视频不存在或已不可用",
  RECOMMENDATION_VIDEO_NOT_FOUND: "视频不存在或已不可用",
  EXPOSURE_VIDEO_NOT_FOUND: "视频不存在或已不可用",
  LIBRARY_VIDEO_NOT_FOUND: "视频不存在或已不可用",
  RESOURCE_NOT_FOUND: "内容不存在或已不可用",
  INTERACTION_RESOURCE_NOT_FOUND: "内容不存在或已不可用",
  MEDIA_ASSET_NOT_FOUND: "媒体文件不存在或已不可用",
  LIKED_VIDEOS_PRIVATE: "该用户的喜欢列表仅自己可见",
  COMMENT_PERMISSION_DENIED: "你没有权限操作这条评论",
  INTERACTION_COMMENT_PERMISSION_DENIED: "你没有权限操作这条评论",
  VIDEO_PERMISSION_DENIED: "你没有权限操作这个视频",
  LOCAL_ASSET_PERMISSION_DENIED: "你没有权限使用这个媒体文件",
  UPLOAD_ASSET_PERMISSION_DENIED: "你没有权限使用这个媒体文件",
  IDEMPOTENCY_KEY_CONFLICT: "操作状态已变化，请刷新后重试",
  IDEMPOTENCY_CONFLICT: "操作状态已变化，请刷新后重试",
  INTERACTION_IDEMPOTENCY_CONFLICT: "操作状态已变化，请刷新后重试",
  RELATION_IDEMPOTENCY_CONFLICT: "操作状态已变化，请刷新后重试",
  VIDEO_IDEMPOTENCY_CONFLICT: "操作状态已变化，请刷新后重试",
  ADMIN_AUTH_INVALID_CREDENTIALS: "管理员账号或密码错误",
  ADMIN_AUTH_INVALID_ACCESS_TOKEN: "后台登录状态已失效，请重新登录",
  ADMIN_AUTHENTICATION_UNAVAILABLE: "后台登录服务暂时不可用，请稍后重试",
  ADMIN_PERMISSION_DENIED: "当前账号没有所需运营权限",
  ADMIN_AUTHORIZATION_UNAVAILABLE: "运营权限暂时无法验证，请稍后重试",
  ADMIN_VIDEO_VALIDATION_FAILED: "视频运营参数有误，请检查后重试",
  ADMIN_VIDEO_CURSOR_INVALID: "筛选条件已变化，请重新查询",
  ADMIN_VIDEO_VERSION_CONFLICT: "视频版本已变化，请刷新后重试",
  ADMIN_VIDEO_STATE_CONFLICT: "视频状态已变化，请刷新后重试",
  ADMIN_VIDEO_UNAVAILABLE: "视频运营服务暂时不可用，请稍后重试",
  ADMIN_USER_ACCOUNT_VALIDATION_FAILED: "账号管理参数有误，请检查后重试",
  ADMIN_USER_ACCOUNT_CURSOR_INVALID: "筛选条件已变化，请重新查询",
  ADMIN_USER_ACCOUNT_NOT_FOUND: "普通用户账号不存在或已不再属于可管理范围",
  ADMIN_USER_ACCOUNT_VERSION_CONFLICT: "账号版本已变化，请刷新后重试",
  ADMIN_USER_ACCOUNT_STATE_CONFLICT: "账号状态已变化，请刷新后重试",
  ADMIN_USER_ACCOUNT_IDEMPOTENCY_CONFLICT: "重试凭据已用于其他账号操作，请重新执行",
  ADMIN_USER_ACCOUNT_UNAVAILABLE: "账号管理服务暂时不可用，请稍后重试",
  REVIEW_LEASE_EXPIRED: "审核租约已过期，请重新领取",
  REVIEW_LEASE_NOT_OWNED: "当前账号不持有该审核租约",
  REVIEW_CASE_VERSION_CONFLICT: "审核案件版本已变化，请刷新后重试",
  REVIEW_SUBJECT_VERSION_CONFLICT: "视频审核版本已变化，请刷新后重试",
  REVIEW_CASE_CLAIMED: "审核案件已被其他审核员领取",
  REVIEW_UNAVAILABLE: "审核服务暂时不可用，请稍后重试",
  RECOMMENDATION_FEEDBACK_CONFLICT: "反馈状态已变化，请刷新后重试",
  EXPOSURE_EVENT_CONFLICT: "请求状态已变化，请刷新后重试",
  PLAYBACK_TELEMETRY_CONFLICT: "请求状态已变化，请刷新后重试",
  UPLOAD_SESSION_CONFLICT: "上传状态已变化，请重新选择文件后重试",
  RATE_LIMITED: "操作过于频繁，请稍后重试",
  TELEMETRY_RATE_LIMITED: "请求过于频繁，请稍后重试",
  PLAYBACK_TELEMETRY_RATE_LIMITED: "请求过于频繁，请稍后重试",
  LIBRARY_LIKED_VIDEOS_PRIVATE: "该用户的喜欢列表仅自己可见",
  UPLOAD_FILE_REQUIRED: "请选择要上传的文件",
  UPLOAD_KIND_INVALID: "上传文件类型不受支持",
  UPLOAD_VALIDATION_FAILED: "上传文件不符合要求，请检查后重试",
  UPLOAD_FILE_TYPE_INVALID: "文件格式不受支持，请重新选择",
  UPLOAD_VIDEO_TOO_LARGE: "视频不能超过 512 MB",
  UPLOAD_COVER_TOO_LARGE: "封面不能超过 20 MB",
  UPLOAD_SESSION_VALIDATION_FAILED: "上传信息无效，请重新选择文件",
  UPLOAD_OBJECT_FAILED: "对象存储上传失败，请稍后重试",
  UPLOAD_SESSION_NOT_FOUND: "上传会话已失效，请重新选择文件",
  UPLOAD_ASSET_NOT_FOUND: "上传文件不存在或已失效",
  UPLOAD_PROCESSING_UNAVAILABLE: "视频处理服务暂时不可用，请稍后重试",
  UPLOAD_PROCESSING_FAILED: "视频处理失败，请检查文件后重试",
  UPLOAD_STORAGE_PREPARATION_FAILED: "上传服务暂时不可用，请稍后重试",
  UPLOAD_STORAGE_WRITE_FAILED: "上传服务暂时不可用，请稍后重试",
  UPLOAD_RECORD_FAILED: "上传服务暂时不可用，请稍后重试",
  UPLOAD_STORAGE_UNAVAILABLE: "上传存储暂时不可用，请稍后重试",
  UPLOAD_UNAVAILABLE: "上传服务暂时不可用，请稍后重试",
  VIDEO_VALIDATION_FAILED: "视频信息有误，请检查标题和简介",
  SERVICE_UNAVAILABLE: "服务暂时不可用，请稍后重试",
  INTERNAL_ERROR: "服务暂时不可用，请稍后重试"
};

export function apiErrorMessage(error: unknown, fallback: string): string {
  if (error instanceof UserFacingError) return error.message;
  if (error instanceof NetworkError) {
    return "网络连接失败，请检查网络后重试";
  }
  if (error instanceof ApiError) {
    const knownMessage = API_ERROR_MESSAGES[error.code];
    if (knownMessage) return knownMessage;
    if (error.status === 429) return "操作过于频繁，请稍后重试";
    if (error.status >= 500) return temporaryFailureMessage(fallback);
  }
  return fallback;
}

function temporaryFailureMessage(fallback: string): string {
  if (fallback.includes("请稍后重试")) return fallback;
  return `${fallback.replace(/[，。]$/, "")}，请稍后重试`;
}

export interface ConsumerAuthController {
  getAccessToken(): string;
  getAccessExpiresAt(): number;
  getSessionEpoch(): number;
  getTokenEpoch(token: string): number | null;
  refreshAccessToken(): Promise<string | null>;
}

let consumerAuthController: ConsumerAuthController | null = null;

export function configureConsumerAuthController(controller: ConsumerAuthController | null): void {
  consumerAuthController = controller;
}

export function currentConsumerAccessToken(fallback = ""): string {
  return consumerAuthController?.getAccessToken() || fallback;
}

export function refreshConsumerAccessToken(): Promise<string | null> {
  return consumerAuthController?.refreshAccessToken() || Promise.resolve(null);
}

export function currentConsumerSessionEpoch(): number {
  return consumerAuthController?.getSessionEpoch() ?? -1;
}

export function requireConsumerSessionEpoch(expectedEpoch: number): void {
  if (expectedEpoch >= 0 &&
    currentConsumerSessionEpoch() !== expectedEpoch) {
    throw new ApiError(
      "session identity changed", 409, "AUTH_SESSION_CHANGED"
    );
  }
}

export async function recoverConsumerAccessToken(
  originalToken: string,
  expectedEpoch: number
): Promise<string | null> {
  if (!consumerAuthController ||
    consumerAuthController.getSessionEpoch() !== expectedEpoch) {
    return null;
  }
  const currentToken = consumerAuthController.getAccessToken();
  if (currentToken && currentToken !== originalToken) {
    return currentToken;
  }
  const refreshed = await consumerAuthController.refreshAccessToken();
  if (consumerAuthController.getSessionEpoch() !== expectedEpoch) {
    return null;
  }
  return refreshed;
}

export interface ApiRequestOptions {
  method?: string;
  token?: string;
  auth?: "consumer";
  headers?: Record<string, string>;
  body?: unknown;
  keepalive?: boolean;
  cache?: RequestCache;
  credentials?: RequestCredentials;
  retryAuth?: boolean;
}

export async function apiRequest<T = unknown>(path: string, options: ApiRequestOptions = {}): Promise<T> {
  const requestEpoch = resolveConsumerRequestEpoch(options);
  assertConsumerResponseEpoch(options, requestEpoch);
  let token = resolveRequestToken(options);
  if (options.auth === "consumer" &&
    options.retryAuth !== false &&
    consumerAuthController &&
    consumerAuthController.getAccessExpiresAt() <= Date.now() + 5_000) {
    const refreshed = await recoverConsumerAccessToken(token, requestEpoch);
    if (refreshed) {
      token = refreshed;
    } else {
      assertConsumerResponseEpoch(options, requestEpoch);
      throw new ApiError(
        "authentication required", 401, "AUTH_INVALID_ACCESS_TOKEN"
      );
    }
  }

  function resolveConsumerRequestEpoch(options: ApiRequestOptions): number {
    if (options.auth !== "consumer") return -1;
    const currentEpoch = currentConsumerSessionEpoch();
    if (!options.token || !consumerAuthController) return currentEpoch;
    const tokenEpoch = consumerAuthController.getTokenEpoch(options.token);
    if (tokenEpoch !== null) return tokenEpoch;
    if (options.token === consumerAuthController.getAccessToken()) {
      return currentEpoch;
    }
    throw new ApiError(
      "session identity changed", 409, "AUTH_SESSION_CHANGED"
    );
  }
  try {
    const result = await performRequest<T>(path, options, token);
    assertConsumerResponseEpoch(options, requestEpoch);
    return result;
  } catch (error) {
    if (
      options.auth === "consumer" &&
      options.retryAuth !== false &&
      isRecoverableConsumerAuthError(error, token) &&
      consumerAuthController
    ) {
      const nextToken = await recoverConsumerAccessToken(token, requestEpoch);
      if (nextToken) {
        const result = await performRequest<T>(
          path, { ...options, retryAuth: false }, nextToken
        );
        assertConsumerResponseEpoch(options, requestEpoch);
        return result;
      }

      if (consumerAuthController.getSessionEpoch() !== requestEpoch &&
        consumerAuthController.getAccessToken()) {
        throw new ApiError(
          "session identity changed", 409, "AUTH_SESSION_CHANGED"
        );
      }
    }
    throw error;
  }
}

function assertConsumerResponseEpoch(
  options: ApiRequestOptions,
  expectedEpoch: number
): void {
  if (options.auth === "consumer") {
    requireConsumerSessionEpoch(expectedEpoch);
  }
}

function isRecoverableConsumerAuthError(
  error: unknown,
  token: string
): boolean {
  return isInvalidAccessTokenError(error) ||
    (Boolean(token) &&
      error instanceof ApiError &&
      error.code === "AUTHENTICATION_REQUIRED");
}

async function performRequest<T>(
  path: string,
  options: ApiRequestOptions,
  token: string
): Promise<T> {
  const headers: Record<string, string> = {
    Accept: "application/json",
    ...(options.headers || {})
  };
  if (options.body !== undefined) headers["Content-Type"] = "application/json";
  if (token) headers.Authorization = `Bearer ${token}`;

  let response: Response;
  try {
    response = await fetch(path, {
      method: options.method || "GET",
      headers,
      body: options.body !== undefined ? JSON.stringify(options.body) : undefined,
      keepalive: options.keepalive,
      cache: options.cache,
      credentials: options.credentials
    });
  } catch {
    throw new NetworkError();
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
        detail: { token }
      }));
    }
    throw error;
  }

  if (response.status === 204) return null as T;
  return (await response.json()) as T;
}

function resolveRequestToken(options: ApiRequestOptions): string {
  if (options.auth === "consumer") {
    const token = consumerAuthController?.getAccessToken() || options.token || "";
    if (token) return token;
    throw new ApiError("authentication required", 401, "AUTHENTICATION_REQUIRED");
  }
  return options.token || "";
}

async function createApiError(response: Response): Promise<ApiError> {
  let message = "请求失败";
  let code = "";
  try {
    const data = (await response.json()) as ApiErrorBody;
    if (data.error) message = data.error;
    if (data.message) message = data.message;
    if (data.code) code = data.code;
  } catch {
    message = response.statusText || message;
  }
  return new ApiError(message, response.status, code);
}

export async function uploadFile(file: File, kind: string, token: string): Promise<UploadResponse> {
  const requestEpoch = currentConsumerSessionEpoch();
  let currentToken = currentConsumerAccessToken(token);
  try {
    return await performUploadFile(file, kind, currentToken);
  } catch (error) {
    if (!isInvalidAccessTokenError(error)) throw error;
    const refreshed = await recoverConsumerAccessToken(currentToken, requestEpoch);
    if (!refreshed) {
      if (currentConsumerSessionEpoch() !== requestEpoch &&
        currentConsumerAccessToken()) {
        throw new ApiError(
          "session identity changed", 409, "AUTH_SESSION_CHANGED"
        );
      }
      throw error;
    }
    currentToken = refreshed;
    return performUploadFile(file, kind, currentToken);
  }
}

async function performUploadFile(
  file: File,
  kind: string,
  token: string
): Promise<UploadResponse> {
  const data = new FormData();
  data.append("file", file);
  data.append("kind", kind);

  let response: Response;
  try {
    response = await fetch("/api/uploads", {
      method: "POST",
      headers: {
        Accept: "application/json",
        Authorization: `Bearer ${token}`
      },
      body: data
    });
  } catch {
    throw new NetworkError();
  }

  if (!response.ok) {
    throw await createApiError(response);
  }

  return (await response.json()) as UploadResponse;
}
