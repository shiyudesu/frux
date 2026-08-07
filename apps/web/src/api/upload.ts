import type {
  ApiErrorBody,
  CompleteUploadSessionResponse,
  ProtectedAssetAccess,
  UploadResponse,
  UploadSessionRequest,
  UploadSessionResponse
} from "../types";
import { ApiError, NetworkError, UserFacingError, apiRequest } from "./client";

export type UploadKind = "video" | "cover";

export type MediaUploadResult =
  | { mode: "direct"; assetID: number }
  | { mode: "multipart"; url: string };

export function fetchProtectedAssetAccess(
  token: string,
  assetID: number
): Promise<ProtectedAssetAccess> {
  return apiRequest<ProtectedAssetAccess>(
    `/api/media-assets/${encodeURIComponent(String(assetID))}/access`,
    { token, cache: "no-store" }
  );
}

export async function uploadMediaFile(
  file: File,
  kind: UploadKind,
  token: string,
  attemptID: string,
  onProgress?: (progress: number) => void
): Promise<MediaUploadResult> {
  const checksum = await sha256File(file);
  const request: UploadSessionRequest = {
    kind,
    filename: file.name,
    content_type: file.type || defaultContentType(kind, file.name),
    size_bytes: file.size,
    checksum_sha256: checksum
  };
  const idempotencyKey = uploadIdempotencyKey(attemptID, kind);
  const session = await apiRequest<UploadSessionResponse>("/api/upload-sessions", {
    method: "POST",
    token,
    headers: { "Idempotency-Key": idempotencyKey },
    body: request
  });
  if (session.mode === "multipart") {
    const uploaded = await uploadMultipart(file, kind, token, onProgress);
    return { mode: "multipart", url: uploaded.url };
  }
  if (session.completed_asset_id) {
    onProgress?.(100);
    return { mode: "direct", assetID: session.completed_asset_id };
  }
  if (!session.id || !session.upload) {
    throw new UserFacingError("上传服务响应异常，请重试");
  }
  await uploadDirect(file, session.upload.url, session.upload.method, session.upload.headers, onProgress);
  const completed = await apiRequest<CompleteUploadSessionResponse>(
    `/api/upload-sessions/${encodeURIComponent(session.id)}/complete`,
    { method: "POST", token }
  );
  return { mode: "direct", assetID: completed.asset.id };
}

async function sha256File(file: File): Promise<string> {
  const digest = await crypto.subtle.digest("SHA-256", await file.arrayBuffer());
  return Array.from(new Uint8Array(digest), (value) => value.toString(16).padStart(2, "0")).join("");
}

function uploadDirect(
  file: File,
  url: string,
  method: string,
  headers: Record<string, string>,
  onProgress?: (progress: number) => void
): Promise<void> {
  return new Promise((resolve, reject) => {
    const request = new XMLHttpRequest();
    request.open(method || "PUT", url);
    for (const [name, value] of Object.entries(headers)) {
      request.setRequestHeader(name, value);
    }
    request.upload.onprogress = (event) => {
      if (event.lengthComputable) onProgress?.(Math.round((event.loaded / event.total) * 100));
    };
    request.onerror = () => reject(new NetworkError());
    request.onload = () => {
      if (request.status >= 200 && request.status < 300) {
        onProgress?.(100);
        resolve();
        return;
      }
      reject(new ApiError("object storage upload failed", request.status, "UPLOAD_OBJECT_FAILED"));
    };
    request.send(file);
  });
}

function uploadMultipart(
  file: File,
  kind: UploadKind,
  token: string,
  onProgress?: (progress: number) => void
): Promise<UploadResponse> {
  return new Promise((resolve, reject) => {
    const body = new FormData();
    body.append("file", file);
    body.append("kind", kind);
    const request = new XMLHttpRequest();
    request.open("POST", "/api/uploads");
    request.setRequestHeader("Accept", "application/json");
    request.setRequestHeader("Authorization", ["Bearer", token].join(" "));
    request.upload.onprogress = (event) => {
      if (event.lengthComputable) onProgress?.(Math.round((event.loaded / event.total) * 100));
    };
    request.onerror = () => reject(new NetworkError());
    request.onload = () => {
      try {
        const payload = parseUploadPayload(request.responseText);
        if (request.status >= 200 && request.status < 300 && payload && "url" in payload) {
          onProgress?.(100);
          resolve(payload);
          return;
        }
        const errorPayload = payload && !("url" in payload) ? payload : null;
        const message = errorPayload?.message || errorPayload?.error || "上传失败";
        reject(new ApiError(message, request.status, errorPayload?.code));
      } catch {
        reject(new ApiError("invalid upload response", request.status || 500));
      }
    };
    request.send(body);
  });
}

function parseUploadPayload(value: string): UploadResponse | ApiErrorBody | null {
  try {
    const payload: unknown = JSON.parse(value);
    if (!isRecord(payload)) return null;
    if (
      typeof payload.url === "string" &&
      typeof payload.kind === "string" &&
      typeof payload.filename === "string" &&
      typeof payload.size === "number"
    ) {
      return {
        url: payload.url,
        kind: payload.kind,
        filename: payload.filename,
        size: payload.size
      };
    }
    return {
      code: typeof payload.code === "string" ? payload.code : undefined,
      error: typeof payload.error === "string" ? payload.error : undefined,
      message: typeof payload.message === "string" ? payload.message : undefined
    };
  } catch {
    return null;
  }
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null;
}

function uploadIdempotencyKey(attemptID: string, kind: UploadKind): string {
  return `web-media-${attemptID}-${kind}`.slice(0, 128);
}

function defaultContentType(kind: UploadKind, filename: string): string {
  const extension = filename.toLowerCase().match(/\.[^.]+$/)?.[0] || "";
  if (kind === "video") {
    if (extension === ".mov") return "video/quicktime";
    if (extension === ".webm") return "video/webm";
    return "video/mp4";
  }
  if (extension === ".png") return "image/png";
  if (extension === ".webp") return "image/webp";
  return "image/jpeg";
}
